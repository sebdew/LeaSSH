package accounts

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/leassh-dev/leassh/internal/store"
)

const testIssuer = "https://idp.example.test"

func mustInsert(t *testing.T, st *store.Store, u store.User) store.User {
	t.Helper()
	var out store.User
	err := st.Tx(context.Background(), func(tx *sql.Tx) error {
		var err error
		if u.OIDCIssuer != "" {
			out, err = store.InsertOIDCUser(context.Background(), tx, u)
		} else {
			out, err = store.InsertUser(context.Background(), tx, u)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUpdateSSOConfigGuards(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	admin, err := svc.InitializeAdmin(ctx, InitAdminInput{Username: "admin", Password: "mot-de-passe-long"})
	if err != nil {
		t.Fatal(err)
	}
	base := store.OIDCConfig{Enabled: true, Name: "SSO", Issuer: testIssuer, ClientID: "c", Scopes: "openid", RoleMap: "{}"}
	update := func(c store.OIDCConfig) error {
		_, err := svc.UpdateSSOConfig(ctx, SSOUpdate{Config: c, Actor: admin, IP: "192.0.2.1"})
		return err
	}

	if err := update(base); err != nil {
		t.Fatalf("configuration simple: %v", err)
	}
	// Sans compte administrateur SSO, le mot de passe local ne peut pas être désactivé.
	c := base
	c.LocalLoginDisabled = true
	if err := update(c); !errors.Is(err, ErrNoSSOAdmin) {
		t.Fatalf("attendu ErrNoSSOAdmin, obtenu %v", err)
	}
	ssoAdmin := mustInsert(t, st, store.User{Username: "sso-admin", DisplayName: "x", Role: "admin", OIDCIssuer: testIssuer, OIDCSub: "s1"})
	_ = st.TouchLogin(ctx, ssoAdmin.ID) // « s'est déjà connecté »
	if err := update(c); err != nil {
		t.Fatalf("avec un admin SSO: %v", err)
	}
	// Restreindre le mot de passe local exige un admin local actif.
	if _, err := st.DB.Exec(`UPDATE users SET disabled=1 WHERE username='admin'`); err != nil {
		t.Fatal(err)
	}
	c = base
	c.LocalPasswordAdminsOnly = true
	if err := update(c); !errors.Is(err, ErrNoLocalAdmin) {
		t.Fatalf("attendu ErrNoLocalAdmin, obtenu %v", err)
	}
}

func TestUpdateSSOConfigSecretAndAudit(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	admin, _ := svc.InitializeAdmin(ctx, InitAdminInput{Username: "admin", Password: "mot-de-passe-long"})
	base := store.OIDCConfig{Enabled: true, Name: "SSO", Issuer: testIssuer, ClientID: "c", Scopes: "openid", RoleMap: "{}"}
	step := func(in SSOUpdate) store.OIDCConfig {
		t.Helper()
		in.Config, in.Actor = base, admin
		c, err := svc.UpdateSSOConfig(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := step(SSOUpdate{ReplaceSecret: true, SecretEnc: []byte("chiffré")}); string(c.ClientSecretEnc) != "chiffré" {
		t.Fatalf("secret non enregistré: %q", c.ClientSecretEnc)
	}
	if c := step(SSOUpdate{}); string(c.ClientSecretEnc) != "chiffré" {
		t.Fatalf("le secret doit être conservé: %q", c.ClientSecretEnc)
	}
	if c := step(SSOUpdate{ReplaceSecret: true}); len(c.ClientSecretEnc) != 0 {
		t.Fatalf("le secret doit être effacé: %q", c.ClientSecretEnc)
	}
	var diff string
	if err := st.DB.QueryRow(`SELECT diff FROM audit_events WHERE action='oidc.update' ORDER BY rowid DESC LIMIT 1`).Scan(&diff); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, `"credential_changed":true`) || strings.Contains(diff, "chiffré") {
		t.Fatalf("audit inattendu: %s", diff)
	}
}

func TestSignInSSO(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	in := SSOSignIn{Issuer: testIssuer, Sub: "sub-1", Username: "alice", DisplayName: "Alice", Role: "viewer", IP: "192.0.2.1"}

	if _, err := svc.SignInSSO(ctx, in); !errors.Is(err, ErrSSONotProvisioned) {
		t.Fatalf("sans JIT: %v", err)
	}
	in.JIT = true
	u, err := svc.SignInSSO(ctx, in)
	if err != nil || u.Username != "alice" || u.Role != "viewer" || u.AuthProvider != "oidc" {
		t.Fatalf("JIT: %+v %v", u, err)
	}
	// Un compte local existant du même nom n'est jamais lié: le nouveau compte est suffixé.
	mustInsert(t, st, store.User{Username: "bob", DisplayName: "bob", Role: "viewer", PasswordHash: "x"})
	b, err := svc.SignInSSO(ctx, SSOSignIn{Issuer: testIssuer, Sub: "sub-2", Username: "bob", DisplayName: "Bob", Role: "viewer", JIT: true})
	if err != nil || b.Username == "bob" {
		t.Fatalf("collision de nom: %+v %v", b, err)
	}
	// Changement de rôle: appliqué, sessions révoquées.
	in.Role = "approver"
	if u, err = svc.SignInSSO(ctx, in); err != nil || u.Role != "approver" {
		t.Fatalf("synchronisation du rôle: %+v %v", u, err)
	}
	// Dernier administrateur: la rétrogradation est ignorée.
	adm := mustInsert(t, st, store.User{Username: "root", DisplayName: "root", Role: "admin", OIDCIssuer: testIssuer, OIDCSub: "sub-adm"})
	_ = adm
	got, err := svc.SignInSSO(ctx, SSOSignIn{Issuer: testIssuer, Sub: "sub-adm", Username: "root", DisplayName: "root", Role: "viewer"})
	if err != nil || got.Role != "admin" {
		t.Fatalf("garde-fou dernier admin: %+v %v", got, err)
	}
	// Compte désactivé: refus.
	if _, err := st.DB.Exec(`UPDATE users SET disabled=1 WHERE username='alice'`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SignInSSO(ctx, in); !errors.Is(err, ErrSSOAccountDisabled) {
		t.Fatalf("compte désactivé: %v", err)
	}
}
