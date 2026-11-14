package webhook

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/store"
)

var (
	ErrURLRequired   = errors.New("une URL est requise pour activer le webhook")
	ErrNotConfigured = errors.New("renseignez une URL avant de tester")
)

// InvalidURLError: l'URL du webhook n'est pas une URL http(s) valide.
type InvalidURLError struct{ Msg string }

func (e *InvalidURLError) Error() string { return e.Msg }

// UpdateInput: nouvelle configuration. Un secret vide conserve le secret existant, sauf ClearSecret.
type UpdateInput struct {
	URL         string
	Secret      string
	ClearSecret bool
	Enabled     bool
}

// Update valide et enregistre la configuration; l'événement d'audit est écrit dans la même transaction.
// Le secret de signature n'apparaît jamais dans l'audit (seulement son changement).
func (d *Dispatcher) Update(ctx context.Context, actor store.User, ip string, in UpdateInput) (Config, error) {
	in.URL = strings.TrimSpace(in.URL)
	if in.URL != "" {
		if err := ValidateURL(in.URL); err != nil {
			return Config{}, &InvalidURLError{Msg: err.Error()}
		}
	}
	var cfg Config
	err := audit.Mutate(ctx, d.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, secret, err := d.ConfigQ(ctx, tx)
		if err != nil {
			return audit.Event{}, err
		}
		if in.Enabled && in.URL == "" {
			return audit.Event{}, ErrURLRequired
		}
		if err := d.Save(ctx, tx, in.URL, in.Secret, in.ClearSecret, in.Enabled); err != nil {
			return audit.Event{}, err
		}
		cfg = Config{URL: in.URL, Enabled: in.Enabled, HasSecret: in.Secret != "" || (secret != "" && !in.ClearSecret)}
		return audit.Event{Action: "webhook.update", TargetType: "webhook", TargetName: in.URL,
			Diff: map[string]any{"before": map[string]any{"url": old.URL, "enabled": old.Enabled},
				"after": map[string]any{"url": in.URL, "enabled": in.Enabled, "signing_key_rotated": in.Secret != "", "signing_key_removed": in.Secret == "" && in.ClearSecret && secret != ""}}}, nil
	})
	return cfg, err
}

// Test envoie un message d'essai immédiat (une seule tentative) et en enregistre le résultat.
func (d *Dispatcher) Test(ctx context.Context) (Result, error) {
	cfg, secret, err := d.Config(ctx)
	if err != nil {
		return Result{}, err
	}
	if cfg.URL == "" {
		return Result{}, ErrNotConfigured
	}
	res := d.Send(ctx, cfg.URL, secret, "webhook.test", map[string]any{"message": "Test d'envoi depuis Leassh"}, d.Link("/reglages"), 1)
	if err := store.InsertDelivery(ctx, d.st.DB, store.WebhookDelivery{Event: "webhook.test", OK: res.OK, Attempts: res.Attempts, HTTPStatus: res.Status, Error: res.Error, URL: cfg.URL}); err != nil {
		d.log.Error("enregistrement de l'envoi d'essai", "err", err) // le résultat de l'essai reste renvoyé à l'admin
	}
	return res, nil
}

// Deliveries renvoie les derniers envois (réussis ou non).
func (d *Dispatcher) Deliveries(ctx context.Context) ([]store.WebhookDelivery, error) {
	return store.ListDeliveries(ctx, d.st.DB, 50)
}
