package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/leash-dev/leash/internal/audit"
	"github.com/leash-dev/leash/internal/oidc"
	"github.com/leash-dev/leash/internal/store"
)

// ---- SSO OpenID Connect (SPEC §9bis) ----

const (
	oidcCookie   = "leash_oidc"
	oidcCbPath   = "/api/v1/auth/oidc/callback"
	oidcFlowTTL  = 10 * time.Minute
	maxUsernames = 64
)

// oidcView: configuration exposée par l'API (jamais le secret client).
type oidcView struct {
	Enabled                 bool         `json:"enabled"`
	Name                    string       `json:"name"`
	Issuer                  string       `json:"issuer"`
	ClientID                string       `json:"client_id"`
	HasClientSecret         bool         `json:"has_client_secret"`
	Scopes                  string       `json:"scopes"`
	RoleClaim               string       `json:"role_claim"`
	RoleMap                 oidc.RoleMap `json:"role_map"`
	DefaultRole             string       `json:"default_role"` // "" = refus | viewer
	JIT                     bool         `json:"jit"`
	LocalPasswordAdminsOnly bool         `json:"local_password_admins_only"`
	EndSession              bool         `json:"end_session"`
	RedirectURL             string       `json:"redirect_url"`
}

type oidcInput struct {
	oidcView
	ClientSecret      string `json:"client_secret"`
	ClearClientSecret bool   `json:"clear_client_secret"`
}

func viewOf(c store.OIDCConfig) oidcView {
	v := oidcView{Enabled: c.Enabled, Name: c.Name, Issuer: c.Issuer, ClientID: c.ClientID, HasClientSecret: len(c.ClientSecretEnc) > 0, Scopes: c.Scopes,
		RoleClaim: c.RoleClaim, DefaultRole: c.DefaultRole, JIT: c.JIT, LocalPasswordAdminsOnly: c.LocalPasswordAdminsOnly, EndSession: c.EndSession}
	_ = json.Unmarshal([]byte(c.RoleMap), &v.RoleMap)
	return v
}

// auditView: ce qui est écrit dans l'audit (jamais le secret).
func (v oidcView) auditView() map[string]any {
	return map[string]any{"enabled": v.Enabled, "name": v.Name, "issuer": v.Issuer, "client_id": v.ClientID, "has_client_secret": v.HasClientSecret,
		"scopes": v.Scopes, "role_claim": v.RoleClaim, "role_map": v.RoleMap, "default_role": v.DefaultRole, "jit": v.JIT,
		"local_password_admins_only": v.LocalPasswordAdminsOnly, "end_session": v.EndSession}
}

func (s *Server) baseURL(r *http.Request) string {
	if b := strings.TrimRight(s.cfg.BaseURL, "/"); b != "" {
		return b
	}
	scheme := "http"
	if s.isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) redirectURL(r *http.Request) string { return s.baseURL(r) + oidcCbPath }

