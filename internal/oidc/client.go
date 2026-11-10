// Package oidc: client OpenID Connect (Authorization Code + PKCE) et vérification stricte des ID tokens.
// La découverte, le JWKS, les signatures et PKCE sont délégués à github.com/coreos/go-oidc/v3 et golang.org/x/oauth2.
package oidc

import (
	"context"
	"crypto/rand"
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
	"sync/atomic"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	maxBody       = 1 << 20
	discoveryTTL  = 15 * time.Minute
	jwksMinReload = 30 * time.Second
	clockSkew     = 60 * time.Second
)

// Algorithmes de signature acceptés. « none » et HMAC sont volontairement exclus
// (un secret partagé ne doit jamais servir à vérifier un jeton signé par le fournisseur).
var allowedAlgs = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512", "EdDSA"}

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
	disc *Discovery
	prov *gooidc.Provider
	at   time.Time
}

// Client: découverte, échange de code, vérification d'ID token; cache de découverte.
// La découverte, le JWKS, la vérification des jetons et PKCE sont délégués à go-oidc et x/oauth2.
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

// Reset vide les caches (après un changement de configuration).
func (c *Client) Reset() {
	c.mu.Lock()
	c.cache = map[string]*cached{}
	c.mu.Unlock()
}

// withHTTP place le client HTTP configuré dans le contexte, comme go-oidc et x/oauth2 l'attendent.
func (c *Client) withHTTP(ctx context.Context) context.Context {
	return gooidc.ClientContext(ctx, c.HTTP)
}

// jwksThrottle limite le rechargement du JWKS: go-oidc le recharge à chaque jeton dont le kid est inconnu,
// ce qui permettrait de faire appeler le fournisseur en boucle. Un seul rechargement par jwksMinReload.
type jwksThrottle struct {
	base http.RoundTripper
	now  func() time.Time

	mu   sync.Mutex
	url  string
	last time.Time
}

func (t *jwksThrottle) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	if t.url != "" && req.Method == http.MethodGet && req.URL.String() == t.url {
		if !t.last.IsZero() && t.now().Sub(t.last) < jwksMinReload {
			t.mu.Unlock()
			return nil, errors.New("rechargement du JWKS trop fréquent")
		}
		t.last = t.now()
	}
	t.mu.Unlock()
	return t.base.RoundTrip(req)
}

// statusRecorder retient le dernier code HTTP reçu: go-oidc cite le corps de la réponse dans ses erreurs,
// on ne garde donc que le code pour construire un message qui ne relaie rien du serveur distant.
type statusRecorder struct {
	base   http.RoundTripper
	status atomic.Int64
}

