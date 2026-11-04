// Package config lit la configuration depuis les variables d'environnement.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen           string
	DataDir          string
	MasterKey        []byte
	BaseURL          string
	ApprovalTTL      time.Duration
	LogRetentionDays int
	SessionTTL       time.Duration
	TrustedProxies   string
	LogLevel         string
	MetricsToken     string // si défini, /metrics exige « Authorization: Bearer <jeton> »
}

// Load construit la configuration à partir de getenv (injectable pour les tests).
func Load(getenv func(string) string) (*Config, error) {
	c := &Config{
		Listen:         or(getenv("LEASSH_LISTEN"), ":8080"),
		DataDir:        or(getenv("LEASSH_DATA_DIR"), "/data"),
		BaseURL:        getenv("LEASSH_BASE_URL"),
		TrustedProxies: getenv("LEASSH_TRUSTED_PROXIES"),
		LogLevel:       or(getenv("LEASSH_LOG_LEVEL"), "info"),
		MetricsToken:   getenv("LEASSH_METRICS_TOKEN"),
	}
	raw := getenv("LEASSH_MASTER_KEY")
	if raw == "" {
		return nil, errors.New("LEASSH_MASTER_KEY est obligatoire (32 octets en base64, ex. `openssl rand -base64 32`)")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("LEASSH_MASTER_KEY n'est pas du base64 valide: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("LEASSH_MASTER_KEY doit faire 32 octets (reçu %d)", len(key))
	}
	c.MasterKey = key

	if c.ApprovalTTL, err = dur(getenv("LEASSH_APPROVAL_TTL"), 30*time.Minute); err != nil {
		return nil, fmt.Errorf("LEASSH_APPROVAL_TTL: %w", err)
	}
	if c.SessionTTL, err = dur(getenv("LEASSH_SESSION_TTL"), 12*time.Hour); err != nil {
		return nil, fmt.Errorf("LEASSH_SESSION_TTL: %w", err)
	}
	c.LogRetentionDays = 365
	if v := getenv("LEASSH_LOG_RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("LEASSH_LOG_RETENTION_DAYS invalide: %q", v)
		}
		c.LogRetentionDays = n
	}
	return c, nil
}

func LoadEnv() (*Config, error) { return Load(Getenv) }

// Getenv lit LEASSH_*; l'ancien préfixe LEASH_* reste accepté (compatibilité avec les déploiements existants).
func Getenv(k string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	if strings.HasPrefix(k, "LEASSH_") {
		return os.Getenv("LEASH_" + strings.TrimPrefix(k, "LEASSH_"))
	}
	return ""
}

func or(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func dur(v string, d time.Duration) (time.Duration, error) {
	if v == "" {
		return d, nil
	}
	return time.ParseDuration(v)
}
