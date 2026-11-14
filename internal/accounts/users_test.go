package accounts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sebdew/LeaSSH/internal/crypto"
	"github.com/sebdew/LeaSSH/internal/store"
)

func newAdmin(t *testing.T) (*Service, *store.Store, store.User) {
	t.Helper()
	svc, st := newService(t)
	admin, err := svc.InitializeAdmin(context.Background(), InitAdminInput{Username: "admin", Password: "mot-de-passe-long"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, st, admin
}

func countAudit(t *testing.T, st *store.Store, action string) int {
	t.Helper()
	var n int
	if err := st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action=?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateUser(t *testing.T) {
	svc, st, admin := newAdmin(t)
	ctx := context.Background()
	ok := UserInput{Username: " bob ", Password: "mot-de-passe-bob", Role: "viewer"}
	cases := []struct {
		name string
		mod  func(UserInput) UserInput
		want error
	}{
		{"nom vide", func(i UserInput) UserInput { i.Username = " "; return i }, ErrInvalidUsername},
		{"nom trop long", func(i UserInput) UserInput { i.Username = strings.Repeat("a", 65); return i }, ErrInvalidUsername},
		{"rôle inconnu", func(i UserInput) UserInput { i.Role = "root"; return i }, ErrInvalidRole},
		{"mot de passe faible", func(i UserInput) UserInput { i.Password = "court"; return i }, ErrWeakPassword},
	}
	for _, tc := range cases {
		if _, err := svc.CreateUser(ctx, admin, "192.0.2.1", tc.mod(ok)); !errors.Is(err, tc.want) {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	if n := countAudit(t, st, "user.create"); n != 1 { // l'admin du setup seulement
		t.Fatalf("aucun audit pour les refus, obtenu %d", n)
	}
	u, err := svc.CreateUser(ctx, admin, "192.0.2.1", ok)
	if err != nil || u.Username != "bob" || u.DisplayName != "bob" || u.Role != "viewer" {
		t.Fatalf("création: %+v %v", u, err)
	}
	if n := countAudit(t, st, "user.create"); n != 2 {
		t.Fatalf("audit de création manquant: %d", n)
	}
}

func TestUpdateAndDeleteUserGuards(t *testing.T) {
	svc, st, admin := newAdmin(t)
	ctx := context.Background()
	bob, err := svc.CreateUser(ctx, admin, "", UserInput{Username: "bob", Password: "mot-de-passe-bob", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	sso := mustInsert(t, st, store.User{Username: "sso", DisplayName: "sso", Role: "viewer", OIDCIssuer: testIssuer, OIDCSub: "s"})

	if _, err := svc.UpdateUser(ctx, admin, "", bob.ID, UserInput{Role: "root"}); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("rôle invalide: %v", err)
	}
	if _, err := svc.UpdateUser(ctx, admin, "", bob.ID, UserInput{Role: "viewer", Password: "court"}); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("mot de passe faible: %v", err)
	}
	if _, err := svc.UpdateUser(ctx, admin, "", sso.ID, UserInput{Role: "admin"}); !errors.Is(err, ErrManagedAccount) {
		t.Fatalf("compte SSO, rôle: %v", err)
	}
	if _, err := svc.UpdateUser(ctx, admin, "", sso.ID, UserInput{Role: "viewer", Password: "mot-de-passe-long"}); !errors.Is(err, ErrManagedAccount) {
		t.Fatalf("compte SSO, mot de passe: %v", err)
	}
	if _, err := svc.UpdateUser(ctx, admin, "", admin.ID, UserInput{Role: "admin", Disabled: true}); !errors.Is(err, ErrSelfDisable) {
		t.Fatalf("auto-désactivation: %v", err)
	}
	if _, err := svc.UpdateUser(ctx, bob, "", admin.ID, UserInput{Role: "viewer"}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("dernier admin rétrogradé: %v", err)
	}
	if err := svc.DeleteUser(ctx, admin, "", admin.ID); !errors.Is(err, ErrSelfDelete) {
		t.Fatalf("auto-suppression: %v", err)
	}
	if err := svc.DeleteUser(ctx, bob, "", admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("dernier admin supprimé: %v", err)
	}

	// Changement de rôle et réinitialisation: sessions révoquées, audit sans mot de passe.
	if err := st.CreateSession(ctx, "h", bob.ID, "c", "192.0.2.1", time.Hour); err != nil {
		t.Fatal(err)
	}
	nu, err := svc.UpdateUser(ctx, admin, "192.0.2.1", bob.ID, UserInput{Role: "approver", DisplayName: "Bob", Password: "nouveau-mot-de-passe"})
	if err != nil || nu.Role != "approver" || nu.DisplayName != "Bob" {
		t.Fatalf("mise à jour: %+v %v", nu, err)
	}
	var sessions int
	_ = st.DB.QueryRow(`SELECT count(*) FROM sessions WHERE user_id=?`, bob.ID).Scan(&sessions)
	if sessions != 0 {
		t.Fatalf("sessions non révoquées: %d", sessions)
	}
	var diff string
	_ = st.DB.QueryRow(`SELECT diff FROM audit_events WHERE action='user.update'`).Scan(&diff)
	if !strings.Contains(diff, "password_reset") || strings.Contains(diff, "nouveau-mot") {
		t.Fatalf("audit: %s", diff)
	}
	if err := svc.DeleteUser(ctx, admin, "", bob.ID); err != nil {
		t.Fatalf("suppression: %v", err)
	}
	if err := svc.DeleteUser(ctx, admin, "", bob.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("suppression d'un inconnu: %v", err)
	}
}

func TestChangePasswordAndTheme(t *testing.T) {
	svc, st, admin := newAdmin(t)
	ctx := context.Background()
	if err := svc.ChangePassword(ctx, admin, "", "faux", "nouveau-mot-de-passe", ""); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("mot de passe actuel: %v", err)
	}
	if err := svc.ChangePassword(ctx, admin, "", "mot-de-passe-long", "court", ""); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("mot de passe faible: %v", err)
	}
	sso := mustInsert(t, st, store.User{Username: "sso", DisplayName: "sso", Role: "viewer", OIDCIssuer: testIssuer, OIDCSub: "s"})
	if err := svc.ChangePassword(ctx, sso, "", "x", "nouveau-mot-de-passe", ""); !errors.Is(err, ErrSSOAccount) {
		t.Fatalf("compte SSO: %v", err)
	}
	if err := svc.ChangePassword(ctx, admin, "", "mot-de-passe-long", "nouveau-mot-de-passe", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := st.UserByUsername(ctx, "admin")
	if !crypto.CheckPassword("nouveau-mot-de-passe", got.PasswordHash) || countAudit(t, st, "user.password_change") != 1 {
		t.Fatal("mot de passe ou audit non enregistré")
	}
	if err := svc.SetTheme(ctx, admin.ID, "neon"); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("thème: %v", err)
	}
	if err := svc.SetTheme(ctx, admin.ID, "dark"); err != nil {
		t.Fatal(err)
	}
}
