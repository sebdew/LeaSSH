package accounts

import (
	"context"
	"errors"
	"testing"

	"github.com/leassh-dev/leassh/internal/store"
)

func newService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st), st
}

func TestInitializeAdmin(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()

	if _, err := svc.InitializeAdmin(ctx, InitAdminInput{Username: "  ", Password: "mot-de-passe-long"}); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("nom vide: %v", err)
	}
	if _, err := svc.InitializeAdmin(ctx, InitAdminInput{Username: "admin", Password: "court"}); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("mot de passe faible: %v", err)
	}
	u, err := svc.InitializeAdmin(ctx, InitAdminInput{Username: " admin ", Password: "mot-de-passe-long", IP: "192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "admin" || u.Role != "admin" || u.DisplayName != "admin" {
		t.Fatalf("utilisateur inattendu: %+v", u)
	}
	if n, _ := st.UserCount(ctx); n != 1 {
		t.Fatalf("utilisateurs = %d, attendu 1", n)
	}
	var audited int
	if err := st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action='user.create'`).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("événements d'audit = %d (%v), attendu 1", audited, err)
	}
	// Second appel: refusé, rien n'est créé.
	if _, err := svc.InitializeAdmin(ctx, InitAdminInput{Username: "autre", Password: "mot-de-passe-long"}); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("second setup: %v", err)
	}
	if n, _ := st.UserCount(ctx); n != 1 {
		t.Fatalf("utilisateurs = %d après second appel", n)
	}
}
