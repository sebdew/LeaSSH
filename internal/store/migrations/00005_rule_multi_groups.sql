-- +goose Up
-- Une règle vise plusieurs groupes de serveurs et plusieurs groupes de commandes.
-- Reconstruction de access_rules (les colonnes à clé étrangère ne peuvent pas être supprimées):
-- l'enfant access_rule_keys est recréé avant le parent, pour que la suppression ne cascade pas.
CREATE TABLE access_rule_server_groups (
  rule_id TEXT NOT NULL,
  group_id TEXT NOT NULL REFERENCES server_groups(id) ON DELETE CASCADE,
  PRIMARY KEY (rule_id, group_id)
);
CREATE TABLE access_rule_command_groups (
  rule_id TEXT NOT NULL,
  group_id TEXT NOT NULL REFERENCES command_groups(id) ON DELETE CASCADE,
  PRIMARY KEY (rule_id, group_id)
);
INSERT INTO access_rule_server_groups (rule_id, group_id) SELECT id, server_group_id FROM access_rules;
INSERT INTO access_rule_command_groups (rule_id, group_id) SELECT id, command_group_id FROM access_rules;

CREATE TABLE access_rules_new (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  profile TEXT NOT NULL CHECK (profile IN ('RO','RW')),
  write_requires_approval INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1
);
INSERT INTO access_rules_new (id,name,profile,write_requires_approval,enabled)
  SELECT id,name,profile,write_requires_approval,enabled FROM access_rules;
CREATE TABLE access_rule_keys_new (
  rule_id TEXT NOT NULL REFERENCES access_rules_new(id) ON DELETE CASCADE,
  key_id TEXT NOT NULL REFERENCES mcp_keys(id) ON DELETE CASCADE,
  PRIMARY KEY (rule_id, key_id)
);
INSERT INTO access_rule_keys_new SELECT rule_id,key_id FROM access_rule_keys;
DROP TABLE access_rule_keys;
DROP TABLE access_rules;
ALTER TABLE access_rules_new RENAME TO access_rules;
ALTER TABLE access_rule_keys_new RENAME TO access_rule_keys;

-- +goose Down
SELECT 1;
