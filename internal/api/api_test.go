package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leash-dev/leash/internal/config"
	"github.com/leash-dev/leash/internal/store"
)

func newTest(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{SessionTTL: time.Hour, MasterKey: make([]byte, 32)}
	s, err := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, st
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
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
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
	if r, _ := do(t, c, "POST", base+"/setup", credentials{Username: "admin", Password: "court"}, nil); r.StatusCode != 400 {
		t.Fatalf("mot de passe faible accepté: %d", r.StatusCode)
	}
	r, out := do(t, c, "POST", base+"/setup", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil)
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
	if r, _ := do(t, client(), "POST", base+"/setup", credentials{Username: "evil", Password: "un-mot-de-passe-solide"}, nil); r.StatusCode != 409 {
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
	do(t, client(), "POST", base+"/setup", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil)
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
	if !strings.Contains(r.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal("CSP absente")
	}
}

func TestClientIPTrustedProxy(t *testing.T) {
	s := &Server{}
	s.proxies = append(s.proxies, mustPrefix("10.0.0.0/8"))
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
