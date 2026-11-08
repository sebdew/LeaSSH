-- +goose Up
-- Expiration des sessions sur inactivité (SEC-04).
ALTER TABLE sessions ADD COLUMN last_seen_at TEXT NOT NULL DEFAULT '';
UPDATE sessions SET last_seen_at = created_at;

-- +goose Down
ALTER TABLE sessions DROP COLUMN last_seen_at;
