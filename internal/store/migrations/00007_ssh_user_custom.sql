-- +goose Up
-- Un admin a personnalisé l'utilisateur SSH d'un serveur synchronisé: la synchro ne l'écrase plus.
ALTER TABLE servers ADD COLUMN ssh_user_custom INTEGER NOT NULL DEFAULT 0;

-- +goose Down
SELECT 1;
