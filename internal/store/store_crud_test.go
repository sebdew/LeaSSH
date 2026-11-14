package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/sebdew/LeaSSH/internal/catalog"
)

func openT(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCommandVersioning(t *testing.T) {
	ctx, s := context.Background(), openT(t)
	var c Command
	err := s.Tx(ctx, func(tx *sql.Tx) (err error) {
		c, err = InsertCommand(ctx, tx, Command{Slug: "system.uptime", Name: "Uptime", Risk: catalog.RiskRead, Template: "uptime", Status: "enabled", Source: "manual"})
		return
	})
	if err != nil || c.Version != 1 || c.Hash == "" || c.TimeoutS != 30 || c.MaxOutputKB != 64 {
		t.Fatalf("insert: %+v %v", c, err)
	}
	h1 := c.Hash
	// modification du nom seulement: pas de nouvelle version
	c.Name = "Uptime 2"
	s.Tx(ctx, func(tx *sql.Tx) (err error) { c, _, err = UpdateCommand(ctx, tx, c); return })
	if c.Version != 1 || c.Hash != h1 {
		t.Fatalf("nom modifié: version %d", c.Version)
	}
	c.Template = "uptime -p"
	s.Tx(ctx, func(tx *sql.Tx) (err error) { c, _, err = UpdateCommand(ctx, tx, c); return })
	if c.Version != 2 || c.Hash == h1 {
		t.Fatalf("template modifié: version %d", c.Version)
	}
	got, _ := CommandBySlug(ctx, s.DB, "system.uptime")
	if got.Version != 2 || got.Template != "uptime -p" {
		t.Fatalf("relecture: %+v", got)
	}
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		_, err := InsertCommand(ctx, tx, Command{Slug: "system.uptime", Name: "x", Risk: catalog.RiskRead, Template: "x", Status: "enabled", Source: "manual"})
		return err
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("doublon: %v", err)
	}
}

func TestGroupsRulesKeys(t *testing.T) {
	ctx, s := context.Background(), openT(t)
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		id, _ := InsertIdentity(ctx, tx, Identity{Name: "root", Username: "root", AuthType: "key", PublicKey: "pk"})
		srv, err := InsertServer(ctx, tx, Server{Slug: "web-01", Name: "Web", Host: "h", IdentityID: id.ID, Enabled: true})
		if err != nil {
			return err
		}
		cmd, _ := InsertCommand(ctx, tx, Command{Slug: "a.b", Name: "x", Risk: catalog.RiskRead, Template: "uptime", Status: "enabled", Source: "manual"})
		sg, _ := InsertGroup(ctx, tx, ServerGroups, Group{Slug: "web", Name: "Web", MemberIDs: []string{srv.ID}})
		cg, _ := InsertGroup(ctx, tx, CommandGroups, Group{Slug: "diag", Name: "Diag", MemberIDs: []string{cmd.ID}})
		k, err := InsertKey(ctx, tx, MCPKey{Name: "N1", Prefix: "lsh_abcd", Hash: "h", CanPropose: true})
		if err != nil {
			return err
		}
		_, err = InsertRule(ctx, tx, AccessRule{Name: "r", ServerGroupIDs: []string{sg.ID}, CommandGroupIDs: []string{cg.ID}, Profile: "RO", Enabled: true, KeyIDs: []string{k.ID}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	rules, _ := ListRules(ctx, s.DB)
	keys, _ := ListKeys(ctx, s.DB)
	gs, _ := ListGroups(ctx, s.DB, ServerGroups)
	if len(rules) != 1 || len(rules[0].KeyIDs) != 1 || len(keys[0].RuleIDs) != 1 || len(gs[0].MemberIDs) != 1 {
		t.Fatalf("relations: %+v %+v %+v", rules, keys, gs)
	}
	// un serveur référencé par une exécution… et une identité utilisée ne peut pas être supprimée
	err = s.Tx(ctx, func(tx *sql.Tx) error { return DeleteIdentity(ctx, tx, gs[0].ID) })
	_ = err
	ident, _ := ListIdentities(ctx, s.DB)
	if err := s.Tx(ctx, func(tx *sql.Tx) error { return DeleteIdentity(ctx, tx, ident[0].ID) }); !errors.Is(err, ErrInUse) {
		t.Fatalf("identité utilisée supprimée: %v", err)
	}
}

func TestExecutionLifecycle(t *testing.T) {
	ctx, s := context.Background(), openT(t)
	var e Execution
	r := "uptime"
	err := s.Tx(ctx, func(tx *sql.Tx) (err error) {
		e, err = InsertExecution(ctx, tx, Execution{KeyID: "k", KeyName: "N1", ServerSlug: "web-01", CommandSlug: "a.b", Status: "pending_approval", Rendered: &r, Reason: "test"})
		return
	})
	if err != nil || e.Seq != 1 {
		t.Fatalf("insert: %+v %v", e, err)
	}
	ok, _ := TransitionExecution(ctx, s.DB, e.ID, "pending_approval", "running")
	again, _ := TransitionExecution(ctx, s.DB, e.ID, "pending_approval", "running")
	if !ok || again {
		t.Fatalf("transition atomique: %v %v", ok, again)
	}
	got, _ := ExecutionByID(ctx, s.DB, e.ID)
	code, ms := 0, int64(12)
	got.Status, got.ExitCode, got.DurationMS = "succeeded", &code, &ms
	if err := SaveExecutionState(ctx, s.DB, got); err != nil {
		t.Fatal(err)
	}
	list, _ := ListExecutions(ctx, s.DB, ExecFilter{Statuses: []string{"succeeded"}})
	if len(list) != 1 || *list[0].ExitCode != 0 {
		t.Fatalf("liste: %+v", list)
	}
	old, _ := ExpirePending(ctx, s.DB, "9999")
	if len(old) != 0 {
		t.Fatal("rien en attente")
	}
}
