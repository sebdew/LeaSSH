package store

import (
	"context"
	"time"
)

// ---- échecs de connexion (limiteur persistant) ----

// CountLoginFailures compte les échecs de (scope, key) postérieurs à since.
func CountLoginFailures(ctx context.Context, q Q, scope, key string, since time.Time) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM login_failures WHERE scope=? AND key=? AND at>?`,
		scope, key, FormatTime(since)).Scan(&n)
	return n, wrapErr("échecs de connexion", err)
}

// AddLoginFailure enregistre un échec et purge les échecs antérieurs à before (toutes clés).
func AddLoginFailure(ctx context.Context, q Q, scope, key string, at, before time.Time) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM login_failures WHERE at<=?`, FormatTime(before)); err != nil {
		return wrapErr("purge des échecs de connexion", err)
	}
	_, err := q.ExecContext(ctx, `INSERT INTO login_failures (scope,key,at) VALUES (?,?,?)`, scope, key, FormatTime(at))
	return wrapErr("échec de connexion", err)
}

// ResetLoginFailures efface les échecs de (scope, key).
func ResetLoginFailures(ctx context.Context, q Q, scope, key string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM login_failures WHERE scope=? AND key=?`, scope, key)
	return wrapErr("réinitialisation des échecs de connexion", err)
}
