package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Une demande rejetée par l'approbateur (status=rejected) est un refus: elle doit
// être comptée comme refusée dans le tableau de bord et les statistiques, pas « autre » (#23).
func TestRefusComptesCommeRefuses(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for i, st := range []string{"denied", "rejected"} {
		_, err := s.DB.ExecContext(ctx, `INSERT INTO executions (id,key_name,server_slug,command_slug,risk,status,requested_at,seq) VALUES (?,?,?,?,?,?,?,?)`,
			fmt.Sprintf("r%d", i), "N1", "web-01", "systemd.restart", "write", st, FormatTime(now.Add(-time.Hour)), i+1)
		if err != nil {
			t.Fatal(err)
		}
	}
	d, err := Dashboard(ctx, s.DB, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Executions24h["denied"] != 2 || d.Executions24h["other"] != 0 {
		t.Fatalf("dashboard: %v", d.Executions24h)
	}
	in, err := GetInsights(ctx, s.DB, now, 30)
	if err != nil {
		t.Fatal(err)
	}
	if in.Denied != 2 {
		t.Fatalf("insights: refusées=%d", in.Denied)
	}
}
