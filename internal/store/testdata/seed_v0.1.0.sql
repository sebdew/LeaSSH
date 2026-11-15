-- Données d'une instance en v0.1.0 (schéma de la migration 17), pour vérifier qu'une mise à jour les conserve.
INSERT INTO users (id, username, display_name, password_hash, role, created_at) VALUES ('U1', 'admin', 'Admin', '$argon2id$x', 'admin', '2026-11-11T10:00:00.000Z');
INSERT INTO identities (id, name, username, auth_type, public_key, created_at) VALUES ('I1', 'deploy', 'deploy', 'key', 'ssh-ed25519 AAAA', '2026-11-11T10:00:00.000Z');
INSERT INTO servers (id, slug, name, host, identity_id, created_at, updated_at) VALUES ('S1', 'web-01', 'Web', '10.0.0.1', 'I1', '2026-11-11T10:00:00.000Z', '2026-11-11T10:00:00.000Z');
INSERT INTO commands (id, slug, name, risk, template, status, hash, source, created_at, updated_at) VALUES ('C1', 'uptime', 'Uptime', 'read', 'uptime', 'enabled', 'h', 'manual', '2026-11-11T10:00:00.000Z', '2026-11-11T10:00:00.000Z');
INSERT INTO settings (key, value) VALUES ('base_url', 'https://leassh.example.com');
