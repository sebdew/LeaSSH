// Package settings: paramètres généraux de l'application, stockés en base (table app_settings) et modifiables depuis l'interface.
// Seuls la clé maître, le répertoire de données et l'adresse d'écoute restent en variables d'environnement (package config).
package settings

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/leassh-dev/leassh/internal/store"
)

// Values: contenu de la ligne unique app_settings.
type Values struct {
	BaseURL          string
	TrustedProxies   string
	SessionTTLHours  int
	ApprovalTTLMin   int
	LogRetentionDays int // 0 = illimité
	LogLevel         string
	MetricsTokenHash string // SHA-256 hex; vide = /metrics fermé (401)
}

// Service: lecture en cache, rechargé après chaque modification.
type Service struct {
	st    *store.Store
	level *slog.LevelVar

	mu      sync.RWMutex
	cur     Values
	proxies []netip.Prefix
}

// New charge les paramètres; level (facultatif) est ajusté au niveau de log enregistré.
func New(ctx context.Context, st *store.Store, level *slog.LevelVar) (*Service, error) {
	s := &Service{st: st, level: level}
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) Reload(ctx context.Context) error {
	v, err := Load(ctx, s.st.DB)
	if err != nil {
		return err
	}
	s.apply(v)
	return nil
}

// Load lit la ligne app_settings.
func Load(ctx context.Context, q store.Q) (Values, error) {
	var v Values
	err := q.QueryRowContext(ctx, `SELECT base_url,trusted_proxies,session_ttl_hours,approval_ttl_minutes,log_retention_days,log_level,metrics_token_hash FROM app_settings WHERE id=1`).
		Scan(&v.BaseURL, &v.TrustedProxies, &v.SessionTTLHours, &v.ApprovalTTLMin, &v.LogRetentionDays, &v.LogLevel, &v.MetricsTokenHash)
	if err != nil {
		return v, fmt.Errorf("paramètres généraux: %w", err)
	}
	return v, nil
}

// Save écrit la ligne (à appeler dans la transaction qui écrit l'audit), puis Apply après validation de la transaction.
func Save(ctx context.Context, q store.Q, v Values) error {
	_, err := q.ExecContext(ctx, `UPDATE app_settings SET base_url=?,trusted_proxies=?,session_ttl_hours=?,approval_ttl_minutes=?,log_retention_days=?,log_level=?,metrics_token_hash=? WHERE id=1`,
		v.BaseURL, v.TrustedProxies, v.SessionTTLHours, v.ApprovalTTLMin, v.LogRetentionDays, v.LogLevel, v.MetricsTokenHash)
	if err != nil {
		return fmt.Errorf("paramètres généraux: %w", err)
	}
	return nil
}

// Apply met le cache à jour (après commit).
func (s *Service) Apply(v Values) { s.apply(v) }

func (s *Service) apply(v Values) {
	proxies, _ := ParseProxies(v.TrustedProxies) // déjà validé à l'enregistrement
	s.mu.Lock()
	s.cur, s.proxies = v, proxies
	s.mu.Unlock()
	if s.level != nil {
		s.level.Set(Level(v.LogLevel))
	}
}

func (s *Service) Get() Values {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

func (s *Service) BaseURL() string { return strings.TrimRight(s.Get().BaseURL, "/") }
func (s *Service) SessionTTL() time.Duration {
	return time.Duration(s.Get().SessionTTLHours) * time.Hour
}
func (s *Service) ApprovalTTL() time.Duration {
	return time.Duration(s.Get().ApprovalTTLMin) * time.Minute
}
func (s *Service) RetentionDays() int { return s.Get().LogRetentionDays }

// Trusted: l'adresse appartient-elle à un reverse proxy de confiance?
func (s *Service) Trusted(a netip.Addr) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.proxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// CheckMetricsToken: true seulement si un jeton est défini et que celui fourni correspond (temps constant).
// Sécurisé par défaut: sans jeton défini, /metrics reste fermé.
func (s *Service) CheckMetricsToken(got string) bool {
	h := s.Get().MetricsTokenHash
	if h == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(HashToken(got)), []byte(h)) == 1
}

func HashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func Level(name string) slog.Level {
	switch name {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}

// ParseProxies: liste d'IP ou de CIDR séparés par des virgules, espaces ou retours à la ligne.
func ParseProxies(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, p := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		if pf, err := netip.ParsePrefix(p); err == nil {
			out = append(out, pf)
			continue
		}
		a, err := netip.ParseAddr(p)
		if err != nil {
			return nil, fmt.Errorf("proxy de confiance invalide: %q", p)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// Validate normalise et contrôle les valeurs saisies.
func (v *Values) Validate() error {
	v.BaseURL = strings.TrimRight(strings.TrimSpace(v.BaseURL), "/")
	if v.BaseURL != "" {
		u, err := url.Parse(v.BaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || len(v.BaseURL) > 255 {
			return errors.New("l'URL publique doit être de la forme https://hôte[:port], sans chemin")
		}
	}
	v.TrustedProxies = strings.Join(strings.FieldsFunc(v.TrustedProxies, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }), ", ")
	if _, err := ParseProxies(v.TrustedProxies); err != nil {
		return err
	}
	if v.SessionTTLHours < 1 || v.SessionTTLHours > 24*90 {
		return errors.New("durée de session: entre 1 heure et 90 jours")
	}
	if v.ApprovalTTLMin < 1 || v.ApprovalTTLMin > 24*60*30 {
		return errors.New("délai d'expiration des validations: entre 1 minute et 30 jours")
	}
	if v.LogRetentionDays < 0 || v.LogRetentionDays > 3650 {
		return errors.New("rétention des journaux: 0 (illimité) à 3650 jours")
	}
	switch v.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("niveau de journalisation: debug, info, warn ou error")
	}
	return nil
}
