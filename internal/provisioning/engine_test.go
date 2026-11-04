package provisioning

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

type fakeConn struct{ ms *[]Machine }

func (f fakeConn) Discover(context.Context) ([]Machine, error) { return *f.ms, nil }

type env struct {
	t    *testing.T
	ctx  context.Context
	st   *store.Store
	svc  *Service
	p    store.Provisioner
	rule store.AccessRule
	ms   []Machine
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, ctx: context.Background()}
	e.ms = []Machine{}
	Register(Type{ID: "fake", Label: "Fake", New: func(map[string]any, string, Env) (Connector, error) { return fakeConn{&e.ms}, nil }})
	var err error
	if e.st, err = store.Open(e.ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.st.Close() })
	box, _ := crypto.NewBox(bytes.Repeat([]byte{1}, 32))
	e.svc = New(e.st, box, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err = e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		id, err := store.InsertIdentity(e.ctx, tx, store.Identity{Name: "wg", Username: "ia", AuthType: "key"})
		if err != nil {
			return err
		}
		if e.rule, err = store.InsertRule(e.ctx, tx, store.AccessRule{Name: "défaut", Profile: "RO", Enabled: true}); err != nil {
			return err
		}
		e.p, err = store.InsertProvisioner(e.ctx, tx, store.Provisioner{Name: "bastion", Type: "fake", Enabled: true, IdentityID: id.ID, Config: map[string]any{}, RuleIDs: []string{e.rule.ID}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func m(ext, name, grp string) Machine {
	x := Machine{ExternalID: ext, Name: name, Description: "desc " + name, Host: "10.0.0.1", Port: 2222, SSHUser: "ia:" + name, HostKeyFingerp: "SHA256:abc"}
	if grp != "" {
		x.GroupID, x.GroupName = "g-"+grp, grp
	}
	return x
}

func (e *env) sync(force bool) (Report, error) {
	e.t.Helper()
	return e.svc.Sync(e.ctx, e.p.ID, force, Actor{Type: "user", Name: "admin"})
}

func (e *env) must(force bool) Report {
	e.t.Helper()
	r, err := e.sync(force)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *env) server(name string) store.Server {
	e.t.Helper()
	all, _ := store.ListServers(e.ctx, e.st.DB)
	for _, s := range all {
		if s.Name == name {
			return s
		}
	}
	e.t.Fatalf("serveur %q introuvable", name)
	return store.Server{}
}

func (e *env) group(name string) (store.Group, bool) {
	gs, _ := store.ListGroups(e.ctx, e.st.DB, store.ServerGroups)
	for _, g := range gs {
		if g.Name == name {
			return g, true
		}
	}
	return store.Group{}, false
}

func TestSyncCreatesServersGroupsAndDefaultRule(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "pve01 - SSH root", "Hyperviseurs"), m("2", "pve04 - SSH root", "Hyperviseurs"), m("3", "seul", "")}
	rep := e.must(false)
	if rep.Machines != 3 {
		t.Fatalf("machines: %d", rep.Machines)
	}
	sv := e.server("pve01 - SSH root")
	if sv.Slug != "pve01-ssh-root" || sv.Description != "desc pve01 - SSH root" || sv.SSHUser != "ia:pve01 - SSH root" || sv.Port != 2222 ||
		sv.HostKeyStatus != "pinned" || sv.ManagedBy != e.p.ID || sv.IdentityID != e.p.IdentityID || !sv.Enabled {
		t.Fatalf("serveur: %+v", sv)
	}
	g, ok := e.group("Hyperviseurs")
	if !ok || g.ManagedBy != e.p.ID || len(g.MemberIDs) != 2 || len(g.ManagedMemberIDs) != 2 {
		t.Fatalf("groupe: %+v", g)
	}
	if solo := e.server("seul"); solo.ManagedBy == "" {
		t.Fatal("serveur sans groupe non importé")
	}
	if gs, _ := store.ListGroups(e.ctx, e.st.DB, store.ServerGroups); len(gs) != 1 {
		t.Fatalf("sans groupe source, aucun groupe ne doit être créé: %d", len(gs))
	}
	r, _ := store.RuleByID(e.ctx, e.st.DB, e.rule.ID)
	if !slices.Equal(r.ServerGroupIDs, []string{g.ID}) {
		t.Fatalf("règle par défaut: %v", r.ServerGroupIDs)
	}
	if again := e.must(false); len(again.Changes) != 0 {
		t.Fatalf("la deuxième synchro doit être sans effet: %+v", again.Changes)
	}
}

func TestPreviewWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "a", "G")}
	rep, err := e.svc.Preview(e.ctx, e.p.ID)
	if err != nil || len(rep.Changes) == 0 {
		t.Fatalf("aperçu: %+v %v", rep, err)
	}
	if all, _ := store.ListServers(e.ctx, e.st.DB); len(all) != 0 {
		t.Fatal("l'aperçu a écrit")
	}
}

