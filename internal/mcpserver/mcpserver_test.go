package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/executions"
	"github.com/leassh-dev/leassh/internal/store"
	"github.com/leassh-dev/leassh/internal/testkit"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func setup(t *testing.T, profile string, wra bool) (*testkit.Kit, *httptest.Server) {
	k := testkit.New(t, profile, wra, time.Hour)
	s := New(Config{Store: k.St, Access: k.Acc, Executions: k.Exec, Proposals: k.Props, Version: "test",
		ClientIP: func(r *http.Request) string { return "10.1.1.1" }})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return k, ts
}

func connect(t *testing.T, url, token string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: bearer{token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("connexion MCP: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	m, _ := res.StructuredContent.(map[string]any)
	return res, m
}

func errText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestAuthRequired(t *testing.T) {
	k, ts := setup(t, "RO", true)
	for _, tok := range []string{"", "lsh_" + strings.Repeat("x", 43), "garbage"} {
		req, _ := http.NewRequest("POST", ts.URL, strings.NewReader(`{}`))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 401 {
			t.Fatalf("jeton %q: %v %v", tok, resp, err)
		}
	}
	var n int
	k.St.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action='key.auth_failed'`).Scan(&n)
	if n != 3 {
		t.Fatalf("échecs d'authentification journalisés: %d", n)
	}
	var diff string
	k.St.DB.QueryRow(`SELECT group_concat(target_name || diff) FROM audit_events WHERE action='key.auth_failed'`).Scan(&diff)
	if !strings.Contains(diff, "ne commence pas par") || !strings.Contains(diff, "jeton inconnu") || !strings.Contains(diff, "absent") {
		t.Fatalf("raisons d'échec non journalisées: %s", diff)
	}
	if strings.Contains(diff, strings.Repeat("x", 20)) {
		t.Fatal("jeton complet journalisé")
	}
}

func TestToolsListedAccordingToKey(t *testing.T) {
	k, ts := setup(t, "RO", true)
	cs := connect(t, ts.URL, k.Token)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
		if len(tl.Description) < 40 {
			t.Errorf("description trop courte pour %s", tl.Name)
		}
	}
	for _, n := range []string{"list_servers", "list_commands", "run_command", "get_execution", "propose_command"} {
		if !names[n] {
			t.Errorf("outil %s absent", n)
		}
	}
	// une clé sans can_propose ne voit pas propose_command
	k.Key.CanPropose = false
	k.St.DB.Exec(`UPDATE mcp_keys SET can_propose=0 WHERE id=?`, k.Key.ID)
	cs2 := connect(t, ts.URL, k.Token)
	tools2, _ := cs2.ListTools(context.Background(), nil)
	for _, tl := range tools2.Tools {
		if tl.Name == "propose_command" {
			t.Fatal("propose_command exposé sans can_propose")
		}
	}
}

// Critère 15.1: une clé RO ne peut ni voir ni lancer une commande write.
func TestROKeyCannotSeeOrRunWrite(t *testing.T) {
	k, ts := setup(t, "RO", true)
	cs := connect(t, ts.URL, k.Token)
	_, servers := call(t, cs, "list_servers", nil)
	if ns, _ := servers["next_step"].(string); !strings.Contains(ns, "list_commands") {
		t.Fatalf("indication d'étape suivante absente: %v", servers)
	}
	if l, _ := servers["servers"].([]any); len(l) != 1 || l[0].(map[string]any)["slug"] != "web-01" {
		t.Fatalf("serveurs: %v", servers)
	}
	_, cmds := call(t, cs, "list_commands", map[string]any{"server": "web-01"})
	for _, c := range cmds["commands"].([]any) {
		slug := c.(map[string]any)["slug"].(string)
		if slug == "t.write" || slug == "t.destroy" {
			t.Fatalf("commande %s visible en RO", slug)
		}
	}
	res, _ := call(t, cs, "run_command", map[string]any{"server": "web-01", "command": "t.write", "reason": "essai"})
	if !res.IsError || errText(res) != "serveur ou commande indisponible pour cette clé" {
		t.Fatalf("refus attendu: %+v %q", res, errText(res))
	}
	if _, err := store.ListExecutions(k.Ctx, k.St.DB, store.ExecFilter{Statuses: []string{"denied"}}); err != nil {
		t.Fatal(err)
	}
	// aucun serveur inconnu ne se distingue d'un serveur interdit
	res2, _ := call(t, cs, "list_commands", map[string]any{"server": "inexistant"})
	if !res2.IsError || errText(res2) != errText(res) {
		t.Fatalf("fuite d'existence: %q vs %q", errText(res2), errText(res))
	}
}

func TestRunAndApprovalFlow(t *testing.T) {
	k, ts := setup(t, "RW", true)
	cs := connect(t, ts.URL, k.Token)
	res, out := call(t, cs, "run_command", map[string]any{"server": "web-01", "command": "t.lines", "params": map[string]any{"lines": 12}, "reason": "Alarme #1"})
	if res.IsError || out["status"] != "succeeded" || strings.TrimSpace(out["stdout"].(string)) != "12" || out["execution_id"] == "" {
		t.Fatalf("lecture directe: %+v %v", res, out)
	}
	bad, _ := call(t, cs, "run_command", map[string]any{"server": "web-01", "command": "t.lines", "params": map[string]any{"lines": "5; id"}, "reason": "x"})
	if !bad.IsError || !strings.Contains(errText(bad), "lines") {
		t.Fatalf("paramètre invalide: %q", errText(bad))
	}
	// write avec approbation requise
	_, p := call(t, cs, "run_command", map[string]any{"server": "web-01", "command": "t.write", "reason": "redémarrage"})
	if p["status"] != "pending_approval" || !strings.Contains(p["message"].(string), "validation humaine") {
		t.Fatalf("pending: %v", p)
	}
	id := p["execution_id"].(string)
	go func() {
		time.Sleep(300 * time.Millisecond)
		k.Exec.Decide(k.Ctx, id, k.Admin, true, "OK", "")
	}()
	_, g := call(t, cs, "get_execution", map[string]any{"execution_id": id, "wait_seconds": 10})
	if g["status"] != "running" && g["status"] != "succeeded" {
		t.Fatalf("après approbation: %v", g)
	}
	k.Exec.Wait()
	_, g = call(t, cs, "get_execution", map[string]any{"execution_id": id})
	if g["status"] != "succeeded" || g["decided_by"] != "sd" || g["decision_comment"] != "OK" || g["rendered"] != "echo wrote" {
		t.Fatalf("final: %v", g)
	}
}

// Invariant 5: une clé ne lit pas les exécutions d'une autre.
func TestCannotReadOtherKeysExecutions(t *testing.T) {
	k, ts := setup(t, "RO", true)
	other := k.Key
	other.ID = "01OTHERKEY"
	k.St.DB.Exec(`INSERT INTO mcp_keys (id,name,prefix,hash,created_at) VALUES ('01OTHERKEY','autre','lsh_zzzz','hh','2026-01-01T00:00:00.000Z')`)
	e, _ := k.Exec.Run(k.Ctx, runReq(other))
	cs := connect(t, ts.URL, k.Token)
	res, _ := call(t, cs, "get_execution", map[string]any{"execution_id": e.Exec.ID})
	if !res.IsError || errText(res) != "exécution introuvable" {
		t.Fatalf("lecture inter-clés: %+v", res)
	}
}

func TestProposeCommand(t *testing.T) {
	k, ts := setup(t, "RO", true)
	cs := connect(t, ts.URL, k.Token)
	_, ok := call(t, cs, "propose_command", map[string]any{"slug": "mysql.replica_status", "name": "Réplication", "description": "État de la réplication MySQL.",
		"template": `mysql --batch -e "SHOW REPLICA STATUS"`, "risk": "read", "justification": "diagnostic"})
	if ok["status"] != "pending" {
		t.Fatalf("proposition valide: %v", ok)
	}
	_, bad := call(t, cs, "propose_command", map[string]any{"slug": "x.evil", "name": "Evil", "description": "Commande dangereuse.",
		"template": "uptime; rm -rf /", "risk": "read", "justification": "x"})
	if bad["status"] != "invalid" {
		t.Fatalf("proposition dangereuse: %v", bad)
	}
}

func runReq(k store.MCPKey) executions.Request {
	return executions.Request{Actor: executions.Actor{Key: &k}, Server: "web-01", Command: "t.read", Reason: "x"}
}

// Régression: un appel run_command mal formé était rejeté par le SDK avant le handler, donc jamais journalisé.
// Toute demande doit laisser une ligne dans le journal (invariant 6), même invalide.
func TestMalformedRunCommandIsStillJournaled(t *testing.T) {
	k, ts := setup(t, "RW", false)
	cs := connect(t, ts.URL, k.Token)
	count := func() int {
		var n int
		k.St.DB.QueryRow(`SELECT count(*) FROM executions`).Scan(&n)
		return n
	}
	cases := []struct {
		name   string
		args   map[string]any
		denial string
	}{
		{"raison absente", map[string]any{"server": "web-01", "command": "t.read"}, "reason_invalid"},
		{"raison vide", map[string]any{"server": "web-01", "command": "t.read", "reason": "  "}, "reason_invalid"},
		{"serveur absent", map[string]any{"command": "t.read", "reason": "x"}, "target_not_found"},
		{"aucun argument", map[string]any{}, "reason_invalid"},
		{"mauvais types", map[string]any{"server": 42, "command": []any{"a"}, "reason": true}, "target_not_found"},
		{"params en texte non JSON", map[string]any{"server": "web-01", "command": "t.lines", "params": "lines=5", "reason": "x"}, "param_invalid:params"},
		{"params de mauvais type", map[string]any{"server": "web-01", "command": "t.lines", "params": 5, "reason": "x"}, "param_invalid:params"},
	}
	for i, c := range cases {
		before := count()
		res, _ := call(t, cs, "run_command", c.args)
		if !res.IsError || count() != before+1 {
			t.Fatalf("%s: erreur MCP attendue et une ligne de journal (lignes: %d → %d)", c.name, before, count())
		}
		var reason string
		k.St.DB.QueryRow(`SELECT denial_reason FROM executions ORDER BY seq DESC LIMIT 1`).Scan(&reason)
		if reason != c.denial {
			t.Errorf("%s (#%d): raison %q, attendue %q", c.name, i, reason, c.denial)
		}
	}
	// params fourni en texte JSON: accepté (certains clients sérialisent ainsi)
	res, out := call(t, cs, "run_command", map[string]any{"server": "web-01", "command": "t.lines", "params": `{"lines": 5}`, "reason": "alarme"})
	if res.IsError || out["status"] != "succeeded" || strings.TrimSpace(out["stdout"].(string)) != "5" {
		t.Fatalf("params en texte JSON: %+v %v", res, out)
	}
}