func (t *statusRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.base.RoundTrip(req)
	if res != nil {
		t.status.Store(int64(res.StatusCode))
	}
	return res, err
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func (c *Client) provider(ctx context.Context, issuer string) (*cached, error) {
	c.mu.Lock()
	if e := c.cache[issuer]; e != nil && c.Now().Sub(e.at) < discoveryTTL {
		c.mu.Unlock()
		return e, nil
	}
	c.mu.Unlock()
	if !httpURL(issuer) {
		return nil, fail(ReasonDiscovery, "l'issuer doit être une URL http(s)")
	}
	base := c.HTTP.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	thr := &jwksThrottle{base: base, now: c.Now}
	hc := *c.HTTP
	rec := &statusRecorder{base: thr}
	hc.Transport = rec
	// NewProvider lit le document de découverte et vérifie que l'issuer annoncé est celui configuré.
	prov, err := gooidc.NewProvider(gooidc.ClientContext(ctx, &hc), issuer)
	if err != nil {
		if st := rec.status.Load(); st >= 300 {
			return nil, fail(ReasonDiscovery, "découverte impossible: le serveur a répondu HTTP %d", st)
		}
		return nil, fail(ReasonDiscovery, "découverte impossible: serveur injoignable ou document invalide")
	}
	var d Discovery
	if err := prov.Claims(&d); err != nil {
		return nil, fail(ReasonDiscovery, "document de découverte invalide")
	}
	switch {
	case !httpURL(d.AuthorizationEndpoint) || !httpURL(d.TokenEndpoint) || !httpURL(d.JWKSURI):
		return nil, fail(ReasonDiscovery, "authorization_endpoint, token_endpoint ou jwks_uri manquant ou invalide")
	case d.UserinfoEndpoint != "" && !httpURL(d.UserinfoEndpoint), d.EndSessionEndpoint != "" && !httpURL(d.EndSessionEndpoint):
		return nil, fail(ReasonDiscovery, "userinfo_endpoint ou end_session_endpoint invalide")
	}
	thr.mu.Lock()
	thr.url = d.JWKSURI
	thr.mu.Unlock()
	e := &cached{disc: &d, prov: prov, at: c.Now()}
	c.mu.Lock()
	c.cache[issuer] = e
	c.mu.Unlock()
	return e, nil
}

// Discover lit (ou ressort du cache) le document de découverte.
func (c *Client) Discover(ctx context.Context, issuer string) (*Discovery, error) {
	e, err := c.provider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return e.disc, nil
}

// KeyCount renvoie le nombre de clés de signature publiées (lecture à jour du JWKS, pour le test de configuration).
func (c *Client) KeyCount(ctx context.Context, issuer string) (int, error) {
	d, err := c.Discover(ctx, issuer)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.JWKSURI, nil)
	if err != nil {
		return 0, fail(ReasonDiscovery, "JWKS inaccessible")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fail(ReasonDiscovery, "JWKS inaccessible: %v", err)
	}
	defer res.Body.Close()
	var set struct {
		Keys []struct {
			Use string `json:"use"`
		} `json:"keys"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, maxBody)).Decode(&set) != nil {
		return 0, fail(ReasonDiscovery, "JWKS invalide")
	}
	n := 0
	for _, k := range set.Keys {
		if k.Use == "" || k.Use == "sig" {
			n++
		}
	}
	if n == 0 {
		return 0, fail(ReasonDiscovery, "le JWKS ne contient aucune clé de signature")
	}
	return n, nil
}

// ---- state, nonce, verifier PKCE ----

func RandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (c *Client) oauth(e *cached, cfg Config, redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: redirectURI, Scopes: cfg.Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: e.disc.AuthorizationEndpoint, TokenURL: e.disc.TokenEndpoint, AuthStyle: oauth2.AuthStyleAutoDetect},
	}
}

// AuthURL construit l'URL d'autorisation (Authorization Code + PKCE S256).
func (c *Client) AuthURL(ctx context.Context, cfg Config, redirectURI, state, nonce, verifier string) (string, error) {
	e, err := c.provider(ctx, cfg.Issuer)
	if err != nil {
		return "", err
	}
	if !e.disc.SupportsS256() {
		return "", fail(ReasonDiscovery, "le fournisseur ne prend pas en charge PKCE S256")
	}
	return c.oauth(e, cfg, redirectURI).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), gooidc.Nonce(nonce)), nil
}

// Tokens: réponse de l'endpoint de jeton (jamais journalisée).
type Tokens struct {
	IDToken     string
	AccessToken string
	TokenType   string
}

// Exchange échange le code d'autorisation contre les jetons (avec le verifier PKCE).
func (c *Client) Exchange(ctx context.Context, cfg Config, redirectURI, code, verifier string) (Tokens, error) {
	e, err := c.provider(ctx, cfg.Issuer)
	if err != nil {
		return Tokens{}, err
	}
	tok, err := c.oauth(e, cfg, redirectURI).Exchange(c.withHTTP(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Tokens{}, fail(ReasonExchange, "échange du code refusé: %v", err)
	}
	id, _ := tok.Extra("id_token").(string)
	if id == "" {
		return Tokens{}, fail(ReasonExchange, "réponse de l'endpoint de jeton sans id_token")
	}
	return Tokens{IDToken: id, AccessToken: tok.AccessToken, TokenType: tok.TokenType}, nil
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

func numDate(v any) (time.Time, bool) {
	f, ok := v.(float64)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(int64(f), 0), true
}

func eq(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// classify rattache une erreur de go-oidc à un motif stable. go-oidc ne type que l'expiration:
// les autres cas sont reconnus à leur message, et chaque correspondance est couverte par un test.
func classify(err error) string {
	var exp *gooidc.TokenExpiredError
	msg := err.Error()
	switch {
	case errors.As(err, &exp):
		return ReasonExpired
	case strings.Contains(msg, "unexpected signature algorithm"):
		return ReasonAlg
	case strings.Contains(msg, "malformed jwt"), strings.Contains(msg, "not signed"):
		return ReasonMalformed
	case strings.Contains(msg, "different provider"):
		return ReasonIssuer
	case strings.Contains(msg, "expected audience"):
		return ReasonAudience
	}
	return ReasonSignature
}

// VerifyIDToken délègue signature, algorithmes, iss et aud à go-oidc, puis contrôle sur les revendications
// vérifiées: azp, exp, nbf, iat (avec 60 s de tolérance d'horloge), nonce et sub. Toute anomalie est un refus.
func (c *Client) VerifyIDToken(ctx context.Context, cfg Config, raw, nonce string) (Claims, error) {
	e, err := c.provider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	// L'expiration est contrôlée plus bas, avec le même décalage toléré que nbf et iat.
	verifier := e.prov.Verifier(&gooidc.Config{ClientID: cfg.ClientID, SupportedSigningAlgs: allowedAlgs, SkipExpiryCheck: true})
	idt, err := verifier.Verify(c.withHTTP(ctx), raw)
	if err != nil {
		return nil, fail(classify(err), "ID token refusé: %v", err)
	}
	var cl Claims
	if err := idt.Claims(&cl); err != nil {
		return nil, fail(ReasonMalformed, "corps de l'ID token invalide")
	}
	now := c.Now()
	if len(idt.Audience) > 1 && cl.String("azp") != cfg.ClientID {
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
	if nonce == "" || !eq(idt.Nonce, nonce) {
		return nil, fail(ReasonNonce, "nonce invalide")
	}
	if idt.Subject == "" {
		return nil, fail(ReasonSubject, "sub manquant")
	}
	return cl, nil
}

// UserInfo lit les revendications absentes de l'ID token (userinfo, avec le jeton d'accès).
func (c *Client) UserInfo(ctx context.Context, cfg Config, accessToken string) (Claims, error) {
	e, err := c.provider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	if e.disc.UserinfoEndpoint == "" || accessToken == "" {
		return nil, fail(ReasonUserinfo, "userinfo indisponible")
	}
	ui, err := e.prov.UserInfo(c.withHTTP(ctx), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken, TokenType: "Bearer"}))
	if err != nil {
		return nil, fail(ReasonUserinfo, "userinfo: %v", err)
	}
	var cl Claims
	if err := ui.Claims(&cl); err != nil {
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
