-- +goose Up
-- Configuration de l'export syslog (ligne unique). La clé privée TLS cliente est chiffrée (AES-256-GCM).
CREATE TABLE syslog_config (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  enabled INTEGER NOT NULL DEFAULT 0,
  protocol TEXT NOT NULL DEFAULT 'udp',
  host TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 514,
  message_format TEXT NOT NULL DEFAULT 'text',
  framing TEXT NOT NULL DEFAULT 'lf',
  facility TEXT NOT NULL DEFAULT 'local0',
  app_name TEXT NOT NULL DEFAULT 'leash',
  hostname TEXT NOT NULL DEFAULT '',
  min_severity INTEGER NOT NULL DEFAULT 6,
  types TEXT NOT NULL DEFAULT 'execution,proposal,audit',
  tls_ca_pem TEXT NOT NULL DEFAULT '',
  tls_server_name TEXT NOT NULL DEFAULT '',
  tls_skip_verify INTEGER NOT NULL DEFAULT 0,
  tls_client_cert_pem TEXT NOT NULL DEFAULT '',
  tls_client_key_enc BLOB,
  audit_cursor INTEGER
);
INSERT INTO syslog_config (id) VALUES (1);

-- +goose Down
DROP TABLE syslog_config;
