package accounts

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionLifecycle(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if ok, err := svc.Initialized(ctx); err != nil || ok {
		t.Fatalf("instance vide: %v %v", ok, err)
	}
	u, err := svc.InitializeAdmin(ctx, InitAdminInput{Username: "admin", Password: "mot-de-passe-long"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := svc.Initialized(ctx); !ok {
		t.Fatal("l'instance doit être initialisée")
	}
	cookie, csrf, err := svc.OpenSession(ctx, u, "192.0.2.1", time.Hour)
	if err != nil || cookie == "" || csrf == "" || cookie == csrf {
		t.Fatalf("session: %q %q %v", cookie, csrf, err)
	}
	got, gotCSRF, err := svc.LookupSession(ctx, cookie, time.Hour)
	if err != nil || got.ID != u.ID || gotCSRF != csrf {
		t.Fatalf("lookup: %+v %q %v", got, gotCSRF, err)
	}
	// seul le haché est stocké
	var n int
	if err := svc.st.DB.QueryRow(`SELECT count(*) FROM sessions WHERE id_hash = ?`, cookie).Scan(&n); err != nil || n != 0 {
		t.Fatalf("le cookie ne doit pas être stocké en clair (%d, %v)", n, err)
	}
	if err := svc.CloseSession(ctx, cookie); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.LookupSession(ctx, cookie, time.Hour); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("après déconnexion: %v", err)
	}
	if _, _, err := svc.LookupSession(ctx, "inconnu", time.Hour); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("cookie inconnu: %v", err)
	}
}

func TestSessionRefusedForDisabledAccount(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	u, err := svc.InitializeAdmin(ctx, InitAdminInput{Username: "admin", Password: "mot-de-passe-long"})
	if err != nil {
		t.Fatal(err)
	}
	cookie, _, err := svc.OpenSession(ctx, u, "192.0.2.1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`UPDATE users SET disabled = 1 WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.LookupSession(ctx, cookie, time.Hour); !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("compte désactivé: %v", err)
	}
}

func TestLimiter(t *testing.T) {
	svc, st := newService(t)
	_ = svc
	l := NewLimiter(st, "account", 2, time.Minute)
	if l.Blocked("a") {
		t.Fatal("pas bloqué au départ")
	}
	l.Fail("a")
	l.Fail("a")
	if !l.Blocked("a") || l.Blocked("b") {
		t.Fatal("a doit être bloqué, pas b")
	}
	l.Reset("a")
	if l.Blocked("a") {
		t.Fatal("reset doit débloquer")
	}
}
