package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sebdew/LeaSSH/internal/crypto"
	"github.com/sebdew/LeaSSH/internal/settings"
	"github.com/sebdew/LeaSSH/internal/store"
)

func newTest(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set, err := settings.New(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(set, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSetupToken(testSetupToken)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

// testSetupToken: jeton d'initialisation armé par les serveurs de test.
const testSetupToken = "jeton-d-initialisation-de-test"

// setupBody: corps valide de POST /setup, avec le jeton d'initialisation de test.
func setupBody(user, pass string) map[string]string {
	return map[string]string{"username": user, "password": pass, "setup_token": testSetupToken}
}

func do(t *testing.T, c *http.Client, method, url string, body any, hdr map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	checkContract(t, req, resp, raw)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp, out
}

func client() *http.Client {
	jar, _ := newJar()
	return &http.Client{Jar: jar}
}

func TestSetupLoginFlow(t *testing.T) {
	ts, _ := newTest(t)
	c := client()
	base := ts.URL + "/api/v1"

	_, st := do(t, c, "GET", base+"/setup/status", nil, nil)
	if st["initialized"] != false {
		t.Fatalf("status: %v", st)
	}
	if r, _ := do(t, c, "POST", base+"/setup", setupBody("admin", "court"), nil); r.StatusCode != 400 {
		t.Fatalf("mot de passe faible accepté: %d", r.StatusCode)
	}
	r, out := do(t, c, "POST", base+"/setup", setupBody("admin", "un-mot-de-passe-solide"), nil)
	if r.StatusCode != 201 || out["role"] != "admin" || out["csrf_token"] == "" {
		t.Fatalf("setup: %d %v", r.StatusCode, out)
	}
	if _, has := out["password_hash"]; has || strings.Contains(out["username"].(string), "$") {
		t.Fatal("fuite du hash")
	}
	ck := r.Header.Get("Set-Cookie")
	if !strings.Contains(ck, "HttpOnly") || !strings.Contains(ck, "SameSite=Strict") {
		t.Fatalf("cookie: %s", ck)
	}
	// second setup refusé
	if r, _ := do(t, client(), "POST", base+"/setup", setupBody("evil", "un-mot-de-passe-solide"), nil); r.StatusCode != 409 {
		t.Fatalf("double setup: %d", r.StatusCode)
	}
	csrf := out["csrf_token"].(string)
	if r, _ := do(t, c, "GET", base+"/auth/me", nil, nil); r.StatusCode != 200 {
		t.Fatalf("me: %d", r.StatusCode)
	}
	// CSRF
	if r, _ := do(t, c, "POST", base+"/auth/logout", nil, nil); r.StatusCode != 403 {
		t.Fatalf("logout sans CSRF: %d", r.StatusCode)
	}
	if r, _ := do(t, c, "POST", base+"/auth/logout", nil, map[string]string{"X-CSRF-Token": csrf, "Origin": "http://evil.example"}); r.StatusCode != 403 {
		t.Fatalf("mauvaise origine: %d", r.StatusCode)
	}
	if r, _ := do(t, c, "POST", base+"/auth/logout", nil, map[string]string{"X-CSRF-Token": csrf}); r.StatusCode != 204 {
		t.Fatalf("logout: %d", r.StatusCode)
	}
	if r, _ := do(t, c, "GET", base+"/auth/me", nil, nil); r.StatusCode != 401 {
		t.Fatalf("session survivante: %d", r.StatusCode)
	}
	// login
	if r, _ := do(t, c, "POST", base+"/auth/login", credentials{Username: "admin", Password: "faux"}, nil); r.StatusCode != 401 {
		t.Fatalf("mauvais mot de passe: %d", r.StatusCode)
	}
	if r, _ := do(t, c, "POST", base+"/auth/login", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil); r.StatusCode != 200 {
		t.Fatalf("login: %d", r.StatusCode)
	}
}

func TestLoginRateLimitAndAudit(t *testing.T) {
	ts, st := newTest(t)
	base := ts.URL + "/api/v1"
	do(t, client(), "POST", base+"/setup", setupBody("admin", "un-mot-de-passe-solide"), nil)
	c := client()
	var last int
	for i := 0; i < maxFailures+2; i++ {
		r, _ := do(t, c, "POST", base+"/auth/login", credentials{Username: "admin", Password: "x"}, nil)
		last = r.StatusCode
	}
	if last != 429 {
		t.Fatalf("limitation attendue, obtenu %d", last)
	}
	// même le bon mot de passe est bloqué pendant la fenêtre
	if r, _ := do(t, c, "POST", base+"/auth/login", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil); r.StatusCode != 429 {
		t.Fatalf("blocage: %d", r.StatusCode)
	}
	var n int
	st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action='user.login_failed'`).Scan(&n)
	if n != maxFailures {
		t.Fatalf("échecs journalisés: %d", n)
	}
}

func TestSecurityHeadersAndErrorFormat(t *testing.T) {
	ts, _ := newTest(t)
	r, out := do(t, client(), "GET", ts.URL+"/api/v1/nope", nil, nil)
	if r.StatusCode != 404 || out["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("format d'erreur: %v", out)
	}
	if strings.Contains(r.Header.Get("Content-Security-Policy"), "unsafe-inline") {
		t.Fatal("la CSP ne doit pas autoriser 'unsafe-inline' (SEC-05)")
	}
	if !strings.Contains(r.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal("CSP absente")
	}
}

func TestClientIPTrustedProxy(t *testing.T) {
	s := &Server{set: testSettings(t, "", "10.0.0.0/8")}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if ip := s.clientIP(req); ip != "203.0.113.9" {
		t.Fatalf("ip: %s", ip)
	}
	req.RemoteAddr = "198.51.100.1:1" // non fiable: en-tête ignoré
	if ip := s.clientIP(req); ip != "198.51.100.1" {
		t.Fatalf("ip non fiable: %s", ip)
	}
}

// testSettings: réglages en mémoire (base temporaire) avec URL publique et proxies de confiance donnés.
func testSettings(t *testing.T, baseURL, proxies string) *settings.Service {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set, err := settings.New(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := set.Get()
	v.BaseURL, v.TrustedProxies = baseURL, proxies
	set.Apply(v)
	return set
}

// SEC-01: sur une instance neuve, /setup sans jeton d'initialisation valide ne doit jamais créer d'administrateur.
func TestSetupRefusedWithoutValidToken(t *testing.T) {
	ts, st := newTest(t)
	base := ts.URL + "/api/v1"
	for name, tok := range map[string]string{"sans jeton": "", "mauvais jeton": "faux", "jeton tronqué": testSetupToken[:5]} {
		b := setupBody("evil", "un-mot-de-passe-solide")
		b["setup_token"] = tok
		if r, _ := do(t, client(), "POST", base+"/setup", b, nil); r.StatusCode != 401 {
			t.Errorf("%s: statut %d, attendu 401", name, r.StatusCode)
		}
	}
	if r, _ := do(t, client(), "POST", base+"/setup", map[string]string{"username": "evil", "password": "un-mot-de-passe-solide"}, nil); r.StatusCode != 401 {
		t.Errorf("champ absent: statut %d, attendu 401", r.StatusCode)
	}
	if n, _ := st.UserCount(context.Background()); n != 0 {
		t.Fatalf("%d utilisateur(s) créé(s) sans jeton valide", n)
	}
	// Le jeton valide fonctionne, une seule fois (le second appel, même avec le bon jeton, est refusé).
	if r, _ := do(t, client(), "POST", base+"/setup", setupBody("admin", "un-mot-de-passe-solide"), nil); r.StatusCode != 201 {
		t.Fatalf("jeton valide: %d", r.StatusCode)
	}
	if r, _ := do(t, client(), "POST", base+"/setup", setupBody("autre", "un-mot-de-passe-solide"), nil); r.StatusCode != 409 {
		t.Fatalf("second setup: %d", r.StatusCode)
	}
}

// Sans jeton armé côté serveur, /setup est fermé, même avec un jeton vide.
func TestSetupClosedWhenNoTokenConfigured(t *testing.T) {
	_, st := newTest(t)
	set, _ := settings.New(context.Background(), st, nil)
	s2, _ := New(set, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	ts2 := httptest.NewServer(s2.Handler())
	t.Cleanup(ts2.Close)
	b := setupBody("evil", "un-mot-de-passe-solide")
	b["setup_token"] = ""
	if r, _ := do(t, client(), "POST", ts2.URL+"/api/v1/setup", b, nil); r.StatusCode != 401 {
		t.Fatalf("attendu 401, reçu %d", r.StatusCode)
	}
}

// Les tentatives de jeton sont limitées (force brute).
func TestSetupTokenBruteForceLimited(t *testing.T) {
	ts, _ := newTest(t)
	b := setupBody("evil", "un-mot-de-passe-solide")
	b["setup_token"] = "faux"
	var last int
	for i := 0; i < maxFailures+1; i++ {
		r, _ := do(t, client(), "POST", ts.URL+"/api/v1/setup", b, nil)
		last = r.StatusCode
	}
	if last != 429 {
		t.Fatalf("attendu 429 après %d échecs, reçu %d", maxFailures, last)
	}
}

// SEC-04: une session inactive depuis plus longtemps que le délai d'inactivité est refusée.
func TestInactiveSessionRefused(t *testing.T) {
	ts, st := newTest(t)
	base := ts.URL + "/api/v1"
	c := client()
	do(t, c, "POST", base+"/setup", setupBody("admin", "un-mot-de-passe-solide"), nil)
	if r, _ := do(t, c, "GET", base+"/auth/me", nil, nil); r.StatusCode != 200 {
		t.Fatalf("session active: %d", r.StatusCode)
	}
	old := store.FormatTime(time.Now().Add(-2 * idleTimeout))
	if _, err := st.DB.Exec(`UPDATE sessions SET last_seen_at=?`, old); err != nil {
		t.Fatal(err)
	}
	if r, _ := do(t, c, "GET", base+"/auth/me", nil, nil); r.StatusCode != 401 {
		t.Fatalf("session inactive attendue refusée, obtenu %d", r.StatusCode)
	}
}

// SEC-04: cinq échecs sur un compte ne verrouillent pas les autres comptes derrière la même IP.
func TestLoginLockIsPerAccount(t *testing.T) {
	ts, st := newTest(t)
	base := ts.URL + "/api/v1"
	do(t, client(), "POST", base+"/setup", setupBody("admin", "un-mot-de-passe-solide"), nil)
	hash, err := crypto.HashPassword("autre-mot-de-passe-solide")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := store.InsertUser(context.Background(), tx, store.User{Username: "bob", Role: "viewer", PasswordHash: hash})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxFailures; i++ {
		do(t, client(), "POST", base+"/auth/login", credentials{Username: "admin", Password: "x"}, nil)
	}
	if r, _ := do(t, client(), "POST", base+"/auth/login", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil); r.StatusCode != 429 {
		t.Fatalf("le compte ciblé doit être verrouillé: %d", r.StatusCode)
	}
	if r, _ := do(t, client(), "POST", base+"/auth/login", credentials{Username: "bob", Password: "autre-mot-de-passe-solide"}, nil); r.StatusCode != 200 {
		t.Fatalf("un autre compte de la même IP ne doit pas être verrouillé: %d", r.StatusCode)
	}
}

// SEC-05: HSTS présent en HTTPS, absent en HTTP clair.
func TestHSTSOnlyOverHTTPS(t *testing.T) {
	s := &Server{set: testSettings(t, "", "")}
	h := s.securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Header().Get("Strict-Transport-Security"), "max-age=") {
		t.Fatal("Strict-Transport-Security absent en HTTPS")
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS ne doit pas être posé en HTTP clair")
	}
}

// SEC-05: derrière un proxy de confiance, c'est le proxy qui pose HSTS (pas de doublon); sans proxy, Leassh pose le sien.
func TestHSTSLeftToTrustedProxy(t *testing.T) {
	s := &Server{set: testSettings(t, "https://leassh.example.com", "10.0.0.0/8")}
	h := s.securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if v := rec.Header().Get("Strict-Transport-Security"); v != "" {
		t.Fatalf("requête d'un proxy de confiance: HSTS laissé au proxy, obtenu %q", v)
	}
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.1:1234" // pas un proxy de confiance
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("hors proxy de confiance, Leassh doit poser HSTS")
	}
}

// SEC-04 (#30): un redémarrage du serveur ne remet pas à zéro les échecs de connexion.
func TestLoginLockoutSurvivesRestart(t *testing.T) {
	ts, st := newTest(t)
	base := ts.URL + "/api/v1"
	do(t, client(), "POST", base+"/setup", setupBody("admin", "un-mot-de-passe-solide"), nil)
	for i := 0; i < maxFailures; i++ {
		do(t, client(), "POST", base+"/auth/login", credentials{Username: "admin", Password: "x"}, nil)
	}
	// « redémarrage »: nouveau serveur sur la même base, compteurs en mémoire perdus.
	set, err := settings.New(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := New(set, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	ts2 := httptest.NewServer(s2.Handler())
	t.Cleanup(ts2.Close)
	r, _ := do(t, client(), "POST", ts2.URL+"/api/v1/auth/login", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil)
	if r.StatusCode != 429 {
		t.Fatalf("compte verrouillé attendu après redémarrage, obtenu %d", r.StatusCode)
	}
}