func TestManualMembershipsSurviveAndManagedOnesAreLocked(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "a", "G"), m("2", "b", "G")}
	e.must(false)
	g, _ := e.group("G")
	a := e.server("a")
	// un hôte manuel rejoint le groupe synchronisé; un serveur synchronisé rejoint un groupe manuel
	var manual store.Server
	err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		var err error
		if manual, err = store.InsertServer(e.ctx, tx, store.Server{Slug: "manuel", Name: "manuel", Host: "h", IdentityID: e.p.IdentityID, Enabled: true}); err != nil {
			return err
		}
		if _, err = store.InsertGroup(e.ctx, tx, store.ServerGroups, store.Group{Slug: "mien", Name: "Mien", MemberIDs: []string{a.ID}}); err != nil {
			return err
		}
		// l'API reçoit une liste SANS les membres synchronisés: ils doivent rester
		return store.SetGroupMembers(e.ctx, tx, store.ServerGroups, g.ID, []string{manual.ID})
	})
	if err != nil {
		t.Fatal(err)
	}
	g, _ = e.group("G")
	if len(g.MemberIDs) != 3 || len(g.ManagedMemberIDs) != 2 {
		t.Fatalf("membres synchronisés non conservés: %+v", g)
	}
	e.must(false) // la synchro ne touche ni à l'hôte manuel ni au groupe manuel
	g, _ = e.group("G")
	mg, _ := e.group("Mien")
	if len(g.MemberIDs) != 3 || !slices.Contains(mg.MemberIDs, a.ID) || mg.ManagedBy != "" {
		t.Fatalf("la synchro a touché à des éléments manuels: %+v %+v", g, mg)
	}
}

func TestMachineMovesBetweenGroupsAndDisappears(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "a", "G1"), m("2", "b", "G1")}
	e.must(false)
	e.ms = []Machine{m("1", "a", "G2"), m("2", "b", "G1")} // a change de groupe
	e.must(false)
	g1, _ := e.group("G1")
	g2, _ := e.group("G2")
	if len(g1.MemberIDs) != 1 || len(g2.MemberIDs) != 1 || g2.MemberIDs[0] != e.server("a").ID {
		t.Fatalf("déplacement: %+v %+v", g1, g2)
	}
	e.ms = []Machine{m("1", "a", "G2")} // b disparaît: désactivé, pas supprimé
	e.must(false)
	if b := e.server("b"); b.Enabled || !b.SyncRemoved {
		t.Fatalf("b devrait être désactivé par la synchro: %+v", b)
	}
	if _, ok := e.group("G1"); ok {
		t.Fatal("G1, vide, devait disparaître avec sa dernière machine")
	}
	e.ms = []Machine{m("1", "a", "G2"), m("2", "b", "G1")} // b revient
	e.must(false)
	if b := e.server("b"); !b.Enabled || b.SyncRemoved {
		t.Fatalf("b devrait être réactivé: %+v", b)
	}
}

func TestAdminDisabledServerStaysDisabled(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "a", "")}
	e.must(false)
	a := e.server("a")
	a.Enabled = false
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error { _, _, err := store.UpdateServer(e.ctx, tx, a); return err }); err != nil {
		t.Fatal(err)
	}
	e.must(false)
	if e.server("a").Enabled {
		t.Fatal("la synchro ne doit pas réactiver un serveur désactivé à la main")
	}
}

