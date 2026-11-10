package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/oidc/oidctest"
)

// ssoEnv: instance Leassh + fournisseur OIDC factice en mémoire (aucun vrai identifiant).
func ssoEnv(t *testing.T, mod func(cfg map[string]any)) (*env, *oidctest.Server) {
	t.Helper()
	e := newEnv(t)
	idp := oidctest.New(t)
	cfg := map[string]any{"enabled": true, "name": "Keycloak", "issuer": idp.URL, "client_id": idp.ClientID, "client_secret": idp.ClientSecret,
		"scopes": "openid profile email", "role_claim": "groups", "default_role": "viewer", "jit": true, "end_session": true,
		"role_map": map[string]any{"admin": []string{"leassh-admins"}, "approver": []string{"ops"}, "viewer": []string{"readers"}}}
	if mod != nil {
		mod(cfg)
	}
	e.must("PUT", "/settings/oidc", cfg, 200)
	return e, idp
}

func noFollow() *http.Client {
	c := client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

func get(t *testing.T, c *http.Client, u string) *http.Response {
	t.Helper()
	res, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

// ssoLogin déroule start → fournisseur → callback; tamper peut altérer l'URL de retour. Renvoie la destination finale et le client.
func ssoLogin(t *testing.T, e *env, tamper func(cb *url.URL)) (string, *http.Client) {
	t.Helper()
	c := noFollow()
	res := get(t, c, e.ts.URL+"/api/v1/auth/oidc/start")
	if res.StatusCode != 302 {
		t.Fatalf("start: %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if strings.HasPrefix(loc, "/?sso_error=") { // refus dès le démarrage (SSO désactivé, découverte invalide)
		return loc, c
	}
	if !strings.Contains(loc, "code_challenge_method=S256") || !strings.Contains(loc, "state=") || !strings.Contains(loc, "nonce=") {
		t.Fatalf("URL d'autorisation sans PKCE/state/nonce: %s", loc)
	}
	idpRes := get(t, c, loc)
	cb, _ := url.Parse(idpRes.Header.Get("Location"))
	if tamper != nil {
		tamper(cb)
	}
	res = get(t, c, cb.String())
	if res.StatusCode != 302 {
		t.Fatalf("callback: %d", res.StatusCode)
	}
	return res.Header.Get("Location"), c
}

func replaceQuery(cb *url.URL, k, v string) {
	q := cb.Query()
	q.Set(k, v)
	cb.RawQuery = q.Encode()
}

func me(t *testing.T, c *http.Client, e *env) (int, map[string]any) {
	t.Helper()
	r, out := do(t, c, "GET", e.ts.URL+"/api/v1/auth/me", nil, nil)
	return r.StatusCode, out
}

func (e *env) auditCount(action string) int {
	var n int
	if err := e.st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action=?`, action).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) lastDiff(action string) string {
	var d string
	_ = e.st.DB.QueryRow(`SELECT diff FROM audit_events WHERE action=? ORDER BY seq DESC LIMIT 1`, action).Scan(&d)
	return d
}

func (e *env) userCount() int {
	var n int
	_ = e.st.DB.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	return n
}

func TestSSOLoginJITAndRoleMapping(t *testing.T) {
	e, idp := ssoEnv(t, nil)
	idp.Who.Groups = []string{"ops", "other"}

	dest, c := ssoLogin(t, e, nil)
	if dest != "/" {
		t.Fatalf("destination: %s", dest)
	}
	code, u := me(t, c, e)
	if code != 200 || u["role"] != "approver" || u["auth_provider"] != "oidc" || u["username"] != "alice" || str(u, "csrf_token") == "" {
		t.Fatalf("me: %d %v", code, u)
	}
	// la session SSO réutilise le CSRF existant: une mutation sans jeton est refusée
	if r, _ := do(t, c, "POST", e.ts.URL+"/api/v1/auth/logout", nil, nil); r.StatusCode != 403 {
		t.Fatalf("logout sans CSRF: %d", r.StatusCode)
	}
	if e.auditCount("user.sso_login") != 1 || e.auditCount("user.create") < 2 { // admin du setup + JIT
		t.Fatalf("audit: login=%d create=%d", e.auditCount("user.sso_login"), e.auditCount("user.create"))
	}
	// approver: peut décider mais pas modifier la configuration
	if r, _ := do(t, c, "GET", e.ts.URL+"/api/v1/settings/oidc", nil, nil); r.StatusCode != 403 {
		t.Fatalf("settings/oidc pour un approver: %d", r.StatusCode)
	}

	// le plus haut rôle gagne, et le compte est retrouvé par (issuer, sub) même si nom/e-mail changent
	idp.Who.Groups = []string{"readers", "leassh-admins"}
	idp.Who.Username, idp.Who.Email, idp.Who.Name = "alice.renamed", "autre@example.test", "Alice R."
	_, c2 := ssoLogin(t, e, nil)
	_, u2 := me(t, c2, e)
	if u2["id"] != u["id"] || u2["role"] != "admin" || e.userCount() != 2 {
		t.Fatalf("lien par sub: %v / %v (utilisateurs: %d)", u, u2, e.userCount())
	}
	if code, _ := me(t, c, e); code != 401 {
		t.Fatalf("l'ancienne session aurait dû être fermée au changement de rôle: %d", code)
	}
	if e.auditCount("user.update") != 1 || !strings.Contains(e.lastDiff("user.update"), `"approver"`) {
		t.Fatalf("audit de la synchro de rôle: %s", e.lastDiff("user.update"))
	}
}

func TestSSONeverLinksByEmailOrUsername(t *testing.T) {
	e, idp := ssoEnv(t, nil)
	// compte local « admin » existant (setup); le fournisseur annonce le même nom et e-mail pour un autre sub
	idp.Who = oidctest.Identity{Sub: "sub-attaquant", Username: "admin", Email: "admin@example.test", Name: "Pas l'admin", Groups: []string{"readers"}}
	_, c := ssoLogin(t, e, nil)
	_, u := me(t, c, e)
	if u["role"] != "viewer" || u["username"] == "admin" || !strings.HasPrefix(str(u, "username"), "admin@") {
		t.Fatalf("collision de nom: %v", u)
	}
}

func TestSSORoleNotMappedAndJITOff(t *testing.T) {
	e, idp := ssoEnv(t, func(c map[string]any) { c["default_role"] = "" })
	idp.Who.Groups = []string{"inconnu"}
	dest, c := ssoLogin(t, e, nil)
	if dest != "/?sso_error=role_not_mapped" {
		t.Fatalf("destination: %s", dest)
	}
	if code, _ := me(t, c, e); code != 401 || e.userCount() != 1 {
		t.Fatalf("aucun compte ni session attendus: %d, %d utilisateurs", code, e.userCount())
	}
	if e.auditCount("user.sso_login_failed") != 1 || !strings.Contains(e.lastDiff("user.sso_login_failed"), "role_not_mapped") {
		t.Fatalf("audit: %s", e.lastDiff("user.sso_login_failed"))
	}

	// JIT désactivé: un inconnu est refusé même avec un rôle mappé
	e.must("PUT", "/settings/oidc", map[string]any{"enabled": true, "issuer": idp.URL, "client_id": idp.ClientID, "jit": false, "default_role": "viewer", "role_claim": "groups"}, 200)
	if dest, _ := ssoLogin(t, e, nil); dest != "/?sso_error=not_provisioned" || e.userCount() != 1 {
		t.Fatalf("JIT off: %s (%d utilisateurs)", dest, e.userCount())
	}
	// ... mais un compte déjà lié continue de se connecter
	e.must("PUT", "/settings/oidc", map[string]any{"enabled": true, "issuer": idp.URL, "client_id": idp.ClientID, "jit": true, "default_role": "viewer", "role_claim": "groups"}, 200)
	if dest, _ := ssoLogin(t, e, nil); dest != "/" {
		t.Fatalf("JIT on: %s", dest)
	}
	e.must("PUT", "/settings/oidc", map[string]any{"enabled": true, "issuer": idp.URL, "client_id": idp.ClientID, "jit": false, "default_role": "viewer", "role_claim": "groups"}, 200)
	if dest, _ := ssoLogin(t, e, nil); dest != "/" {
		t.Fatalf("compte lié avec JIT off: %s", dest)
	}
}

func TestSSOStateAndNonce(t *testing.T) {
	e, idp := ssoEnv(t, nil)
	idp.Who.Groups = []string{"readers"}

	if dest, _ := ssoLogin(t, e, func(cb *url.URL) { replaceQuery(cb, "state", "forgé") }); dest != "/?sso_error=state_mismatch" {
		t.Fatalf("state: %s", dest)
	}
	// callback sans cookie d'amorce (autre navigateur / rejeu)
	c := noFollow()
	if res := get(t, c, e.ts.URL+"/api/v1/auth/oidc/callback?code=x&state=y"); res.Header.Get("Location") != "/?sso_error=state_missing" {
		t.Fatalf("sans cookie: %s", res.Header.Get("Location"))
	}
	// rejeu du même retour: le cookie est à usage unique
	c = noFollow()
	res := get(t, c, e.ts.URL+"/api/v1/auth/oidc/start")
	idpRes := get(t, c, res.Header.Get("Location"))
	cb := idpRes.Header.Get("Location")
	if get(t, c, cb).Header.Get("Location") != "/" {
		t.Fatal("premier retour refusé")
	}
	if got := get(t, c, cb).Header.Get("Location"); got != "/?sso_error=state_missing" {
		t.Fatalf("rejeu: %s", got)
	}
	// erreur renvoyée par le fournisseur
	if dest, _ := ssoLogin(t, e, func(cb *url.URL) {
		replaceQuery(cb, "error", "access_denied")
		cb.RawQuery = strings.Replace(cb.RawQuery, "code=", "x=", 1)
	}); dest != "/?sso_error=provider_error" {
		t.Fatalf("provider_error: %s", dest)
	}

	idp.OverrideNonce = "autre-nonce"
	if dest, _ := ssoLogin(t, e, nil); dest != "/?sso_error=token_nonce" {
		t.Fatalf("nonce: %s", dest)
	}
	idp.OverrideNonce = ""
	if dest, _ := ssoLogin(t, e, nil); dest != "/" {
		t.Fatalf("contrôle: %s", dest)
	}
}

func TestSSOTokenValidation(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		mutate func(p map[string]any)
		setup  func(idp *oidctest.Server)
	}{
		{name: "expiré", want: "token_expired", mutate: func(p map[string]any) { p["exp"] = time.Now().Add(-10 * time.Minute).Unix() }},
		{name: "mauvais aud", want: "token_audience", mutate: func(p map[string]any) { p["aud"] = "un-autre-client" }},
		{name: "aud multiple sans azp", want: "token_audience", mutate: func(p map[string]any) { p["aud"] = []string{"leassh", "autre"}; delete(p, "azp") }},
		{name: "mauvais azp", want: "token_audience", mutate: func(p map[string]any) { p["azp"] = "autre" }},
		{name: "mauvais iss", want: "token_issuer", mutate: func(p map[string]any) { p["iss"] = "https://evil.example.test" }},
		{name: "sans exp", want: "token_malformed", mutate: func(p map[string]any) { delete(p, "exp") }},
		{name: "sans sub", want: "token_subject", mutate: func(p map[string]any) { delete(p, "sub") }},
		{name: "nbf futur", want: "token_not_yet_valid", mutate: func(p map[string]any) { p["nbf"] = time.Now().Add(time.Hour).Unix() }},
		{name: "mauvaise clé", want: "token_signature", setup: func(idp *oidctest.Server) {
			other := oidctest.New(t)
			idp.SignKey = other.Key
		}},
		{name: "issuer de découverte différent", want: "discovery_failed", setup: func(idp *oidctest.Server) { idp.IssuerOverride = "https://autre.example.test" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, idp := ssoEnv(t, nil)
			idp.Who.Groups = []string{"readers"}
			idp.Mutate = tc.mutate
			if tc.setup != nil {
				tc.setup(idp)
			}
			dest, c := ssoLogin(t, e, nil)
			if dest != "/?sso_error="+tc.want {
				t.Fatalf("destination %s, attendu %s", dest, tc.want)
			}
			if code, _ := me(t, c, e); code != 401 || e.userCount() != 1 {
				t.Fatalf("session ou compte créés malgré l'échec: %d, %d", code, e.userCount())
			}
			if e.auditCount("user.sso_login_failed") != 1 || e.auditCount("user.sso_login") != 0 {
				t.Fatal("audit de l'échec manquant")
			}
		})
	}
}

func TestSSORoleClaimPathAndUserinfo(t *testing.T) {
	// groupes imbriqués (Keycloak: realm_access.roles)
	e, idp := ssoEnv(t, func(c map[string]any) { c["role_claim"] = "realm_access.roles" })
	idp.Groups, idp.Who.Groups = "realm_access.roles", []string{"ops"}
	_, c := ssoLogin(t, e, nil)
	if _, u := me(t, c, e); u["role"] != "approver" {
		t.Fatalf("chemin pointé: %v", u)
	}
	// revendication absente de l'ID token: lue dans userinfo (avec sub concordant)
	e, idp = ssoEnv(t, nil)
	idp.Groups, idp.Who.Groups = "userinfo", []string{"leassh-admins"}
	_, c = ssoLogin(t, e, nil)
	if _, u := me(t, c, e); u["role"] != "admin" {
		t.Fatalf("userinfo: %v", u)
	}
}

func TestSSOLocalPasswordPolicy(t *testing.T) {
	e, idp := ssoEnv(t, nil)
	idp.Who.Groups = []string{"readers"}
	_, sc := ssoLogin(t, e, nil)
	_, u := me(t, sc, e)

	// un compte SSO n'a pas de mot de passe local, ne peut pas en changer et son rôle n'est pas éditable à la main
	if r, _ := do(t, client(), "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "alice", Password: ""}, nil); r.StatusCode != 401 {
		t.Fatalf("mot de passe vide sur compte SSO: %d", r.StatusCode)
	}
	if r, _ := do(t, sc, "POST", e.ts.URL+"/api/v1/auth/password", map[string]string{"current": "", "new": "un-mot-de-passe-solide"}, map[string]string{"X-CSRF-Token": str(u, "csrf_token")}); r.StatusCode != 400 {
		t.Fatalf("changement de mot de passe SSO: %d", r.StatusCode)
	}
	e.must("PUT", "/users/"+str(u, "id"), map[string]any{"role": "admin", "display_name": "x"}, 409)
	e.must("PUT", "/users/"+str(u, "id"), map[string]any{"role": "viewer", "password": "un-mot-de-passe-solide-2"}, 409)

	// viewer local: autorisé tant que le réglage n'est pas posé
	e.must("POST", "/users", map[string]any{"username": "local-viewer", "role": "viewer", "password": "mot-de-passe-viewer"}, 201)
	lc := client()
	if r, _ := do(t, lc, "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "local-viewer", Password: "mot-de-passe-viewer"}, nil); r.StatusCode != 200 {
		t.Fatalf("viewer local avant restriction: %d", r.StatusCode)
	}
	cfg := e.must("GET", "/settings/oidc", nil, 200)
	cfg["local_password_admins_only"] = true
	delete(cfg, "has_client_secret")
	delete(cfg, "redirect_url")
	e.must("PUT", "/settings/oidc", cfg, 200)
	if r, out := do(t, client(), "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "local-viewer", Password: "mot-de-passe-viewer"}, nil); r.StatusCode != 403 || toJSON(out) == "" || !strings.Contains(toJSON(out), "local_login_disabled") {
		t.Fatalf("viewer local après restriction: %d %v", r.StatusCode, out)
	}
	if r, _ := do(t, client(), "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "local-viewer", Password: "faux-mot-de-passe"}, nil); r.StatusCode != 401 {
		t.Fatalf("mauvais mot de passe: %d", r.StatusCode)
	}
	// l'admin local (secours) reste utilisable
	if r, _ := do(t, client(), "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil); r.StatusCode != 200 {
		t.Fatalf("admin local: %d", r.StatusCode)
	}
	if e.auditCount("user.login_failed") < 2 {
		t.Fatal("refus non audité")
	}
	// la page de connexion le sait; sans SSO actif le réglage ne verrouille personne
	_, p := do(t, client(), "GET", e.ts.URL+"/api/v1/auth/providers", nil, nil)
	if o, _ := p["oidc"].(map[string]any); o["name"] != "Keycloak" || p["local_password_admins_only"] != true {
		t.Fatalf("providers: %v", p)
	}
	cfg["enabled"] = false
	e.must("PUT", "/settings/oidc", cfg, 200)
	if r, _ := do(t, client(), "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "local-viewer", Password: "mot-de-passe-viewer"}, nil); r.StatusCode != 200 {
		t.Fatalf("SSO désactivé: %d", r.StatusCode)
	}
	if _, p := do(t, client(), "GET", e.ts.URL+"/api/v1/auth/providers", nil, nil); p["oidc"] != nil {
		t.Fatalf("providers sans SSO: %v", p)
	}
	if dest, _ := ssoLogin(t, e, nil); dest != "/?sso_error=disabled" {
		t.Fatalf("SSO désactivé: %s", dest)
	}
}

func TestSSODisabledAccountRefused(t *testing.T) {
	e, idp := ssoEnv(t, nil)
	idp.Who.Groups = []string{"readers"}
	_, c := ssoLogin(t, e, nil)
	_, u := me(t, c, e)
	e.must("PUT", "/users/"+str(u, "id"), map[string]any{"role": "viewer", "display_name": "Alice", "disabled": true}, 200)
	if dest, _ := ssoLogin(t, e, nil); dest != "/?sso_error=account_disabled" {
		t.Fatalf("compte désactivé: %s", dest)
	}
}

func TestSSOLogoutEndSession(t *testing.T) {
	e, idp := ssoEnv(t, nil)
	idp.Who.Groups = []string{"readers"}
	_, c := ssoLogin(t, e, nil)
	_, u := me(t, c, e)
	r, out := do(t, c, "POST", e.ts.URL+"/api/v1/auth/logout", nil, map[string]string{"X-CSRF-Token": str(u, "csrf_token")})
	end := str(out, "end_session_url")
	if r.StatusCode != 200 || !strings.HasPrefix(end, idp.URL+"/logout") || !strings.Contains(end, "client_id=leassh") || !strings.Contains(end, "post_logout_redirect_uri=") {
		t.Fatalf("déconnexion SSO: %d %v", r.StatusCode, out)
	}
	if code, _ := me(t, c, e); code != 401 {
		t.Fatalf("session locale encore valide: %d", code)
	}
	// option désactivée → déconnexion locale seule
	cfg := e.must("GET", "/settings/oidc", nil, 200)
	cfg["end_session"] = false
	delete(cfg, "has_client_secret")
	delete(cfg, "redirect_url")
	e.must("PUT", "/settings/oidc", cfg, 200)
	_, c = ssoLogin(t, e, nil)
	_, u = me(t, c, e)
	if r, _ := do(t, c, "POST", e.ts.URL+"/api/v1/auth/logout", nil, map[string]string{"X-CSRF-Token": str(u, "csrf_token")}); r.StatusCode != 204 {
		t.Fatalf("déconnexion locale: %d", r.StatusCode)
	}
}

// Le test de configuration ne doit jamais relayer (ni journaliser) le corps de la réponse d'une URL distante.
func TestSSOTestDoesNotLeakRemoteBody(t *testing.T) {
	const sentinel = "SECRET-INTERNAL-BODY"
	e := newEnv(t)
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, sentinel, http.StatusInternalServerError)
	}))
	defer internal.Close()
	res := e.must("POST", "/settings/oidc/test", map[string]any{"issuer": internal.URL}, 200)
	if res["ok"] != false || str(res, "error") == "" {
		t.Fatalf("test en échec attendu: %v", res)
	}
	if strings.Contains(toJSON(res), sentinel) {
		t.Fatalf("corps distant renvoyé à l'administrateur: %v", res)
	}
	if !strings.Contains(str(res, "error"), "500") {
		t.Fatalf("le code HTTP devrait figurer dans le message: %v", res)
	}
	if d := e.lastDiff("oidc.test"); strings.Contains(d, sentinel) {
		t.Fatalf("corps distant écrit dans le journal d'audit: %s", d)
	}
}

func TestSSOSettings(t *testing.T) {
	e := newEnv(t)
	idp := oidctest.New(t)

	got := e.must("GET", "/settings/oidc", nil, 200)
	if got["enabled"] != false || got["default_role"] != "viewer" || !strings.HasSuffix(str(got, "redirect_url"), "/api/v1/auth/oidc/callback") {
		t.Fatalf("défauts: %v", got)
	}
	// validations
	e.must("PUT", "/settings/oidc", map[string]any{"enabled": true}, 400)                              // pas d'issuer
	e.must("PUT", "/settings/oidc", map[string]any{"issuer": "ftp://x", "scopes": "openid"}, 400)      // schéma
	e.must("PUT", "/settings/oidc", map[string]any{"issuer": idp.URL, "scopes": "profile email"}, 400) // pas de openid
	e.must("PUT", "/settings/oidc", map[string]any{"issuer": idp.URL, "default_role": "admin"}, 400)   // rôle par défaut interdit
	e.must("PUT", "/settings/oidc", map[string]any{"issuer": idp.URL + "?x=1"}, 400)                   // paramètres
	in := map[string]any{"enabled": true, "name": "Keycloak", "issuer": idp.URL, "client_id": "leassh", "client_secret": "secret-très-confidentiel", "default_role": "viewer", "jit": true,
		"role_map": map[string]any{"admin": []string{" g-admin ", ""}}}
	out := e.must("PUT", "/settings/oidc", in, 200)
	if out["has_client_secret"] != true || strings.Contains(toJSON(out), "confidentiel") || strings.Contains(toJSON(e.must("GET", "/settings/oidc", nil, 200)), "confidentiel") {
		t.Fatalf("secret exposé: %v", out)
	}
	if rm := out["role_map"].(map[string]any); len(rm["admin"].([]any)) != 1 || rm["admin"].([]any)[0] != "g-admin" {
		t.Fatalf("role_map: %v", rm)
	}
	// chiffré au repos
	var enc []byte
	_ = e.st.DB.QueryRow(`SELECT client_secret_enc FROM oidc_config`).Scan(&enc)
	if len(enc) == 0 || strings.Contains(string(enc), "confidentiel") {
		t.Fatal("secret non chiffré en base")
	}
	// secret vide = conservé; effacement explicite
	in["client_secret"] = ""
	if e.must("PUT", "/settings/oidc", in, 200)["has_client_secret"] != true {
		t.Fatal("secret perdu")
	}
	in["clear_client_secret"] = true
	if e.must("PUT", "/settings/oidc", in, 200)["has_client_secret"] != false {
		t.Fatal("secret non effacé")
	}
	// audit: modification tracée, secret jamais présent
	if e.auditCount("oidc.update") != 3 || strings.Contains(e.lastDiff("oidc.update"), "confidentiel") {
		t.Fatalf("audit: %d %s", e.auditCount("oidc.update"), e.lastDiff("oidc.update"))
	}

	// test de découverte
	res := e.must("POST", "/settings/oidc/test", map[string]any{}, 200)
	if res["ok"] != true || res["jwks_keys"] != float64(1) || res["token_endpoint"] != idp.URL+"/token" {
		t.Fatalf("test: %v", res)
	}
	if w := toJSON(res["warnings"]); !strings.Contains(w, "issuer_http") {
		t.Fatalf("avertissements: %s", w)
	}
	bad := e.must("POST", "/settings/oidc/test", map[string]any{"issuer": "http://127.0.0.1:1"}, 200)
	if bad["ok"] != false || str(bad, "error") == "" {
		t.Fatalf("test en échec: %v", bad)
	}
	if e.auditCount("oidc.test") != 2 {
		t.Fatalf("audit des tests: %d", e.auditCount("oidc.test"))
	}

	// réservé à l'admin
	e.must("POST", "/users", map[string]any{"username": "v", "role": "viewer", "password": "mot-de-passe-viewer"}, 201)
	vc := client()
	_, lo := do(t, vc, "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "v", Password: "mot-de-passe-viewer"}, nil)
	h := map[string]string{"X-CSRF-Token": str(lo, "csrf_token")}
	for _, call := range [][2]string{{"GET", "/settings/oidc"}, {"PUT", "/settings/oidc"}, {"POST", "/settings/oidc/test"}} {
		if r, _ := do(t, vc, call[0], e.ts.URL+"/api/v1"+call[1], map[string]any{}, h); r.StatusCode != 403 {
			t.Errorf("%s %s: %d", call[0], call[1], r.StatusCode)
		}
	}
}

// Une clé MCP ne passe jamais par le SSO ni par la session: ces routes n'acceptent pas Bearer.
func TestSSOEndpointsIgnoreBearer(t *testing.T) {
	e, _ := ssoEnv(t, nil)
	r, _ := do(t, client(), "GET", e.ts.URL+"/api/v1/settings/oidc", nil, map[string]string{"Authorization": "Bearer lsh_x"})
	if r.StatusCode != 401 {
		t.Fatalf("Bearer sur la config SSO: %d", r.StatusCode)
	}
}

func TestSSOLocalOnlyRestrictionNeedsLocalAdmin(t *testing.T) {
	e := newEnv(t)
	idp := oidctest.New(t)
	// l'unique admin local est désactivé impossible (garde-fou existant) → on vérifie le cas par le store
	in := map[string]any{"enabled": true, "issuer": idp.URL, "client_id": "leassh", "local_password_admins_only": true, "default_role": "viewer"}
	e.must("PUT", "/settings/oidc", in, 200) // admin local présent: accepté
	if _, err := e.st.DB.Exec(`UPDATE users SET auth_provider='oidc' WHERE username='admin'`); err != nil {
		t.Fatal(err)
	}
	e.must("PUT", "/settings/oidc", in, 400)
}

func TestBaseURLDerivation(t *testing.T) {
	mk := func(base, proxies string) *Server {
		return &Server{set: testSettings(t, base, proxies)}
	}
	req := func(remote string, h map[string]string) *http.Request {
		r := httptest.NewRequest("GET", "http://192.168.6.167:8080/x", nil)
		r.RemoteAddr = remote
		for k, v := range h {
			r.Header.Set(k, v)
		}
		return r
	}
	fwd := map[string]string{"X-Forwarded-Host": "leassh.example.com", "X-Forwarded-Proto": "https"}
	cases := []struct {
		name string
		s    *Server
		r    *http.Request
		want string
	}{
		{"URL fixe prioritaire", mk("https://fixe.example.com/", "10.0.0.0/8"), req("10.0.0.5:1234", fwd), "https://fixe.example.com"},
		{"proxy de confiance", mk("", "10.0.0.0/8"), req("10.0.0.5:1234", fwd), "https://leassh.example.com"},
		{"en-têtes ignorés hors proxy de confiance", mk("", "10.0.0.0/8"), req("203.0.113.9:1234", fwd), "http://192.168.6.167:8080"},
		{"hôte transféré invalide ignoré", mk("", "10.0.0.0/8"), req("10.0.0.5:1234", map[string]string{"X-Forwarded-Host": "evil.com/\r\n x", "X-Forwarded-Proto": "https"}), "https://192.168.6.167:8080"},
		{"sans proxy configuré", mk("", ""), req("10.0.0.5:1234", fwd), "http://192.168.6.167:8080"},
	}
	for _, c := range cases {
		if got := c.s.baseURL(c.r); got != c.want {
			t.Errorf("%s: %q, attendu %q", c.name, got, c.want)
		}
	}
}

func TestAuthProvidersAutoRedirect(t *testing.T) {
	e, _ := ssoEnv(t, func(cfg map[string]any) { cfg["auto_redirect"] = true })
	res, err := client().Get(e.ts.URL + "/api/v1/auth/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body["auto_redirect"] != true {
		t.Fatalf("auto_redirect attendu: %v %v", body, err)
	}
}

// ARC-01: SSO obligatoire. Une fois un admin SSO opérationnel, le mot de passe local peut être désactivé pour tous (même l'admin local).
func TestSSOLocalLoginDisabled(t *testing.T) {
	e, idp := ssoEnv(t, nil)
	login := func() int {
		r, _ := do(t, client(), "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil)
		return r.StatusCode
	}
	cfg := e.must("GET", "/settings/oidc", nil, 200)
	delete(cfg, "has_client_secret")
	delete(cfg, "redirect_url")

	// garde-fou: sans administrateur SSO ayant déjà ouvert une session, la désactivation est refusée (risque de verrouillage)
	cfg["local_login_disabled"] = true
	e.must("PUT", "/settings/oidc", cfg, 400)
	if login() != 200 {
		t.Fatal("admin local doit rester utilisable tant que le réglage n'est pas posé")
	}

	// un administrateur SSO se connecte une première fois
	idp.Who.Groups = []string{"leassh-admins"}
	if dest, _ := ssoLogin(t, e, nil); dest != "/" {
		t.Fatalf("connexion SSO admin: %s", dest)
	}
	e.must("PUT", "/settings/oidc", cfg, 200)
	if got := login(); got != 403 {
		t.Fatalf("admin local après désactivation: %d (403 attendu)", got)
	}
	if !strings.Contains(e.lastDiff("user.login_failed"), "local_login_disabled") {
		t.Fatal("refus non audité avec sa raison")
	}
	if _, p := do(t, client(), "GET", e.ts.URL+"/api/v1/auth/providers", nil, nil); p["local_login_disabled"] != true {
		t.Fatalf("providers: %v", p)
	}
	// le SSO continue de fonctionner
	if dest, _ := ssoLogin(t, e, nil); dest != "/" {
		t.Fatalf("SSO après désactivation: %s", dest)
	}
	// on ne peut pas couper le SSO tout en gardant le local désactivé
	cfg["enabled"] = false
	e.must("PUT", "/settings/oidc", cfg, 400)
}
