// Package api: API JSON de l'interface d'administration (session + CSRF).
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/leassh-dev/leassh/internal/access"
	"github.com/leassh-dev/leassh/internal/accounts"
	"github.com/leassh-dev/leassh/internal/auditreport"
	"github.com/leassh-dev/leassh/internal/commands"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/executions"
	"github.com/leassh-dev/leassh/internal/groups"
	"github.com/leassh-dev/leassh/internal/infra"
	"github.com/leassh-dev/leassh/internal/journal"
	"github.com/leassh-dev/leassh/internal/metrics"
	"github.com/leassh-dev/leassh/internal/oidc"
	"github.com/leassh-dev/leassh/internal/packs"
	"github.com/leassh-dev/leassh/internal/proposals"
	"github.com/leassh-dev/leassh/internal/provisioning"
	"github.com/leassh-dev/leassh/internal/settings"
	"github.com/leassh-dev/leassh/internal/sshexec"
	"github.com/leassh-dev/leassh/internal/store"
	"github.com/leassh-dev/leassh/internal/syslog"
	"github.com/leassh-dev/leassh/internal/webhook"
)

const (
	cookieName  = "leassh_session"
	maxFailures = 5
	failWindow  = 15 * time.Minute
	// maxFailuresPerIP: seuil par adresse, volontairement bien plus haut que maxFailures (NAT, proxy):
	// il freine le bourrage d'identifiants sans verrouiller les comptes voisins d'une même IP (SEC-04).
	maxFailuresPerIP = 50
	// idleTimeout: durée sans requête au-delà de laquelle une session est refusée (SEC-04, ASVS V3.3).
	idleTimeout = 30 * time.Minute
	hstsValue   = "max-age=31536000; includeSubDomains"
)

type Server struct {
	set      *settings.Service
	st       *store.Store
	accounts *accounts.Service
	journal  *journal.Service
	commands *commands.Service
	groups   *groups.Service
	infra    *infra.Service
	auditrep *auditreport.Service
	log      *slog.Logger
	lim      *accounts.Limiter
	limIP    *accounts.Limiter
	static   http.Handler

	// services attachés par Attach (nécessaires aux routes de configuration et d'exécution)
	box   *crypto.Box
	pool  *sshexec.Pool
	acc   *access.Service
	exec  *executions.Service
	props *proposals.Service
	packs *packs.Service
	hook  *webhook.Dispatcher
	sysl  *syslog.Forwarder
	prov  *provisioning.Service
	mcp   http.Handler
	oidc  *oidc.Client

	metrics *metrics.Registry
	version string

	setupTokenHash string // SHA-256 du jeton d'initialisation; vide = /setup refusé
}

// Services regroupe les dépendances métier de l'API.
type Services struct {
	Box    *crypto.Box
	Pool   *sshexec.Pool
	Acc    *access.Service
	Exec   *executions.Service
	Props  *proposals.Service
	Packs  *packs.Service
	Hook   *webhook.Dispatcher
	Syslog *syslog.Forwarder
	Prov   *provisioning.Service
	MCP    http.Handler

	Metrics    *metrics.Registry
	Version    string
	AIDefaults auditreport.AIDefaults
}

// Attach branche les services métier; à appeler avant Handler().
func (s *Server) Attach(sv Services) {
	s.box, s.pool, s.acc, s.exec, s.props, s.packs, s.hook, s.mcp = sv.Box, sv.Pool, sv.Acc, sv.Exec, sv.Props, sv.Packs, sv.Hook, sv.MCP
	s.prov, s.sysl = sv.Prov, sv.Syslog
	s.infra = infra.New(s.st, sv.Box, sv.Pool, sv.Exec)
	s.metrics, s.version = sv.Metrics, sv.Version
	s.auditrep = auditreport.NewService(s.st, sv.Box, sv.Version, s.set, sv.AIDefaults, s.metrics.Transport("ai", nil))
	s.oidc.HTTP.Transport = s.metrics.Transport("idp", s.oidc.HTTP.Transport) // durée des appels au fournisseur d'identité
}

// SetSetupToken définit le jeton d'initialisation exigé par POST /setup (SEC-01). Sans jeton, /setup est refusé.
func (s *Server) SetSetupToken(tok string) {
	if tok == "" {
		s.setupTokenHash = ""
		return
	}
	s.setupTokenHash = settings.HashToken(tok)
}

// ClientIP expose la résolution d'IP (proxies de confiance) aux autres composants.
func (s *Server) ClientIP(r *http.Request) string { return s.clientIP(r) }

