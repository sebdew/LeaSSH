package groups

import (
	"context"
	"database/sql"
	"errors"
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

func TestGroupLifecycle(t *testing.T) {
	svc, st, u := setup(t)
	ctx := context.Background()
	for _, kind := range []store.GroupKind{store.ServerGroups, store.CommandGroups} {
		if _, err := svc.Create(ctx, u, "", kind, Input{Slug: "Bad", Name: "x"}); !errors.Is(err, catalog.ErrInvalidSlug) {
			t.Fatalf("%s slug: %v", kind, err)
		}
		if _, err := svc.Create(ctx, u, "", kind, Input{Slug: "g1", Name: " "}); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("%s nom: %v", kind, err)
		}
		g, err := svc.Create(ctx, u, "", kind, Input{Slug: "g1", Name: "G1"})
		if err != nil {
			t.Fatal(err)
		}
		if up, err := svc.Update(ctx, u, "", kind, g.ID, Input{Slug: "g1", Name: "G1 bis"}); err != nil || up.Name != "G1 bis" {
			t.Fatalf("%s mise à jour: %+v %v", kind, up, err)
		}
		if ids, err := svc.SetMembers(ctx, u, "", kind, g.ID, nil); err != nil || len(ids) != 0 {
			t.Fatalf("%s membres: %v %v", kind, ids, err)
		}
		if list, err := svc.List(ctx, kind); err != nil || len(list) != 1 {
			t.Fatalf("%s liste: %v", kind, err)
		}
		if err := svc.Delete(ctx, u, "", kind, g.ID); err != nil {
			t.Fatal(err)
		}
		for _, a := range []string{"create", "update", "members", "delete"} {
			if audits(t, st, string(kind)+"_group."+a) != 1 {
				t.Errorf("audit %s_group.%s manquant", kind, a)
			}
		}
	}
}

func TestSynchronisedGroupIsLocked(t *testing.T) {
	svc, st, u := setup(t)
	ctx := context.Background()
	g, err := svc.Create(ctx, u, "", store.ServerGroups, Input{Slug: "sync", Name: "Sync"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`UPDATE server_groups SET managed_by='warpgate' WHERE id=?`, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, u, "", store.ServerGroups, g.ID, Input{Slug: "sync", Name: "Autre"}); !errors.Is(err, ErrManagedLocked) {
		t.Fatalf("modification: %v", err)
	}
	if err := svc.Delete(ctx, u, "", store.ServerGroups, g.ID); !errors.Is(err, ErrManagedUndeletable) {
		t.Fatalf("suppression: %v", err)
	}
	// Les membres, eux, se modifient.
	if _, err := svc.SetMembers(ctx, u, "", store.ServerGroups, g.ID, nil); err != nil {
		t.Fatalf("membres: %v", err)
	}
}
