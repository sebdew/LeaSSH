package store

import (
	"context"
	"database/sql"
	"errors"
)

// ---- réglages clé/valeur ----

func GetSetting(ctx context.Context, q Q, key string) (string, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func SetSetting(ctx context.Context, q Q, key, value string) error {
	_, err := q.ExecContext(ctx, `INSERT INTO settings (key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return wrapErr("réglage", err)
}

// ---- packs installés ----

type Pack struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	InstalledAt  string `json:"installed_at"`
	InstalledBy  string `json:"installed_by"`
	CommandCount int    `json:"command_count"`
}

func ListPacks(ctx context.Context, q Q) ([]Pack, error) {
	rows, err := q.QueryContext(ctx, `SELECT p.id,p.name,p.version,p.description,p.installed_at,p.installed_by,
		(SELECT count(*) FROM commands c WHERE c.pack_id=p.id) FROM packs p ORDER BY p.id`)
	if err != nil {
		return nil, wrapErr("packs", err)
	}
	return collect(rows, func(r *sql.Rows) (Pack, error) {
		var p Pack
		return p, r.Scan(&p.ID, &p.Name, &p.Version, &p.Description, &p.InstalledAt, &p.InstalledBy, &p.CommandCount)
	})
}

func UpsertPack(ctx context.Context, tx Q, p Pack) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO packs (id,name,version,description,installed_at,installed_by) VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,version=excluded.version,description=excluded.description,installed_at=excluded.installed_at,installed_by=excluded.installed_by`,
		p.ID, p.Name, p.Version, p.Description, Now(), p.InstalledBy)
	return wrapErr("pack", err)
}

// MarkCommandFromPack rattache une commande existante à un pack (import d'une version modifiée).
func MarkCommandFromPack(ctx context.Context, tx Q, commandID, packID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE commands SET source='pack',pack_id=? WHERE id=?`, packID, commandID)
	return wrapErr("source de la commande", err)
}

// ---- livraisons de webhook ----

type WebhookDelivery struct {
	ID         string `json:"id"`
	TS         string `json:"ts"`
	Event      string `json:"event"`
	OK         bool   `json:"ok"`
	Attempts   int    `json:"attempts"`
	HTTPStatus int    `json:"http_status"`
	Error      string `json:"error"`
	URL        string `json:"url"`
}

func InsertDelivery(ctx context.Context, q Q, d WebhookDelivery) error {
	d.ID, d.TS = NewID(), Now()
	if _, err := q.ExecContext(ctx, `INSERT INTO webhook_deliveries (id,ts,event,ok,attempts,http_status,error,url) VALUES (?,?,?,?,?,?,?,?)`,
		d.ID, d.TS, d.Event, d.OK, d.Attempts, d.HTTPStatus, d.Error, d.URL); err != nil {
		return wrapErr("livraison webhook", err)
	}
	// on ne garde que les 200 dernières
	_, err := q.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE id NOT IN (SELECT id FROM webhook_deliveries ORDER BY id DESC LIMIT 200)`)
	return err
}

func ListDeliveries(ctx context.Context, q Q, limit int) ([]WebhookDelivery, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,ts,event,ok,attempts,http_status,error,url FROM webhook_deliveries ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, wrapErr("livraisons", err)
	}
	return collect(rows, func(r *sql.Rows) (WebhookDelivery, error) {
		var d WebhookDelivery
		return d, r.Scan(&d.ID, &d.TS, &d.Event, &d.OK, &d.Attempts, &d.HTTPStatus, &d.Error, &d.URL)
	})
}

// UpdateIdentitySecrets remplace les secrets chiffrés d'une identité (rotation de la clé maître).
func UpdateIdentitySecrets(ctx context.Context, tx Q, id string, privateKeyEnc, passwordEnc []byte) error {
	_, err := tx.ExecContext(ctx, `UPDATE identities SET private_key_enc=?,password_enc=? WHERE id=?`, privateKeyEnc, passwordEnc, id)
	return wrapErr("secrets de l'identité", err)
}
