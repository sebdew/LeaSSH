package store

import (
	"context"
	"testing"
)

func TestOpenMigrates(t *testing.T) {
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('users','executions','audit_events','commands')`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("tables manquantes: %d %v", n, err)
	}
	var fk int
	s.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("clés étrangères désactivées")
	}
	if NewID() == NewID() {
		t.Fatal("ULID non uniques")
	}
}

func TestBackup(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.DB.Exec(`INSERT INTO settings (key,value) VALUES ('k','v')`)
	dest := dir + "/backup.db"
	if err := Backup(ctx, dir, dest); err != nil {
		t.Fatal(err)
	}
	if err := Backup(ctx, dir, dest); err == nil {
		t.Fatal("écrasement d'une sauvegarde existante accepté")
	}
	b, err := OpenPath(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var v string
	if err := b.DB.QueryRow(`SELECT value FROM settings WHERE key='k'`).Scan(&v); err != nil || v != "v" {
		t.Fatalf("sauvegarde incomplète: %q %v", v, err)
	}
}
