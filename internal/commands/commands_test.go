package commands

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/sebdew/LeaSSH/internal/catalog"
	"github.com/sebdew/LeaSSH/internal/store"
)

func setup(t *testing.T) (*Service, *store.Store, store.User) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var u store.User
	err = st.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		u, err = store.InsertUser(ctx, tx, store.User{Username: "admin", DisplayName: "admin", Role: "admin", PasswordHash: "x"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return New(st), st, u
}

func audits(t *testing.T, st *store.Store, action string) int {
	t.Helper()
	var n int
	if err := st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action=?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func valid() Input {
	return Input{Slug: "sys.uptime", Name: "Uptime", Risk: catalog.RiskRead, Template: "uptime"}
}

func TestCreateValidation(t *testing.T) {
	svc, st, u := setup(t)
	ctx := context.Background()
	cases := []struct {
		name string
		mod  func(Input) Input
		want error
	}{
		{"slug invalide", func(i Input) Input { i.Slug = "Bad Slug"; return i }, catalog.ErrInvalidSlug},
		{"nom vide", func(i Input) Input { i.Name = " "; return i }, ErrInvalidName},
		{"nom trop long", func(i Input) Input { i.Name = strings.Repeat("a", 121); return i }, ErrInvalidName},
		{"description trop longue", func(i Input) Input { i.Description = strings.Repeat("a", 4001); return i }, ErrInvalidDescription},
		{"statut", func(i Input) Input { i.Status = "oops"; return i }, ErrInvalidStatus},
		{"délai", func(i Input) Input { i.TimeoutS = 601; return i }, ErrInvalidTimeout},
		{"sortie max", func(i Input) Input { i.MaxOutputKB = 4097; return i }, ErrInvalidMaxOutput},
	}
	for _, tc := range cases {
		if _, _, err := svc.Create(ctx, u, "", tc.mod(valid())); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	// Le linter bloque un gabarit dangereux, et rien n'est écrit.
	bad := valid()
	bad.Template = "ls {{nope}}"
	_, _, err := svc.Create(ctx, u, "", bad)
	var le *LintError
	if !errors.As(err, &le) || len(le.Result.Errors) == 0 {
		t.Fatalf("lint attendu: %v", err)
	}
	if n := audits(t, st, "command.create"); n != 0 {
		t.Fatalf("aucun audit pour un refus: %d", n)
	}
}

func TestCommandLifecycle(t *testing.T) {
	svc, st, u := setup(t)
	ctx := context.Background()
	c, lint, err := svc.Create(ctx, u, "192.0.2.1", valid())
	if err != nil || !lint.OK() {
		t.Fatalf("création: %v", err)
	}
	if c.Source != "manual" || c.CreatedBy != u.ID || c.Status != "enabled" || c.TimeoutS != 30 || c.MaxOutputKB != 64 {
		t.Fatalf("défauts: %+v", c)
	}
	in := valid()
	in.Name, in.Status = "Uptime 2", "disabled"
	up, _, err := svc.Update(ctx, u, "", c.ID, in)
	if err != nil || up.Name != "Uptime 2" || up.Status != "disabled" {
		t.Fatalf("mise à jour: %+v %v", up, err)
	}
	if got, err := svc.Get(ctx, c.ID); err != nil || got.Name != "Uptime 2" {
		t.Fatalf("lecture: %+v %v", got, err)
	}
	if list, err := svc.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("liste: %v", err)
	}
	if err := svc.Delete(ctx, u, "", c.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, u, "", c.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("suppression d'une commande inconnue: %v", err)
	}
	for _, a := range []string{"command.create", "command.update", "command.delete"} {
		if audits(t, st, a) != 1 {
			t.Errorf("audit %s: %d", a, audits(t, st, a))
		}
	}
}