func (s *Server) getOIDC(w http.ResponseWriter, r *http.Request) {
	c, err := store.GetOIDCConfig(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	v := viewOf(c)
	v.RedirectURL = s.redirectURL(r)
	writeJSON(w, 200, v)
}

var scopeRe = regexp.MustCompile(`^[\x21\x23-\x5B\x5D-\x7E]+$`) // RFC 6749 §3.3

func (in *oidcInput) validate() error {
	in.Name, in.Issuer, in.ClientID = strings.TrimSpace(in.Name), strings.TrimSpace(in.Issuer), strings.TrimSpace(in.ClientID)
	in.RoleClaim, in.Scopes = strings.TrimSpace(in.RoleClaim), strings.Join(strings.Fields(in.Scopes), " ")
	if in.Name == "" {
		in.Name = "SSO"
	}
	if len(in.Name) > 64 {
		return bad("invalid_oidc", "nom du fournisseur: 64 caractères maximum")
	}
	if in.Issuer != "" {
		u, err := url.Parse(in.Issuer)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || len(in.Issuer) > 512 {
			return bad("invalid_oidc", "l'issuer doit être une URL http(s) sans paramètre ni fragment")
		}
	}
	if len(in.ClientID) > 256 || len(in.ClientSecret) > 1024 || len(in.RoleClaim) > 128 {
		return bad("invalid_oidc", "valeur trop longue")
	}
	if in.Scopes == "" {
		in.Scopes = "openid profile email"
	}
	hasOpenID := false
	for _, sc := range strings.Fields(in.Scopes) {
		if !scopeRe.MatchString(sc) {
			return bad("invalid_oidc", "scope invalide: "+sc)
		}
		hasOpenID = hasOpenID || sc == "openid"
	}
	if !hasOpenID {
		return bad("invalid_oidc", "le scope « openid » est obligatoire")
	}
	if in.DefaultRole != "" && in.DefaultRole != "viewer" {
		return bad("invalid_oidc", "rôle par défaut: viewer ou refus")
	}
	for _, l := range []*[]string{&in.RoleMap.Admin, &in.RoleMap.Approver, &in.RoleMap.Viewer} {
		var clean []string
		for _, v := range *l {
			if v = strings.TrimSpace(v); v != "" {
				if len(v) > 256 {
					return bad("invalid_oidc", "valeur de mapping trop longue")
				}
				clean = append(clean, v)
			}
		}
		if len(clean) > 100 {
			return bad("invalid_oidc", "trop de valeurs de mapping")
		}
		*l = clean
	}
	if in.Enabled && (in.Issuer == "" || in.ClientID == "") {
		return bad("invalid_oidc", "issuer et client ID sont requis pour activer le SSO")
	}
	return nil
}

func (s *Server) putOIDC(w http.ResponseWriter, r *http.Request) {
	var in oidcInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.validate(); err != nil {
		s.fail(w, err)
		return
	}
	u := currentUser(r)
	var after oidcView
	err := s.st.Tx(r.Context(), func(tx *sql.Tx) error {
		old, err := store.GetOIDCConfig(r.Context(), tx)
		if err != nil {
			return err
		}
		secret := old.ClientSecretEnc
		switch {
		case in.ClientSecret != "":
			if secret, err = s.box.Seal([]byte(in.ClientSecret)); err != nil {
				return err
			}
		case in.ClearClientSecret:
			secret = nil
		}
		if in.LocalPasswordAdminsOnly {
			if n, err := store.CountLocalAdmins(r.Context(), tx); err != nil {
				return err
			} else if n == 0 {
				return bad("no_local_admin", "il faut au moins un administrateur local actif (compte de secours) pour restreindre le mot de passe local")
			}
		}
		rm, _ := json.Marshal(in.RoleMap)
		c := store.OIDCConfig{Enabled: in.Enabled, Name: in.Name, Issuer: in.Issuer, ClientID: in.ClientID, ClientSecretEnc: secret, Scopes: in.Scopes,
			RoleClaim: in.RoleClaim, RoleMap: string(rm), DefaultRole: in.DefaultRole, JIT: in.JIT, LocalPasswordAdminsOnly: in.LocalPasswordAdminsOnly, EndSession: in.EndSession}
		if err := store.SaveOIDCConfig(r.Context(), tx, c); err != nil {
			return err
		}
		after = viewOf(c)
		return audit.Record(r.Context(), tx, audit.Event{ActorType: "user", ActorID: u.ID, ActorName: u.Username, IP: s.clientIP(r), Action: "oidc.update",
			TargetType: "oidc", TargetName: c.Issuer,
			Diff: map[string]any{"before": viewOf(old).auditView(), "after": after.auditView(), "credential_changed": in.ClientSecret != "" || in.ClearClientSecret}})
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.oidc.Reset()
	after.RedirectURL = s.redirectURL(r)
	writeJSON(w, 200, after)
}

// testOIDC: découverte à jour de l'issuer (celui du corps, sinon l'enregistré) et contrôle du JWKS. Chaque test est audité.
func (s *Server) testOIDC(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Issuer string `json:"issuer"`
	}
	if !decode(w, r, &in) {
		return
	}
	issuer := strings.TrimSpace(in.Issuer)
	if issuer == "" {
		c, err := store.GetOIDCConfig(r.Context(), s.st.DB)
		if err != nil {
			s.fail(w, err)
			return
		}
		issuer = c.Issuer
	}
	if issuer == "" {
		s.fail(w, bad("not_configured", "renseignez d'abord l'issuer"))
		return
	}
	s.oidc.Forget(issuer)
	res := map[string]any{"ok": false, "issuer": issuer}
	d, err := s.oidc.Discover(r.Context(), issuer)
	if err == nil {
		var n int
		if n, err = s.oidc.KeyCount(r.Context(), issuer); err == nil {
			warns := []string{}
			if strings.HasPrefix(issuer, "http://") {
				warns = append(warns, "issuer_http")
			}
			if d.UserinfoEndpoint == "" {
				warns = append(warns, "no_userinfo")
			}
			if d.EndSessionEndpoint == "" {
				warns = append(warns, "no_end_session")
			}
			if !d.SupportsS256() {
				warns = append(warns, "no_pkce")
			}
			res = map[string]any{"ok": true, "issuer": issuer, "authorization_endpoint": d.AuthorizationEndpoint, "token_endpoint": d.TokenEndpoint,
				"jwks_uri": d.JWKSURI, "jwks_keys": n, "userinfo_endpoint": d.UserinfoEndpoint, "end_session_endpoint": d.EndSessionEndpoint, "warnings": warns}
		}
	}
	if err != nil {
		res["error"] = err.Error()
	}
	u := currentUser(r)
	_ = audit.RecordStandalone(r.Context(), s.st, audit.Event{ActorType: "user", ActorID: u.ID, ActorName: u.Username, IP: s.clientIP(r), Action: "oidc.test",
		TargetType: "oidc", TargetName: issuer, Diff: map[string]any{"ok": res["ok"], "error": res["error"]}})
	writeJSON(w, 200, res)
}

// ---- côté public: page de connexion ----

func (s *Server) authProviders(w http.ResponseWriter, r *http.Request) {
	c, err := store.GetOIDCConfig(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	body := map[string]any{"oidc": nil, "local_password_admins_only": false}
	if c.Enabled {
		body["oidc"] = map[string]any{"name": c.Name, "start_url": "/api/v1/auth/oidc/start"}
		body["local_password_admins_only"] = c.LocalPasswordAdminsOnly
	}
	writeJSON(w, 200, body)
}

// oidcLoaded: configuration active + secret déchiffré. ok=false si le SSO est désactivé ou incomplet.
func (s *Server) oidcLoaded(ctx context.Context) (store.OIDCConfig, oidc.Config, bool, error) {
	c, err := store.GetOIDCConfig(ctx, s.st.DB)
	if err != nil || !c.Enabled || c.Issuer == "" || c.ClientID == "" || s.box == nil {
		return c, oidc.Config{}, false, err
	}
	cfg := oidc.Config{Issuer: c.Issuer, ClientID: c.ClientID, Scopes: strings.Fields(c.Scopes)}
	if len(c.ClientSecretEnc) > 0 {
		b, err := s.box.Open(c.ClientSecretEnc)
		if err != nil {
			return c, cfg, false, err
		}
		cfg.ClientSecret = string(b)
	}
	return c, cfg, true, nil
}

// flow: contenu du cookie d'amorce, scellé (AES-GCM) pour ne pas pouvoir être lu ni modifié par le navigateur.
type flow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Redirect string `json:"r"`
	Exp      int64  `json:"e"`
}

func (s *Server) setFlowCookie(w http.ResponseWriter, r *http.Request, f *flow) error {
	val, maxAge := "", -1
	if f != nil {
		b, _ := json.Marshal(f)
		sealed, err := s.box.Seal(b)
		if err != nil {
			return err
		}
		val, maxAge = base64.RawURLEncoding.EncodeToString(sealed), int(oidcFlowTTL.Seconds())
	}
	// Lax (et non Strict): le retour du fournisseur est une navigation inter-sites qui doit porter ce cookie.
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: val, Path: "/api/v1/auth/oidc", HttpOnly: true, Secure: s.isHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
	return nil
}

func (s *Server) readFlow(r *http.Request) (flow, bool) {
	var f flow
	c, err := r.Cookie(oidcCookie)
	if err != nil || s.box == nil {
		return f, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return f, false
	}
	plain, err := s.box.Open(raw)
	if err != nil || json.Unmarshal(plain, &f) != nil || time.Now().Unix() > f.Exp {
		return f, false
	}
	return f, f.State != "" && f.Nonce != "" && f.Verifier != ""
}

func (s *Server) ssoFailure(w http.ResponseWriter, r *http.Request, actor, reason string, detail map[string]any) {
	d := map[string]any{"reason": reason}
	for k, v := range detail {
		d[k] = v
	}
	if actor == "" {
		actor = "sso"
	}
	_ = audit.RecordStandalone(r.Context(), s.st, audit.Event{ActorType: "system", ActorName: actor, Action: "user.sso_login_failed",
		TargetType: "user", TargetName: actor, Diff: d, IP: s.clientIP(r)})
	_ = s.setFlowCookie(w, r, nil)
	http.Redirect(w, r, "/?sso_error="+url.QueryEscape(reason), http.StatusFound)
}

func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	_, cfg, ok, err := s.oidcLoaded(r.Context())
	if err != nil {
		s.log.Error("sso: configuration", "err", err)
	}
	if !ok {
		http.Redirect(w, r, "/?sso_error=disabled", http.StatusFound)
		return
	}
	var f flow
	var e1, e2, e3 error
	f.State, e1 = oidc.RandomString(32)
	f.Nonce, e2 = oidc.RandomString(32)
	f.Verifier, e3 = oidc.RandomString(48) // 64 caractères, dans les bornes RFC 7636 (43-128)
	if err := errors.Join(e1, e2, e3); err != nil {
		s.fail(w, err)
		return
	}
	f.Redirect, f.Exp = s.redirectURL(r), time.Now().Add(oidcFlowTTL).Unix()
	loc, err := s.oidc.AuthURL(r.Context(), cfg, f.Redirect, f.State, f.Nonce, f.Verifier)
	if err != nil {
		s.log.Warn("sso: démarrage impossible", "err", err)
		s.ssoFailure(w, r, "", oidc.ReasonOf(err), nil)
		return
	}
	if err := s.setFlowCookie(w, r, &f); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, loc, http.StatusFound)
}

