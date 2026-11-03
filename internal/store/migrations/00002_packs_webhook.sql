-- +goose Up
CREATE TABLE packs (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  version TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  installed_at TEXT NOT NULL,
  installed_by TEXT NOT NULL DEFAULT ''
);

CREATE TABLE webhook_deliveries (
  id TEXT PRIMARY KEY,
  ts TEXT NOT NULL,
  event TEXT NOT NULL,
  ok INTEGER NOT NULL,
  attempts INTEGER NOT NULL,
  http_status INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_webhook_deliveries_ts ON webhook_deliveries(ts DESC);

-- +goose Down
DROP TABLE webhook_deliveries; DROP TABLE packs;
