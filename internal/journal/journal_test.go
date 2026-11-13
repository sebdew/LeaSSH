package journal

import (
	"context"
	"testing"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/store"
)

func TestServiceReadsAndVerifies(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := audit.RecordStandalone(ctx, st, audit.Event{ActorType: "system", ActorName: "test", Action: "test.event", TargetType: "test"}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)

	events, err := svc.ListAudit(ctx, store.AuditFilter{Limit: 10})
	if err != nil || len(events) != 1 || events[0].Action != "test.event" {
		t.Fatalf("audit: %+v %v", events, err)
	}
	if ex, err := svc.ListExecutions(ctx, store.ExecFilter{Limit: 10}); err != nil || len(ex) != 0 {
		t.Fatalf("exécutions: %+v %v", ex, err)
	}
	if _, err := svc.Execution(ctx, "absente"); err == nil {
		t.Fatal("une exécution inconnue doit être refusée")
	}
	res, err := svc.VerifyIntegrity(ctx)
	if err != nil || !res.OK || !res.Audit.OK || !res.Executions.OK || res.Audit.Checked != 1 {
		t.Fatalf("intégrité: %+v %v", res, err)
	}
}
