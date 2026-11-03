package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

type Identity struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Username      string `json:"username"`
	AuthType      string `json:"auth_type"`
	PublicKey     string `json:"public_key"`
	PrivateKeyEnc []byte `json:"-"`
	PasswordEnc   []byte `json:"-"`
	CreatedAt     string `json:"created_at"`
}

const identCols = `id,name,username,auth_type,public_key,private_key_enc,password_enc,created_at`

func scanIdentity(r scanner) (Identity, error) {
	var i Identity
	return i, r.Scan(&i.ID, &i.Name, &i.Username, &i.AuthType, &i.PublicKey, &i.PrivateKeyEnc, &i.PasswordEnc, &i.CreatedAt)
}

func ListIdentities(ctx context.Context, q Q) ([]Identity, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+identCols+` FROM identities ORDER BY name`)
	if err != nil {
		return nil, wrapErr("identités", err)
	}
	return collect(rows, func(r *sql.Rows) (Identity, error) { return scanIdentity(r) })
}

func IdentityByID(ctx context.Context, q Q, id string) (Identity, error) {
	i, err := scanIdentity(q.QueryRowContext(ctx, `SELECT `+identCols+` FROM identities WHERE id=?`, id))
	return i, wrapErr("identité", err)
}

func InsertIdentity(ctx context.Context, tx Q, i Identity) (Identity, error) {
	i.ID, i.CreatedAt = NewID(), Now()
	_, err := tx.ExecContext(ctx, `INSERT INTO identities (`+identCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		i.ID, i.Name, i.Username, i.AuthType, i.PublicKey, i.PrivateKeyEnc, i.PasswordEnc, i.CreatedAt)
	return i, wrapErr("création identité", err)
}

