package api

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/access"
	"github.com/leassh-dev/leassh/internal/config"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/executions"
	"github.com/leassh-dev/leassh/internal/mcpserver"
	"github.com/leassh-dev/leassh/internal/metrics"
	"github.com/leassh-dev/leassh/internal/packs"
	"github.com/leassh-dev/leassh/internal/proposals"
	"github.com/leassh-dev/leassh/internal/provisioning"
	"github.com/leassh-dev/leassh/internal/provisioning/warpgate"
	"github.com/leassh-dev/leassh/internal/sshexec"
	"github.com/leassh-dev/leassh/internal/sshtest"
	"github.com/leassh-dev/leassh/internal/store"
	"github.com/leassh-dev/leassh/internal/syslog"
	"github.com/leassh-dev/leassh/internal/webhook"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type env struct {
	t     *testing.T
	ts    *httptest.Server
	c     *http.Client
	csrf  string
	ssh   *sshtest.Server
	st    *store.Store
	exec  *executions.Service
	props *proposals.Service
}

func init() { provisioning.Register(warpgate.Type()) }

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{SessionTTL: time.Hour, MasterKey: bytes.Repeat([]byte{2}, 32), ApprovalTTL: time.Hour, MetricsToken: testMetricsToken}
	box, _ := crypto.NewBox(cfg.MasterKey)
	pool := sshexec.NewPool(time.Minute, 2)
	t.Cleanup(pool.Close)
	acc := access.New(st)
	ex := executions.New(ctx, st, acc, box, pool, log, cfg.ApprovalTTL)
	t.Cleanup(ex.Wait)
	props := proposals.New(st)
	pk := packs.New(st)
	hook := webhook.New(st, box, "https://leassh.example.com", log)
	hook.Backoff = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	reg := metrics.New()
	ex.Metrics = reg
	sl := syslog.New(st, box, "test", log)
	sl.MinBackoff, sl.MaxBackoff, sl.AuditPoll = 10*time.Millisecond, 50*time.Millisecond, 20*time.Millisecond
	ex.Observe = sl.OnExecution
	go sl.Run(ctx)
	ex.Notify, props.Notify = hook.OnExecution, hook.OnProposal
	go hook.Run(ctx)
	srv, _ := New(cfg, st, log, nil)
	m := mcpserver.New(mcpserver.Config{Store: st, Access: acc, Executions: ex, Proposals: props, ClientIP: srv.ClientIP, Version: "t"})
	srv.Attach(Services{Box: box, Pool: pool, Acc: acc, Exec: ex, Props: props, Packs: pk, Hook: hook, Syslog: sl, Prov: provisioning.New(st, box, log), MCP: m.Handler(), Metrics: reg, Version: "test"})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	e := &env{t: t, ts: ts, c: client(), ssh: sshtest.New(t), st: st, exec: ex, props: props}
	_, out := do(t, e.c, "POST", ts.URL+"/api/v1/setup", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil)
	e.csrf = out["csrf_token"].(string)
	return e
}

func (e *env) call(method, path string, body any) (*http.Response, map[string]any) {
	e.t.Helper()
	return do(e.t, e.c, method, e.ts.URL+"/api/v1"+path, body, map[string]string{"X-CSRF-Token": e.csrf})
}

