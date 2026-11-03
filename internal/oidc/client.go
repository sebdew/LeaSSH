package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	maxBody       = 1 << 20
	discoveryTTL  = 15 * time.Minute
	jwksMinReload = 30 * time.Second
	clockSkew     = 60 * time.Second
)

// Codes de motif d'échec (journal d'audit et interface). Ils ne contiennent jamais de jeton.
const (
	ReasonDiscovery = "discovery_failed"
	ReasonExchange  = "token_exchange_failed"
	ReasonMalformed = "token_malformed"
	ReasonAlg       = "token_alg"
	ReasonSignature = "token_signature"
	ReasonIssuer    = "token_issuer"
	ReasonAudience  = "token_audience"
	ReasonExpired   = "token_expired"
	ReasonNotYet    = "token_not_yet_valid"
	ReasonNonce     = "token_nonce"
	ReasonSubject   = "token_subject"
	ReasonUserinfo  = "userinfo_failed"
)

// Error: échec OIDC avec un motif stable.
type Error struct {
	Reason string
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func fail(reason, format string, a ...any) error {
	return &Error{Reason: reason, Msg: fmt.Sprintf(format, a...)}
}

// ReasonOf extrait le motif d'une erreur (« error » si inconnu).
func ReasonOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return "error"
}

// Config: paramètres d'un fournisseur (le secret est déjà déchiffré, en mémoire seulement).
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// Discovery: document .well-known/openid-configuration (champs utilisés).
type Discovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	EndSessionEndpoint    string   `json:"end_session_endpoint"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
	TokenAuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported       []string `json:"scopes_supported"`
	ClaimsSupported       []string `json:"claims_supported"`
}

// SupportsS256: PKCE S256 annoncé, ou non précisé (cas des fournisseurs qui omettent la liste).
func (d *Discovery) SupportsS256() bool {
	if len(d.CodeChallengeMethods) == 0 {
		return true
	}
	for _, m := range d.CodeChallengeMethods {
		if m == "S256" {
			return true
		}
	}
	return false
}

type cached struct {
	disc    *Discovery
	at      time.Time
	keys    []parsedKey
	keysAt  time.Time
	keysURI string
}

// Client: découverte, échange de code, vérification d'ID token; caches de découverte et de JWKS.
type Client struct {
	HTTP *http.Client
	Now  func() time.Time

	mu    sync.Mutex
	cache map[string]*cached
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second}, Now: time.Now, cache: map[string]*cached{}}
}

// Forget oublie le cache d'un issuer (test de découverte: lecture à jour).
func (c *Client) Forget(issuer string) {
	c.mu.Lock()
	delete(c.cache, issuer)
	c.mu.Unlock()
}

// KeyCount renvoie le nombre de clés de signature publiées (recharge le JWKS).
func (c *Client) KeyCount(ctx context.Context, issuer string) (int, error) {
	d, err := c.Discover(ctx, issuer)
	if err != nil {
		return 0, err
	}
	keys, err := c.loadKeys(ctx, issuer, d)
	return len(keys), err
}

// Reset vide les caches (après un changement de configuration).
func (c *Client) Reset() {
	c.mu.Lock()
	c.cache = map[string]*cached{}
	c.mu.Unlock()
}

func (c *Client) get(ctx context.Context, u string, bearer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.do(req)
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requête vers %s: %w", req.URL.Host, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("lecture de la réponse: %w", err)
	}
	if res.StatusCode/100 != 2 {
		return body, fmt.Errorf("%s a répondu %d", req.URL.Host, res.StatusCode)
	}
	return body, nil
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// Discover lit (ou ressort du cache) le document de découverte et vérifie que l'issuer annoncé est celui configuré.
func (c *Client) Discover(ctx context.Context, issuer string) (*Discovery, error) {
	c.mu.Lock()
	if e := c.cache[issuer]; e != nil && e.disc != nil && c.Now().Sub(e.at) < discoveryTTL {
		d := e.disc
		c.mu.Unlock()
		return d, nil
	}
	c.mu.Unlock()
	if !httpURL(issuer) {
		return nil, fail(ReasonDiscovery, "l'issuer doit être une URL http(s)")
	}
	body, err := c.get(ctx, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", "")
	if err != nil {
		return nil, fail(ReasonDiscovery, "découverte impossible: %v", err)
	}
	var d Discovery
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fail(ReasonDiscovery, "document de découverte invalide")
	}
	switch {
	case d.Issuer != issuer:
		return nil, fail(ReasonDiscovery, "l'issuer annoncé (%q) diffère de celui configuré", d.Issuer)
	case !httpURL(d.AuthorizationEndpoint) || !httpURL(d.TokenEndpoint) || !httpURL(d.JWKSURI):
		return nil, fail(ReasonDiscovery, "authorization_endpoint, token_endpoint ou jwks_uri manquant ou invalide")
	case d.UserinfoEndpoint != "" && !httpURL(d.UserinfoEndpoint), d.EndSessionEndpoint != "" && !httpURL(d.EndSessionEndpoint):
		return nil, fail(ReasonDiscovery, "userinfo_endpoint ou end_session_endpoint invalide")
	}
	c.mu.Lock()
	c.cache[issuer] = &cached{disc: &d, at: c.Now()}
	c.mu.Unlock()
	return &d, nil
}

// loadKeys télécharge le JWKS; les clés inutilisables sont ignorées. Renvoie le nombre de clés de signature.
func (c *Client) loadKeys(ctx context.Context, issuer string, d *Discovery) ([]parsedKey, error) {
	body, err := c.get(ctx, d.JWKSURI, "")
	if err != nil {
		return nil, fail(ReasonDiscovery, "JWKS inaccessible: %v", err)
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fail(ReasonDiscovery, "JWKS invalide")
	}
	var keys []parsedKey
	for _, k := range set.Keys {
		if pk, err := k.parse(); err == nil {
			keys = append(keys, pk)
		}
	}
	if len(keys) == 0 {
		return nil, fail(ReasonDiscovery, "le JWKS ne contient aucune clé de signature exploitable")
	}
	c.mu.Lock()
	if e := c.cache[issuer]; e != nil {
		e.keys, e.keysAt, e.keysURI = keys, c.Now(), d.JWKSURI
	}
	c.mu.Unlock()
	return keys, nil
}

// keysFor renvoie les clés candidates; si le kid est inconnu, le JWKS est rechargé (rotation), au plus une fois par 30 s.
func (c *Client) keysFor(ctx context.Context, issuer string, d *Discovery, kid, alg string) ([]parsedKey, error) {
	pick := func(keys []parsedKey) []parsedKey {
		var out []parsedKey
		for _, k := range keys {
			if k.compatible(alg) && (kid == "" || k.kid == kid) {
				out = append(out, k)
			}
		}
		return out
	}
	c.mu.Lock()
	var have []parsedKey
	var age time.Duration = time.Hour
	if e := c.cache[issuer]; e != nil && e.keys != nil && e.keysURI == d.JWKSURI {
		have, age = e.keys, c.Now().Sub(e.keysAt)
	}
	c.mu.Unlock()
	if m := pick(have); len(m) > 0 {
		return m, nil
	}
	if have != nil && age < jwksMinReload {
		return nil, nil
	}
	keys, err := c.loadKeys(ctx, issuer, d)
	if err != nil {
		return nil, err
	}
	return pick(keys), nil
}

// ---- PKCE, state, nonce ----

func RandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Challenge: code_challenge S256 (RFC 7636).
func Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// AuthURL construit l'URL d'autorisation (Authorization Code + PKCE S256).
func (c *Client) AuthURL(ctx context.Context, cfg Config, redirectURI, state, nonce, verifier string) (string, error) {
	d, err := c.Discover(ctx, cfg.Issuer)
	if err != nil {
		return "", err
	}
	if !d.SupportsS256() {
		return "", fail(ReasonDiscovery, "le fournisseur ne prend pas en charge PKCE S256")
	}
	u, _ := url.Parse(d.AuthorizationEndpoint)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(cfg.Scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", Challenge(verifier))
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Tokens: réponse de l'endpoint de jeton (jamais journalisée).
type Tokens struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

// Exchange échange le code d'autorisation contre les jetons.
func (c *Client) Exchange(ctx context.Context, cfg Config, redirectURI, code, verifier string) (Tokens, error) {
	d, err := c.Discover(ctx, cfg.Issuer)
	if err != nil {
		return Tokens{}, err
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "code_verifier": {verifier}}
	post := false
	if len(d.TokenAuthMethods) > 0 {
		post = true
		for _, m := range d.TokenAuthMethods {
			if m == "client_secret_basic" {
				post = false
			}
		}
	}
	if cfg.ClientSecret == "" || post {
		form.Set("client_id", cfg.ClientID)
		if cfg.ClientSecret != "" {
			form.Set("client_secret", cfg.ClientSecret)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, fail(ReasonExchange, "requête invalide")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if cfg.ClientSecret != "" && !post {
		req.SetBasicAuth(url.QueryEscape(cfg.ClientID), url.QueryEscape(cfg.ClientSecret))
	}
	body, err := c.do(req)
	if err != nil {
		return Tokens{}, fail(ReasonExchange, "échange du code refusé: %v", err)
	}
	var t Tokens
	if err := json.Unmarshal(body, &t); err != nil || t.IDToken == "" {
		return Tokens{}, fail(ReasonExchange, "réponse de l'endpoint de jeton sans id_token")
	}
	return t, nil
}

// Claims: revendications d'un ID token vérifié (ou du userinfo).
type Claims map[string]any

func (c Claims) String(k string) string { s, _ := c[k].(string); return s }

// Lookup lit une revendication par chemin pointé (« realm_access.roles »); une clé littérale avec points est essayée d'abord.
func (c Claims) Lookup(path string) (any, bool) {
	if v, ok := c[path]; ok {
		return v, true
	}
	var cur any = map[string]any(c)
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// Strings renvoie les valeurs textuelles d'une revendication (chaîne seule, liste, ou chaîne séparée par des espaces pour « scope »).
func (c Claims) Strings(path string) []string {
	v, ok := c.Lookup(path)
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func audiences(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func numDate(v any) (time.Time, bool) {
	f, ok := v.(float64)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(int64(f), 0), true
}

func eq(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// VerifyIDToken vérifie signature (JWKS), iss, aud (+azp), exp, nbf, iat et nonce. Toute anomalie est un refus.
func (c *Client) VerifyIDToken(ctx context.Context, cfg Config, raw, nonce string) (Claims, error) {
	hdr, payload, sig, signed, err := splitJWT(raw)
	if err != nil {
		return nil, fail(ReasonMalformed, "%v", err)
	}
	if !allowedAlgs[hdr.Alg] {
		return nil, fail(ReasonAlg, "algorithme de signature %q refusé", hdr.Alg)
	}
	d, err := c.Discover(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	keys, err := c.keysFor(ctx, cfg.Issuer, d, hdr.Kid, hdr.Alg)
	if err != nil {
		return nil, err
	}
	okSig := false
	for _, k := range keys {
		if k.verify(hdr.Alg, signed, sig) {
			okSig = true
			break
		}
	}
	if !okSig {
		return nil, fail(ReasonSignature, "signature de l'ID token invalide ou clé inconnue")
	}
	var cl Claims
	if err := json.Unmarshal(payload, &cl); err != nil {
		return nil, fail(ReasonMalformed, "corps de l'ID token invalide")
	}
	now := c.Now()
	if cl.String("iss") != cfg.Issuer {
		return nil, fail(ReasonIssuer, "issuer %q inattendu", cl.String("iss"))
	}
	aud := audiences(cl["aud"])
	found := false
	for _, a := range aud {
		if a == cfg.ClientID {
			found = true
		}
	}
	if !found {
		return nil, fail(ReasonAudience, "l'ID token n'est pas destiné à ce client")
	}
	if len(aud) > 1 && cl.String("azp") != cfg.ClientID {
		return nil, fail(ReasonAudience, "azp absent ou différent du client pour un jeton à plusieurs audiences")
	}
	if azp := cl.String("azp"); azp != "" && azp != cfg.ClientID {
		return nil, fail(ReasonAudience, "azp différent du client")
	}
	exp, ok := numDate(cl["exp"])
	if !ok {
		return nil, fail(ReasonMalformed, "exp manquant")
	}
	if !now.Before(exp.Add(clockSkew)) {
		return nil, fail(ReasonExpired, "ID token expiré")
	}
	if nbf, ok := numDate(cl["nbf"]); ok && now.Add(clockSkew).Before(nbf) {
		return nil, fail(ReasonNotYet, "ID token pas encore valide (nbf)")
	}
	if iat, ok := numDate(cl["iat"]); ok && now.Add(clockSkew).Before(iat) {
		return nil, fail(ReasonNotYet, "ID token émis dans le futur (iat)")
	}
	if nonce == "" || !eq(cl.String("nonce"), nonce) {
		return nil, fail(ReasonNonce, "nonce invalide")
	}
	if cl.String("sub") == "" {
		return nil, fail(ReasonSubject, "sub manquant")
	}
	return cl, nil
}

// UserInfo interroge l'endpoint userinfo avec le jeton d'accès (lecture des revendications absentes de l'ID token).
func (c *Client) UserInfo(ctx context.Context, cfg Config, accessToken string) (Claims, error) {
	d, err := c.Discover(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	if d.UserinfoEndpoint == "" || accessToken == "" {
		return nil, fail(ReasonUserinfo, "userinfo indisponible")
	}
	body, err := c.get(ctx, d.UserinfoEndpoint, accessToken)
	if err != nil {
		return nil, fail(ReasonUserinfo, "userinfo: %v", err)
	}
	var cl Claims
	if err := json.Unmarshal(body, &cl); err != nil {
		return nil, fail(ReasonUserinfo, "réponse userinfo invalide")
	}
	return cl, nil
}

// EndSessionURL construit l'URL de déconnexion du fournisseur (RP-Initiated Logout); vide si non pris en charge.
func (c *Client) EndSessionURL(ctx context.Context, cfg Config, postLogoutRedirect string) string {
	d, err := c.Discover(ctx, cfg.Issuer)
	if err != nil || d.EndSessionEndpoint == "" {
		return ""
	}
	u, _ := url.Parse(d.EndSessionEndpoint)
	q := u.Query()
	q.Set("client_id", cfg.ClientID)
	if postLogoutRedirect != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirect)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
