-- +goose Up
-- Redirection automatique vers le fournisseur SSO (contournable avec ?login).
ALTER TABLE oidc_config ADD COLUMN auto_redirect INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE oidc_config DROP COLUMN auto_redirect;
