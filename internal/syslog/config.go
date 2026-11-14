package syslog

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/sebdew/LeaSSH/internal/store"
)

// Config est la vue de la configuration exposée par l'API (jamais la clé privée TLS).
type Config struct {
	Enabled          bool     `json:"enabled"`
	Protocol         string   `json:"protocol"`        // udp | tcp | tls
	Host             string   `json:"host"`            // serveur syslog
	Port             int      `json:"port"`            // 514 par défaut (6514 en TLS)
	MessageFormat    string   `json:"message_format"`  // text | json | cef
	Framing          string   `json:"framing"`         // lf | octet (TCP/TLS)
	Facility         string   `json:"facility"`        // kern … local7
	AppName          string   `json:"app_name"`        // APP-NAME RFC 5424
	Hostname         string   `json:"hostname"`        // vide = nom de la machine
	MinSeverity      string   `json:"min_severity"`    // emerg … debug: les événements moins graves sont ignorés
	Types            []string `json:"types"`           // execution | proposal | audit
	TLSCAPEM         string   `json:"tls_ca_pem"`      // vide = autorités du système
	TLSServerName    string   `json:"tls_server_name"` // vide = host
	TLSSkipVerify    bool     `json:"tls_skip_verify"`
	TLSClientCertPEM string   `json:"tls_client_cert_pem"`
	HasClientKey     bool     `json:"has_tls_client_key"`
}

// Input: configuration reçue de l'API. La clé privée cliente est en écriture seule.
type Input struct {
	Config
	TLSClientKeyPEM string `json:"tls_client_key_pem"`
	ClearClientKey  bool   `json:"clear_tls_client_key"`
}

// ValidationError: configuration refusée (message destiné à l'administrateur).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ValidationError{fmt.Sprintf(format, a...)} }

var (
	hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)
	appRe  = regexp.MustCompile(`^[!-~]{1,48}$`)
	hostnR = regexp.MustCompile(`^[!-~]{1,255}$`)
)

// normalize applique les valeurs par défaut et valide (sans toucher aux secrets).
func (c *Config) normalize() error {
	def := func(p *string, v string) {
		*p = strings.TrimSpace(*p)
		if *p == "" {
			*p = v
		}
	}
	def(&c.Protocol, "udp")
	def(&c.MessageFormat, "text")
	def(&c.Framing, "lf")
	def(&c.Facility, "local0")
	def(&c.AppName, "leassh")
	def(&c.MinSeverity, "info")
	c.Host = strings.TrimSpace(c.Host)
	c.Hostname = strings.TrimSpace(c.Hostname)
	c.TLSServerName = strings.TrimSpace(c.TLSServerName)
	if indexOf([]string{"udp", "tcp", "tls"}, c.Protocol) < 0 {
		return invalid("protocole inconnu %q (udp, tcp ou tls)", c.Protocol)
	}
	if indexOf([]string{"text", "json", "cef"}, c.MessageFormat) < 0 {
		return invalid("format de message inconnu %q (text, json ou cef)", c.MessageFormat)
	}
	if indexOf([]string{"lf", "octet"}, c.Framing) < 0 {
		return invalid("cadrage inconnu %q (lf ou octet)", c.Framing)
	}
	if indexOf(facilities, c.Facility) < 0 {
		return invalid("facility inconnue %q", c.Facility)
	}
	if indexOf(severities, c.MinSeverity) < 0 {
		return invalid("niveau inconnu %q", c.MinSeverity)
	}
	if !appRe.MatchString(c.AppName) {
		return invalid("app-name invalide: 1 à 48 caractères ASCII imprimables, sans espace")
	}
	if c.Hostname != "" && !hostnR.MatchString(c.Hostname) {
		return invalid("nom d'hôte invalide: ASCII imprimable, sans espace")
	}
	if c.Port == 0 {
		c.Port = 514
		if c.Protocol == "tls" {
			c.Port = 6514
		}
	}
	if c.Port < 1 || c.Port > 65535 {
		return invalid("port invalide (1 à 65535)")
	}
	if c.Host != "" {
		h := strings.Trim(c.Host, "[]")
		if net.ParseIP(h) == nil && !hostRe.MatchString(h) {
			return invalid("adresse du serveur invalide (nom d'hôte ou IP, sans schéma ni port)")
		}
	}
	if c.Enabled && c.Host == "" {
		return invalid("l'adresse du serveur est requise pour activer l'export")
	}
	seen := map[string]bool{}
	var types []string
	for _, t := range c.Types {
		t = strings.TrimSpace(t)
		if indexOf(Types, t) < 0 {
			return invalid("type d'événement inconnu %q", t)
		}
		if !seen[t] {
			seen[t] = true
			types = append(types, t)
		}
	}
	if types == nil {
		types = append(types, Types...)
	}
	c.Types = types
	if c.TLSCAPEM = strings.TrimSpace(c.TLSCAPEM); c.TLSCAPEM != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(c.TLSCAPEM)) {
		return invalid("l'autorité de certification n'est pas un PEM valide")
	}
	c.TLSClientCertPEM = strings.TrimSpace(c.TLSClientCertPEM)
	return nil
}

func fromStore(sc store.SyslogConfig) Config {
	var types []string
	for _, t := range strings.Split(sc.Types, ",") {
		if t = strings.TrimSpace(t); t != "" {
			types = append(types, t)
		}
	}
	return Config{Enabled: sc.Enabled, Protocol: sc.Protocol, Host: sc.Host, Port: sc.Port, MessageFormat: sc.MessageFormat, Framing: sc.Framing,
		Facility: sc.Facility, AppName: sc.AppName, Hostname: sc.Hostname, MinSeverity: severityName(sc.MinSeverity), Types: types,
		TLSCAPEM: sc.TLSCAPEM, TLSServerName: sc.TLSServerName, TLSSkipVerify: sc.TLSSkipVerify, TLSClientCertPEM: sc.TLSClientCertPEM, HasClientKey: len(sc.TLSClientKeyEnc) > 0}
}

