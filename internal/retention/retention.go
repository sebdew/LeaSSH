// Package retention: purge automatique des journaux (réglage de rétention). Seul moyen de supprimer des lignes de journal.
package retention

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/store"
)

// Purge supprime les lignes plus vieilles que `days` jours; l'opération est elle-même journalisée.
func Purge(ctx context.Context, st *store.Store, days int, now time.Time) (execs, audits int64, err error) {
	if days <= 0 {
		return 0, 0, nil
	}
	cutoff := store.FormatTime(now.AddDate(0, 0, -days))
	err = st.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if execs, audits, err = store.PurgeBefore(ctx, tx, cutoff); err != nil {
			return err
		}
		if execs+audits == 0 {
			return nil
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "system", ActorName: "retention", Action: "logs.purge", TargetType: "logs",
			Diff: map[string]any{"cutoff": cutoff, "retention_days": days, "executions_deleted": execs, "audit_events_deleted": audits}})
	})
	if err != nil {
		return 0, 0, fmt.Errorf("purge des journaux: %w", err)
	}
	return execs, audits, nil
}

// Loop purge une fois par jour (première passe 1 minute après le démarrage).
func Loop(ctx context.Context, st *store.Store, days func() int, log *slog.Logger) {
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// La durée est relue à chaque passe: elle se règle à chaud dans l'interface (0 = illimité).
			if d := days(); d > 0 {
				if e, a, err := Purge(ctx, st, d, time.Now()); err != nil {
					log.Error("rétention", "err", err)
				} else if e+a > 0 {
					log.Info("journaux purgés", "executions", e, "audit", a)
				}
			}
			t.Reset(24 * time.Hour)
		}
	}
}
