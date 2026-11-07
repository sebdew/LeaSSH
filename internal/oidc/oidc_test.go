package oidc_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/oidc"
	"github.com/leassh-dev/leassh/internal/oidc/oidctest"
)

func payload(idp *oidctest.Server, over map[string]any) map[string]any {
	p := map[string]any{"iss": idp.URL, "sub": "s1", "aud": idp.ClientID, "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "nonce": "n1"}
	for k, v := range over {
		p[k] = v
	}
	return p
}

func cfgOf(idp *oidctest.Server) oidc.Config {
	return oidc.Config{Issuer: idp.URL, ClientID: idp.ClientID, ClientSecret: idp.ClientSecret, Scopes: []string{"openid"}}
}

func reason(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || oidc.ReasonOf(err) != want {
		t.Fatalf("erreur %v (motif %q), attendu %q", err, oidc.ReasonOf(err), want)
	}
}

func TestVerifyAcceptsValidAndRejectsForged(t *testing.T) {
	idp := oidctest.New(t)
	c, ctx, cfg := oidc.New(), context.Background(), cfgOf(idp)

	if cl, err := c.VerifyIDToken(ctx, cfg, oidctest.Sign(t, payload(idp, nil), idp.Key, idp.Kid), "n1"); err != nil || cl.String("sub") != "s1" {
		t.Fatalf("jeton valide: %v", err)
	}
	// alg=none et HMAC (confusion d'algorithme avec le secret client) sont refusés
	none := oidctest.SignRaw(t, map[string]any{"alg": "none", "typ": "JWT"}, payload(idp, nil), nil)
	_, err := c.VerifyIDToken(ctx, cfg, none, "n1")
	reason(t, err, oidc.ReasonAlg)
	hs := oidctest.SignRaw(t, map[string]any{"alg": "HS256", "kid": idp.Kid}, payload(idp, nil), func(in []byte) []byte {
		m := hmac.New(sha256.New, []byte(idp.ClientSecret))
		m.Write(in)
		return m.Sum(nil)
	})
	_, err = c.VerifyIDToken(ctx, cfg, hs, "n1")
	reason(t, err, oidc.ReasonAlg)
	// signature altérée
	good := oidctest.Sign(t, payload(idp, nil), idp.Key, idp.Kid)
	parts := strings.Split(good, ".")
	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"`+idp.URL+`","sub":"admin","aud":"leassh","exp":9999999999,"nonce":"n1"}`)) + "." + parts[2]
	_, err = c.VerifyIDToken(ctx, cfg, forged, "n1")
	reason(t, err, oidc.ReasonSignature)
	for _, bad := range []string{"", "a.b", "a.b.c", good + ".x"} {
		if _, err := c.VerifyIDToken(ctx, cfg, bad, "n1"); err == nil {
			t.Fatalf("jeton mal formé accepté: %q", bad)
		}
	}
	// nonce vide côté attendu: jamais accepté
	_, err = c.VerifyIDToken(ctx, cfg, oidctest.Sign(t, payload(idp, map[string]any{"nonce": ""}), idp.Key, idp.Kid), "")
	reason(t, err, oidc.ReasonNonce)
}

func TestVerifyClockSkew(t *testing.T) {
	idp := oidctest.New(t)
	c, ctx, cfg := oidc.New(), context.Background(), cfgOf(idp)
	// expiré depuis 30 s: toléré (décalage d'horloge de 60 s); depuis 2 min: refusé
	tok := func(off time.Duration) string {
		return oidctest.Sign(t, payload(idp, map[string]any{"exp": time.Now().Add(off).Unix()}), idp.Key, idp.Kid)
	}
	if _, err := c.VerifyIDToken(ctx, cfg, tok(-30*time.Second), "n1"); err != nil {
		t.Fatalf("tolérance: %v", err)
	}
	_, err := c.VerifyIDToken(ctx, cfg, tok(-2*time.Minute), "n1")
	reason(t, err, oidc.ReasonExpired)
}

func TestJWKSRotation(t *testing.T) {
	idp := oidctest.New(t)
	c, ctx, cfg := oidc.New(), context.Background(), cfgOf(idp)
	now := time.Now()
	c.Now = func() time.Time { return now }
	if _, err := c.VerifyIDToken(ctx, cfg, oidctest.Sign(t, payload(idp, nil), idp.Key, idp.Kid), "n1"); err != nil {
		t.Fatal(err)
	}
	old, oldKid := idp.Key, idp.Kid
	idp.Rotate("k2")
	tok := oidctest.Sign(t, payload(idp, nil), idp.Key, idp.Kid)
	// rechargement du JWKS limité: dans les 30 s, kid inconnu = refus
	_, err := c.VerifyIDToken(ctx, cfg, tok, "n1")
	reason(t, err, oidc.ReasonSignature)
	now = now.Add(time.Minute)
	if _, err := c.VerifyIDToken(ctx, cfg, tok, "n1"); err != nil {
		t.Fatalf("après rotation: %v", err)
	}
	// l'ancienne clé n'est plus publiée: un jeton signé avec elle est refusé
	_, err = c.VerifyIDToken(ctx, cfg, oidctest.Sign(t, payload(idp, nil), old, oldKid), "n1")
	reason(t, err, oidc.ReasonSignature)
}

// Clés EC et Ed25519 publiées dans le JWKS.
func TestVerifyECAndEdDSA(t *testing.T) {
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ed, edPriv, _ := ed25519.GenerateKey(rand.Reader)
	var issuer string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/a", "token_endpoint": issuer + "/t", "jwks_uri": issuer + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		pad := func(b []byte) []byte { return append(make([]byte, 32-len(b)), b...) }
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{
			{"kty": "EC", "crv": "P-256", "kid": "ec", "use": "sig", "x": base64.RawURLEncoding.EncodeToString(pad(ec.X.Bytes())), "y": base64.RawURLEncoding.EncodeToString(pad(ec.Y.Bytes()))},
			{"kty": "OKP", "crv": "Ed25519", "kid": "ed", "x": base64.RawURLEncoding.EncodeToString(ed)},
			{"kty": "oct", "kid": "hmac", "k": "AAAA"}, // ignorée
		}})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	issuer = ts.URL
	cfg := oidc.Config{Issuer: issuer, ClientID: "leassh"}
	c := oidc.New()
	pl := map[string]any{"iss": issuer, "sub": "s", "aud": "leassh", "exp": time.Now().Add(time.Minute).Unix(), "nonce": "n"}

	es := oidctest.SignRaw(t, map[string]any{"alg": "ES256", "kid": "ec"}, pl, func(in []byte) []byte {
		h := sha256.Sum256(in)
		r, s, _ := ecdsa.Sign(rand.Reader, ec, h[:])
		sig := make([]byte, 64)
		r.FillBytes(sig[:32])
		s.FillBytes(sig[32:])
		return sig
	})
	if _, err := c.VerifyIDToken(context.Background(), cfg, es, "n"); err != nil {
		t.Fatalf("ES256: %v", err)
	}
	eddsa := oidctest.SignRaw(t, map[string]any{"alg": "EdDSA", "kid": "ed"}, pl, func(in []byte) []byte { return ed25519.Sign(edPriv, in) })
	if _, err := c.VerifyIDToken(context.Background(), cfg, eddsa, "n"); err != nil {
		t.Fatalf("EdDSA: %v", err)
	}
	// une clé EC ne vérifie pas un jeton déclaré RS256 (confusion de type de clé)
	mixed := oidctest.SignRaw(t, map[string]any{"alg": "RS256", "kid": "ec"}, pl, func(in []byte) []byte { return []byte("x") })
	_, err := c.VerifyIDToken(context.Background(), cfg, mixed, "n")
	reason(t, err, oidc.ReasonSignature)
	_ = crypto.SHA256
}

func TestDiscoveryValidation(t *testing.T) {
	idp := oidctest.New(t)
	c := oidc.New()
	idp.IssuerOverride = "https://autre.example.test"
	_, err := c.Discover(context.Background(), idp.URL)
	reason(t, err, oidc.ReasonDiscovery)
	if _, err := c.Discover(context.Background(), "ftp://x"); err == nil {
		t.Fatal("schéma ftp accepté")
	}
	if _, err := c.Discover(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Fatal("serveur injoignable accepté")
	}
}

func TestExchangeRequiresPKCEAndSecret(t *testing.T) {
	idp := oidctest.New(t)
	c, ctx, cfg := oidc.New(), context.Background(), cfgOf(idp)
	verifier, _ := oidc.RandomString(48)
	loc, err := c.AuthURL(ctx, cfg, "http://leassh.test/cb", "st", "no", verifier)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(loc)
	cb := res.Header.Get("Location")
	code := cb[strings.Index(cb, "code=")+5 : strings.Index(cb, "&state")]
	// mauvais verifier → refus; mauvais secret → refus
	if _, err := c.Exchange(ctx, cfg, "http://leassh.test/cb", code, "autre-verifier"); err == nil {
		t.Fatal("PKCE ignoré")
	}
	res, _ = (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(loc)
	cb = res.Header.Get("Location")
	code = cb[strings.Index(cb, "code=")+5 : strings.Index(cb, "&state")]
	bad := cfg
	bad.ClientSecret = "faux"
	_, err = c.Exchange(ctx, bad, "http://leassh.test/cb", code, verifier)
	reason(t, err, oidc.ReasonExchange)
	if tk, err := c.Exchange(ctx, cfg, "http://leassh.test/cb", code, verifier); err != nil || tk.IDToken == "" {
		t.Fatalf("échange: %v", err)
	}
	if _, err := c.Exchange(ctx, cfg, "http://leassh.test/cb", code, verifier); err == nil {
		t.Fatal("code rejoué accepté")
	}
}

func TestMapRole(t *testing.T) {
	m := oidc.RoleMap{Admin: []string{"adm"}, Approver: []string{"ops"}, Viewer: []string{"ro"}}
	cl := oidc.Claims{"groups": []any{"ro", "ops"}, "realm_access": map[string]any{"roles": []any{"adm"}}, "single": "ro", "a.b": "ops"}
	tests := []struct {
		claim, def, want string
		ok               bool
	}{
		{"groups", "", "approver", true},
		{"realm_access.roles", "", "admin", true},
		{"single", "", "viewer", true},
		{"a.b", "", "approver", true}, // clé littérale avec point
		{"absent", "viewer", "viewer", true},
		{"absent", "", "", false},
		{"", "viewer", "viewer", true},
		{"", "", "", false},
	}
	for _, tc := range tests {
		got, ok := oidc.MapRole(cl, tc.claim, m, tc.def)
		if got != tc.want || ok != tc.ok {
			t.Errorf("claim=%q def=%q: %q,%v (attendu %q,%v)", tc.claim, tc.def, got, ok, tc.want, tc.ok)
		}
	}
	// aucune valeur de mapping vide ne doit matcher une revendication vide
	if r, ok := oidc.MapRole(oidc.Claims{"groups": []any{""}}, "groups", oidc.RoleMap{Admin: []string{""}}, ""); ok {
		t.Fatalf("valeur vide mappée: %q", r)
	}
}

// ---- filet de non-régression exigé par le ticket #2 (jetons forgés ou invalides) ----

func TestVerifyRejectsAlgNone(t *testing.T) {
	idp := oidctest.New(t)
	none := oidctest.SignRaw(t, map[string]any{"alg": "none", "typ": "JWT", "kid": idp.Kid}, payload(idp, nil), nil)
	_, err := oidc.New().VerifyIDToken(context.Background(), cfgOf(idp), none, "n1")
	reason(t, err, oidc.ReasonAlg)
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	idp := oidctest.New(t)
	tok := oidctest.Sign(t, payload(idp, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}), idp.Key, idp.Kid)
	_, err := oidc.New().VerifyIDToken(context.Background(), cfgOf(idp), tok, "n1")
	reason(t, err, oidc.ReasonExpired)
}

func TestVerifyRejectsWrongNonce(t *testing.T) {
	idp := oidctest.New(t)
	tok := oidctest.Sign(t, payload(idp, nil), idp.Key, idp.Kid)
	_, err := oidc.New().VerifyIDToken(context.Background(), cfgOf(idp), tok, "autre-nonce")
	reason(t, err, oidc.ReasonNonce)
}

func TestVerifyRejectsWrongIssuerAudienceAndAzp(t *testing.T) {
	idp := oidctest.New(t)
	c, ctx, cfg := oidc.New(), context.Background(), cfgOf(idp)
	sign := func(over map[string]any) string { return oidctest.Sign(t, payload(idp, over), idp.Key, idp.Kid) }
	_, err := c.VerifyIDToken(ctx, cfg, sign(map[string]any{"iss": "https://autre.example.test"}), "n1")
	reason(t, err, oidc.ReasonIssuer)
	_, err = c.VerifyIDToken(ctx, cfg, sign(map[string]any{"aud": "un-autre-client"}), "n1")
	reason(t, err, oidc.ReasonAudience)
	_, err = c.VerifyIDToken(ctx, cfg, sign(map[string]any{"aud": []string{idp.ClientID, "autre"}}), "n1")
	reason(t, err, oidc.ReasonAudience) // plusieurs audiences sans azp
	_, err = c.VerifyIDToken(ctx, cfg, sign(map[string]any{"azp": "autre"}), "n1")
	reason(t, err, oidc.ReasonAudience)
}

func TestVerifyRejectsNotYetValidAndMissingSubject(t *testing.T) {
	idp := oidctest.New(t)
	c, ctx, cfg := oidc.New(), context.Background(), cfgOf(idp)
	sign := func(over map[string]any) string { return oidctest.Sign(t, payload(idp, over), idp.Key, idp.Kid) }
	_, err := c.VerifyIDToken(ctx, cfg, sign(map[string]any{"nbf": time.Now().Add(time.Hour).Unix()}), "n1")
	reason(t, err, oidc.ReasonNotYet)
	_, err = c.VerifyIDToken(ctx, cfg, sign(map[string]any{"iat": time.Now().Add(time.Hour).Unix()}), "n1")
	reason(t, err, oidc.ReasonNotYet)
	_, err = c.VerifyIDToken(ctx, cfg, sign(map[string]any{"sub": ""}), "n1")
	reason(t, err, oidc.ReasonSubject)
}

// Principe 2 de la constitution: aucune vérification de signature écrite à la main.
func TestNoHandWrittenJWTVerification(t *testing.T) {
	if _, err := os.Stat("jwt.go"); err == nil {
		t.Fatal("internal/oidc/jwt.go existe encore: la vérification des jetons doit venir de go-oidc")
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, _ := os.ReadFile(e.Name())
		for _, banned := range []string{"rsa.VerifyPKCS1v15", "rsa.VerifyPSS", "ecdsa.Verify", "ed25519.Verify"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s appelle %s: vérification de signature maison", e.Name(), banned)
			}
		}
	}
}
