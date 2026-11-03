// Package oidctest: fournisseur OpenID Connect factice, en mémoire, pour les tests (jamais de vrais identifiants).
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Identity: la personne « connectée » chez le fournisseur factice.
type Identity struct {
	Sub      string
	Username string
	Name     string
	Email    string
	Groups   []string
}

type grant struct {
	nonce, challenge, redirect string
	id                         Identity
	used                       bool
}

type Server struct {
	t            *testing.T
	ts           *httptest.Server
	URL          string
	ClientID     string
	ClientSecret string

	mu     sync.Mutex
	Key    *rsa.PrivateKey
	Kid    string
	Now    func() time.Time
	Who    Identity
	Groups string // nom de la revendication de groupes dans l'ID token ("" = pas de groupes); "userinfo" = seulement dans userinfo

	// Mutate modifie la charge de l'ID token avant signature (cas d'erreur).
	Mutate func(p map[string]any)
	// OverrideNonce remplace le nonce signé.
	OverrideNonce string
	// SignKey: signer avec une autre clé que celle publiée.
	SignKey *rsa.PrivateKey
	// NoEndSession retire end_session_endpoint de la découverte.
	NoEndSession bool
	// IssuerOverride: issuer annoncé dans la découverte.
	IssuerOverride string

	grants map[string]*grant
}

func New(t *testing.T) *Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{t: t, ClientID: "leash", ClientSecret: "s3cret-factice", Key: key, Kid: "k1", Now: time.Now, grants: map[string]*grant{},
		Who: Identity{Sub: "sub-1", Username: "alice", Name: "Alice Martin", Email: "alice@example.test"}, Groups: "groups"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("/jwks", s.jwks)
	mux.HandleFunc("/authorize", s.authorize)
	mux.HandleFunc("/token", s.token)
	mux.HandleFunc("/userinfo", s.userinfo)
	s.ts = httptest.NewServer(mux)
	s.URL = s.ts.URL
	t.Cleanup(s.ts.Close)
	return s
}

func (s *Server) issuer() string {
	if s.IssuerOverride != "" {
		return s.IssuerOverride
	}
	return s.URL
}

func (s *Server) discovery(w http.ResponseWriter, _ *http.Request) {
	d := map[string]any{"issuer": s.issuer(), "authorization_endpoint": s.URL + "/authorize", "token_endpoint": s.URL + "/token",
		"jwks_uri": s.URL + "/jwks", "userinfo_endpoint": s.URL + "/userinfo", "code_challenge_methods_supported": []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"}}
	if !s.NoEndSession {
		d["end_session_endpoint"] = s.URL + "/logout"
	}
	_ = json.NewEncoder(w).Encode(d)
}

func (s *Server) jwks(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pub := s.Key.PublicKey
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": s.Kid,
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})}}})
}

// Rotate publie une nouvelle clé (l'ancienne disparaît du JWKS).
func (s *Server) Rotate(kid string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		s.t.Fatal(err)
	}
	s.mu.Lock()
	s.Key, s.Kid = key, kid
	s.mu.Unlock()
}

// authorize: connecte immédiatement l'identité courante et redirige vers redirect_uri avec code et state.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != s.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("nonce") == "" {
		http.Error(w, "requête d'autorisation invalide", 400)
		return
	}
	code, _ := randomString()
	s.mu.Lock()
	s.grants[code] = &grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), id: s.Who}
	s.mu.Unlock()
	u, _ := url.Parse(q.Get("redirect_uri"))
	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func randomString() (string, error) {
	b := make([]byte, 24)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}

func (s *Server) claims(g *grant) map[string]any {
	now := s.Now()
	p := map[string]any{"iss": s.URL, "sub": g.id.Sub, "aud": s.ClientID, "azp": s.ClientID, "exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(),
		"nonce": g.nonce, "preferred_username": g.id.Username, "name": g.id.Name, "email": g.id.Email}
	if s.OverrideNonce != "" {
		p["nonce"] = s.OverrideNonce
	}
	if s.Groups != "" && s.Groups != "userinfo" {
		setPath(p, s.Groups, g.id.Groups)
	}
	if s.Mutate != nil {
		s.Mutate(p)
	}
	return p
}

func setPath(p map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for _, k := range parts[:len(parts)-1] {
		m, _ := p[k].(map[string]any)
		if m == nil {
			m = map[string]any{}
			p[k] = m
		}
		p = m
	}
	p[parts[len(parts)-1]] = v
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, secret, basic := r.BasicAuth()
	if !basic {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	} else {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	}
	if id != s.ClientID || secret != s.ClientSecret {
		w.WriteHeader(401)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
		return
	}
	s.mu.Lock()
	g := s.grants[r.PostForm.Get("code")]
	if g != nil && g.used {
		g = nil
	}
	if g != nil {
		g.used = true
	}
	key, kid := s.Key, s.Kid
	if s.SignKey != nil {
		key = s.SignKey
	}
	var p map[string]any
	if g != nil {
		p = s.claims(g)
	}
	s.mu.Unlock()
	h := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if g == nil || r.PostForm.Get("grant_type") != "authorization_code" || g.redirect != r.PostForm.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(h[:]) != g.challenge {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"token_type": "Bearer", "access_token": "at-" + g.id.Sub, "id_token": Sign(s.t, p, key, kid)})
}

func (s *Server) userinfo(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer at-")
	s.mu.Lock()
	who := s.Who
	s.mu.Unlock()
	if sub != who.Sub {
		w.WriteHeader(401)
		return
	}
	out := map[string]any{"sub": who.Sub, "preferred_username": who.Username}
	if s.Groups == "userinfo" {
		out["groups"] = who.Groups
	}
	_ = json.NewEncoder(w).Encode(out)
}

// Sign signe une charge JWT en RS256.
func Sign(t *testing.T, payload map[string]any, key *rsa.PrivateKey, kid string) string {
	t.Helper()
	return SignRaw(t, map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}, payload, func(in []byte) []byte {
		h := sha256.Sum256(in)
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
		if err != nil {
			t.Fatal(err)
		}
		return sig
	})
}

// SignRaw: en-tête libre (alg « none », HS256…) pour les jetons forgés.
func SignRaw(t *testing.T, header, payload map[string]any, sign func([]byte) []byte) string {
	t.Helper()
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	in := enc(header) + "." + enc(payload)
	var sig []byte
	if sign != nil {
		sig = sign([]byte(in))
	}
	return in + "." + base64.RawURLEncoding.EncodeToString(sig)
}
