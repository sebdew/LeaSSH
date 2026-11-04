-- +goose Up
-- Renommage du produit: les rapports déjà générés portent l'ancien format et une étiquette HMAC dérivée de l'ancien libellé,
-- ils ne sont plus vérifiables et sont supprimés.
DELETE FROM audit_reports;
ALTER TABLE audit_reports RENAME COLUMN leash_version TO leassh_version;

-- +goose Down
ALTER TABLE audit_reports RENAME COLUMN leassh_version TO leash_version;
