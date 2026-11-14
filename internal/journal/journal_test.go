package journal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/store"
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

func TestDashboardChecklistAndInsightsPeriod(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st)

	ov, err := svc.Dashboard(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Checklist) != 6 {
		t.Fatalf("checklist: %+v", ov.Checklist)
	}
	for _, it := range ov.Checklist {
		if it.Done {
			t.Fatalf("instance vide: l'étape %s ne doit pas être cochée", it.ID)
		}
	}
	if ov.Recent == nil && len(ov.Recent) != 0 {
		t.Fatalf("activité récente: %+v", ov.Recent)
	}
	for _, d := range []int{7, 30, 90} {
		if _, err := svc.Insights(ctx, time.Now(), d); err != nil {
			t.Fatalf("%d jours: %v", d, err)
		}
	}
	for _, d := range []int{0, 1, 365, -7} {
		if _, err := svc.Insights(ctx, time.Now(), d); !errors.Is(err, ErrInvalidPeriod) {
			t.Fatalf("%d jours doit être refusé: %v", d, err)
		}
	}
	if _, err := svc.MetricsState(ctx); err != nil {
		t.Fatal(err)
	}
}
