package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	Disabled     bool   `json:"disabled"`
	Theme        string `json:"theme"`
	CreatedAt    string `json:"created_at"`
	LastLoginAt  string `json:"last_login_at"`
	PasswordHash string `json:"-"`
	AuthProvider string `json:"auth_provider"` // local | oidc
	OIDCIssuer   string `json:"-"`
	OIDCSub      string `json:"-"`
}

var ErrNotFound = errors.New("introuvable")

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// InsertUser s'exécute dans la transaction de l'appelant (audit atomique).
func InsertUser(ctx context.Context, tx *sql.Tx, u User) (User, error) {
	u.ID = NewID()
	u.AuthProvider = "local"
	if u.Theme == "" {
		u.Theme = "system"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO users (id,username,display_name,password_hash,role,disabled,theme,created_at) VALUES (?,?,?,?,?,?,?,?)`,
		u.ID, u.Username, u.DisplayName, u.PasswordHash, u.Role, u.Disabled, u.Theme, Now())
	if err != nil {
		return User{}, wrapErr("création utilisateur", err)
	}
	return u, nil
}

const userCols = `id,username,display_name,role,disabled,theme,created_at,coalesce(last_login_at,''),password_hash,auth_provider,oidc_issuer,oidc_sub`

func (s *Store) UserByUsername(ctx context.Context, name string) (User, error) {
	return s.scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username=?`, name))
}

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	return s.scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func (s *Store) scanUser(r *sql.Row) (User, error) { return scanUser(r) }

func scanUser(r scanner) (User, error) {
	var u User
	err := r.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Disabled, &u.Theme, &u.CreatedAt, &u.LastLoginAt, &u.PasswordHash, &u.AuthProvider, &u.OIDCIssuer, &u.OIDCSub)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("lecture utilisateur: %w", err)
	}
	return u, nil
}

func (s *Store) TouchLogin(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET last_login_at=? WHERE id=?`, Now(), id)
	return err
}

type Session struct {
	UserID    string
	CSRF      string
	ExpiresAt time.Time
}

func (s *Store) CreateSession(ctx context.Context, idHash, userID, csrf, ip string, ttl time.Duration) error {
	now := time.Now()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions (id_hash,user_id,csrf_token,created_at,last_seen_at,expires_at,ip) VALUES (?,?,?,?,?,?,?)`,
		idHash, userID, csrf, FormatTime(now), FormatTime(now), FormatTime(now.Add(ttl)), ip)
	return err
}

// SessionByHash renvoie la session si elle existe, n'est pas expirée et n'est pas restée inactive plus de idle.
// Elle rafraîchit last_seen_at au plus une fois par minute pour limiter les écritures.
func (s *Store) SessionByHash(ctx context.Context, idHash string, idle time.Duration) (Session, error) {
	var se Session
	var exp, seen string
	err := s.DB.QueryRowContext(ctx, `SELECT user_id,csrf_token,expires_at,last_seen_at FROM sessions WHERE id_hash=?`, idHash).Scan(&se.UserID, &se.CSRF, &exp, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return se, ErrNotFound
	}
	if err != nil {
		return se, fmt.Errorf("lecture session: %w", err)
	}
	if se.ExpiresAt, err = ParseTime(exp); err != nil || !time.Now().Before(se.ExpiresAt) {
		_ = s.DeleteSession(ctx, idHash)
		return se, ErrNotFound
	}
	last, err := ParseTime(seen)
	if err != nil || time.Since(last) >= idle {
		_ = s.DeleteSession(ctx, idHash)
		return se, ErrNotFound
	}
	if time.Since(last) > time.Minute {
		_, _ = s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id_hash=?`, Now(), idHash)
	}
	return se, nil
}

func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash=?`, idHash)
	return err
}

func ListUsers(ctx context.Context, q Q) ([]User, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY username`)
	if err != nil {
		return nil, wrapErr("utilisateurs", err)
	}
	return collect(rows, func(r *sql.Rows) (User, error) { return scanUser(r) })
}

func UserByIDQ(ctx context.Context, q Q, id string) (User, error) {
	u, err := scanUser(q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id))
	return u, wrapErr("utilisateur", err)
}

// UpdateUser met à jour profil, rôle et état; passwordHash vide = inchangé.
func UpdateUser(ctx context.Context, tx Q, u User, passwordHash string) error {
	res, err := tx.ExecContext(ctx, `UPDATE users SET display_name=?,role=?,disabled=? WHERE id=?`, u.DisplayName, u.Role, u.Disabled, u.ID)
	if err != nil {
		return wrapErr("mise à jour utilisateur", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if passwordHash != "" {
		return SetPassword(ctx, tx, u.ID, passwordHash)
	}
	return nil
}

func SetPassword(ctx context.Context, tx Q, id, hash string) error {
	_, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	return wrapErr("mot de passe", err)
}

func SetTheme(ctx context.Context, q Q, id, theme string) error {
	_, err := q.ExecContext(ctx, `UPDATE users SET theme=? WHERE id=?`, theme, id)
	return wrapErr("thème", err)
}

func DeleteUser(ctx context.Context, tx Q, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	return wrapErr("suppression utilisateur", err)
}

// CountEnabledAdmins: garde-fou « dernier administrateur ».
func CountEnabledAdmins(ctx context.Context, q Q) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role='admin' AND disabled=0`).Scan(&n)
	return n, err
}

// DeleteSessionsOf ferme les sessions d'un utilisateur, sauf `keepHash` (la session courante) si non vide.
func DeleteSessionsOf(ctx context.Context, q Q, userID, keepHash string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND id_hash<>?`, userID, keepHash)
	return err
}

// CountUsers compte les comptes; accepte une transaction pour les tests « premier compte ».
func CountUsers(ctx context.Context, q Q) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}
