package syslog_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
	"github.com/leassh-dev/leassh/internal/syslog"
	"github.com/leassh-dev/leassh/internal/syslog/syslogtest"
)

type rig struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	f   *syslog.Forwarder
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := crypto.NewBox(bytes.Repeat([]byte{3}, 32))
	f := syslog.New(st, box, "1.2.3", slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.MinBackoff, f.MaxBackoff, f.AuditPoll = 10*time.Millisecond, 50*time.Millisecond, 20*time.Millisecond
	go f.Run(ctx)
	return &rig{t: t, ctx: ctx, st: st, f: f}
}

func (r *rig) save(in syslog.Input) {
	r.t.Helper()
	err := r.st.Tx(r.ctx, func(tx *sql.Tx) error {
		_, _, err := r.f.Save(r.ctx, tx, in)
		return err
	})
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.f.Reload(r.ctx); err != nil {
		r.t.Fatal(err)
	}
}

func cfgFor(s *syslogtest.Server) syslog.Config {
	c := syslog.Config{Enabled: true, Protocol: s.Proto, Host: s.Host, Port: s.Port}
	if s.Proto == "tls" {
		c.TLSCAPEM = s.CAPEM
	}
	return c
}

func exec(status string) store.Execution {
	code := 0
	ms := int64(12)
	rendered := "systemctl status nginx"
	return store.Execution{ID: "E1", Status: status, KeyName: "Agent N1", ServerSlug: "web-01", CommandSlug: "svc.status", Risk: "read", Rendered: &rendered,
		ExitCode: &code, DurationMS: &ms, ClientIP: "10.0.0.5", Stdout: "TOP-SECRET-OUTPUT", Stderr: "boom password=hunter2"}
}

var header = regexp.MustCompile(`^<(\d+)>1 (\d{4}-\d\d-\d\dT[\d:.]+Z) \S+ (\S+) \d+ (\S+) - (.*)$`)

func TestTransportsAndRFC5424(t *testing.T) {
	for _, proto := range []string{"udp", "tcp", "tls"} {
		t.Run(proto, func(t *testing.T) {
			srv := syslogtest.New(t, proto)
			r := newRig(t)
			c := cfgFor(srv)
			c.Facility, c.AppName = "local3", "leassh-prod"
			r.save(syslog.Input{Config: c})
			r.f.OnExecution("execution.finished", exec("succeeded"))
			m := header.FindStringSubmatch(srv.WaitFor(1)[0])
			if m == nil {
				t.Fatalf("pas du RFC 5424: %q", srv.Messages())
			}
			if m[1] != "158" { // local3 (19) * 8 + info (6)
				t.Fatalf("PRI = %s, attendu 158", m[1])
			}
			if m[3] != "leassh-prod" || m[4] != "execution.finished" {
				t.Fatalf("app-name/msgid: %v", m)
			}
			for _, want := range []string{"id=E1", `agent="Agent N1"`, "server=web-01", "command=svc.status", "exit_code=0", "stdout_bytes=17", `rendered="systemctl status nginx"`} {
				if !strings.Contains(m[5], want) {
					t.Errorf("%q absent de %q", want, m[5])
				}
			}
			all := strings.Join(srv.Messages(), "\n")
			if strings.Contains(all, "TOP-SECRET-OUTPUT") || strings.Contains(all, "hunter2") || strings.Contains(all, "boom") {
				t.Fatalf("sortie de commande ou secret dans le message: %q", all)
			}
		})
	}
}

func TestOctetCountingFraming(t *testing.T) {
	srv := syslogtest.New(t, "tcp")
	r := newRig(t)
	c := cfgFor(srv)
	c.Framing = "octet"
	r.save(syslog.Input{Config: c})
	r.f.OnExecution("execution.denied", exec("denied"))
	r.f.OnExecution("execution.denied", exec("denied"))
	if got := srv.WaitFor(2); !header.MatchString(got[0]) || !header.MatchString(got[1]) {
		t.Fatalf("messages: %q", got)
	}
}

