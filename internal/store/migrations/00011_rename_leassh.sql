-- +goose Up
-- Renommage du produit (Leash -> Leassh).
UPDATE syslog_config SET app_name = 'leassh' WHERE app_name = 'leash';
-- Les rapports déjà générés portent l'ancien format et une étiquette HMAC dérivée de l'ancien libellé: ils ne sont plus vérifiables.
DELETE FROM audit_reports;
ALTER TABLE audit_reports RENAME COLUMN leash_version TO leassh_version;

-- +goose Down
ALTER TABLE audit_reports RENAME COLUMN leassh_version TO leash_version;
UPDATE syslog_config SET app_name = 'leash' WHERE app_name = 'leassh';
