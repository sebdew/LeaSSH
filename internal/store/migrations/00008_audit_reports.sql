-- +goose Up
-- Rapports d'audit: instantané JSON des faits (immuable, empreinte SHA-256) + synthèse IA facultative, stockée à part.
CREATE TABLE audit_reports (
  id TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  created_by_name TEXT NOT NULL DEFAULT '',
  period_from TEXT NOT NULL,
  period_to TEXT NOT NULL,
  leash_version TEXT NOT NULL DEFAULT '',
  schema_version INTEGER NOT NULL,
  sha256 TEXT NOT NULL,                 -- empreinte de `snapshot` (les faits, sans la synthèse)
  hmac TEXT NOT NULL DEFAULT '',        -- étiquette d'authenticité dérivée de la clé maître (vérifiable par cette instance)
  snapshot TEXT NOT NULL,
  findings_high INTEGER NOT NULL DEFAULT 0,
  findings_medium INTEGER NOT NULL DEFAULT 0,
  findings_low INTEGER NOT NULL DEFAULT 0,
  summary TEXT NOT NULL DEFAULT '',     -- synthèse rédigée par une IA (hors empreinte), vide si non demandée
  summary_model TEXT NOT NULL DEFAULT '',
  summary_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_audit_reports_created ON audit_reports(created_at DESC);

-- +goose Down
DROP TABLE audit_reports;