func TestVanishedGroupWithManualMemberIsReleased(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "a", "G")}
	e.must(false)
	g, _ := e.group("G")
	err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		sv, err := store.InsertServer(e.ctx, tx, store.Server{Slug: "x", Name: "x", Host: "h", IdentityID: e.p.IdentityID, Enabled: true})
		if err != nil {
			return err
		}
		return store.SetGroupMembers(e.ctx, tx, store.ServerGroups, g.ID, []string{sv.ID})
	})
	if err != nil {
		t.Fatal(err)
	}
	e.ms = []Machine{m("1", "a", "")}
	e.must(false)
	g, ok := e.group("G")
	if !ok || g.ManagedBy != "" || len(g.MemberIDs) != 1 {
		t.Fatalf("le groupe doit être rendu à l'admin avec son membre manuel: %+v %v", g, ok)
	}
}

func TestMassRemovalIsBlockedUnlessForced(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "a", ""), m("2", "b", ""), m("3", "c", ""), m("4", "d", ""), m("5", "e", "")}
	e.must(false)
	e.ms = []Machine{m("5", "e", "")}
	_, err := e.sync(false)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("attendu ErrBlocked, obtenu %v", err)
	}
	for _, n := range []string{"a", "b", "c", "d"} {
		if !e.server(n).Enabled {
			t.Fatalf("%s désactivé malgré le blocage", n)
		}
	}
	e.ms = nil // source vide
	if _, err := e.sync(false); !errors.Is(err, ErrBlocked) {
		t.Fatalf("une source vide doit bloquer: %v", err)
	}
	e.ms = []Machine{m("5", "e", "")}
	e.must(true)
	if e.server("a").Enabled {
		t.Fatal("la synchro forcée devait désactiver a")
	}
}

func TestHostKeyMismatchIsNotSilentlyReset(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "a", "")}
	e.must(false)
	a := e.server("a")
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error { return store.SetHostKeyStatus(e.ctx, tx, a.ID, "mismatch") }); err != nil {
		t.Fatal(err)
	}
	e.must(false)
	if e.server("a").HostKeyStatus != "mismatch" {
		t.Fatal("une divergence de clé d'hôte ne doit pas être effacée par la synchro")
	}
	e.ms = []Machine{func() Machine { x := m("1", "a", ""); x.HostKeyFingerp = "SHA256:new"; return x }()}
	e.must(false) // l'admin a épinglé une nouvelle empreinte dans le provisioner
	if got := e.server("a"); got.HostKeyStatus != "pinned" || got.HostKeyFingerprint != "SHA256:new" {
		t.Fatalf("nouvelle empreinte non appliquée: %+v", got)
	}
}

func TestDeleteProvisionerReleasesEverything(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "a", "G")}
	e.must(false)
	err := e.st.Tx(e.ctx, func(tx *sql.Tx) error { return store.DeleteProvisioner(e.ctx, tx, e.p.ID) })
	if err != nil {
		t.Fatal(err)
	}
	g, _ := e.group("G")
	if sv := e.server("a"); sv.ManagedBy != "" || g.ManagedBy != "" || len(g.ManagedMemberIDs) != 0 || len(g.MemberIDs) != 1 {
		t.Fatalf("rien ne doit rester verrouillé: %+v %+v", sv, g)
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{"pve01 - SSH root": "pve01-ssh-root", "Gîte Été": "gite-ete", "!!!": "item", "reservations-dev - LXC ct145": "reservations-dev-lxc-ct145"} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q)=%q, attendu %q", in, got, want)
		}
	}
	taken := map[string]bool{}
	if a, b := uniqueSlug("x", taken), uniqueSlug("x", taken); a != "x" || b != "x-2" {
		t.Fatalf("unicité: %s %s", a, b)
	}
}

func TestCustomSSHUserSurvivesSync(t *testing.T) {
	e := newEnv(t)
	e.ms = []Machine{m("1", "a", "")}
	e.must(false)
	a := e.server("a")
	a.SSHUser, a.SSHUserCustom = "autre:a", true
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error { _, _, err := store.UpdateServer(e.ctx, tx, a); return err }); err != nil {
		t.Fatal(err)
	}
	if !e.server("a").SSHUserCustom {
		t.Fatal("drapeau perdu par UpdateServer")
	}
	renamed := m("1", "a2", "") // la cible est renommée: nom mis à jour, utilisateur personnalisé conservé
	e.ms = []Machine{renamed}
	e.must(false)
	if got := e.server("a2"); got.SSHUser != "autre:a" || got.Name != "a2" {
		t.Fatalf("utilisateur personnalisé écrasé: %+v", got)
	}
}
