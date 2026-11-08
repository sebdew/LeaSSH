package executions

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/access"
	"github.com/leassh-dev/leassh/internal/catalog"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/sshexec"
	"github.com/leassh-dev/leassh/internal/sshtest"
	"github.com/leassh-dev/leassh/internal/store"
)

type fixture struct {
	t     *testing.T
	st    *store.Store
	svc   *Service
	ssh   *sshtest.Server
	srv   store.Server
	key   store.MCPKey
	rule  store.AccessRule
	admin store.User
	cmds  map[string]store.Command
	ctx   context.Context
}

func i64(n int64) *int64 { return &n }

func newFixture(t *testing.T, profile string, wra bool, ttl time.Duration) *fixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := crypto.NewBox(bytes.Repeat([]byte{1}, 32))
	fs := sshtest.New(t)
	pool := sshexec.NewPool(time.Minute, 2)
	t.Cleanup(pool.Close)
	f := &fixture{t: t, st: st, ssh: fs, cmds: map[string]store.Command{}, ctx: ctx}
	f.svc = New(ctx, st, access.New(st), box, pool, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Duration { return ttl })
	t.Cleanup(f.svc.Wait)
	enc, _ := box.Seal(fs.ClientPEM)
	err = st.Tx(ctx, func(tx *sql.Tx) error {
		id, err := store.InsertIdentity(ctx, tx, store.Identity{Name: "i", Username: "u", AuthType: "key", PrivateKeyEnc: enc})
		if err != nil {
			return err
		}
		f.srv, err = store.InsertServer(ctx, tx, store.Server{Slug: "web-01", Name: "Web", Host: "127.0.0.1", Port: fs.Port, IdentityID: id.ID,
			HostKeyFingerprint: fs.Fingerprint, HostKeyStatus: "pinned", Enabled: true})
		if err != nil {
			return err
		}
		var ids []string
		for _, c := range []store.Command{
			{Slug: "t.read", Risk: catalog.RiskRead, Template: "echo hello"},
			{Slug: "t.write", Risk: catalog.RiskWrite, Template: "echo wrote"},
			{Slug: "t.destroy", Risk: catalog.RiskDestructive, Template: "echo destroyed"},
			{Slug: "t.lines", Risk: catalog.RiskRead, Template: "echo {{lines}}", Params: map[string]catalog.ParamDef{"lines": {Type: catalog.TypeInt, Min: i64(1), Max: i64(100)}}},
			{Slug: "t.fail", Risk: catalog.RiskRead, Template: "exit 4"},
		} {
			c.Name, c.Status, c.Source = c.Slug, "enabled", "manual"
			c, err = store.InsertCommand(ctx, tx, c)
			if err != nil {
				return err
			}
			f.cmds[c.Slug] = c
			ids = append(ids, c.ID)
		}
		sg, _ := store.InsertGroup(ctx, tx, store.ServerGroups, store.Group{Slug: "sg", Name: "sg", MemberIDs: []string{f.srv.ID}})
		cg, _ := store.InsertGroup(ctx, tx, store.CommandGroups, store.Group{Slug: "cg", Name: "cg", MemberIDs: ids})
		f.key, err = store.InsertKey(ctx, tx, store.MCPKey{Name: "Agent N1", Prefix: "lsh_test", Hash: "h", CanPropose: true})
		if err != nil {
			return err
		}
		f.rule, err = store.InsertRule(ctx, tx, store.AccessRule{Name: "rule", ServerGroupIDs: []string{sg.ID}, CommandGroupIDs: []string{cg.ID}, Profile: profile, WriteRequiresApproval: wra, Enabled: true, KeyIDs: []string{f.key.ID}})
		if err != nil {
			return err
		}
		f.admin, err = store.InsertUser(ctx, tx, store.User{Username: "sd", DisplayName: "SD", Role: "admin", PasswordHash: "x"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) run(cmd string, params map[string]any) Outcome {
	f.t.Helper()
	k := f.key
	o, err := f.svc.Run(f.ctx, Request{Actor: Actor{Key: &k, IP: "10.0.0.9", SessionID: "s1"}, Server: "web-01", Command: cmd, Params: params, Reason: "alarme #42"})
	if err != nil {
		f.t.Fatalf("Run: %v", err)
	}
	return o
}

func (f *fixture) get(id string) store.Execution {
	f.t.Helper()
	e, err := store.ExecutionByID(f.ctx, f.st.DB, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

func TestDirectRead(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	o := f.run("t.lines", map[string]any{"lines": 7})
	e := f.get(o.Exec.ID)
	if e.Status != StSucceeded || strings.TrimSpace(e.Stdout) != "7" || e.Rendered == nil || *e.Rendered != "echo '7'" ||
		e.KeyName != "Agent N1" || e.RuleName != "rule" || e.ClientIP != "10.0.0.9" || e.Reason != "alarme #42" || e.DurationMS == nil || e.CommandHash == "" {
		t.Fatalf("exécution journalisée: %+v", e)
	}
	if o := f.run("t.fail", nil); f.get(o.Exec.ID).Status != StFailed || *f.get(o.Exec.ID).ExitCode != 4 {
		t.Fatalf("échec attendu: %+v", f.get(o.Exec.ID))
	}
}

// Critère 15.1 + 15.5: un refus est journalisé avec la clé, le serveur, la commande et la raison.
func TestDenialsAreLogged(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	o := f.run("t.write", nil)
	e := f.get(o.Exec.ID)
	if e.Status != StDenied || e.DenialReason != "profile_forbids_risk" || e.KeyName != "Agent N1" || e.ServerSlug != "web-01" || e.CommandSlug != "t.write" {
		t.Fatalf("refus profil: %+v", e)
	}
	if o.DenialMessage != GenericDenial {
		t.Fatalf("message: %q", o.DenialMessage)
	}
	unknown := f.run("nope.nope", nil)
	if unknown.DenialMessage != GenericDenial || f.get(unknown.Exec.ID).DenialReason != "target_not_found" {
		t.Fatalf("inconnu: %+v", unknown)
	}
	// l'agent ne peut pas distinguer « inexistant » de « interdit »
	if unknown.DenialMessage != o.DenialMessage {
		t.Fatal("les messages diffèrent: fuite d'existence")
	}
	bad := f.run("t.lines", map[string]any{"lines": 50000})
	if f.get(bad.Exec.ID).DenialReason != "param_invalid:lines" || !strings.Contains(bad.DenialMessage, "entre 1 et 100") || f.get(bad.Exec.ID).Rendered != nil {
		t.Fatalf("paramètre invalide: %+v", bad)
	}
	k := f.key
	r, _ := f.svc.Run(f.ctx, Request{Actor: Actor{Key: &k}, Server: "web-01", Command: "t.read", Reason: "  "})
	if r.Exec.DenialReason != "reason_invalid" {
		t.Fatalf("raison vide: %+v", r.Exec)
	}
	list, _ := store.ListExecutions(f.ctx, f.st.DB, store.ExecFilter{Statuses: []string{StDenied}})
	if len(list) != 4 {
		t.Fatalf("%d refus journalisés, 4 attendus", len(list))
	}
}

func TestExpiredAndDisabledKeyLogged(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	f.key.ExpiresAt = store.FormatTime(time.Now().Add(-time.Minute))
	if e := f.get(f.run("t.read", nil).Exec.ID); e.DenialReason != "key_expired" {
		t.Fatalf("expirée: %+v", e)
	}
	f.key.ExpiresAt, f.key.Disabled = "", true
	if e := f.get(f.run("t.read", nil).Exec.ID); e.DenialReason != "key_inactive" {
		t.Fatalf("désactivée: %+v", e)
	}
}

// Critère 15.2: destructive passe toujours par la validation, même RW sans exigence d'approbation.
func TestApprovalFlow(t *testing.T) {
	f := newFixture(t, "RW", false, time.Hour)
	if e := f.get(f.run("t.write", nil).Exec.ID); e.Status != StSucceeded {
		t.Fatalf("write sans approbation requise: %+v", e)
	}
	o := f.run("t.destroy", nil)
	if o.Exec.Status != StPending || o.Exec.Rendered == nil {
		t.Fatalf("attendu pending: %+v", o.Exec)
	}
	viewer := store.User{ID: "v", Username: "v", Role: "viewer"}
	if _, err := f.svc.Decide(f.ctx, o.Exec.ID, viewer, true, "", "web-01"); err != ErrForbidden {
		t.Fatalf("viewer: %v", err)
	}
	if _, err := f.svc.Decide(f.ctx, o.Exec.ID, f.admin, true, "", "autre"); err != ErrConfirm {
		t.Fatalf("confirmation du slug: %v", err)
	}
	e, err := f.svc.Decide(f.ctx, o.Exec.ID, f.admin, true, "OK", "web-01")
	if err != nil || e.DecidedBy != f.admin.ID {
		t.Fatalf("approbation: %+v %v", e, err)
	}
	if _, err := f.svc.Decide(f.ctx, o.Exec.ID, f.admin, true, "", "web-01"); err != ErrNotPending {
		t.Fatalf("double approbation: %v", err)
	}
	f.svc.Wait()
	done := f.get(o.Exec.ID)
	if done.Status != StSucceeded || strings.TrimSpace(done.Stdout) != "destroyed" || done.DecidedByName != "sd" || done.DecisionComment != "OK" {
		t.Fatalf("résultat: %+v", done)
	}
}

func TestRejectNeedsComment(t *testing.T) {
	f := newFixture(t, "RW", true, time.Hour)
	o := f.run("t.write", nil)
	if o.Exec.Status != StPending {
		t.Fatalf("write+wra doit attendre: %+v", o.Exec)
	}
	if _, err := f.svc.Decide(f.ctx, o.Exec.ID, f.admin, false, " ", ""); err != ErrNeedComment {
		t.Fatalf("commentaire: %v", err)
	}
	e, err := f.svc.Decide(f.ctx, o.Exec.ID, f.admin, false, "non", "")
	if err != nil || f.get(o.Exec.ID).Status != StRejected || e.Stdout != "" {
		t.Fatalf("rejet: %+v %v", e, err)
	}
}

func TestRevalidationOnApproval(t *testing.T) {
	// la commande est modifiée entre la demande et l'approbation
	f := newFixture(t, "RW", true, time.Hour)
	o := f.run("t.write", nil)
	c := f.cmds["t.write"]
	c.Template = "echo changed"
	f.st.Tx(f.ctx, func(tx *sql.Tx) error { _, _, err := store.UpdateCommand(f.ctx, tx, c); return err })
	e, _ := f.svc.Decide(f.ctx, o.Exec.ID, f.admin, true, "", "")
	if e.Status != StError || !strings.HasPrefix(e.DenialReason, "revalidation_failed") || strings.Contains(e.Stdout, "changed") {
		t.Fatalf("revalidation (hash): %+v", e)
	}
	// la règle est supprimée entre la demande et l'approbation
	o2 := f.run("t.read", nil)
	_ = o2
	o3 := f.run("t.destroy", nil)
	f.st.Tx(f.ctx, func(tx *sql.Tx) error { return store.DeleteRule(f.ctx, tx, f.rule.ID) })
	e, _ = f.svc.Decide(f.ctx, o3.Exec.ID, f.admin, true, "", "web-01")
	if e.Status != StError || !strings.Contains(e.DenialReason, "no_matching_rule") {
		t.Fatalf("revalidation (règle): %+v", e)
	}
}

func TestPendingExpires(t *testing.T) {
	f := newFixture(t, "RW", true, 50*time.Millisecond)
	o := f.run("t.write", nil)
	time.Sleep(120 * time.Millisecond)
	f.svc.ExpireNow(f.ctx)
	if e := f.get(o.Exec.ID); e.Status != StExpired {
		t.Fatalf("expiration: %+v", e)
	}
	if _, err := f.svc.Decide(f.ctx, o.Exec.ID, f.admin, true, "", ""); err != ErrNotPending {
		t.Fatalf("décision après expiration: %v", err)
	}
}

// Critère 15.7: un changement de clé d'hôte bloque le serveur et apparaît dans l'audit.
func TestHostKeyMismatchBlocksServer(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	f.st.Tx(f.ctx, func(tx *sql.Tx) error { return store.SetHostKey(f.ctx, tx, f.srv.ID, "SHA256:ancienne", "pinned") })
	e := f.get(f.run("t.read", nil).Exec.ID)
	if e.Status != StError || e.DenialReason != "host_key_mismatch" || e.Stdout != "" {
		t.Fatalf("divergence: %+v", e)
	}
	s, _ := store.ServerByID(f.ctx, f.st.DB, f.srv.ID)
	if s.HostKeyStatus != "mismatch" {
		t.Fatalf("serveur non bloqué: %s", s.HostKeyStatus)
	}
	if e2 := f.get(f.run("t.read", nil).Exec.ID); e2.Status != StDenied || e2.DenialReason != "host_key_not_pinned" {
		t.Fatalf("serveur bloqué: %+v", e2)
	}
	var n int
	f.st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action='host_key.mismatch'`).Scan(&n)
	if n != 1 {
		t.Fatalf("événements d'audit: %d", n)
	}
}

func TestAdminTest(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	admin := f.admin
	o, err := f.svc.Run(f.ctx, Request{Actor: Actor{User: &admin}, Server: "web-01", Command: "t.destroy", Reason: "test éditeur"})
	if err != nil {
		t.Fatal(err)
	}
	e := f.get(o.Exec.ID)
	if e.Status != StSucceeded || e.ActorUserID != admin.ID || e.KeyID != "" || e.RuleName != "test administrateur" {
		t.Fatalf("test admin: %+v", e)
	}
	viewer := store.User{ID: "v", Role: "viewer"}
	o, _ = f.svc.Run(f.ctx, Request{Actor: Actor{User: &viewer}, Server: "web-01", Command: "t.read", Reason: "x"})
	if o.Exec.Status != StDenied {
		t.Fatalf("un non-admin ne peut pas tester: %+v", o.Exec)
	}
}

func TestChainHashesPresent(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	f.run("t.read", nil)
	f.run("t.read", nil)
	var prev, row, prev2 string
	f.st.DB.QueryRow(`SELECT row_hash FROM executions WHERE seq=1`).Scan(&row)
	f.st.DB.QueryRow(`SELECT prev_hash,row_hash FROM executions WHERE seq=2`).Scan(&prev, &prev2)
	if row == "" || prev != row || prev2 == row {
		t.Fatalf("chaîne: %q %q %q", row, prev, prev2)
	}
}

// Derrière un bastion comme Warpgate, l'utilisateur SSH est propre à chaque cible (« utilisateur:cible »).
func TestServerSSHUserOverridesIdentityUser(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	last := func() string { f.ssh.Lock(); defer f.ssh.Unlock(); return f.ssh.LastUser }
	if e := f.get(f.run("t.read", nil).Exec.ID); e.Status != StSucceeded || last() != "u" {
		t.Fatalf("utilisateur de l'identité: %q (%s)", last(), e.Status)
	}
	f.st.DB.Exec(`UPDATE servers SET ssh_user='ia:resa' WHERE id=?`, f.srv.ID)
	f.svc.pool.Invalidate(f.srv.ID)
	if e := f.get(f.run("t.read", nil).Exec.ID); e.Status != StSucceeded || last() != "ia:resa" {
		t.Fatalf("utilisateur du serveur: %q (%s)", last(), e.Status)
	}
}

func TestCooldownAfterSSHAuthFailureIsJournaled(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	other, _, _ := sshexec.GenerateKey("autre")
	box, _ := crypto.NewBox(bytes.Repeat([]byte{1}, 32))
	enc, _ := box.Seal(other)
	f.st.DB.Exec(`UPDATE identities SET private_key_enc=?`, enc) // l'identité ne correspond plus à la clé autorisée
	first := f.get(f.run("t.read", nil).Exec.ID)
	if first.Status != StError || first.DenialReason != "ssh_error" || !strings.Contains(first.Stderr, "unable to authenticate") {
		t.Fatalf("premier essai: %+v", first)
	}
	f.ssh.Lock()
	before := f.ssh.Accepted
	f.ssh.Unlock()
	second := f.get(f.run("t.read", nil).Exec.ID)
	f.ssh.Lock()
	after := f.ssh.Accepted
	f.ssh.Unlock()
	if second.Status != StError || second.DenialReason != "ssh_cooldown" || !strings.Contains(second.Stderr, "pause") || after != before {
		t.Fatalf("deuxième essai (pas de nouvelle connexion): %+v (%d→%d)", second, before, after)
	}
}

// Une anomalie « injoignable » disparaît dès qu'une connexion réussit (et ne dépend plus de la présence d'une exécution réussie).
func TestSSHAnomalyClearsWhenConnectionWorksAgain(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	anomalies := func() []store.Anomaly {
		d, err := store.Dashboard(f.ctx, f.st.DB, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return d.Anomalies
	}
	good := f.get(f.run("t.read", nil).Exec.ID)
	if good.Status != StSucceeded || len(anomalies()) != 0 {
		t.Fatalf("serveur sain: %v %v", good.Status, anomalies())
	}
	// panne d'authentification
	other, _, _ := sshexec.GenerateKey("autre")
	box, _ := crypto.NewBox(bytes.Repeat([]byte{1}, 32))
	bad, _ := box.Seal(other)
	var orig []byte
	f.st.DB.QueryRow(`SELECT private_key_enc FROM identities`).Scan(&orig)
	f.st.DB.Exec(`UPDATE identities SET private_key_enc=?`, bad)
	f.svc.pool.Invalidate(f.srv.ID)
	time.Sleep(5 * time.Millisecond)
	f.run("t.read", nil)
	if a := anomalies(); len(a) != 1 || a[0].Slug != "web-01" || a[0].Reason != "ssh_unreachable" {
		t.Fatalf("anomalie attendue: %v", a)
	}
	// la pause (cooldown) n'ajoute pas d'échec et ne change rien
	f.run("t.read", nil)
	// réparation: une connexion qui réussit efface l'anomalie, sans qu'une exécution « réussie » soit nécessaire
	f.st.DB.Exec(`UPDATE identities SET private_key_enc=?`, orig)
	f.svc.pool.Invalidate(f.srv.ID)
	time.Sleep(5 * time.Millisecond)
	if e := f.get(f.run("t.fail", nil).Exec.ID); e.Status != StFailed { // code retour 4: la connexion SSH, elle, fonctionne
		t.Fatalf("exécution après réparation: %+v", e)
	}
	if a := anomalies(); len(a) != 0 {
		t.Fatalf("l'anomalie devrait avoir disparu: %v", a)
	}
}

// Modifier la connexion d'un serveur (ex. nouvel utilisateur) efface l'ancien échec.
func TestEditingConnectionClearsStaleSSHFailure(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	store.MarkSSH(f.ctx, f.st.DB, f.srv.ID, false)
	if d, _ := store.Dashboard(f.ctx, f.st.DB, time.Now()); len(d.Anomalies) != 1 {
		t.Fatalf("anomalie attendue: %v", d.Anomalies)
	}
	f.st.Tx(f.ctx, func(tx *sql.Tx) error {
		s := f.srv
		s.Description = "juste une description"
		_, _, err := store.UpdateServer(f.ctx, tx, s)
		return err
	})
	if d, _ := store.Dashboard(f.ctx, f.st.DB, time.Now()); len(d.Anomalies) != 1 {
		t.Fatal("une simple description ne doit pas effacer l'échec")
	}
	f.st.Tx(f.ctx, func(tx *sql.Tx) error {
		s := f.srv
		s.SSHUser = "ia:autre"
		_, _, err := store.UpdateServer(f.ctx, tx, s)
		return err
	})
	if d, _ := store.Dashboard(f.ctx, f.st.DB, time.Now()); len(d.Anomalies) != 0 {
		t.Fatalf("changer l'utilisateur SSH doit effacer l'échec: %v", d.Anomalies)
	}
}

// SEC-03 (#5): une exécution restée « running » après un plantage passe en erreur (interrupted) au démarrage.
func TestReconcileInterruptedRunning(t *testing.T) {
	f := newFixture(t, "RO", true, time.Hour)
	id := f.run("t.read", nil).Exec.ID
	f.svc.Wait()
	old := store.FormatTime(time.Now().Add(-time.Hour))
	if _, err := f.st.DB.ExecContext(f.ctx, `UPDATE executions SET status='running',finished_at=NULL,exit_code=NULL,started_at=? WHERE id=?`, old, id); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ReconcileRunning(f.ctx); err != nil {
		t.Fatal(err)
	}
	e := f.get(id)
	if e.Status != StError || e.DenialReason != "interrupted" || e.FinishedAt == "" {
		t.Fatalf("exécution orpheline non réconciliée: %+v", e)
	}
}

// SEC-03 (#5): si l'écriture d'audit échoue, la décision n'est pas appliquée.
func TestDecideAtomicWithAudit(t *testing.T) {
	f := newFixture(t, "RW", true, time.Hour)
	o := f.run("t.write", nil)
	if _, err := f.st.DB.ExecContext(f.ctx, `CREATE TRIGGER fail_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT,'audit indisponible'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Decide(f.ctx, o.Exec.ID, f.admin, true, "OK", ""); err == nil {
		t.Fatal("la décision doit échouer si l'audit échoue")
	}
	f.svc.Wait()
	if e := f.get(o.Exec.ID); e.Status != StPending || e.DecidedBy != "" || e.Stdout != "" {
		t.Fatalf("décision appliquée malgré l'échec de l'audit: %+v", e)
	}
}
