-- +goose Up
-- Connexion unique (SSO) OpenID Connect pour l'interface d'administration. Le secret client est chiffré (AES-256-GCM).
CREATE TABLE oidc_config (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  enabled INTEGER NOT NULL DEFAULT 0,
  name TEXT NOT NULL DEFAULT 'SSO',
  issuer TEXT NOT NULL DEFAULT '',
  client_id TEXT NOT NULL DEFAULT '',
  client_secret_enc BLOB,
  scopes TEXT NOT NULL DEFAULT 'openid profile email',
  role_claim TEXT NOT NULL DEFAULT 'groups',
  role_map TEXT NOT NULL DEFAULT '{}',
  default_role TEXT NOT NULL DEFAULT 'viewer' CHECK (default_role IN ('', 'viewer')),
  jit INTEGER NOT NULL DEFAULT 1,
  local_password_admins_only INTEGER NOT NULL DEFAULT 0,
  end_session INTEGER NOT NULL DEFAULT 1
);
INSERT INTO oidc_config (id) VALUES (1);

-- Un compte SSO est lié par (issuer, sub), jamais par e-mail.
ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN oidc_issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN oidc_sub TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_users_oidc ON users(oidc_issuer, oidc_sub) WHERE oidc_sub <> '';

-- +goose Down
DROP INDEX idx_users_oidc;
ALTER TABLE users DROP COLUMN oidc_sub;
ALTER TABLE users DROP COLUMN oidc_issuer;
ALTER TABLE users DROP COLUMN auth_provider;
DROP TABLE oidc_config;
