// Package store: accès à la base SQLite et migrations (goose).
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	DB *sql.DB
}

// Open ouvre (ou crée) la base dans dir, active WAL et clés étrangères, applique les migrations.
func Open(ctx context.Context, dir string) (*Store, error) {
	return OpenPath(ctx, filepath.Join(dir, "leash.db"))
}

func OpenPath(ctx context.Context, file string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", file)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("ouverture base: %w", err)
	}
	// Une seule connexion en écriture évite SQLITE_BUSY; lectures concurrentes limitées mais suffisantes.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping base: %w", err)
	}
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		db.Close()
		return nil, fmt.Errorf("dialecte goose: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// NewID renvoie un ULID (triable chronologiquement).
func NewID() string { return ulid.Make().String() }

// Now renvoie l'instant courant en UTC, RFC 3339 à la milliseconde (triable lexicalement).
func Now() string { return FormatTime(time.Now()) }

const timeLayout = "2006-01-02T15:04:05.000Z"

func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func ParseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

// Tx exécute fn dans une transaction (rollback si erreur).
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Backup écrit une copie cohérente de la base dans dest (VACUUM INTO), serveur démarré ou non.
// dest ne doit pas exister. La clé maître n'est PAS incluse: elle se sauvegarde séparément.
func Backup(ctx context.Context, dataDir, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s existe déjà", dest)
	}
	src := filepath.Join(dataDir, "leash.db")
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("base introuvable: %w", err)
	}
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", src))
	if err != nil {
		return fmt.Errorf("ouverture base: %w", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("sauvegarde: %w", err)
	}
	return os.Chmod(dest, 0o600)
}
