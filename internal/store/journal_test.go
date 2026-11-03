package store

import (
	"context"
	"database/sql"
	"testing"
)

func seedExecs(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	r := "echo hi"
	rows := []Execution{
		{KeyID: "k1", KeyName: "N1", ServerID: "s1", ServerSlug: "web-01", CommandSlug: "system.uptime", Risk: "read", Status: "succeeded", Reason: "alarme #1", Rendered: &r, RequestedAt: "2026-10-18T10:00:00.000Z"},
		{KeyID: "k1", KeyName: "N1", ServerID: "s2", ServerSlug: "db-02", CommandSlug: "mysql.kill", Risk: "destructive", Status: "denied", Reason: "100% sûr_", RequestedAt: "2026-10-18T11:00:00.000Z"},
		{KeyID: "k2", KeyName: "N2", ServerID: "s1", ServerSlug: "web-01", CommandSlug: "systemd.restart", Risk: "write", Status: "pending_approval", Reason: "redémarrage", DecidedBy: "u1", RequestedAt: "2026-10-18T12:00:00.000Z"},
	}
	for _, e := range rows {
		if err := s.Tx(ctx, func(tx *sql.Tx) error { _, err := InsertExecution(ctx, tx, e); return err }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecutionFilters(t *testing.T) {
	ctx, s := context.Background(), openT(t)
	seedExecs(t, s)
	if err := s.Tx(ctx, func(tx *sql.Tx) error {
		id, err := InsertIdentity(ctx, tx, Identity{Name: "i", Username: "u", AuthType: "key"})
		if err != nil {
			return err
		}
		web, err := InsertServer(ctx, tx, Server{Slug: "web-01", Name: "w", Host: "h", IdentityID: id.ID, Enabled: true})
		if err != nil {
			return err
		}
		if _, err := InsertGroup(ctx, tx, ServerGroups, Group{Slug: "web", Name: "web", MemberIDs: []string{web.ID}}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE executions SET server_id=? WHERE server_slug='web-01'`, web.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	groups, _ := ListGroups(ctx, s.DB, ServerGroups)
	cases := []struct {
		name string
		f    ExecFilter
		want int
	}{
		{"tout", ExecFilter{}, 3},
		{"clé", ExecFilter{KeyID: "k1"}, 2},
		{"serveur", ExecFilter{ServerSlug: "db-02"}, 1},
		{"commande", ExecFilter{CommandSlug: "mysql.kill"}, 1},
		{"risque", ExecFilter{Risk: "write"}, 1},
		{"statuts multiples", ExecFilter{Statuses: []string{"denied", "succeeded"}}, 2},
		{"validateur", ExecFilter{DecidedBy: "u1"}, 1},
		{"période", ExecFilter{From: "2026-10-18T10:30:00.000Z", To: "2026-10-18T11:30:00.000Z"}, 1},
		{"texte dans la raison", ExecFilter{Text: "redémar"}, 1},
		{"texte dans la commande rendue", ExecFilter{Text: "echo hi"}, 1},
		{"jokers LIKE littéraux (%)", ExecFilter{Text: "100%"}, 1},
		{"jokers LIKE littéraux (_)", ExecFilter{Text: "r_"}, 1},
		{"% seul ne matche pas tout", ExecFilter{Text: "%zzz"}, 0},
		{"groupe de serveurs", ExecFilter{ServerGroupID: groups[0].ID}, 2},
		{"combiné", ExecFilter{KeyID: "k1", Risk: "read"}, 1},
	}
	for _, c := range cases {
		got, err := ListExecutions(ctx, s.DB, c.f)
		if err != nil || len(got) != c.want {
			t.Errorf("%s: %d résultats (attendu %d) %v", c.name, len(got), c.want, err)
		}
	}
	// curseur: plus récent d'abord, page suivante = plus ancien
	p1, _ := ListExecutions(ctx, s.DB, ExecFilter{Limit: 2})
	p2, _ := ListExecutions(ctx, s.DB, ExecFilter{Limit: 2, Before: p1[1].ID})
	if len(p1) != 2 || len(p2) != 1 || p1[0].RequestedAt < p1[1].RequestedAt || p2[0].ID >= p1[1].ID {
		t.Fatalf("curseur: %v %v", p1, p2)
	}
	old, _ := ListExecutions(ctx, s.DB, ExecFilter{Oldest: true})
	if old[0].ID >= old[2].ID {
		t.Fatal("tri croissant attendu")
	}
}

func TestChainVerification(t *testing.T) {
	ctx, s := context.Background(), openT(t)
	seedExecs(t, s)
	if res, err := VerifyExecutions(ctx, s.DB); err != nil || !res.OK || res.Checked != 3 {
		t.Fatalf("chaîne saine: %+v %v", res, err)
	}
	// le résultat (statut, sortie) change légitimement sans casser la chaîne
	s.DB.Exec(`UPDATE executions SET status='succeeded', stdout='ok' WHERE seq=3`)
	if res, _ := VerifyExecutions(ctx, s.DB); !res.OK {
		t.Fatalf("mise à jour du résultat: %+v", res)
	}
	// altération d'un champ couvert
	s.DB.Exec(`UPDATE executions SET reason='autre' WHERE seq=2`)
	if res, _ := VerifyExecutions(ctx, s.DB); res.OK || res.BadSeq != 2 || res.Reason != "contenu altéré" {
		t.Fatalf("altération: %+v", res)
	}
	s.DB.Exec(`UPDATE executions SET reason='100% sûr_' WHERE seq=2`)
	// suppression au milieu
	s.DB.Exec(`DELETE FROM executions WHERE seq=2`)
	if res, _ := VerifyExecutions(ctx, s.DB); res.OK || res.BadSeq != 3 {
		t.Fatalf("trou: %+v", res)
	}
}

func TestPurgeKeepsChainVerifiable(t *testing.T) {
	ctx, s := context.Background(), openT(t)
	seedExecs(t, s)
	var e, a int64
	s.Tx(ctx, func(tx *sql.Tx) (err error) { e, a, err = PurgeBefore(ctx, tx, "2026-10-18T11:30:00.000Z"); return })
	if e != 2 || a != 0 {
		t.Fatalf("purge: %d %d", e, a)
	}
	// la ligne en attente reste; la chaîne repart de la première ligne restante
	res, _ := VerifyExecutions(ctx, s.DB)
	if !res.OK || res.Checked != 1 {
		t.Fatalf("après purge: %+v", res)
	}
}
