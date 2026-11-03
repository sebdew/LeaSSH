-- +goose Up
-- Auto-provisioning: des connecteurs (Warpgate, …) créent et tiennent à jour serveurs et groupes.
CREATE TABLE provisioners (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  type TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  interval_min INTEGER NOT NULL DEFAULT 0,       -- 0 = synchronisation manuelle uniquement
  identity_id TEXT NOT NULL REFERENCES identities(id),
  default_user TEXT NOT NULL DEFAULT '',
  config TEXT NOT NULL DEFAULT '{}',              -- paramètres non secrets du connecteur
  secret_enc BLOB,                                -- jeton d'API chiffré (clé maître)
  last_sync_at TEXT NOT NULL DEFAULT '',
  last_sync_status TEXT NOT NULL DEFAULT '',      -- '', ok, error
  last_sync_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE provisioner_rules (
  provisioner_id TEXT NOT NULL REFERENCES provisioners(id) ON DELETE CASCADE,
  rule_id TEXT NOT NULL REFERENCES access_rules(id) ON DELETE CASCADE,
  PRIMARY KEY (provisioner_id, rule_id)
);

-- Propriété: managed_by vide = créé à la main; sinon id du provisioner. external_id = identifiant côté source.
ALTER TABLE servers ADD COLUMN managed_by TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN sync_removed INTEGER NOT NULL DEFAULT 0;  -- désactivé par la synchro (disparu de la source)
ALTER TABLE server_groups ADD COLUMN managed_by TEXT NOT NULL DEFAULT '';
ALTER TABLE server_groups ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
ALTER TABLE server_group_members ADD COLUMN managed INTEGER NOT NULL DEFAULT 0;  -- appartenance posée par la synchro

-- +goose Down
SELECT 1;