// New construit le routeur. static sert l'interface (SPA); peut être nil.
func New(set *settings.Service, st *store.Store, log *slog.Logger, static http.Handler) (*Server, error) {
	return &Server{set: set, st: st, accounts: accounts.New(st), journal: journal.New(st), commands: commands.New(st), groups: groups.New(st), log: log, lim: accounts.NewLimiter(st, "account", maxFailures, failWindow), limIP: accounts.NewLimiter(st, "ip", maxFailuresPerIP, failWindow), static: static, oidc: oidc.New()}, nil
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.securityHeaders)
	health := func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }
	r.Get("/health", health)
	r.Get("/healthz", health) // ancien nom, conservé pour les sondes déjà en place
	r.Get("/metrics", s.metricsHandler)
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(langMiddleware)
		r.Use(s.originCheck)
		r.Get("/setup/status", s.setupStatus)
		r.With(s.requireSession).Get("/meta", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]any{"approval_ttl_seconds": int(s.set.ApprovalTTL().Seconds()), "base_url": s.set.BaseURL(), "log_retention_days": s.set.RetentionDays()})
		})
		r.Post("/setup", s.setup)
		r.Post("/auth/login", s.login)
		r.Get("/auth/providers", s.authProviders)
		r.Get("/auth/oidc/start", s.oidcStart)
		r.Get("/auth/oidc/callback", s.oidcCallback)
		r.Group(func(r chi.Router) {
			r.Use(s.requireSession)
			r.Get("/auth/me", s.me)
			r.Post("/auth/logout", s.logout)
			s.configRoutes(r)
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeErr(w, 404, "not_found", "ressource introuvable") })
	})
	if s.mcp != nil {
		r.Handle("/mcp", s.mcp)
	}
	if s.static != nil {
		r.NotFound(s.static.ServeHTTP)
	}
	return r
}

// ---- helpers HTTP ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, 400, "bad_request", "corps JSON invalide")
		return false
	}
	return true
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		// Derrière un proxy de confiance, HSTS est la politique du proxy (qui termine le TLS): on n'ajoute pas
		// un second en-tête, que les navigateurs gèrent mal. Sinon, Leassh impose sa valeur.
		if s.isHTTPS(r) && !s.fromTrustedProxy(r) {
			h.Set("Strict-Transport-Security", hstsValue)
		}
		next.ServeHTTP(w, r)
	})
}

// originCheck: défense en profondeur contre le CSRF (en plus de SameSite=Strict et du jeton).
func (s *Server) originCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" {
				if !strings.HasSuffix(o, "://"+r.Host) {
					writeErr(w, 403, "bad_origin", "origine refusée")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP: IP réelle, X-Forwarded-For pris en compte seulement derrière un proxy de confiance.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !s.trusted(addr) {
		return host
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- { // de droite à gauche: premier saut non fiable
		a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			break
		}
		if !s.trusted(a) {
			return a.String()
		}
		host = a.String()
	}
	return host
}

func (s *Server) trusted(a netip.Addr) bool { return s.set.Trusted(a) }

func (s *Server) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.HasPrefix(s.set.BaseURL(), "https://") {
		return true
	}
	if s.fromTrustedProxy(r) {
		return r.Header.Get("X-Forwarded-Proto") == "https"
	}
	return false
}

// fromTrustedProxy: la connexion TCP directe vient d'un proxy déclaré dans les réglages généraux.
func (s *Server) fromTrustedProxy(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	a, err := netip.ParseAddr(host)
	return err == nil && s.trusted(a)
}

// ---- sessions ----

type ctxKey int

const (
	userKey ctxKey = iota
	csrfKey
)

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User) (string, error) {
	id, csrf, err := s.accounts.OpenSession(r.Context(), u, s.clientIP(r), s.set.SessionTTL())
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: id, Path: "/", HttpOnly: true, Secure: s.isHTTPS(r),
		SameSite: http.SameSiteStrictMode, MaxAge: int(s.set.SessionTTL().Seconds()),
	})
	return csrf, nil
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			writeErr(w, 401, "unauthenticated", "connexion requise")
			return
		}
		u, sessionCSRF, err := s.accounts.LookupSession(r.Context(), c.Value, idleTimeout)
		if err != nil {
			writeErr(w, 401, "unauthenticated", err.Error())
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(sessionCSRF)) != 1 {
				writeErr(w, 403, "csrf", "jeton CSRF invalide")
				return
			}
		}
		ctx := context.WithValue(context.WithValue(r.Context(), userKey, u), csrfKey, sessionCSRF)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Role: middleware de contrôle de rôle (admin ⊃ approver ⊃ viewer).
