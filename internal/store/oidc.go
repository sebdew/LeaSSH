package store

import (
	"context"
	"database/sql"
	"errors"
)

// OIDCConfig: configuration SSO OpenID Connect (table à ligne unique, SPEC §9bis).
type OIDCConfig struct {
	Enabled                 bool
	Name                    string
	Issuer                  string
	ClientID                string
	ClientSecretEnc         []byte
	Scopes                  string
	RoleClaim               string
	RoleMap                 string // JSON {"admin":[…],"approver":[…],"viewer":[…]}
	DefaultRole             string // "" = refus | viewer
	JIT                     bool
	LocalPasswordAdminsOnly bool
	EndSession              bool
	AutoRedirect            bool
}

func GetOIDCConfig(ctx context.Context, q Q) (OIDCConfig, error) {
	var c OIDCConfig
	err := q.QueryRowContext(ctx, `SELECT enabled,name,issuer,client_id,client_secret_enc,scopes,role_claim,role_map,default_role,jit,local_password_admins_only,end_session,auto_redirect
		FROM oidc_config WHERE id=1`).
		Scan(&c.Enabled, &c.Name, &c.Issuer, &c.ClientID, &c.ClientSecretEnc, &c.Scopes, &c.RoleClaim, &c.RoleMap, &c.DefaultRole, &c.JIT, &c.LocalPasswordAdminsOnly, &c.EndSession, &c.AutoRedirect)
	return c, wrapErr("configuration SSO", err)
}

func SaveOIDCConfig(ctx context.Context, q Q, c OIDCConfig) error {
	_, err := q.ExecContext(ctx, `UPDATE oidc_config SET enabled=?,name=?,issuer=?,client_id=?,client_secret_enc=?,scopes=?,role_claim=?,role_map=?,default_role=?,jit=?,
		local_password_admins_only=?,end_session=?,auto_redirect=? WHERE id=1`,
		c.Enabled, c.Name, c.Issuer, c.ClientID, c.ClientSecretEnc, c.Scopes, c.RoleClaim, c.RoleMap, c.DefaultRole, c.JIT, c.LocalPasswordAdminsOnly, c.EndSession, c.AutoRedirect)
	return wrapErr("configuration SSO", err)
}

// UserByOIDC retrouve un compte par (issuer, sub); jamais par e-mail ou nom.
func UserByOIDC(ctx context.Context, q Q, issuer, sub string) (User, error) {
	u, err := scanUser(q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE oidc_issuer=? AND oidc_sub=? AND oidc_sub<>''`, issuer, sub))
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrNotFound
	}
	return u, err
}

// InsertOIDCUser crée un compte SSO (mot de passe local inutilisable).
func InsertOIDCUser(ctx context.Context, tx *sql.Tx, u User) (User, error) {
	u.ID = NewID()
	u.AuthProvider = "oidc"
	if u.Theme == "" {
		u.Theme = "system"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO users (id,username,display_name,password_hash,role,disabled,theme,created_at,auth_provider,oidc_issuer,oidc_sub) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.Username, u.DisplayName, "", u.Role, false, u.Theme, Now(), u.AuthProvider, u.OIDCIssuer, u.OIDCSub)
	if err != nil {
		return User{}, wrapErr("création utilisateur SSO", err)
	}
	return u, nil
}

// SyncOIDCUser met à jour nom affiché et rôle d'un compte SSO à chaque connexion.
func SyncOIDCUser(ctx context.Context, q Q, id, displayName, role string) error {
	_, err := q.ExecContext(ctx, `UPDATE users SET display_name=?,role=? WHERE id=?`, displayName, role, id)
	return wrapErr("synchronisation SSO", err)
}

// UsernameExists teste la disponibilité d'un nom d'utilisateur.
func UsernameExists(ctx context.Context, q Q, name string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE username=?`, name).Scan(&n)
	return n > 0, err
}

// CountLocalAdmins: administrateurs locaux actifs (compte de secours quand le SSO est en panne).
func CountLocalAdmins(ctx context.Context, q Q) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role='admin' AND disabled=0 AND auth_provider='local'`).Scan(&n)
	return n, err
}
