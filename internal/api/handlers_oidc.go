package api

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/leassh-dev/leassh/internal/accounts"
	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/oidc"
	"github.com/leassh-dev/leassh/internal/store"
)

// ---- SSO OpenID Connect (SPEC §9bis) ----

const (
	oidcCookie  = "leassh_oidc"
	oidcCbPath  = "/api/v1/auth/oidc/callback"
	oidcFlowTTL = 10 * time.Minute
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
	LocalLoginDisabled      bool         `json:"local_login_disabled"`
	EndSession              bool         `json:"end_session"`
	AutoRedirect            bool         `json:"auto_redirect"`
	RedirectURL             string       `json:"redirect_url"`
}

type oidcInput struct {
	oidcView
	ClientSecret      string `json:"client_secret"`
	ClearClientSecret bool   `json:"clear_client_secret"`
}

func viewOf(c store.OIDCConfig) oidcView {
	v := oidcView{Enabled: c.Enabled, Name: c.Name, Issuer: c.Issuer, ClientID: c.ClientID, HasClientSecret: len(c.ClientSecretEnc) > 0, Scopes: c.Scopes,
		RoleClaim: c.RoleClaim, DefaultRole: c.DefaultRole, JIT: c.JIT, LocalPasswordAdminsOnly: c.LocalPasswordAdminsOnly, LocalLoginDisabled: c.LocalLoginDisabled, EndSession: c.EndSession, AutoRedirect: c.AutoRedirect}
	_ = json.Unmarshal([]byte(c.RoleMap), &v.RoleMap)
	return v
}

// baseURL: URL publique de l'application, par ordre de priorité:
//  1. l'URL publique réglée dans Réglages → Général (fixe, recommandé: aucune dépendance aux en-têtes de la requête);
//  2. X-Forwarded-Host / X-Forwarded-Proto, uniquement si la requête vient d'un proxy de confiance (Réglages → Général);
//  3. l'hôte de la requête.
func (s *Server) baseURL(r *http.Request) string {
	if b := s.set.BaseURL(); b != "" {
		return b
	}
	scheme := "http"
	if s.isHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if s.fromTrustedProxy(r) {
		if h := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); validHost(h) {
			host = h
		}
	}
	return scheme + "://" + host
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9.\-]+(:[0-9]{1,5})?$|^\[[0-9A-Fa-f:.]+\](:[0-9]{1,5})?$`)

func validHost(h string) bool { return h != "" && len(h) <= 255 && hostRe.MatchString(h) }

func (s *Server) redirectURL(r *http.Request) string { return s.baseURL(r) + oidcCbPath }

func (s *Server) getOIDC(w http.ResponseWriter, r *http.Request) {
	c, err := s.accounts.SSOConfig(r.Context())
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
	if in.LocalLoginDisabled && !in.Enabled {
		return bad("invalid_oidc", "le mot de passe local ne peut être désactivé que si le SSO est actif")
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
	up := accounts.SSOUpdate{Actor: currentUser(r), IP: s.clientIP(r)}
	switch {
	case in.ClientSecret != "":
		sealed, err := s.box.Seal([]byte(in.ClientSecret))
		if err != nil {
			s.fail(w, err)
			return
		}
		up.ReplaceSecret, up.SecretEnc = true, sealed
	case in.ClearClientSecret:
		up.ReplaceSecret = true
	}
	rm, _ := json.Marshal(in.RoleMap)
	up.Config = store.OIDCConfig{Enabled: in.Enabled, Name: in.Name, Issuer: in.Issuer, ClientID: in.ClientID, Scopes: in.Scopes,
		RoleClaim: in.RoleClaim, RoleMap: string(rm), DefaultRole: in.DefaultRole, JIT: in.JIT, LocalPasswordAdminsOnly: in.LocalPasswordAdminsOnly,
		LocalLoginDisabled: in.LocalLoginDisabled, EndSession: in.EndSession, AutoRedirect: in.AutoRedirect}
	saved, err := s.accounts.UpdateSSOConfig(r.Context(), up)
	switch {
	case errors.Is(err, accounts.ErrNoLocalAdmin):
		s.fail(w, bad("no_local_admin", accounts.ErrNoLocalAdmin.Error()))
		return
	case errors.Is(err, accounts.ErrNoSSOAdmin):
		s.fail(w, bad("no_sso_admin", accounts.ErrNoSSOAdmin.Error()))
		return
	case err != nil:
		s.fail(w, err)
		return
	}
	after := viewOf(saved)
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
		c, err := s.accounts.SSOConfig(r.Context())
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
	c, err := s.accounts.SSOConfig(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	body := map[string]any{"oidc": nil, "local_password_admins_only": false, "local_login_disabled": false, "auto_redirect": false}
	if c.Enabled {
		body["oidc"] = map[string]any{"name": c.Name, "start_url": "/api/v1/auth/oidc/start"}
		body["local_password_admins_only"] = c.LocalPasswordAdminsOnly
		body["local_login_disabled"] = c.LocalLoginDisabled
		body["auto_redirect"] = c.AutoRedirect
	}
	writeJSON(w, 200, body)
}

// oidcLoaded: configuration active + secret déchiffré. ok=false si le SSO est désactivé ou incomplet.
func (s *Server) oidcLoaded(ctx context.Context) (store.OIDCConfig, oidc.Config, bool, error) {
	c, err := s.accounts.SSOConfig(ctx)
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
	if s.lim.Blocked(limKey) {
		s.ssoFailure(w, r, "", "too_many_attempts", nil)
		return
	}
	fail := func(actor, reason string, detail map[string]any) {
		s.lim.Fail(limKey)
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
	uname := accounts.CleanName(firstNonEmpty(cl.String("preferred_username"), cl.String("email"), sub))
	if !mapped {
		fail(uname, "role_not_mapped", map[string]any{"sub": sub})
		return
	}
	display := accounts.CleanName(firstNonEmpty(cl.String("name"), uname))

	user, err := s.accounts.SignInSSO(ctx, accounts.SSOSignIn{Issuer: cfg.Issuer, Sub: sub, Username: uname, DisplayName: display, Role: role, JIT: c.JIT, IP: ip})
	switch {
	case errors.Is(err, accounts.ErrSSONotProvisioned):
		fail(uname, "not_provisioned", map[string]any{"sub": sub})
		return
	case errors.Is(err, accounts.ErrSSOAccountDisabled):
		fail(uname, "account_disabled", map[string]any{"sub": sub})
		return
	case err != nil:
		s.log.Error("sso: connexion", "err", err)
		fail(uname, "internal", nil)
		return
	}
	if _, err := s.startSession(w, r, user); err != nil {
		s.log.Error("sso: session", "err", err)
		fail(uname, "internal", nil)
		return
	}
	s.lim.Reset(limKey)
	http.Redirect(w, r, "/", http.StatusFound)
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