func TestJSONAndCEF(t *testing.T) {
	srv := syslogtest.New(t, "tcp")
	r := newRig(t)
	c := cfgFor(srv)
	c.MessageFormat = "json"
	r.save(syslog.Input{Config: c})
	r.f.OnExecution("execution.denied", exec("denied"))
	m := header.FindStringSubmatch(srv.WaitFor(1)[0])
	var j map[string]any
	if err := json.Unmarshal([]byte(m[5]), &j); err != nil {
		t.Fatalf("JSON invalide: %v: %s", err, m[5])
	}
	if j["event"] != "execution.denied" || j["category"] != "execution" || j["severity"] != "warning" || j["server"] != "web-01" || j["exit_code"] != float64(0) || j["app"] != "leassh" {
		t.Fatalf("JSON: %v", j)
	}
	if m[1] != "132" { // local0 * 8 + warning (4)
		t.Fatalf("PRI = %s", m[1])
	}

	c.MessageFormat = "cef"
	r.save(syslog.Input{Config: c})
	r.f.OnExecution("execution.finished", exec("failed"))
	cef := header.FindStringSubmatch(srv.WaitFor(2)[1])[5]
	if !strings.HasPrefix(cef, "CEF:0|Leassh|Leassh|1.2.3|execution.finished|execution.finished|5|") || !strings.Contains(cef, "suser=Agent N1") ||
		!strings.Contains(cef, "dvchost=web-01") || !strings.Contains(cef, "act=svc.status") || !strings.Contains(cef, "outcome=failed") || strings.Contains(cef, "\n") {
		t.Fatalf("CEF: %s", cef)
	}
}

func TestFilters(t *testing.T) {
	srv := syslogtest.New(t, "udp")
	r := newRig(t)
	c := cfgFor(srv)
	c.MinSeverity = "warning"
	c.Types = []string{"execution"}
	r.save(syslog.Input{Config: c})
	r.f.OnExecution("execution.finished", exec("succeeded"))     // info: filtré par le niveau
	r.f.OnProposal("proposal.created", store.Proposal{ID: "P1"}) // type exclu
	r.f.OnExecution("execution.denied", exec("denied"))          // warning: passe
	srv.WaitFor(1)
	time.Sleep(150 * time.Millisecond)
	if got := srv.Messages(); len(got) != 1 || !strings.Contains(got[0], "execution.denied") {
		t.Fatalf("messages: %q", got)
	}
}

func TestDisabledSendsNothingAndTestMessage(t *testing.T) {
	srv := syslogtest.New(t, "tcp")
	r := newRig(t)
	c := cfgFor(srv)
	c.Enabled = false
	r.save(syslog.Input{Config: c})
	r.f.OnExecution("execution.denied", exec("denied"))
	time.Sleep(100 * time.Millisecond)
	if len(srv.Messages()) != 0 {
		t.Fatal("envoi alors que l'export est désactivé")
	}
	if st := r.f.Status(); st.State != "disabled" || st.Queued != 0 {
		t.Fatalf("statut: %+v", st)
	}
	if err := r.f.SendTest(r.ctx); err != nil { // le test fonctionne même désactivé
		t.Fatal(err)
	}
	if m := srv.WaitFor(1)[0]; !strings.Contains(m, "syslog.test") {
		t.Fatalf("message de test: %q", m)
	}
}

func TestSendTestReportsErrors(t *testing.T) {
	srv := syslogtest.New(t, "tls")
	r := newRig(t)
	c := cfgFor(srv)
	c.TLSCAPEM = "" // autorité inconnue: la vérification doit échouer
	r.save(syslog.Input{Config: c})
	err := r.f.SendTest(r.ctx)
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("erreur attendue sur le certificat: %v", err)
	}
	c.TLSSkipVerify = true
	r.save(syslog.Input{Config: c})
	if err := r.f.SendTest(r.ctx); err != nil {
		t.Fatalf("avec tls_skip_verify: %v", err)
	}
}

