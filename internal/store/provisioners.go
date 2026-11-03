package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Provisioner: une source (Warpgate, …) qui crée et tient à jour des serveurs et des groupes.
type Provisioner struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Type            string         `json:"type"`
	Enabled         bool           `json:"enabled"`
	IntervalMin     int            `json:"interval_min"`
	IdentityID      string         `json:"identity_id"`
	DefaultUser     string         `json:"default_user"`
	Config          map[string]any `json:"config"`
	SecretEnc       []byte         `json:"-"`
	HasSecret       bool           `json:"has_secret"`
	RuleIDs         []string       `json:"rule_ids"`
	LastSyncAt      string         `json:"last_sync_at"`
	LastSyncStatus  string         `json:"last_sync_status"`
	LastSyncMessage string         `json:"last_sync_message"`
	CreatedAt       string         `json:"created_at"`
}

const provCols = `id,name,type,enabled,interval_min,identity_id,default_user,config,secret_enc,last_sync_at,last_sync_status,last_sync_message,created_at`

func scanProvisioner(r scanner) (Provisioner, error) {
	var p Provisioner
	var cfg string
	if err := r.Scan(&p.ID, &p.Name, &p.Type, &p.Enabled, &p.IntervalMin, &p.IdentityID, &p.DefaultUser, &cfg, &p.SecretEnc,
		&p.LastSyncAt, &p.LastSyncStatus, &p.LastSyncMessage, &p.CreatedAt); err != nil {
		return p, err
	}
	_ = json.Unmarshal([]byte(cfg), &p.Config)
	if p.Config == nil {
		p.Config = map[string]any{}
	}
	p.HasSecret = len(p.SecretEnc) > 0
	p.RuleIDs = []string{}
	return p, nil
}

func ListProvisioners(ctx context.Context, q Q) ([]Provisioner, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+provCols+` FROM provisioners ORDER BY name`)
	if err != nil {
		return nil, wrapErr("provisioners", err)
	}
	ps, err := collect(rows, func(r *sql.Rows) (Provisioner, error) { return scanProvisioner(r) })
	if err != nil {
		return nil, wrapErr("provisioners", err)
	}
	pairs, err := pairsOf(ctx, q, `SELECT provisioner_id,rule_id FROM provisioner_rules ORDER BY rule_id`)
	if err != nil {
		return nil, err
	}
	for i := range ps {
		if v := pairs[ps[i].ID]; v != nil {
			ps[i].RuleIDs = v
		}
	}
	return ps, nil
}

func ProvisionerByID(ctx context.Context, q Q, id string) (Provisioner, error) {
	all, err := ListProvisioners(ctx, q)
	if err != nil {
		return Provisioner{}, err
	}
	for _, p := range all {
		if p.ID == id {
			return p, nil
		}
	}
	return Provisioner{}, ErrNotFound
}

func InsertProvisioner(ctx context.Context, tx Q, p Provisioner) (Provisioner, error) {
	p.ID, p.CreatedAt = NewID(), Now()
	cfg, _ := json.Marshal(p.Config)
	_, err := tx.ExecContext(ctx, `INSERT INTO provisioners (`+provCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Type, p.Enabled, p.IntervalMin, p.IdentityID, p.DefaultUser, string(cfg), p.SecretEnc, "", "", "", p.CreatedAt)
	if err != nil {
		return p, wrapErr("création provisioner", err)
	}
	p.HasSecret = len(p.SecretEnc) > 0
	return p, SetProvisionerRules(ctx, tx, p.ID, p.RuleIDs)
}

// UpdateProvisioner remplace les champs éditables; secret_enc n'est modifié que s'il est fourni.
func UpdateProvisioner(ctx context.Context, tx Q, p Provisioner) error {
	cfg, _ := json.Marshal(p.Config)
	res, err := tx.ExecContext(ctx, `UPDATE provisioners SET name=?,enabled=?,interval_min=?,identity_id=?,default_user=?,config=?,secret_enc=coalesce(?,secret_enc) WHERE id=?`,
		p.Name, p.Enabled, p.IntervalMin, p.IdentityID, p.DefaultUser, string(cfg), nilIfEmpty(p.SecretEnc), p.ID)
	if err != nil {
		return wrapErr("mise à jour provisioner", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return SetProvisionerRules(ctx, tx, p.ID, p.RuleIDs)
}

func nilIfEmpty(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func SetProvisionerRules(ctx context.Context, tx Q, id string, ruleIDs []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM provisioner_rules WHERE provisioner_id=?`, id); err != nil {
		return wrapErr("règles par défaut", err)
	}
	for _, r := range ruleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO provisioner_rules (provisioner_id,rule_id) VALUES (?,?)`, id, r); err != nil {
			return wrapErr("règle "+r, err)
		}
	}
	return nil
}

func SetProvisionerSync(ctx context.Context, q Q, id, status, message string) error {
	_, err := q.ExecContext(ctx, `UPDATE provisioners SET last_sync_at=?,last_sync_status=?,last_sync_message=? WHERE id=?`, Now(), status, message, id)
	return wrapErr("état de synchro", err)
}

// ReleaseProvisioner rend serveurs et groupes à la gestion manuelle puis supprime le provisioner.
func DeleteProvisioner(ctx context.Context, tx Q, id string) error {
	for _, stmt := range []string{
		`UPDATE servers SET managed_by='',external_id='',sync_removed=0 WHERE managed_by=?`,
		`UPDATE server_group_members SET managed=0 WHERE group_id IN (SELECT id FROM server_groups WHERE managed_by=?)`,
		`UPDATE server_groups SET managed_by='',external_id='' WHERE managed_by=?`,
		`DELETE FROM provisioners WHERE id=?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return wrapErr("suppression provisioner", err)
		}
	}
	return nil
}

// UpdateProvisionerSecret remplace le jeton chiffré d'un provisioner (rotation de la clé maître).
func UpdateProvisionerSecret(ctx context.Context, tx Q, id string, enc []byte) error {
	_, err := tx.ExecContext(ctx, `UPDATE provisioners SET secret_enc=? WHERE id=?`, nilIfEmpty(enc), id)
	return wrapErr("jeton du provisioner", err)
}