func DeleteIdentity(ctx context.Context, tx Q, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM identities WHERE id=?`, id)
	return wrapErr("suppression identité", err)
}

type Server struct {
	ID                 string   `json:"id"`
	Slug               string   `json:"slug"`
	Name               string   `json:"name"`
	Host               string   `json:"host"`
	Port               int      `json:"port"`
	IdentityID         string   `json:"identity_id"`
	HostKeyFingerprint string   `json:"host_key_fingerprint"`
	HostKeyStatus      string   `json:"host_key_status"`
	Description        string   `json:"description"`
	Tags               []string `json:"tags"`
	Enabled            bool     `json:"enabled"`
	SSHUser            string   `json:"ssh_user"`   // surcharge de l'utilisateur de l'identité (vide = utilisateur de l'identité)
	ManagedBy          string   `json:"managed_by"` // id du provisioner qui tient ce serveur (vide = saisi à la main)
	ExternalID         string   `json:"external_id"`
	SSHUserCustom      bool     `json:"ssh_user_custom"` // utilisateur SSH choisi par un admin sur un serveur synchronisé: la synchro n'y touche plus
	SyncRemoved        bool     `json:"sync_removed"`    // désactivé par la synchro car disparu de la source
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}

const srvCols = `id,slug,name,host,port,identity_id,host_key_fingerprint,host_key_status,description,tags,enabled,created_at,updated_at,ssh_user,managed_by,external_id,sync_removed,ssh_user_custom`

func scanServer(r scanner) (Server, error) {
	var s Server
	var tags string
	if err := r.Scan(&s.ID, &s.Slug, &s.Name, &s.Host, &s.Port, &s.IdentityID, &s.HostKeyFingerprint, &s.HostKeyStatus, &s.Description, &tags, &s.Enabled, &s.CreatedAt, &s.UpdatedAt, &s.SSHUser, &s.ManagedBy, &s.ExternalID, &s.SyncRemoved, &s.SSHUserCustom); err != nil {
		return s, err
	}
	_ = json.Unmarshal([]byte(tags), &s.Tags)
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return s, nil
}

func ListServers(ctx context.Context, q Q) ([]Server, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+srvCols+` FROM servers ORDER BY slug`)
	if err != nil {
		return nil, wrapErr("serveurs", err)
	}
	return collect(rows, func(r *sql.Rows) (Server, error) { return scanServer(r) })
}

func ServerByID(ctx context.Context, q Q, id string) (Server, error) {
	s, err := scanServer(q.QueryRowContext(ctx, `SELECT `+srvCols+` FROM servers WHERE id=?`, id))
	return s, wrapErr("serveur", err)
}

func ServerBySlug(ctx context.Context, q Q, slug string) (Server, error) {
	s, err := scanServer(q.QueryRowContext(ctx, `SELECT `+srvCols+` FROM servers WHERE slug=?`, slug))
	return s, wrapErr("serveur", err)
}

func InsertServer(ctx context.Context, tx Q, s Server) (Server, error) {
	s.ID, s.CreatedAt = NewID(), Now()
	s.UpdatedAt = s.CreatedAt
	if s.Port == 0 {
		s.Port = 22
	}
	if s.HostKeyStatus == "" {
		s.HostKeyStatus = "unknown"
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	tags, _ := json.Marshal(s.Tags)
	_, err := tx.ExecContext(ctx, `INSERT INTO servers (`+srvCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Slug, s.Name, s.Host, s.Port, s.IdentityID, s.HostKeyFingerprint, s.HostKeyStatus, s.Description, string(tags), s.Enabled, s.CreatedAt, s.UpdatedAt, s.SSHUser, s.ManagedBy, s.ExternalID, s.SyncRemoved, s.SSHUserCustom)
	return s, wrapErr("création serveur", err)
}

// UpdateServer met à jour les champs éditables. Un changement d'hôte/port/identité remet la clé d'hôte à « unknown ».
func UpdateServer(ctx context.Context, tx Q, s Server) (Server, Server, error) {
	old, err := ServerByID(ctx, tx, s.ID)
	if err != nil {
		return s, old, err
	}
	s.HostKeyFingerprint, s.HostKeyStatus, s.CreatedAt = old.HostKeyFingerprint, old.HostKeyStatus, old.CreatedAt
	s.ManagedBy, s.ExternalID = old.ManagedBy, old.ExternalID
	s.SyncRemoved = old.SyncRemoved && !s.Enabled // réactivé à la main: la synchro n'en reprend pas la main
	if s.Host != old.Host || s.Port != old.Port {
		s.HostKeyFingerprint, s.HostKeyStatus = "", "unknown"
	}
	s.UpdatedAt = Now()
	if s.Tags == nil {
		s.Tags = []string{}
	}
	tags, _ := json.Marshal(s.Tags)
	if s.Host != old.Host || s.Port != old.Port || s.IdentityID != old.IdentityID || s.SSHUser != old.SSHUser {
		if _, err = tx.ExecContext(ctx, `UPDATE servers SET last_ssh_error_at='' WHERE id=?`, s.ID); err != nil { // l'ancien échec ne concerne plus cette configuration
			return s, old, wrapErr("état SSH", err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE servers SET slug=?,name=?,host=?,port=?,identity_id=?,host_key_fingerprint=?,host_key_status=?,description=?,tags=?,enabled=?,updated_at=?,ssh_user=?,sync_removed=?,ssh_user_custom=? WHERE id=?`,
		s.Slug, s.Name, s.Host, s.Port, s.IdentityID, s.HostKeyFingerprint, s.HostKeyStatus, s.Description, string(tags), s.Enabled, s.UpdatedAt, s.SSHUser, s.SyncRemoved, s.SSHUserCustom, s.ID)
	return s, old, wrapErr("mise à jour serveur", err)
}

// SetHostKey épingle (pinned) ou marque une divergence (mismatch).
func SetHostKey(ctx context.Context, tx Q, id, fingerprint, status string) error {
	_, err := tx.ExecContext(ctx, `UPDATE servers SET host_key_fingerprint=?,host_key_status=?,updated_at=? WHERE id=?`, fingerprint, status, Now(), id)
	return wrapErr("clé d'hôte", err)
}

// MarkSSH enregistre le résultat d'une connexion SSH authentifiée (ok) ou son échec (ssh_error).
func MarkSSH(ctx context.Context, q Q, id string, ok bool) error {
	col := "last_ssh_error_at"
	if ok {
		col = "last_ssh_ok_at"
	}
	_, err := q.ExecContext(ctx, `UPDATE servers SET `+col+`=? WHERE id=?`, Now(), id)
	return wrapErr("état SSH", err)
}

func SetHostKeyStatus(ctx context.Context, tx Q, id, status string) error {
	_, err := tx.ExecContext(ctx, `UPDATE servers SET host_key_status=?,updated_at=? WHERE id=?`, status, Now(), id)
	return wrapErr("clé d'hôte", err)
}

func DeleteServer(ctx context.Context, tx Q, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, id)
	return wrapErr("suppression serveur", err)
}