func TestTLSClientCertificate(t *testing.T) {
	srv := syslogtest.New(t, "tls")
	crt, key, cas := syslogtest.ClientCert(t)
	srv.ClientCAs = cas
	srv.Stop()
	srv.Start()
	r := newRig(t)
	c := cfgFor(srv)
	r.save(syslog.Input{Config: c})
	if err := r.f.SendTest(r.ctx); err == nil {
		t.Fatal("le serveur exige un certificat client")
	}
	c.TLSClientCertPEM = crt
	r.save(syslog.Input{Config: c, TLSClientKeyPEM: key})
	if err := r.f.SendTest(r.ctx); err != nil {
		t.Fatalf("mTLS: %v", err)
	}
	// la clé est chiffrée en base et jamais renvoyée
	var enc []byte
	_ = r.st.DB.QueryRow(`SELECT tls_client_key_enc FROM syslog_config`).Scan(&enc)
	if len(enc) == 0 || bytes.Contains(enc, []byte("PRIVATE KEY")) {
		t.Fatal("clé TLS non chiffrée")
	}
	cfg, _ := r.f.Config(r.ctx, r.st.DB)
	if !cfg.HasClientKey {
		t.Fatal("has_tls_client_key attendu")
	}
	if b, _ := json.Marshal(cfg); strings.Contains(string(b), "PRIVATE KEY") {
		t.Fatal("clé privée dans la configuration exposée")
	}
	// clé conservée si le champ est vide
	r.save(syslog.Input{Config: c})
	if err := r.f.SendTest(r.ctx); err != nil {
		t.Fatalf("clé non conservée: %v", err)
	}
}

func TestReconnectAndResume(t *testing.T) {
	srv := syslogtest.New(t, "tcp")
	r := newRig(t)
	r.save(syslog.Input{Config: cfgFor(srv)})
	r.f.OnExecution("execution.finished", exec("succeeded"))
	srv.WaitFor(1)

	srv.Stop() // le serveur disparaît: les événements s'accumulent sans bloquer
	start := time.Now()
	for i := 0; i < 5; i++ {
		r.f.OnExecution("execution.denied", exec("denied"))
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("Push a bloqué")
	}
	time.Sleep(200 * time.Millisecond)
	if st := r.f.Status(); st.Queued == 0 || st.LastError == "" || st.State != "retrying" {
		t.Fatalf("statut pendant la panne: %+v", st)
	}
	srv.Start()
	got := srv.WaitFor(1 + 5)
	if len(got) < 6 {
		t.Fatalf("reprise incomplète: %d", len(got))
	}
	for i := 0; i < 100 && r.f.Status().Queued > 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if st := r.f.Status(); st.Queued != 0 || st.State != "connected" {
		t.Fatalf("statut après reprise: %+v", st)
	}
}

func TestReconnectAfterServerClosesConnection(t *testing.T) {
	srv := syslogtest.New(t, "tcp")
	r := newRig(t)
	r.save(syslog.Input{Config: cfgFor(srv)})
	r.f.OnExecution("execution.denied", exec("denied"))
	srv.WaitFor(1)
	srv.DropConns()
	time.Sleep(50 * time.Millisecond)
	r.f.OnExecution("execution.denied", exec("denied"))
	if got := srv.WaitFor(2); len(got) != 2 {
		t.Fatalf("messages: %d", len(got))
	}
}

func TestQueueIsBounded(t *testing.T) {
	r := newRig(t)
	r.f.MaxQueue = 3
	r.save(syslog.Input{Config: syslog.Config{Enabled: true, Protocol: "tcp", Host: "127.0.0.1", Port: 1}}) // rien n'écoute
	for i := 0; i < 10; i++ {
		r.f.OnExecution("execution.denied", exec("denied"))
	}
	if st := r.f.Status(); st.Queued > 3 || st.Dropped < 6 {
		t.Fatalf("file non bornée: %+v", st)
	}
}

