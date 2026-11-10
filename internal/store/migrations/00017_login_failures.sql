-- +goose Up
-- Échecs de connexion persistés: un redémarrage ne remet plus les compteurs à zéro (SEC-04, #30).
CREATE TABLE login_failures (
  scope TEXT NOT NULL,
  key   TEXT NOT NULL,
  at    TEXT NOT NULL
);
CREATE INDEX idx_login_failures_key ON login_failures(scope, key, at);

-- +goose Down
DROP TABLE login_failures;
