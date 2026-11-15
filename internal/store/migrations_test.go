package store

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// v010Version: dernière migration livrée dans la v0.1.0. Une mise à jour part de ce schéma, jamais d'une base vide.
const v010Version = 17

// Une migration publiée ne change jamais: les bases déjà déployées ne la rejouent pas (ticket #88).
// Les migrations absentes du manifeste testdata/published_migrations.sha256 ne sont pas encore livrées et restent libres.
// À chaque release, ajouter leurs empreintes (voir CONTRIBUTING.md).
func TestPublishedMigrationsAreImmutable(t *testing.T) {
	f, err := os.Open("testdata/published_migrations.sha256")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) != 2 {
			continue
		}
		b, err := fs.ReadFile(migrations, "migrations/"+parts[1])
		if err != nil {
			t.Fatalf("migration publiée %s supprimée ou renommée: %v", parts[1], err)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != parts[0] {
			t.Fatalf("migration publiée %s modifiée: ajoutez une nouvelle migration au lieu de l'éditer", parts[1])
		}
		n++
	}
	if n < v010Version {
		t.Fatalf("manifeste incomplet: %d migrations, %d attendues au moins", n, v010Version)
	}
}

// Une base créée par la v0.1.0 est mise à niveau sans perte.
func TestUpgradeFromV010KeepsData(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "leassh.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, db, "migrations", v010Version); err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile("testdata/seed_v0.1.0.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(seed)); err != nil {
		t.Fatalf("données v0.1.0: %v", err)
	}
	db.Close()

	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("mise à niveau: %v", err)
	}
	defer s.Close()
	for q, want := range map[string]int{`SELECT count(*) FROM users WHERE username='admin'`: 1, `SELECT count(*) FROM servers WHERE slug='web-01'`: 1,
		`SELECT count(*) FROM commands WHERE slug='uptime'`: 1, `SELECT count(*) FROM settings WHERE key='base_url'`: 1} {
		var n int
		if err := s.DB.QueryRow(q).Scan(&n); err != nil || n != want {
			t.Fatalf("%s: %d %v", q, n, err)
		}
	}
}

// Un binaire plus ancien que la base refuse de démarrer: il n'y a pas de migration descendante, il faut restaurer la sauvegarde.
func TestOpenRefusesNewerDatabase(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (9999, 1)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(ctx, dir); err == nil || !strings.Contains(err.Error(), "plus récent") {
		t.Fatalf("base plus récente acceptée: %v", err)
	}
}