func (e *env) must(method, path string, body any, want int) map[string]any {
	e.t.Helper()
	r, out := e.call(method, path, body)
	if r.StatusCode != want {
		e.t.Fatalf("%s %s: %d (attendu %d): %v", method, path, r.StatusCode, want, out)
	}
	return out
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

type bearerRT struct{ tok string }

func (b bearerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

// Parcours complet: configuration via l'API → agent via MCP → validation humaine → journal.
func TestEndToEnd(t *testing.T) {
	e := newEnv(t)

	// identité (clé importée = celle que le serveur SSH factice accepte); aucun secret dans la réponse
	idRes := e.must("POST", "/identities", map[string]any{"name": "root lab", "username": "u", "mode": "import", "private_key": string(e.ssh.ClientPEM)}, 201)
	ident := idRes["identity"].(map[string]any)
	if _, leak := ident["private_key_enc"]; leak || strings.Contains(toJSON(idRes), "PRIVATE KEY") {
		t.Fatal("fuite de la clé privée")
	}
	// génération ed25519
	gen := e.must("POST", "/identities", map[string]any{"name": "générée", "username": "root"}, 201)
	if !strings.HasPrefix(str(gen["identity"].(map[string]any), "public_key"), "ssh-ed25519 ") || !strings.Contains(str(gen, "authorized_keys_line"), "authorized_keys") {
		t.Fatalf("génération: %v", gen)
	}

	// serveur: inconnu → test de connexion → épinglage
	srv := e.must("POST", "/servers", map[string]any{"slug": "web-01", "name": "Web 01", "host": "127.0.0.1", "port": e.ssh.Port, "identity_id": ident["id"], "description": "Frontal"}, 201)
	sid := str(srv, "id")
	tc := e.must("POST", "/servers/"+sid+"/test-connection", nil, 200)
	if tc["fingerprint"] != e.ssh.Fingerprint || tc["host_key_status"] != "unknown" {
		t.Fatalf("test de connexion: %v", tc)
	}
	e.must("POST", "/servers/"+sid+"/host-key", map[string]any{"fingerprint": "SHA256:faux"}, 400)
	pinned := e.must("POST", "/servers/"+sid+"/host-key", map[string]any{"fingerprint": e.ssh.Fingerprint}, 200)
	if pinned["host_key_status"] != "pinned" {
		t.Fatalf("épinglage: %v", pinned)
	}
	if tc := e.must("POST", "/servers/"+sid+"/test-connection", nil, 200); tc["auth_ok"] != true || tc["matches"] != true {
		t.Fatalf("authentification: %v", tc)
	}

	// commandes: lint, aperçu, création
	e.must("POST", "/commands", map[string]any{"slug": "bad.cmd", "name": "Bad", "risk": "read", "template": `echo "{{x}}"`, "params": map[string]any{"x": map[string]any{"type": "service"}}}, 400)
	lint := e.must("POST", "/commands/lint", map[string]any{"template": "ps aux | sort", "risk": "read"}, 200)
	if w := lint["lint"].(map[string]any)["warnings"].([]any); len(w) == 0 {
		t.Fatalf("avertissement de pipe attendu: %v", lint)
	}
	prev := e.must("POST", "/commands/preview", map[string]any{"template": "echo {{n}}", "params": map[string]any{"n": map[string]any{"type": "int", "min": 1, "max": 10}}, "values": map[string]any{"n": 5}}, 200)
	if prev["rendered"] != "echo '5'" {
		t.Fatalf("aperçu: %v", prev)
	}
	rd := e.must("POST", "/commands", map[string]any{"slug": "sys.hello", "name": "Hello", "description": "Dit bonjour", "risk": "read", "template": "echo hello"}, 201)
	wr := e.must("POST", "/commands", map[string]any{"slug": "sys.write", "name": "Write", "description": "Écrit", "risk": "write", "template": "echo wrote"}, 201)
	rdID, wrID := str(rd["command"].(map[string]any), "id"), str(wr["command"].(map[string]any), "id")

	// test depuis l'éditeur (actor = user)
	tr := e.must("POST", "/commands/"+rdID+"/test", map[string]any{"server": "web-01"}, 200)
	te := tr["execution"].(map[string]any)
	if te["status"] != "succeeded" || te["actor_user_id"] == "" || te["key_id"] != "" {
		t.Fatalf("test éditeur: %v", te)
	}

	// groupes, clé, règle
	sg := e.must("POST", "/server-groups", map[string]any{"slug": "all", "name": "Tous", "member_ids": []string{sid}}, 201)
	cg := e.must("POST", "/command-groups", map[string]any{"slug": "std", "name": "Std", "member_ids": []string{rdID, wrID}}, 201)
	kr := e.must("POST", "/keys", map[string]any{"name": "Agent N1", "expires_at": "2099-01-01"}, 201)
	token := str(kr, "token")
	key := kr["key"].(map[string]any)
	if !strings.HasPrefix(token, "lsh_") || key["prefix"] != token[:8] || strings.Contains(toJSON(e.must("GET", "/keys", nil, 200)), token) {
		t.Fatal("jeton: format ou fuite")
	}
	e.must("POST", "/rules", map[string]any{"name": "N1 std", "server_group_ids": []any{sg["id"]}, "command_group_ids": []any{cg["id"]}, "profile": "RW", "key_ids": []string{str(key, "id")}}, 201)

	// simulateur
	sim := e.must("GET", "/simulator?key="+str(key, "id"), nil, 200)
	got := map[string]string{}
	for _, c := range sim["cells"].([]any) {
		m := c.(map[string]any)
		switch {
		case m["allowed"] == true && m["requires_approval"] == true:
			got[str(m, "command")] = "approval"
		case m["allowed"] == true:
			got[str(m, "command")] = "direct"
		}
	}
	if got["sys.hello"] != "direct" || got["sys.write"] != "approval" {
		t.Fatalf("simulateur: %v", got)
	}

	// l'agent (MCP)
	cl := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "1"}, nil)
	cs, err := cl.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.ts.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerRT{token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "run_command", Arguments: map[string]any{"server": "web-01", "command": "sys.write", "reason": "redémarrage demandé"}})
	if err != nil {
		t.Fatal(err)
	}
	pend := res.StructuredContent.(map[string]any)
	if pend["status"] != "pending_approval" {
		t.Fatalf("attendu pending: %v", pend)
	}
	execID := str(pend, "execution_id")

	// validation humaine: liste des demandes en attente puis approbation
	list := e.must("GET", "/executions?status=pending_approval", nil, 200)
	if items := list["items"].([]any); len(items) != 1 || str(items[0].(map[string]any), "reason") != "redémarrage demandé" {
		t.Fatalf("validations en attente: %v", list)
	}
	e.must("POST", "/executions/"+execID+"/decision", map[string]any{"decision": "reject"}, 400) // commentaire obligatoire
	e.must("POST", "/executions/"+execID+"/decision", map[string]any{"decision": "approve", "comment": "OK"}, 200)
	e.exec.Wait()
	final := e.must("GET", "/executions/"+execID, nil, 200)
	if final["status"] != "succeeded" || final["decided_by_name"] != "admin" || final["rendered"] != "echo wrote" || strings.TrimSpace(str(final, "stdout")) != "wrote" {
		t.Fatalf("exécution finale: %v", final)
	}

	// révocation: effet immédiat
	e.must("POST", "/keys/"+str(key, "id")+"/revoke", nil, 200)
	res, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "run_command", Arguments: map[string]any{"server": "web-01", "command": "sys.hello", "reason": "après révocation"}})
	if !res.IsError {
		t.Fatal("clé révoquée encore active")
	}

	// audit: toutes les mutations sont tracées, sans secret
	rows, _ := st2(e).Query(`SELECT action, diff FROM audit_events`)
	actions := map[string]bool{}
	for rows.Next() {
		var a, d string
		rows.Scan(&a, &d)
		actions[a] = true
		if strings.Contains(d, "PRIVATE KEY") || strings.Contains(d, token) {
			t.Fatalf("secret dans l'audit (%s): %s", a, d)
		}
	}
	rows.Close()
	for _, a := range []string{"user.create", "identity.create", "server.create", "server.trust_host_key", "command.create", "key.create", "key.revoke", "rule.create", "execution.approve", "server_group.create"} {
		if !actions[a] {
			t.Errorf("événement d'audit manquant: %s (%v)", a, actions)
		}
	}
}

