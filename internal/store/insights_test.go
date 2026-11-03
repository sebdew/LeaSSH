package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestInsights(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	seq := 0
	add := func(daysAgo int, hour int, status, cmd, srv, key, risk string, dur *int, denial, decidedBy string, decidedAfter int) {
		seq++
		at := time.Date(2026, 11, 2-daysAgo, hour, 0, 0, 0, time.UTC)
		var decided any
		if decidedBy != "" {
			decided = FormatTime(at.Add(time.Duration(decidedAfter) * time.Second))
		}
		_, err := s.DB.ExecContext(ctx, `INSERT INTO executions (id,key_name,server_slug,command_slug,risk,status,denial_reason,duration_ms,requested_at,decided_by,decided_at,seq) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			fmt.Sprintf("e%d", seq), key, srv, cmd, risk, status, denial, dur, FormatTime(at), decidedBy, decided, seq)
		if err != nil {
			t.Fatal(err)
		}
	}
	d := func(n int) *int { return &n }
	add(0, 10, "succeeded", "system.uptime", "web-01", "N1", "read", d(100), "", "", 0)
	add(0, 10, "succeeded", "system.uptime", "web-02", "N1", "read", d(300), "", "", 0)
	add(0, 11, "failed", "system.uptime", "web-01", "N2", "read", d(200), "", "", 0)
	add(1, 9, "denied", "systemd.restart", "web-01", "N2", "write", nil, "rule_not_found:abc", "", 0)
	add(1, 9, "succeeded", "systemd.restart", "web-01", "N1", "write", d(1000), "", "u1", 40)
	add(2, 9, "succeeded", "systemd.restart", "web-01", "N1", "write", d(2000), "", "u1", 80)
	add(40, 9, "succeeded", "hors.periode", "web-01", "N1", "read", d(5), "", "", 0) // hors fenêtre de 30 jours

	in, err := GetInsights(ctx, s.DB, now, 30)
	if err != nil {
		t.Fatal(err)
	}
	if in.Requests != 6 || in.Succeeded != 4 || in.Failed != 1 || in.Denied != 1 {
		t.Fatalf("totaux: %+v", in)
	}
	if in.Executed != 5 || in.AvgMs != 720 || in.MaxMs != 2000 || in.P50Ms != 300 {
		t.Fatalf("durées: %+v", in)
	}
	if in.Approvals != 2 || in.AvgApprovalSec != 60 {
		t.Fatalf("validations: %d %ds", in.Approvals, in.AvgApprovalSec)
	}
	if len(in.TopCommands) != 2 || in.TopCommands[0].Name != "system.uptime" || in.TopCommands[0].Count != 3 || in.TopCommands[0].Failed != 1 || in.TopCommands[0].AvgMs != 200 || in.TopCommands[0].Servers != 2 {
		t.Fatalf("top commandes: %+v", in.TopCommands)
	}
	if in.TopServers[0].Name != "web-01" || in.TopServers[0].Count != 5 {
		t.Fatalf("top serveurs: %+v", in.TopServers)
	}
	if len(in.TopKeys) != 2 || in.TopKeys[0].Name != "N1" {
		t.Fatalf("top clés: %+v", in.TopKeys)
	}
	if len(in.Slowest) != 1 || in.Slowest[0].Name != "system.uptime" { // seules les commandes lancées au moins 3 fois
		t.Fatalf("plus lentes: %+v", in.Slowest)
	}
	if len(in.DenialReasons) != 1 || in.DenialReasons[0].Name != "rule_not_found" {
		t.Fatalf("refus: %+v", in.DenialReasons)
	}
	if in.Risk["read"] != 3 || in.Risk["write"] != 2 {
		t.Fatalf("risques: %+v", in.Risk)
	}
	if len(in.Hourly) != 4 || in.Hourly[0].Hour != "2026-10-31T09" || in.Hourly[2].OK != 2 || in.Hourly[2].Hour != "2026-11-02T10" || in.Hourly[1].Denied != 1 || in.Hourly[1].OK != 1 || in.Hourly[3].KO != 1 {
		t.Fatalf("heures: %+v", in.Hourly)
	}
	if wide, _ := GetInsights(ctx, s.DB, now, 90); wide.Requests != 7 {
		t.Fatalf("fenêtre 90 jours: %d", wide.Requests)
	}
	if empty, err := GetInsights(ctx, s.DB, now.AddDate(1, 0, 0), 7); err != nil || empty.Requests != 0 || empty.Hourly == nil || empty.TopCommands == nil {
		t.Fatalf("période vide: %+v %v", empty, err)
	}
}
