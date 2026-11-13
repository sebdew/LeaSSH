package access_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/access"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

type env struct {
	ctx    context.Context
	st     *store.Store
	svc    *access.Service
	actor  store.User
	sg, cg store.Group
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &env{ctx: ctx, st: st, svc: access.New(st)}
	err = st.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if e.actor, err = store.InsertUser(ctx, tx, store.User{Username: "admin", DisplayName: "admin", Role: "admin", PasswordHash: "x"}); err != nil {
			return err
		}
		if e.sg, err = store.InsertGroup(ctx, tx, store.ServerGroups, store.Group{Slug: "sg", Name: "sg"}); err != nil {
			return err
		}
		e.cg, err = store.InsertGroup(ctx, tx, store.CommandGroups, store.Group{Slug: "cg", Name: "cg"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) audits(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := e.st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action=?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestKeyLifecycle(t *testing.T) {
	e := newEnv(t)
	yes := true
	future := time.Now().Add(48 * time.Hour).Format(time.RFC3339)

	for name, in := range map[string]access.KeyInput{
		"nom vide":      {Name: " "},
		"nom trop long": {Name: strings.Repeat("a", 121)},
		"date invalide": {Name: "k", ExpiresAt: "demain"},
		"date au passé": {Name: "k", ExpiresAt: "2001-01-01"},
	} {
		if _, _, err := e.svc.CreateKey(e.ctx, e.actor, "", in); err == nil {
			t.Fatalf("%s: refus attendu", name)
		}
	}
	if n := e.audits(t, "key.create"); n != 0 {
		t.Fatalf("aucun audit pour un refus: %d", n)
	}
	if _, _, err := e.svc.CreateKey(e.ctx, e.actor, "", access.KeyInput{Name: ""}); !errors.Is(err, access.ErrInvalidKeyName) {
		t.Fatalf("nom: %v", err)
	}
	if _, _, err := e.svc.CreateKey(e.ctx, e.actor, "", access.KeyInput{Name: "k", ExpiresAt: "2001-01-01"}); !errors.Is(err, access.ErrExpiryPast) {
		t.Fatalf("passé: %v", err)
	}
	if _, _, err := e.svc.CreateKey(e.ctx, e.actor, "", access.KeyInput{Name: "k", ExpiresAt: "demain"}); !errors.Is(err, access.ErrInvalidExpiry) {
		t.Fatalf("format: %v", err)
	}

	k, token, err := e.svc.CreateKey(e.ctx, e.actor, "192.0.2.1", access.KeyInput{Name: " agent ", ExpiresAt: future})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, crypto.TokenPrefix) || k.Name != "agent" || !k.CanPropose || k.Hash != crypto.HashToken(token) {
		t.Fatalf("clé: %+v token=%q", k, token)
	}
	// Le jeton ne figure ni dans l'audit ni dans la liste (hash et jeton jamais exposés).
	var diff string
	_ = e.st.DB.QueryRow(`SELECT diff FROM audit_events WHERE action='key.create'`).Scan(&diff)
	if strings.Contains(diff, token) || strings.Contains(diff, k.Hash) {
		t.Fatalf("secret dans l'audit: %s", diff)
	}
	if got, err := e.svc.Authenticate(e.ctx, token); err != nil || got.ID != k.ID {
		t.Fatalf("authentification: %v", err)
	}

	// Mise à jour: CanPropose et RuleIDs absents = inchangés.
	no := false
	upd, err := e.svc.UpdateKey(e.ctx, e.actor, "", k.ID, access.KeyInput{Name: "agent2", CanPropose: &no, Disabled: true})
	if err != nil || upd.Name != "agent2" || upd.CanPropose || !upd.Disabled {
		t.Fatalf("mise à jour: %+v %v", upd, err)
	}
	upd, err = e.svc.UpdateKey(e.ctx, e.actor, "", k.ID, access.KeyInput{Name: "agent2", CanPropose: &yes})
	if err != nil || !upd.CanPropose || upd.Disabled {
		t.Fatalf("réactivation: %+v %v", upd, err)
	}

	// Révocation: désactivée et retirée de toutes les règles.
	rule, err := e.svc.CreateRule(e.ctx, e.actor, "", access.RuleInput{Name: "r", Profile: "RO", ServerGroupIDs: []string{e.sg.ID}, CommandGroupIDs: []string{e.cg.ID}, KeyIDs: []string{k.ID}})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := e.svc.RevokeKey(e.ctx, e.actor, "", k.ID)
	if err != nil || !rev.Disabled || len(rev.RuleIDs) != 0 {
		t.Fatalf("révocation: %+v %v", rev, err)
	}
	rules, _ := e.svc.ListRules(e.ctx)
	if len(rules) != 1 || rules[0].ID != rule.ID || len(rules[0].KeyIDs) != 0 {
		t.Fatalf("la clé doit sortir des règles: %+v", rules)
	}

	if err := e.svc.DeleteKey(e.ctx, e.actor, "", k.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteKey(e.ctx, e.actor, "", k.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("suppression d'une clé inconnue: %v", err)
	}
	for _, a := range []string{"key.create", "key.update", "key.revoke", "key.delete"} {
		if e.audits(t, a) == 0 {
			t.Errorf("audit %s manquant", a)
		}
	}
	if keys, err := e.svc.ListKeys(e.ctx); err != nil || len(keys) != 0 {
		t.Fatalf("liste: %+v %v", keys, err)
	}
}

func TestRuleValidationAndLifecycle(t *testing.T) {
	e := newEnv(t)
	ok := access.RuleInput{Name: " r ", Profile: "RW", ServerGroupIDs: []string{e.sg.ID}, CommandGroupIDs: []string{e.cg.ID}}
	cases := []struct {
		name string
		mod  func(access.RuleInput) access.RuleInput
		want error
	}{
		{"nom vide", func(i access.RuleInput) access.RuleInput { i.Name = " "; return i }, access.ErrInvalidRuleName},
		{"profil", func(i access.RuleInput) access.RuleInput { i.Profile = "ADMIN"; return i }, access.ErrInvalidProfile},
		{"sans groupe de serveurs", func(i access.RuleInput) access.RuleInput { i.ServerGroupIDs = nil; return i }, access.ErrInvalidRule},
		{"sans groupe de commandes", func(i access.RuleInput) access.RuleInput { i.CommandGroupIDs = nil; return i }, access.ErrInvalidRule},
	}
	for _, tc := range cases {
		if _, err := e.svc.CreateRule(e.ctx, e.actor, "", tc.mod(ok)); !errors.Is(err, tc.want) {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	r, err := e.svc.CreateRule(e.ctx, e.actor, "", ok)
	if err != nil || r.Name != "r" || !r.WriteRequiresApproval || !r.Enabled {
		t.Fatalf("défauts sûrs (validation requise, active): %+v %v", r, err)
	}
	no := false
	ok.WriteRequiresApproval, ok.Enabled = &no, &no
	up, err := e.svc.UpdateRule(e.ctx, e.actor, "", r.ID, ok)
	if err != nil || up.ID != r.ID || up.WriteRequiresApproval || up.Enabled {
		t.Fatalf("mise à jour: %+v %v", up, err)
	}
	if _, err := e.svc.UpdateRule(e.ctx, e.actor, "", "inconnue", ok); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("règle inconnue: %v", err)
	}
	if err := e.svc.DeleteRule(e.ctx, e.actor, "", r.ID); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"rule.create", "rule.update", "rule.delete"} {
		if e.audits(t, a) != 1 {
			t.Errorf("audit %s: %d", a, e.audits(t, a))
		}
	}
}

func TestSimulateUnknownKey(t *testing.T) {
	e := newEnv(t)
	if _, _, err := e.svc.Simulate(e.ctx, "inconnue", "", time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("clé inconnue: %v", err)
	}
	k, _, err := e.svc.CreateKey(e.ctx, e.actor, "", access.KeyInput{Name: "k"})
	if err != nil {
		t.Fatal(err)
	}
	name, cells, err := e.svc.Simulate(e.ctx, k.ID, "", time.Now())
	if err != nil || name != "k" || len(cells) != 0 {
		t.Fatalf("simulation: %q %+v %v", name, cells, err)
	}
}
