package infra

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/leassh-dev/leassh/internal/catalog"
	"github.com/leassh-dev/leassh/internal/sshexec"
	"github.com/leassh-dev/leassh/internal/store"
)

type fakeBox struct{}

func (fakeBox) Seal(p []byte) ([]byte, error) { return append([]byte("SEALED:"), p...), nil }

type fakePool struct{ invalidated []string }

func (p *fakePool) Invalidate(id string) { p.invalidated = append(p.invalidated, id) }

type noTarget struct{}

func (noTarget) TargetFor(context.Context, store.Server) (sshexec.Target, error) {
	return sshexec.Target{}, errors.New("non utilisé")
}

type env struct {
	ctx   context.Context
	svc   *Service
	st    *store.Store
	pool  *fakePool
	actor store.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &env{ctx: ctx, st: st, pool: &fakePool{}}
	e.svc = New(st, fakeBox{}, e.pool, noTarget{})
	err = st.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		e.actor, err = store.InsertUser(ctx, tx, store.User{Username: "admin", DisplayName: "admin", Role: "admin", PasswordHash: "x"})
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

func TestIdentities(t *testing.T) {
	e := newEnv(t)
	for name, in := range map[string]IdentityInput{
		"nom vide":     {Name: " ", Username: "u"},
		"user vide":    {Name: "n", Username: ""},
		"mode inconnu": {Name: "n", Username: "u", Mode: "telepathie"},
		"mdp vide":     {Name: "n", Username: "u", Mode: "password"},
		"clé invalide": {Name: "n", Username: "u", Mode: "import", PrivateKey: "pas une clé"},
	} {
		if _, _, err := e.svc.CreateIdentity(e.ctx, e.actor, "", in); err == nil {
			t.Fatalf("%s: refus attendu", name)
		}
	}
	if _, _, err := e.svc.CreateIdentity(e.ctx, e.actor, "", IdentityInput{Name: "n", Username: "u", Mode: "x"}); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("mode: %v", err)
	}
	var ke *KeyError
	if _, _, err := e.svc.CreateIdentity(e.ctx, e.actor, "", IdentityInput{Name: "n", Username: "u", Mode: "import", PrivateKey: "x"}); !errors.As(err, &ke) {
		t.Fatalf("clé: %v", err)
	}
	if n := e.audits(t, "identity.create"); n != 0 {
		t.Fatalf("aucun audit pour un refus: %d", n)
	}

	id, line, err := e.svc.CreateIdentity(e.ctx, e.actor, "192.0.2.1", IdentityInput{Name: " deploy ", Username: " svc "})
	if err != nil || id.AuthType != "key" || id.Name != "deploy" || id.Username != "svc" || !strings.HasPrefix(line, "echo 'ssh-ed25519 ") {
		t.Fatalf("génération: %+v %q %v", id, line, err)
	}
	if !bytes.HasPrefix(id.PrivateKeyEnc, []byte("SEALED:")) {
		t.Fatal("la clé privée doit être scellée")
	}
	pw, line, err := e.svc.CreateIdentity(e.ctx, e.actor, "", IdentityInput{Name: "pw", Username: "u", Mode: "password", Password: "s3cret-pw"})
	if err != nil || pw.AuthType != "password" || line != "" || string(pw.PasswordEnc) != "SEALED:s3cret-pw" {
		t.Fatalf("mot de passe: %+v %q %v", pw, line, err)
	}
	// Aucun secret dans l'audit.
	rows, _ := e.st.DB.Query(`SELECT diff FROM audit_events WHERE action='identity.create'`)
	defer rows.Close()
	for rows.Next() {
		var d string
		_ = rows.Scan(&d)
		if strings.Contains(d, "s3cret-pw") || strings.Contains(d, "PRIVATE KEY") || strings.Contains(d, "SEALED") {
			t.Fatalf("secret dans l'audit: %s", d)
		}
	}
	if list, err := e.svc.ListIdentities(e.ctx); err != nil || len(list) != 2 {
		t.Fatalf("liste: %v", err)
	}
	if err := e.svc.DeleteIdentity(e.ctx, e.actor, "", pw.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteIdentity(e.ctx, e.actor, "", pw.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("suppression d'une identité inconnue: %v", err)
	}
}

func TestServerValidationAndLifecycle(t *testing.T) {
	e := newEnv(t)
	id, _, err := e.svc.CreateIdentity(e.ctx, e.actor, "", IdentityInput{Name: "n", Username: "u"})
	if err != nil {
		t.Fatal(err)
	}
	ok := ServerInput{Slug: "web-01", Name: "web", Host: "10.0.0.1", IdentityID: id.ID}
	cases := []struct {
		name string
		mod  func(ServerInput) ServerInput
		want error
	}{
		{"slug", func(i ServerInput) ServerInput { i.Slug = "Web 01"; return i }, catalog.ErrInvalidSlug},
		{"nom", func(i ServerInput) ServerInput { i.Name = " "; return i }, ErrInvalidName},
		{"hôte vide", func(i ServerInput) ServerInput { i.Host = ""; return i }, ErrInvalidHost},
		{"hôte avec espace", func(i ServerInput) ServerInput { i.Host = "a b"; return i }, ErrInvalidHost},
		{"hôte avec chemin", func(i ServerInput) ServerInput { i.Host = "a/b"; return i }, ErrInvalidHost},
		{"port", func(i ServerInput) ServerInput { i.Port = 70000; return i }, ErrInvalidPort},
		{"identité requise", func(i ServerInput) ServerInput { i.IdentityID = ""; return i }, ErrIdentityRequired},
		{"identité inconnue", func(i ServerInput) ServerInput { i.IdentityID = "nope"; return i }, ErrUnknownIdentity},
		{"utilisateur SSH", func(i ServerInput) ServerInput { i.SSHUser = "a\x00b"; return i }, ErrInvalidSSHUser},
	}
	for _, tc := range cases {
		if _, err := e.svc.CreateServer(e.ctx, e.actor, "", tc.mod(ok)); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	// Warpgate: utilisateur SSH avec espaces permis.
	wg := ok
	wg.SSHUser = "ia:reservations-dev - LXC ct145"
	sv, err := e.svc.CreateServer(e.ctx, e.actor, "", wg)
	if err != nil || sv.Port != 22 || !sv.Enabled || sv.SSHUser != wg.SSHUser {
		t.Fatalf("création: %+v %v", sv, err)
	}
	no := false
	up := ok
	up.Name, up.Enabled = "web2", &no
	got, err := e.svc.UpdateServer(e.ctx, e.actor, "", sv.ID, up)
	if err != nil || got.Name != "web2" || got.Enabled {
		t.Fatalf("mise à jour: %+v %v", got, err)
	}
	if len(e.pool.invalidated) == 0 || e.pool.invalidated[len(e.pool.invalidated)-1] != sv.ID {
		t.Fatal("le pool SSH doit être invalidé après une mise à jour")
	}
	if err := e.svc.DeleteServer(e.ctx, e.actor, "", sv.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.pool.invalidated) != 2 {
		t.Fatalf("invalidations: %v", e.pool.invalidated)
	}
	for _, a := range []string{"server.create", "server.update", "server.delete"} {
		if e.audits(t, a) != 1 {
			t.Errorf("audit %s: %d", a, e.audits(t, a))
		}
	}
}

func TestSynchronisedServerGuards(t *testing.T) {
	e := newEnv(t)
	id, _, _ := e.svc.CreateIdentity(e.ctx, e.actor, "", IdentityInput{Name: "n", Username: "u"})
	in := ServerInput{Slug: "wg-1", Name: "wg", Host: "10.0.0.2", IdentityID: id.ID, SSHUser: "u:t"}
	sv, err := e.svc.CreateServer(e.ctx, e.actor, "", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.DB.Exec(`UPDATE servers SET managed_by='warpgate' WHERE id=?`, sv.ID); err != nil {
		t.Fatal(err)
	}
	renamed := in
	renamed.Name = "autre"
	if _, err := e.svc.UpdateServer(e.ctx, e.actor, "", sv.ID, renamed); !errors.Is(err, ErrManagedLocked) {
		t.Fatalf("modification: %v", err)
	}
	if err := e.svc.DeleteServer(e.ctx, e.actor, "", sv.ID); !errors.Is(err, ErrManagedUndeletable) {
		t.Fatalf("suppression: %v", err)
	}
	// L'utilisateur SSH reste modifiable et devient « choix d'un admin ».
	custom := in
	custom.SSHUser = "u:autre"
	got, err := e.svc.UpdateServer(e.ctx, e.actor, "", sv.ID, custom)
	if err != nil || got.SSHUser != "u:autre" || !got.SSHUserCustom {
		t.Fatalf("utilisateur SSH: %+v %v", got, err)
	}
	// Rendre la main à la synchro.
	follow := custom
	follow.SSHUserFollowSync = true
	if got, err = e.svc.UpdateServer(e.ctx, e.actor, "", sv.ID, follow); err != nil || got.SSHUserCustom || got.SSHUser != "u:autre" {
		t.Fatalf("retour à la synchro: %+v %v", got, err)
	}
}

func TestHostKeyUnreachable(t *testing.T) {
	e := newEnv(t)
	id, _, _ := e.svc.CreateIdentity(e.ctx, e.actor, "", IdentityInput{Name: "n", Username: "u"})
	sv, err := e.svc.CreateServer(e.ctx, e.actor, "", ServerInput{Slug: "down", Name: "down", Host: "127.0.0.1", Port: 1, IdentityID: id.ID})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.TestConnection(e.ctx, sv.ID, "")
	if err != nil || res.Reachable || res.Error == "" {
		t.Fatalf("test de connexion: %+v %v", res, err)
	}
	var ue *UnreachableError
	if _, err := e.svc.PinHostKey(e.ctx, e.actor, "", sv.ID, "SHA256:x"); !errors.As(err, &ue) {
		t.Fatalf("épinglage d'un hôte injoignable: %v", err)
	}
	if _, err := e.svc.TestConnection(e.ctx, "inconnu", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("serveur inconnu: %v", err)
	}
	if n := e.audits(t, "server.trust_host_key"); n != 0 {
		t.Fatalf("aucun audit sans épinglage: %d", n)
	}
}
