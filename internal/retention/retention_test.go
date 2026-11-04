package retention

import (
	"context"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/store"
)

func TestPurgeIsAuditedAndKeepsPending(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	old := store.FormatTime(time.Now().AddDate(0, 0, -400))
	for i, status := range []string{"succeeded", "pending_approval", "denied"} {
		st.DB.Exec(`INSERT INTO executions (id,status,requested_at,seq,row_hash) VALUES (?,?,?,?,'x')`, "0"+string(rune('a'+i)), status, old, i+1)
	}
	audit.RecordStandalone(ctx, st, audit.Event{ActorType: "system", Action: "old"})
	st.DB.Exec(`UPDATE audit_events SET ts=?`, old)

	if e, a, err := Purge(ctx, st, 0, time.Now()); err != nil || e+a != 0 {
		t.Fatalf("0 = illimité: %d %d %v", e, a, err)
	}
	e, a, err := Purge(ctx, st, 365, time.Now())
	if err != nil || e != 2 || a != 1 {
		t.Fatalf("purge: %d %d %v", e, a, err)
	}
	var n int
	st.DB.QueryRow(`SELECT count(*) FROM executions`).Scan(&n)
	if n != 1 {
		t.Fatalf("la demande en attente doit rester: %d", n)
	}
	var act string
	st.DB.QueryRow(`SELECT action FROM audit_events ORDER BY seq DESC LIMIT 1`).Scan(&act)
	if act != "logs.purge" {
		t.Fatalf("la purge doit être journalisée: %s", act)
	}
	if res, _ := audit.Verify(ctx, st.DB); !res.OK {
		t.Fatalf("chaîne d'audit après purge: %+v", res)
	}
}