func st2(e *env) interface {
	Query(string, ...any) (*sqlRows, error)
} {
	return rowsQ{e.st}
}

func TestRoles(t *testing.T) {
	e := newEnv(t)
	for _, role := range []string{"viewer", "approver"} {
		hash, _ := crypto.HashPassword("mot-de-passe-" + role)
		if err := e.st.Tx(context.Background(), func(tx *sql.Tx) error {
			_, err := store.InsertUser(context.Background(), tx, store.User{Username: role, Role: role, PasswordHash: hash})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		c := client()
		_, out := do(t, c, "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: role, Password: "mot-de-passe-" + role}, nil)
		h := map[string]string{"X-CSRF-Token": str(out, "csrf_token")}
		base := e.ts.URL + "/api/v1"
		if r, _ := do(t, c, "GET", base+"/commands", nil, h); r.StatusCode != 200 {
			t.Fatalf("%s doit pouvoir lire: %d", role, r.StatusCode)
		}
		for _, p := range []string{"/commands", "/servers", "/keys", "/rules", "/identities", "/server-groups"} {
			if r, _ := do(t, c, "POST", base+p, map[string]any{}, h); r.StatusCode != 403 {
				t.Errorf("%s: POST %s → %d (403 attendu)", role, p, r.StatusCode)
			}
		}
		r, _ := do(t, c, "POST", base+"/executions/x/decision", map[string]any{"decision": "approve"}, h)
		if role == "viewer" && r.StatusCode != 403 {
			t.Errorf("viewer ne doit pas décider: %d", r.StatusCode)
		}
		if role == "approver" && r.StatusCode == 403 {
			t.Errorf("approver doit pouvoir décider")
		}
	}
}

// testMetricsToken est positionné par les tests qui vérifient la protection de /metrics.
var testMetricsToken string
