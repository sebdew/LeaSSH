-- +goose Up
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('admin','approver','viewer')),
  disabled INTEGER NOT NULL DEFAULT 0,
  theme TEXT NOT NULL DEFAULT 'system',
  created_at TEXT NOT NULL,
  last_login_at TEXT
);

CREATE TABLE sessions (
  id_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE identities (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  username TEXT NOT NULL,
  auth_type TEXT NOT NULL CHECK (auth_type IN ('key','password')),
  private_key_enc BLOB,
  public_key TEXT NOT NULL DEFAULT '',
  password_enc BLOB,
  created_at TEXT NOT NULL
);

CREATE TABLE servers (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  host TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 22,
  identity_id TEXT NOT NULL REFERENCES identities(id),
  host_key_fingerprint TEXT NOT NULL DEFAULT '',
  host_key_status TEXT NOT NULL DEFAULT 'unknown' CHECK (host_key_status IN ('unknown','pinned','mismatch')),
  description TEXT NOT NULL DEFAULT '',
  tags TEXT NOT NULL DEFAULT '[]',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE server_groups (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT ''
);
CREATE TABLE server_group_members (
  group_id TEXT NOT NULL REFERENCES server_groups(id) ON DELETE CASCADE,
  server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, server_id)
);

CREATE TABLE commands (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  risk TEXT NOT NULL CHECK (risk IN ('read','write','destructive')),
  template TEXT NOT NULL,
  params TEXT NOT NULL DEFAULT '{}',
  timeout_s INTEGER NOT NULL DEFAULT 30 CHECK (timeout_s BETWEEN 1 AND 600),
  max_output_kb INTEGER NOT NULL DEFAULT 64,
  status TEXT NOT NULL CHECK (status IN ('pending','enabled','disabled')),
  hash TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  source TEXT NOT NULL CHECK (source IN ('manual','pack','proposal')),
  pack_id TEXT NOT NULL DEFAULT '',
  created_by TEXT REFERENCES users(id),
  approved_by TEXT REFERENCES users(id),
  approved_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE command_groups (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT ''
);
CREATE TABLE command_group_members (
  group_id TEXT NOT NULL REFERENCES command_groups(id) ON DELETE CASCADE,
  command_id TEXT NOT NULL REFERENCES commands(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, command_id)
);

CREATE TABLE mcp_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  prefix TEXT NOT NULL,
  hash TEXT NOT NULL UNIQUE,
  expires_at TEXT,
  last_used_at TEXT,
  last_used_ip TEXT NOT NULL DEFAULT '',
  use_count INTEGER NOT NULL DEFAULT 0,
  can_propose INTEGER NOT NULL DEFAULT 1,
  disabled INTEGER NOT NULL DEFAULT 0,
  created_by TEXT REFERENCES users(id),
  created_at TEXT NOT NULL
);

CREATE TABLE access_rules (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  server_group_id TEXT NOT NULL REFERENCES server_groups(id) ON DELETE CASCADE,
  command_group_id TEXT NOT NULL REFERENCES command_groups(id) ON DELETE CASCADE,
  profile TEXT NOT NULL CHECK (profile IN ('RO','RW')),
  write_requires_approval INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE access_rule_keys (
  rule_id TEXT NOT NULL REFERENCES access_rules(id) ON DELETE CASCADE,
  key_id TEXT NOT NULL REFERENCES mcp_keys(id) ON DELETE CASCADE,
  PRIMARY KEY (rule_id, key_id)
);

-- Journaux: pas de clé étrangère (noms dénormalisés), chaîne d'intégrité via prev_hash/row_hash.
CREATE TABLE executions (
  id TEXT PRIMARY KEY,
  key_id TEXT NOT NULL DEFAULT '', key_name TEXT NOT NULL DEFAULT '',
  actor_user_id TEXT NOT NULL DEFAULT '',
  server_id TEXT NOT NULL DEFAULT '', server_slug TEXT NOT NULL DEFAULT '',
  command_id TEXT NOT NULL DEFAULT '', command_slug TEXT NOT NULL DEFAULT '',
  command_version INTEGER NOT NULL DEFAULT 0, command_hash TEXT NOT NULL DEFAULT '',
  risk TEXT NOT NULL DEFAULT '',
  params TEXT NOT NULL DEFAULT '{}',
  rendered TEXT,
  reason TEXT NOT NULL DEFAULT '',
  rule_id TEXT NOT NULL DEFAULT '', rule_name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('denied','pending_approval','rejected','expired','running','succeeded','failed','timeout','error')),
  denial_reason TEXT NOT NULL DEFAULT '',
  exit_code INTEGER,
  stdout TEXT NOT NULL DEFAULT '', stderr TEXT NOT NULL DEFAULT '',
  output_truncated INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER,
  requested_at TEXT NOT NULL, started_at TEXT, finished_at TEXT,
  decided_by TEXT NOT NULL DEFAULT '', decided_by_name TEXT NOT NULL DEFAULT '',
  decided_at TEXT, decision_comment TEXT NOT NULL DEFAULT '',
  client_ip TEXT NOT NULL DEFAULT '', mcp_session_id TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '',
  seq INTEGER NOT NULL UNIQUE,
  prev_hash TEXT NOT NULL DEFAULT '', row_hash TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_exec_requested ON executions(requested_at DESC, id DESC);
CREATE INDEX idx_exec_status ON executions(status);
CREATE INDEX idx_exec_key ON executions(key_id, requested_at DESC);
CREATE INDEX idx_exec_server ON executions(server_id, requested_at DESC);
CREATE INDEX idx_exec_command ON executions(command_slug, requested_at DESC);

CREATE TABLE proposals (
  id TEXT PRIMARY KEY,
  key_id TEXT NOT NULL, key_name TEXT NOT NULL,
  payload TEXT NOT NULL,
  justification TEXT NOT NULL DEFAULT '',
  lint TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL CHECK (status IN ('pending','approved','rejected','invalid')),
  reviewed_by TEXT, reviewed_at TEXT, review_comment TEXT NOT NULL DEFAULT '',
  command_id TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_proposals_status ON proposals(status, created_at DESC);

CREATE TABLE audit_events (
  id TEXT PRIMARY KEY,
  ts TEXT NOT NULL,
  actor_type TEXT NOT NULL CHECK (actor_type IN ('user','key','system')),
  actor_id TEXT NOT NULL DEFAULT '', actor_name TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  target_type TEXT NOT NULL DEFAULT '', target_id TEXT NOT NULL DEFAULT '', target_name TEXT NOT NULL DEFAULT '',
  diff TEXT NOT NULL DEFAULT '{}',
  ip TEXT NOT NULL DEFAULT '',
  seq INTEGER NOT NULL UNIQUE,
  prev_hash TEXT NOT NULL DEFAULT '', row_hash TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_audit_ts ON audit_events(ts DESC, id DESC);
CREATE INDEX idx_audit_action ON audit_events(action);

CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- +goose Down
DROP TABLE settings; DROP TABLE audit_events; DROP TABLE proposals; DROP TABLE executions;
DROP TABLE access_rule_keys; DROP TABLE access_rules; DROP TABLE mcp_keys;
DROP TABLE command_group_members; DROP TABLE command_groups; DROP TABLE commands;
DROP TABLE server_group_members; DROP TABLE server_groups; DROP TABLE servers;
DROP TABLE identities; DROP TABLE sessions; DROP TABLE users;
