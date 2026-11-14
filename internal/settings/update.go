package settings

import (
	"context"
	"database/sql"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/store"
)

// InvalidError: les réglages proposés sont refusés (valeur hors bornes, jeton trop long…).
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

const maxMetricsToken = 256

// UpdateInput: nouvelles valeurs. Les champs du jeton /metrics sont facultatifs: vide = inchangé, sauf ClearMetricsToken.
type UpdateInput struct {
	Values
	MetricsToken      string
	ClearMetricsToken bool
}

// AuditView: ce qui est écrit dans l'audit (jamais le jeton, seulement sa présence).
func (v Values) AuditView() map[string]any {
	return map[string]any{"base_url": v.BaseURL, "trusted_proxies": v.TrustedProxies, "session_ttl_hours": v.SessionTTLHours, "approval_ttl_minutes": v.ApprovalTTLMin,
		"log_retention_days": v.LogRetentionDays, "log_level": v.LogLevel, "has_metrics_token": v.MetricsTokenHash != ""}
}

// Update valide, enregistre (audit dans la même transaction) puis applique à chaud les réglages généraux.
// Le jeton /metrics n'est conservé que sous forme de haché.
func (s *Service) Update(ctx context.Context, actor store.User, ip string, in UpdateInput) (Values, error) {
	nv := in.Values
	nv.MetricsTokenHash = ""
	if err := nv.Validate(); err != nil {
		return Values{}, &InvalidError{Msg: err.Error()}
	}
	if len(in.MetricsToken) > maxMetricsToken {
		return Values{}, &InvalidError{Msg: "jeton trop long"}
	}
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := Load(ctx, tx)
		if err != nil {
			return audit.Event{}, err
		}
		nv.MetricsTokenHash = old.MetricsTokenHash
		switch {
		case in.MetricsToken != "":
			nv.MetricsTokenHash = HashToken(in.MetricsToken)
		case in.ClearMetricsToken:
			nv.MetricsTokenHash = ""
		}
		if err := Save(ctx, tx, nv); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "settings.update", TargetType: "settings", TargetName: "general",
			Diff: map[string]any{"before": old.AuditView(), "after": nv.AuditView(), "credential_changed": in.MetricsToken != "" || in.ClearMetricsToken}}, nil
	})
	if err != nil {
		return Values{}, err
	}
	s.Apply(nv)
	return nv, nil
}
