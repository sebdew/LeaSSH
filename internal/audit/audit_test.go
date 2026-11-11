package audit

import (
	"context"
	"strings"
	"testing"

	"github.com/leassh-dev/leassh/internal/store"
)

func TestChainAndMask(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 3; i++ {
		e := Event{ActorType: "user", ActorID: "u", ActorName: "admin", Action: "server.create", Diff: map[string]any{
			"after": map[string]any{"name": "web", "password_enc": "TOPSECRET", "nested": []any{map[string]any{"private_key": "KEY"}}}}}
		if err := RecordStandalone(ctx, s, e); err != nil {
			t.Fatal(err)
		}
	}
	var diff string
	s.DB.QueryRow(`SELECT diff FROM audit_events WHERE seq=1`).Scan(&diff)
	if strings.Contains(diff, "TOPSECRET") || strings.Contains(diff, "KEY") || !strings.Contains(diff, "web") {
		t.Fatalf("masquage: %s", diff)
	}
	if res, err := Verify(ctx, s.DB); err != nil || !res.OK || res.Checked != 3 {
		t.Fatalf("chaîne saine jugée altérée: %+v %v", res, err)
	}
	s.DB.Exec(`UPDATE audit_events SET action='server.delete' WHERE seq=2`)
	if res, _ := Verify(ctx, s.DB); res.OK || res.BadSeq != 2 || res.Reason != "contenu altéré" {
		t.Fatalf("altération non détectée: %+v", res)
	}
}

// SEC-09: les valeurs d'origine externe sont tronquées avant écriture, la chaîne reste valide.
func TestRecordTruncatesExternalValues(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	long := strings.Repeat("é", 5000)
	if err := RecordStandalone(ctx, s, Event{ActorType: "system", ActorName: long, Action: "user.login_failed", TargetType: "user", TargetName: long, IP: long}); err != nil {
		t.Fatal(err)
	}
	var an, tn, ip string
	_ = s.DB.QueryRow(`SELECT actor_name, target_name, ip FROM audit_events WHERE seq=1`).Scan(&an, &tn, &ip)
	for _, v := range []string{an, tn, ip} {
		if len([]rune(v)) > 128 {
			t.Fatalf("valeur non tronquée: %d caractères", len([]rune(v)))
		}
	}
	if res, err := Verify(ctx, s.DB); err != nil || !res.OK {
		t.Fatalf("chaîne: %+v %v", res, err)
	}
}
