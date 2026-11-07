-- +goose Up
-- Désactive la connexion par mot de passe local pour tous les comptes (SSO obligatoire, ARC-01).
ALTER TABLE oidc_config ADD COLUMN local_login_disabled INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE oidc_config DROP COLUMN local_login_disabled;
