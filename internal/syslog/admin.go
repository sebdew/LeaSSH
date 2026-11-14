package syslog

import (
	"context"
	"database/sql"
	"errors"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/store"
)

// Current renvoie la configuration enregistrée (sans secret).
func (f *Forwarder) Current(ctx context.Context) (Config, error) { return f.Config(ctx, f.st.DB) }

// auditView: ce qui est écrit dans l'audit (jamais la clé privée; les PEM publics sont réduits à un indicateur).
func auditView(c Config) map[string]any {
	return map[string]any{"enabled": c.Enabled, "protocol": c.Protocol, "host": c.Host, "port": c.Port, "message_format": c.MessageFormat, "framing": c.Framing,
		"facility": c.Facility, "app_name": c.AppName, "hostname": c.Hostname, "min_severity": c.MinSeverity, "types": c.Types,
		"tls_ca": c.TLSCAPEM != "", "tls_server_name": c.TLSServerName, "tls_skip_verify": c.TLSSkipVerify,
		"tls_client_cert": c.TLSClientCertPEM != "", "tls_client_auth": c.HasClientKey}
}

// Update valide et enregistre la configuration (audit dans la même transaction), puis recharge l'export.
// Une configuration refusée renvoie *ValidationError. Un échec de rechargement est journalisé sans annuler l'enregistrement.
func (f *Forwarder) Update(ctx context.Context, actor store.User, ip string, in Input) (Config, error) {
	var after Config
	err := audit.Mutate(ctx, f.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		before, a, err := f.Save(ctx, tx, in)
		if err != nil {
			return audit.Event{}, err
		}
		after = a
		return audit.Event{Action: "syslog.update", TargetType: "syslog", TargetName: a.Host,
			Diff: map[string]any{"before": auditView(before), "after": auditView(a), "tls_client_key_changed": in.TLSClientKeyPEM != "" || in.ClearClientKey}}, nil
	})
	if err != nil {
		return Config{}, err
	}
	if err := f.Reload(ctx); err != nil {
		f.log.Error("rechargement syslog", "err", err)
	}
	return after, nil
}

// TestResult: résultat détaillé d'un message de test.
type TestResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// TestAudited envoie un message de test immédiat (hors file) et audite chaque essai. Une configuration absente
// renvoie *ValidationError (rien n'est audité); un échec réseau est rapporté dans le résultat.
func (f *Forwarder) TestAudited(ctx context.Context, actor store.User, ip string) (TestResult, error) {
	err := f.SendTest(ctx)
	var ve *ValidationError
	if errors.As(err, &ve) {
		return TestResult{}, err
	}
	res := TestResult{OK: err == nil}
	if err != nil {
		res.Error = err.Error()
	}
	diff := map[string]any{"ok": res.OK}
	if res.Error != "" {
		diff["error"] = res.Error
	}
	_ = audit.RecordStandalone(ctx, f.st, audit.Event{ActorType: "user", ActorID: actor.ID, ActorName: actor.Username, IP: ip, Action: "syslog.test", TargetType: "syslog", Diff: diff})
	return res, nil
}