func cleanName(v string) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if len(v) > maxUsernames {
		v = strings.ToValidUTF8(v[:maxUsernames], "")
	}
	return v
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := s.clientIP(r)
	c, cfg, ok, err := s.oidcLoaded(ctx)
	if err != nil {
		s.log.Error("sso: configuration", "err", err)
	}
	if !ok {
		s.ssoFailure(w, r, "", "disabled", nil)
		return
	}
	limKey := "sso|" + ip
	if s.lim.blocked(limKey) {
		s.ssoFailure(w, r, "", "too_many_attempts", nil)
		return
	}
	fail := func(actor, reason string, detail map[string]any) {
		s.lim.fail(limKey)
		s.ssoFailure(w, r, actor, reason, detail)
	}
	f, have := s.readFlow(r)
	_ = s.setFlowCookie(w, r, nil) // usage unique
	q := r.URL.Query()
	switch {
	case !have:
		fail("", "state_missing", nil)
		return
	case subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(f.State)) != 1:
		fail("", "state_mismatch", nil)
		return
	case q.Get("error") != "":
		fail("", "provider_error", map[string]any{"provider_error": truncate(sanitizeCode(q.Get("error")), 40)})
		return
	case q.Get("code") == "":
		fail("", "code_missing", nil)
		return
	}
	tok, err := s.oidc.Exchange(ctx, cfg, f.Redirect, q.Get("code"), f.Verifier)
	if err != nil {
		fail("", oidc.ReasonOf(err), map[string]any{"detail": truncate(err.Error(), 200)})
		return
	}
	cl, err := s.oidc.VerifyIDToken(ctx, cfg, tok.IDToken, f.Nonce)
	if err != nil {
		fail("", oidc.ReasonOf(err), map[string]any{"detail": truncate(err.Error(), 200), "issuer": cfg.Issuer})
		return
	}
	// Revendications absentes de l'ID token (cas fréquent des groupes Keycloak): complétées par userinfo, si le sub concorde.
	if c.RoleClaim != "" {
		if _, found := cl.Lookup(c.RoleClaim); !found {
			if ui, err := s.oidc.UserInfo(ctx, cfg, tok.AccessToken); err == nil && ui.String("sub") == cl.String("sub") {
				for k, v := range ui {
					if _, exists := cl[k]; !exists {
						cl[k] = v
					}
				}
			}
		}
	}
	var rm oidc.RoleMap
	_ = json.Unmarshal([]byte(c.RoleMap), &rm)
	role, mapped := oidc.MapRole(cl, c.RoleClaim, rm, c.DefaultRole)
	sub := cl.String("sub")
	uname := cleanName(firstNonEmpty(cl.String("preferred_username"), cl.String("email"), sub))
	if !mapped {
		fail(uname, "role_not_mapped", map[string]any{"sub": sub})
		return
	}
	display := cleanName(firstNonEmpty(cl.String("name"), uname))

	var user store.User
	reason := ""
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		actor := audit.Event{ActorType: "system", ActorName: "sso", IP: ip}
		u, err := store.UserByOIDC(ctx, tx, cfg.Issuer, sub)
		switch {
		case errors.Is(err, store.ErrNotFound):
			if !c.JIT {
				reason = "not_provisioned"
				return errSSORefused
			}
			name, err := s.freeUsername(ctx, tx, uname, cfg.Issuer, sub)
			if err != nil {
				return err
			}
			if u, err = store.InsertOIDCUser(ctx, tx, store.User{Username: name, DisplayName: display, Role: role, OIDCIssuer: cfg.Issuer, OIDCSub: sub}); err != nil {
				return err
			}
			ev := actor
			ev.Action, ev.TargetType, ev.TargetID, ev.TargetName = "user.create", "user", u.ID, u.Username
			ev.Diff = map[string]any{"after": map[string]any{"username": u.Username, "role": u.Role, "auth_provider": "oidc", "issuer": cfg.Issuer}, "jit": true}
			if err := audit.Record(ctx, tx, ev); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if u.Disabled {
				reason = "account_disabled"
				return errSSORefused
			}
			if u.Role != role || u.DisplayName != display {
				newRole := role
				diff := map[string]any{"before": map[string]any{"role": u.Role, "display_name": u.DisplayName}}
				if u.Role == "admin" && role != "admin" {
					if n, err := store.CountEnabledAdmins(ctx, tx); err != nil {
						return err
					} else if n <= 1 {
						newRole = "admin" // garde-fou « dernier administrateur »
						diff["role_sync_skipped"] = "last_admin"
					}
				}
				if err := store.SyncOIDCUser(ctx, tx, u.ID, display, newRole); err != nil {
					return err
				}
				if newRole != u.Role {
					if err := store.DeleteSessionsOf(ctx, tx, u.ID, ""); err != nil { // le nouveau rôle s'applique tout de suite
						return err
					}
				}
				diff["after"] = map[string]any{"role": newRole, "display_name": display}
				ev := actor
				ev.Action, ev.TargetType, ev.TargetID, ev.TargetName, ev.Diff = "user.update", "user", u.ID, u.Username, diff
				if err := audit.Record(ctx, tx, ev); err != nil {
					return err
				}
				u.Role, u.DisplayName = newRole, display
			}
		}
		user = u
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: u.ID, ActorName: u.Username, Action: "user.sso_login",
			TargetType: "user", TargetID: u.ID, TargetName: u.Username, Diff: map[string]any{"issuer": cfg.Issuer, "role": u.Role}, IP: ip})
	})
	if errors.Is(err, errSSORefused) {
		fail(uname, reason, map[string]any{"sub": sub})
		return
	}
	if err != nil {
		s.log.Error("sso: connexion", "err", err)
		fail(uname, "internal", nil)
		return
	}
	if _, err := s.startSession(w, r, user); err != nil {
		s.log.Error("sso: session", "err", err)
		fail(uname, "internal", nil)
		return
	}
	s.lim.reset(limKey)
	_ = s.st.TouchLogin(ctx, user.ID)
	http.Redirect(w, r, "/", http.StatusFound)
}

var errSSORefused = errors.New("connexion SSO refusée")

// freeUsername: nom d'utilisateur libre. Un compte existant du même nom n'est JAMAIS lié: on suffixe.
func (s *Server) freeUsername(ctx context.Context, tx *sql.Tx, want, issuer, sub string) (string, error) {
	host := issuer
	if u, err := url.Parse(issuer); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	short, _ := oidc.RandomString(4)
	for _, cand := range []string{want, want + "@" + host, want + "-" + short} {
		cand = cleanName(cand)
		if cand == "" {
			continue
		}
		taken, err := store.UsernameExists(ctx, tx, cand)
		if err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
	}
	return "sso-" + short + "-" + truncate(sub, 8), nil
}

var codeRe = regexp.MustCompile(`[^a-z_]`)

func sanitizeCode(v string) string { return codeRe.ReplaceAllString(strings.ToLower(v), "") }

func truncate(v string, n int) string {
	if len(v) > n {
		return strings.ToValidUTF8(v[:n], "")
	}
	return v
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
