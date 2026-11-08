// Package config lit l'amorçage depuis les variables d'environnement: uniquement ce qui est nécessaire avant l'ouverture de la base
// (clé maître, répertoire de données, adresse d'écoute). Tout le reste se règle dans l'interface (package settings).
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

type Config struct {
	Listen    string
	DataDir   string
	MasterKey []byte
	// SetupToken: jeton d'initialisation à usage unique (LEASSH_SETUP_TOKEN). Vide = généré au démarrage si l'instance est neuve.
	SetupToken string
}

// Load construit la configuration à partir de getenv (injectable pour les tests).
func Load(getenv func(string) string) (*Config, error) {
	c := &Config{
		Listen:     or(getenv("LEASSH_LISTEN"), ":8080"),
		DataDir:    or(getenv("LEASSH_DATA_DIR"), "/data"),
		SetupToken: getenv("LEASSH_SETUP_TOKEN"),
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
	return c, nil
}

func LoadEnv() (*Config, error) { return Load(os.Getenv) }

func or(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
