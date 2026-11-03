package access_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/leash-dev/leash/internal/store"
	"github.com/leash-dev/leash/internal/testkit"
)

// Une règle couvre l'union de tous ses groupes de serveurs et de commandes.
func TestRuleUnionOfGroups(t *testing.T) {
	k := testkit.New(t, "RW", false, time.Minute)
	var other store.Server
	var extraCmd store.Command
	err := k.St.Tx(k.Ctx, func(tx *sql.Tx) error {
		var err error
		if other, err = store.InsertServer(k.Ctx, tx, store.Server{Slug: "db-01", Name: "db", Host: "h", IdentityID: k.Server.IdentityID, Enabled: true, HostKeyFingerprint: "SHA256:x", HostKeyStatus: "pinned"}); err != nil {
			return err
		}
		if extraCmd, err = store.InsertCommand(k.Ctx, tx, store.Command{Slug: "t.extra", Name: "x", Risk: "read", Template: "echo x", Status: "enabled", Source: "manual"}); err != nil {
			return err
		}
		sg, err := store.InsertGroup(k.Ctx, tx, store.ServerGroups, store.Group{Slug: "sg2", Name: "sg2", MemberIDs: []string{other.ID}})
		if err != nil {
			return err
		}
		cg, err := store.InsertGroup(k.Ctx, tx, store.CommandGroups, store.Group{Slug: "cg2", Name: "cg2", MemberIDs: []string{extraCmd.ID}})
		if err != nil {
			return err
		}
		r, err := store.RuleByID(k.Ctx, tx, k.Rule.ID)
		if err != nil {
			return err
		}
		r.ServerGroupIDs, r.CommandGroupIDs = append(r.ServerGroupIDs, sg.ID), append(r.CommandGroupIDs, cg.ID)
		return store.UpdateRule(k.Ctx, tx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := k.Acc.Load(k.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	key, _ := store.KeyByID(k.Ctx, k.St.DB, k.Key.ID)
	for _, srv := range []store.Server{k.Server, other} {
		for _, c := range []store.Command{k.Cmds["t.read"], extraCmd} {
			if d := snap.Evaluate(now, key, srv, c); !d.Allowed {
				t.Errorf("%s × %s devrait être autorisé: %+v", srv.Slug, c.Slug, d)
			}
		}
	}
}
