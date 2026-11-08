package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
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

func TestTxReportsItsDuration(t *testing.T) {
	st := openT(t)
	var calls int
	var last error
	st.Observe = func(_ time.Duration, err error) { calls, last = calls+1, err }
	if err := st.Tx(context.Background(), func(*sql.Tx) error { return nil }); err != nil || calls != 1 || last != nil {
		t.Fatalf("succès: %d %v %v", calls, last, err)
	}
	boom := errors.New("boom")
	if err := st.Tx(context.Background(), func(*sql.Tx) error { return boom }); !errors.Is(err, boom) || calls != 2 || !errors.Is(last, boom) {
		t.Fatalf("échec: %d %v %v", calls, last, err)
	}
}
