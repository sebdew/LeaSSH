package accounts

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/leassh-dev/leassh/internal/crypto"
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

func TestAuthenticate(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	if _, err := svc.InitializeAdmin(ctx, InitAdminInput{Username: "admin", Password: "mot-de-passe-long"}); err != nil {
		t.Fatal(err)
	}
	hash, err := crypto.HashPassword("mot-de-passe-bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := store.InsertUser(ctx, tx, store.User{Username: "bob", DisplayName: "bob", Role: "viewer", PasswordHash: hash})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	setOIDC := func(c store.OIDCConfig) {
		t.Helper()
		if err := st.Tx(ctx, func(tx *sql.Tx) error { return store.SaveOIDCConfig(ctx, tx, c) }); err != nil {
			t.Fatal(err)
		}
	}
	auditCount := func(reason string) int {
		var n int
		q := `SELECT count(*) FROM audit_events WHERE action='user.login_failed' AND diff LIKE ?`
		if err := st.DB.QueryRow(q, "%"+reason+"%").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	cases := []struct {
		name, user, pass string
		oidc             store.OIDCConfig
		want             error
		reason           string
	}{
		{"succès", "bob", "mot-de-passe-bob", store.OIDCConfig{}, nil, ""},
		{"mauvais mot de passe", "bob", "faux", store.OIDCConfig{}, ErrInvalidCredentials, ""},
		{"utilisateur inconnu", "nul", "mot-de-passe-bob", store.OIDCConfig{}, ErrInvalidCredentials, ""},
		{"SSO obligatoire", "bob", "mot-de-passe-bob", store.OIDCConfig{Enabled: true, LocalLoginDisabled: true}, ErrLocalLoginDisabled, "local_login_disabled"},
		{"SSO obligatoire, même un mauvais mot de passe", "bob", "faux", store.OIDCConfig{Enabled: true, LocalLoginDisabled: true}, ErrLocalLoginDisabled, "local_login_disabled"},
		{"SSO inactif: le réglage est ignoré", "bob", "mot-de-passe-bob", store.OIDCConfig{LocalLoginDisabled: true}, nil, ""},
		{"réservé aux admins: bob refusé", "bob", "mot-de-passe-bob", store.OIDCConfig{Enabled: true, LocalPasswordAdminsOnly: true}, ErrLocalPasswordAdminsOnly, "local_password_disabled"},
		{"réservé aux admins: admin accepté", "admin", "mot-de-passe-long", store.OIDCConfig{Enabled: true, LocalPasswordAdminsOnly: true}, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setOIDC(tc.oidc)
			before := 0
			if tc.reason != "" {
				before = auditCount(tc.reason)
			}
			u, err := svc.Authenticate(ctx, LoginInput{Username: tc.user, Password: tc.pass, IP: "192.0.2.1"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("erreur = %v, attendu %v", err, tc.want)
			}
			if tc.want == nil && u.Username != tc.user {
				t.Fatalf("utilisateur = %q", u.Username)
			}
			if tc.reason != "" && auditCount(tc.reason) != before+1 {
				t.Fatalf("événement d'audit %q non écrit", tc.reason)
			}
		})
	}

	t.Run("compte désactivé", func(t *testing.T) {
		setOIDC(store.OIDCConfig{})
		if _, err := st.DB.Exec(`UPDATE users SET disabled=1 WHERE username='bob'`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(ctx, LoginInput{Username: "bob", Password: "mot-de-passe-bob"}); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("erreur = %v", err)
		}
	})
}
