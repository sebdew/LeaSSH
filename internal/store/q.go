package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Q est satisfait par *sql.DB et *sql.Tx: les lectures fonctionnent dans ou hors transaction.
type Q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ErrConflict: violation d'unicité (slug/nom déjà pris).
var ErrConflict = errors.New("déjà existant")

// ErrInUse: suppression refusée car l'objet est référencé.
var ErrInUse = errors.New("objet encore utilisé")

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return fmt.Errorf("%s: %w", op, ErrConflict)
	}
	if strings.Contains(msg, "FOREIGN KEY constraint failed") {
		return fmt.Errorf("%s: %w", op, ErrInUse)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func collect[T any](rows *sql.Rows, scan func(*sql.Rows) (T, error)) ([]T, error) {
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
