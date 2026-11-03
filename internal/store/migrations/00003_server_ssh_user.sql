-- +goose Up
-- Utilisateur SSH propre au serveur (surcharge celui de l'identité): ex. Warpgate « utilisateur:cible ».
ALTER TABLE servers ADD COLUMN ssh_user TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE servers DROP COLUMN ssh_user;
