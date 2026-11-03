// Package config lit la configuration depuis les variables d'environnement.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
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
		Listen:         or(getenv("LEASH_LISTEN"), ":8080"),
		DataDir:        or(getenv("LEASH_DATA_DIR"), "/data"),
		BaseURL:        getenv("LEASH_BASE_URL"),
		TrustedProxies: getenv("LEASH_TRUSTED_PROXIES"),
		LogLevel:       or(getenv("LEASH_LOG_LEVEL"), "info"),
		MetricsToken:   getenv("LEASH_METRICS_TOKEN"),
	}
	raw := getenv("LEASH_MASTER_KEY")
	if raw == "" {
		return nil, errors.New("LEASH_MASTER_KEY est obligatoire (32 octets en base64, ex. `openssl rand -base64 32`)")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("LEASH_MASTER_KEY n'est pas du base64 valide: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("LEASH_MASTER_KEY doit faire 32 octets (reçu %d)", len(key))
	}
	c.MasterKey = key

	if c.ApprovalTTL, err = dur(getenv("LEASH_APPROVAL_TTL"), 30*time.Minute); err != nil {
		return nil, fmt.Errorf("LEASH_APPROVAL_TTL: %w", err)
	}
	if c.SessionTTL, err = dur(getenv("LEASH_SESSION_TTL"), 12*time.Hour); err != nil {
		return nil, fmt.Errorf("LEASH_SESSION_TTL: %w", err)
	}
	c.LogRetentionDays = 365
	if v := getenv("LEASH_LOG_RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("LEASH_LOG_RETENTION_DAYS invalide: %q", v)
		}
		c.LogRetentionDays = n
	}
	return c, nil
}

func LoadEnv() (*Config, error) { return Load(os.Getenv) }

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