// runtime: configuration résolue, utilisée par l'envoi.
type runtime struct {
	cfg     Config
	version string
	minSev  int
	types   map[string]bool
	tls     *tls.Config
	gen     uint64
}

// allows applique le filtre par type et par niveau.
func (r *runtime) allows(e Event) bool {
	return r.cfg.Enabled && r.types[e.Category()] && e.Severity <= r.minSev
}

func buildTLS(c Config, keyPEM []byte) (*tls.Config, error) {
	t := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.TLSServerName, InsecureSkipVerify: c.TLSSkipVerify} //nolint:gosec // choix explicite de l'administrateur, signalé dans l'interface
	if t.ServerName == "" {
		t.ServerName = strings.Trim(c.Host, "[]")
	}
	if c.TLSCAPEM != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(c.TLSCAPEM)) {
			return nil, invalid("l'autorité de certification n'est pas un PEM valide")
		}
		t.RootCAs = pool
	}
	switch {
	case c.TLSClientCertPEM != "" && len(keyPEM) == 0:
		return nil, invalid("une clé privée est requise avec le certificat client")
	case c.TLSClientCertPEM == "" && len(keyPEM) > 0:
		return nil, invalid("un certificat client est requis avec la clé privée")
	case len(keyPEM) > 0:
		pair, err := tls.X509KeyPair([]byte(c.TLSClientCertPEM), keyPEM)
		if err != nil {
			return nil, invalid("certificat client et clé privée incohérents")
		}
		t.Certificates = []tls.Certificate{pair}
	}
	return t, nil
}

// Config lit la configuration (DB ou transaction courante).
func (f *Forwarder) Config(ctx context.Context, q store.Q) (Config, error) {
	sc, err := store.GetSyslogConfig(ctx, q)
	if err != nil {
		return Config{}, err
	}
	return fromStore(sc), nil
}

func (f *Forwarder) clientKey(sc store.SyslogConfig) ([]byte, error) {
	if len(sc.TLSClientKeyEnc) == 0 {
		return nil, nil
	}
	return f.box.Open(sc.TLSClientKeyEnc)
}

// Save valide et enregistre la configuration dans la transaction de l'appelant (qui y écrit l'audit).
// Clé privée vide = inchangée. Renvoie la configuration avant et après.
func (f *Forwarder) Save(ctx context.Context, tx *sql.Tx, in Input) (before, after Config, err error) {
	prev, err := store.GetSyslogConfig(ctx, tx)
	if err != nil {
		return before, after, err
	}
	before = fromStore(prev)
	c := in.Config
	if err := c.normalize(); err != nil {
		return before, after, err
	}
	sc := prev
	sc.Enabled, sc.Protocol, sc.Host, sc.Port = c.Enabled, c.Protocol, c.Host, c.Port
	sc.MessageFormat, sc.Framing, sc.Facility, sc.AppName, sc.Hostname = c.MessageFormat, c.Framing, c.Facility, c.AppName, c.Hostname
	sc.MinSeverity, sc.Types = indexOf(severities, c.MinSeverity), strings.Join(c.Types, ",")
	sc.TLSCAPEM, sc.TLSServerName, sc.TLSSkipVerify, sc.TLSClientCertPEM = c.TLSCAPEM, c.TLSServerName, c.TLSSkipVerify, c.TLSClientCertPEM
	var key []byte
	switch {
	case in.ClearClientKey:
		sc.TLSClientKeyEnc = nil
	case in.TLSClientKeyPEM != "":
		key = []byte(strings.TrimSpace(in.TLSClientKeyPEM))
		if sc.TLSClientKeyEnc, err = f.box.Seal(key); err != nil {
			return before, after, fmt.Errorf("chiffrement de la clé TLS: %w", err)
		}
	default:
		if key, err = f.clientKey(prev); err != nil {
			return before, after, fmt.Errorf("clé TLS illisible: %w", err)
		}
	}
	if c.Protocol == "tls" || c.TLSClientCertPEM != "" || len(key) > 0 {
		if _, err := buildTLS(c, key); err != nil {
			return before, after, err
		}
	}
	// réactivation: on repart de la fin du journal d'administration (pas de rejeu de l'historique)
	if c.Enabled && !prev.Enabled {
		sc.AuditCursor = sql.NullInt64{}
	}
	if err := store.SaveSyslogConfig(ctx, tx, sc); err != nil {
		return before, after, err
	}
	return before, fromStore(sc), nil
}

// build résout la configuration stockée en configuration d'exécution.
func (f *Forwarder) build(sc store.SyslogConfig) (*runtime, error) {
	c := fromStore(sc)
	if err := c.normalize(); err != nil {
		return nil, err
	}
	rt := &runtime{cfg: c, version: f.Version, minSev: sc.MinSeverity, types: map[string]bool{}}
	for _, t := range c.Types {
		rt.types[t] = true
	}
	key, err := f.clientKey(sc)
	if err != nil {
		return nil, fmt.Errorf("clé TLS illisible: %w", err)
	}
	if c.Protocol == "tls" {
		if rt.tls, err = buildTLS(c, key); err != nil {
			return nil, err
		}
	}
	return rt, nil
}
