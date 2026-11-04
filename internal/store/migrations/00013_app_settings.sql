-- +goose Up
-- Paramètres généraux de l'application (ligne unique), modifiables depuis l'interface (Réglages → Général).
-- Seuls la clé maître, le répertoire de données et l'adresse d'écoute restent en variables d'environnement.
CREATE TABLE app_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  base_url TEXT NOT NULL DEFAULT '',            -- URL publique (https://leassh.example.com); vide = déduite de la requête
  trusted_proxies TEXT NOT NULL DEFAULT '',     -- IP/CIDR des reverse proxies de confiance, séparés par des virgules
  session_ttl_hours INTEGER NOT NULL DEFAULT 12,
  approval_ttl_minutes INTEGER NOT NULL DEFAULT 30,
  log_retention_days INTEGER NOT NULL DEFAULT 365, -- 0 = illimité
  log_level TEXT NOT NULL DEFAULT 'info',
  metrics_token_hash TEXT NOT NULL DEFAULT ''   -- SHA-256 (hex) du jeton /metrics; vide = /metrics ouvert
);
INSERT INTO app_settings (id) VALUES (1);

-- +goose Down
DROP TABLE app_settings;
