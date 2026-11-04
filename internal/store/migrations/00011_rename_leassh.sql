-- +goose Up
-- Renommage du produit (Leash -> Leassh) : nom d'application syslog par défaut.
UPDATE syslog_config SET app_name = 'leassh' WHERE app_name = 'leash';

-- +goose Down
UPDATE syslog_config SET app_name = 'leash' WHERE app_name = 'leassh';
