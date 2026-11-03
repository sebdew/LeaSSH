-- +goose Up
-- Dernier succès / dernier échec SSH par serveur: une anomalie disparaît dès qu'une connexion réussit.
ALTER TABLE servers ADD COLUMN last_ssh_ok_at TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN last_ssh_error_at TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE servers DROP COLUMN last_ssh_error_at;
ALTER TABLE servers DROP COLUMN last_ssh_ok_at;