func Role(min string) func(http.Handler) http.Handler {
	rank := map[string]int{"viewer": 1, "approver": 2, "admin": 3}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, _ := r.Context().Value(userKey).(store.User)
			if rank[u.Role] < rank[min] {
				writeErr(w, 403, "forbidden", "droits insuffisants")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---- handlers ----

type userDTO struct {
	store.User
	CSRF string `json:"csrf_token,omitempty"`
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	ok, err := s.accounts.Initialized(r.Context())
	if err != nil {
		writeErr(w, 500, "internal", "erreur interne")
		return
	}
	writeJSON(w, 200, map[string]bool{"initialized": ok})
}

type credentials struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type setupInput struct {
	credentials
	SetupToken string `json:"setup_token"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var in setupInput
	if !decode(w, r, &in) {
		return
	}
	// SEC-01: le jeton d'initialisation est vérifié avant toute autre chose (temps constant, tentatives limitées).
	limKey := "setup|" + s.clientIP(r)
	if s.lim.Blocked(limKey) {
		writeErr(w, 429, "too_many_attempts", "trop de tentatives, réessayez dans quelques minutes")
		return
	}
	if s.setupTokenHash == "" || subtle.ConstantTimeCompare([]byte(settings.HashToken(in.SetupToken)), []byte(s.setupTokenHash)) != 1 {
		s.lim.Fail(limKey)
		writeErr(w, 401, "invalid_setup_token", "jeton d'initialisation invalide")
		return
	}
	u, err := s.accounts.InitializeAdmin(r.Context(), accounts.InitAdminInput{
		Username: in.Username, DisplayName: in.DisplayName, Password: in.Password, IP: s.clientIP(r)})
	switch {
	case errors.Is(err, accounts.ErrInvalidUsername):
		writeErr(w, 400, "invalid_username", err.Error())
		return
	case errors.Is(err, accounts.ErrWeakPassword):
		writeErr(w, 400, "weak_password", err.Error())
		return
	case errors.Is(err, accounts.ErrAlreadyInitialized):
		writeErr(w, 409, "already_initialized", err.Error())
		return
	case err != nil:
		s.log.Error("setup", "err", err)
		writeErr(w, 500, "internal", "erreur interne")
		return
	}
	csrf, err := s.startSession(w, r, u)
	if err != nil {
		writeErr(w, 500, "internal", "erreur interne")
		return
	}
	writeJSON(w, 201, userDTO{User: u, CSRF: csrf})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	// SEC-09: borne la taille avant tout traitement (audit, hachage, clé du limiteur).
	if len(in.Username) > accounts.MaxUsername {
		writeErr(w, 400, "invalid_username", "nom d'utilisateur trop long")
		return
	}
	ip := s.clientIP(r)
	key := ip + "|" + strings.ToLower(in.Username)
	if s.lim.Blocked(key) || s.limIP.Blocked(ip) {
		writeErr(w, 429, "too_many_attempts", "trop de tentatives, réessayez dans quelques minutes")
		return
	}
	u, err := s.accounts.Authenticate(r.Context(), accounts.LoginInput{Username: in.Username, Password: in.Password, IP: ip})
	switch {
	case errors.Is(err, accounts.ErrLocalLoginDisabled):
		s.limIP.Fail(ip) // SEC-09: ces refus comptent dans le plafond par IP (pas de remplissage illimité de l'audit)
		writeErr(w, 403, "local_login_disabled", err.Error())
		return
	case errors.Is(err, accounts.ErrInvalidCredentials):
		s.lim.Fail(key)
		s.limIP.Fail(ip)
		writeErr(w, 401, "invalid_credentials", err.Error())
		return
	case errors.Is(err, accounts.ErrLocalPasswordAdminsOnly):
		writeErr(w, 403, "local_login_disabled", err.Error())
		return
	case err != nil:
		s.log.Error("login", "err", err)
		writeErr(w, 500, "internal", "erreur interne")
		return
	}
	s.lim.Reset(key)
	csrf, err := s.startSession(w, r, u)
	if err != nil {
		writeErr(w, 500, "internal", "erreur interne")
		return
	}
	s.accounts.CompleteLogin(r.Context(), u, ip)
	writeJSON(w, 200, userDTO{User: u, CSRF: csrf})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(store.User)
	writeJSON(w, 200, userDTO{User: u, CSRF: r.Context().Value(csrfKey).(string)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = s.accounts.CloseSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.isHTTPS(r), SameSite: http.SameSiteStrictMode})
	// Compte SSO: si le fournisseur sait fermer sa propre session, l'interface y est renvoyée (RP-Initiated Logout).
	if u := currentUser(r); u.AuthProvider == "oidc" {
		if oc, cfg, ok, err := s.oidcLoaded(r.Context()); err == nil && ok && oc.EndSession {
			if loc := s.oidc.EndSessionURL(r.Context(), cfg, s.baseURL(r)+"/"); loc != "" {
				writeJSON(w, 200, map[string]string{"end_session_url": loc})
				return
			}
		}
	}
	w.WriteHeader(204)
}
