package webhook

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leassh-dev/leassh/internal/store"
)

func adminUser(t *testing.T, st *store.Store) store.User {
	t.Helper()
	var u store.User
	if err := st.Tx(context.Background(), func(tx *sql.Tx) (err error) {
		u, err = store.InsertUser(context.Background(), tx, store.User{Username: "adm", Role: "admin", PasswordHash: "x"})
		return
	}); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestUpdateValidatesAndAuditsWithoutSecret(t *testing.T) {
	d, st := newD(t)
	ctx := context.Background()
	u := adminUser(t, st)

	var iu *InvalidURLError
	if _, err := d.Update(ctx, u, "192.0.2.1", UpdateInput{URL: "ftp://x"}); !errors.As(err, &iu) {
		t.Fatalf("URL invalide: %v", err)
	}
	if _, err := d.Update(ctx, u, "", UpdateInput{Enabled: true}); !errors.Is(err, ErrURLRequired) {
		t.Fatalf("activation sans URL: %v", err)
	}
	cfg, err := d.Update(ctx, u, "192.0.2.1", UpdateInput{URL: " https://hook.example.com/in ", Secret: "tres-secret", Enabled: true})
	if err != nil || cfg.URL != "https://hook.example.com/in" || !cfg.Enabled || !cfg.HasSecret {
		t.Fatalf("mise à jour: %+v %v", cfg, err)
	}
	// secret conservé quand il n'est pas fourni, supprimé sur demande
	if cfg, _ = d.Update(ctx, u, "", UpdateInput{URL: "https://hook.example.com/in", Enabled: true}); !cfg.HasSecret {
		t.Fatal("le secret doit être conservé")
	}
	if cfg, _ = d.Update(ctx, u, "", UpdateInput{URL: "https://hook.example.com/in", ClearSecret: true}); cfg.HasSecret {
		t.Fatal("le secret doit être supprimé")
	}
	rows, err := st.DB.Query(`SELECT action, diff FROM audit_events WHERE action = 'webhook.update'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var a, diff string
		_ = rows.Scan(&a, &diff)
		n++
		if strings.Contains(diff, "tres-secret") {
			t.Fatalf("le secret ne doit pas apparaître dans l'audit: %s", diff)
		}
	}
	if n != 3 {
		t.Fatalf("événements d'audit = %d, attendu 3 (les échecs n'en écrivent pas)", n)
	}
}

func TestTestSendsOnceAndRecords(t *testing.T) {
	d, st := newD(t)
	ctx := context.Background()
	u := adminUser(t, st)
	if _, err := d.Test(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("sans URL: %v", err)
	}
	rc := &recv{}
	ts := httptest.NewServer(rc)
	defer ts.Close()
	if _, err := d.Update(ctx, u, "", UpdateInput{URL: ts.URL, Secret: "s", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	res, err := d.Test(ctx)
	if err != nil || !res.OK || res.Attempts != 1 || len(rc.hits) != 1 {
		t.Fatalf("essai: %+v %v (%d envois)", res, err, len(rc.hits))
	}
	items, err := d.Deliveries(ctx)
	if err != nil || len(items) != 1 || items[0].Event != "webhook.test" || !items[0].OK {
		t.Fatalf("envois enregistrés: %+v %v", items, err)
	}
}
