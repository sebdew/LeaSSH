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
	// Observe (facultatif) reçoit la durée de chaque transaction et son erreur éventuelle (mesure des appels à la base).
	Observe func(d time.Duration, err error)
}

// Open ouvre (ou crée) la base dans dir, active WAL et clés étrangères, applique les migrations.
func Open(ctx context.Context, dir string) (*Store, error) {
	return OpenPath(ctx, filepath.Join(dir, "leassh.db"))
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
	if err := refuseNewerDatabase(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return &Store{DB: db}, nil
}

// refuseNewerDatabase empêche un binaire ancien de tourner sur une base migrée par une version plus récente (retour arrière
// sans restauration): les migrations sont à sens unique, le schéma ne correspondrait plus au code.
func refuseNewerDatabase(ctx context.Context, db *sql.DB) error {
	ms, err := goose.CollectMigrations("migrations", 0, goose.MaxVersion)
	if err != nil || len(ms) == 0 {
		return fmt.Errorf("lecture des migrations: %w", err)
	}
	last := ms[len(ms)-1].Version
	cur, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("version de la base: %w", err)
	}
	if cur > last {
		return fmt.Errorf("la base est à la version de schéma %d, plus récente que ce programme (%d): installez une version de Leassh au moins aussi récente, ou restaurez la sauvegarde faite avant la mise à jour", cur, last)
	}
	return nil
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
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	if s.Observe != nil {
		start := time.Now()
		defer func() { s.Observe(time.Since(start), err) }()
	}
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
	src := filepath.Join(dataDir, "leassh.db")
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