func TestAuditEventsAreForwardedWithoutSecrets(t *testing.T) {
	srv := syslogtest.New(t, "tcp")
	r := newRig(t)
	rec := func(e audit.Event) {
		t.Helper()
		if err := audit.RecordStandalone(r.ctx, r.st, e); err != nil {
			t.Fatal(err)
		}
	}
	rec(audit.Event{ActorType: "user", ActorName: "old", Action: "server.create", TargetType: "server", TargetName: "ancien"}) // avant l'activation: jamais rejoué
	r.save(syslog.Input{Config: cfgFor(srv)})
	time.Sleep(100 * time.Millisecond)
	rec(audit.Event{ActorType: "user", ActorID: "u1", ActorName: "sd", Action: "identity.create", TargetType: "identity", TargetName: "ssh-prod", IP: "10.0.0.9",
		Diff: map[string]any{"password": "hunter2", "name": "ssh-prod", "private_key": "-----BEGIN"}})
	rec(audit.Event{ActorType: "system", Action: "host_key.mismatch", TargetType: "server", TargetName: "web-01"})
	srv.WaitFor(2)
	time.Sleep(100 * time.Millisecond)
	got := srv.Messages()
	if len(got) != 2 {
		t.Fatalf("messages: %q", got)
	}
	m := header.FindStringSubmatch(got[0])
	if m == nil || m[4] != "audit.identity.create" || !strings.Contains(m[5], "actor=sd") || !strings.Contains(m[5], "target=ssh-prod") || !strings.Contains(m[5], "ip=10.0.0.9") {
		t.Fatalf("audit: %q", got[0])
	}
	if strings.Contains(got[0], "hunter2") || strings.Contains(got[0], "BEGIN") {
		t.Fatalf("secret dans l'audit syslog: %q", got[0])
	}
	if m2 := header.FindStringSubmatch(got[1]); m2 == nil || m2[1] != "130" { // crit (2) en local0
		t.Fatalf("host_key.mismatch: %q", got[1])
	}
	for _, g := range got {
		if strings.Contains(g, "server.create") {
			t.Fatal("rejeu de l'historique d'audit")
		}
	}
	// le curseur de livraison est enregistré (reprise après redémarrage)
	var cur sql.NullInt64
	for i := 0; i < 50; i++ {
		_ = r.st.DB.QueryRow(`SELECT audit_cursor FROM syslog_config`).Scan(&cur)
		if cur.Valid && cur.Int64 == 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if cur.Int64 != 3 {
		t.Fatalf("curseur = %v", cur)
	}
}

func TestValidation(t *testing.T) {
	r := newRig(t)
	bad := []syslog.Config{
		{Enabled: true},                                       // pas d'hôte
		{Protocol: "icmp"},                                    // protocole
		{Host: "http://x"},                                    // schéma dans l'hôte
		{Host: "ok", Port: 70000},                             // port
		{Host: "ok", MessageFormat: "xml"},                    // format
		{Host: "ok", Facility: "local9"},                      // facility
		{Host: "ok", AppName: "a b"},                          // app-name avec espace
		{Host: "ok", MinSeverity: "loud"},                     // niveau
		{Host: "ok", Types: []string{"secrets"}},              // type
		{Host: "ok", Protocol: "tls", TLSCAPEM: "pas un pem"}, // CA
		{Host: "ok", Protocol: "tls", TLSClientCertPEM: "x"},  // certificat sans clé
	}
	for i, c := range bad {
		err := r.st.Tx(r.ctx, func(tx *sql.Tx) error { _, _, err := r.f.Save(r.ctx, tx, syslog.Input{Config: c}); return err })
		var ve *syslog.ValidationError
		if err == nil || !asValidation(err, &ve) {
			t.Errorf("cas %d (%+v): erreur de validation attendue, obtenu %v", i, c, err)
		}
	}
}

func asValidation(err error, target **syslog.ValidationError) bool {
	ve, ok := err.(*syslog.ValidationError)
	*target = ve
	return ok
}

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"password=hunter2 ok":     "password=*** ok",
		`token: "abc def" fin`:    "token: *** fin",
		"api_key=zzz&x=1":         "api_key=***",
		"rien à masquer":          "rien à masquer",
		"Authorization: Bearer x": "Authorization: *** x",
	} {
		if got := syslog.Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestMessagesAreSingleLine(t *testing.T) {
	srv := syslogtest.New(t, "tcp")
	r := newRig(t)
	r.save(syslog.Input{Config: cfgFor(srv)})
	e := exec("denied")
	e.Reason = "ligne1\nligne2\r\x00 `id`; $(id)"
	e.DenialReason = "a=\"b\""
	r.f.OnExecution("execution.denied", e)
	r.f.OnExecution("execution.denied", exec("denied"))
	got := srv.WaitFor(2)
	if len(got) != 2 || !header.MatchString(got[0]) {
		t.Fatalf("un retour à la ligne a cassé le cadrage: %q", got)
	}
}
