package api

import (
	"github.com/sebdew/LeaSSH/internal/store"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func checklist(m map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, it := range m["checklist"].([]any) {
		x := it.(map[string]any)
		out[x["id"].(string)] = x["done"].(bool)
	}
	return out
}

func TestDashboardChecklistAndStats(t *testing.T) {
	e := newEnv(t)
	d := e.must("GET", "/dashboard", nil, 200)
	for id, done := range checklist(d) {
		if done {
			t.Fatalf("étape %s cochée sur une instance vide", id)
		}
	}
	if len(d["checklist"].([]any)) != 6 || len(d["recent"].([]any)) != 0 {
		t.Fatalf("instance vide: %v", d)
	}

	ident := e.must("POST", "/identities", map[string]any{"name": "i", "username": "u", "mode": "import", "private_key": string(e.ssh.ClientPEM)}, 201)["identity"].(map[string]any)
	srv := e.must("POST", "/servers", map[string]any{"slug": "web-01", "name": "Web", "host": "127.0.0.1", "port": e.ssh.Port, "identity_id": ident["id"]}, 201)
	if got := checklist(e.must("GET", "/dashboard", nil, 200)); !got["identity"] || got["server"] {
		t.Fatalf("serveur non épinglé: %v", got)
	}
	// un serveur non épinglé est une anomalie
	st := e.must("GET", "/dashboard", nil, 200)["stats"].(map[string]any)
	an := st["servers_in_anomaly"].([]any)
	if len(an) != 1 || an[0].(map[string]any)["reason"] != "host_key_unknown" {
		t.Fatalf("anomalies: %v", an)
	}
	e.must("POST", "/servers/"+str(srv, "id")+"/host-key", map[string]any{"fingerprint": e.ssh.Fingerprint}, 200)
	e.must("POST", "/packs/import", map[string]any{"bundled": "system", "create_group": true}, 200)
	sg := e.must("POST", "/server-groups", map[string]any{"slug": "all", "name": "Tous", "member_ids": []string{str(srv, "id")}}, 201)
	groups := e.must("GET", "/command-groups", nil, 200)["items"].([]any)
	key := e.must("POST", "/keys", map[string]any{"name": "N1"}, 201)["key"].(map[string]any)
	e.must("POST", "/rules", map[string]any{"name": "r", "server_group_ids": []any{sg["id"]}, "command_group_ids": []any{groups[0].(map[string]any)["id"]}, "profile": "RO", "key_ids": []string{str(key, "id")}}, 201)
	got := checklist(e.must("GET", "/dashboard", nil, 200))
	for _, id := range []string{"identity", "server", "pack", "rule", "key"} {
		if !got[id] {
			t.Errorf("étape %s non cochée: %v", id, got)
		}
	}
	if got["agent"] {
		t.Fatal("« agent connecté » cochée alors que la clé n'a jamais servi")
	}
	e.st.DB.Exec(`UPDATE mcp_keys SET last_used_at='2026-10-18T10:00:00.000Z'`)
	if !checklist(e.must("GET", "/dashboard", nil, 200))["agent"] {
		t.Fatal("agent connecté non détecté")
	}

	// statistiques 24 h
	cmds := e.must("GET", "/commands", nil, 200)["items"].([]any)
	uptime := ""
	for _, c := range cmds {
		if c.(map[string]any)["slug"] == "system.uptime" {
			uptime = str(c.(map[string]any), "id")
		}
	}
	e.must("POST", "/commands/"+uptime+"/test", map[string]any{"server": "web-01"}, 200)
	e.must("POST", "/commands/"+uptime+"/test", map[string]any{"server": "inconnu"}, 200) // refus journalisé
	d = e.must("GET", "/dashboard", nil, 200)
	ex := d["stats"].(map[string]any)["executions_24h"].(map[string]any)
	if ex["succeeded"].(float64)+ex["failed"].(float64) != 1 || ex["denied"].(float64) != 1 || len(d["recent"].([]any)) != 2 {
		t.Fatalf("stats 24 h: %v", d["stats"])
	}
	if len(d["stats"].(map[string]any)["servers_in_anomaly"].([]any)) != 0 {
		t.Fatalf("serveur sain en anomalie: %v", d["stats"])
	}
}

func TestMetricsEndpoint(t *testing.T) {
	e := newEnv(t)
	ident := e.must("POST", "/identities", map[string]any{"name": "i", "username": "u", "mode": "import", "private_key": string(e.ssh.ClientPEM)}, 201)["identity"].(map[string]any)
	srv := e.must("POST", "/servers", map[string]any{"slug": "web-01", "name": "Web", "host": "127.0.0.1", "port": e.ssh.Port, "identity_id": ident["id"]}, 201)
	e.must("POST", "/servers/"+str(srv, "id")+"/host-key", map[string]any{"fingerprint": e.ssh.Fingerprint}, 200)
	cmd := e.must("POST", "/commands", map[string]any{"slug": "t.hello", "name": "Hello", "risk": "read", "template": "echo hello"}, 201)["command"].(map[string]any)
	e.must("POST", "/commands/"+str(cmd, "id")+"/test", map[string]any{"server": "web-01"}, 200)
	e.must("POST", "/commands/"+str(cmd, "id")+"/test", map[string]any{"server": "nope"}, 200)

	// /metrics est fermé par défaut (SEC-02): on définit un jeton puis on le présente.
	cfg := e.must("GET", "/settings/general", nil, 200)
	cfg["metrics_token"] = "m3trics-token"
	e.must("PUT", "/settings/general", cfg, 200)
	req, _ := http.NewRequest("GET", e.ts.URL+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer m3trics-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(raw)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("/metrics: %d %v", resp.StatusCode, resp.Header)
	}
	for _, want := range []string{`leassh_executions_total{status="succeeded"} 1`, `leassh_executions_total{status="denied"} 1`, `leassh_execution_duration_seconds_count 1`,
		`leassh_servers{host_key_status="pinned"} 1`, `leassh_build_info{version="test"} 1`, "leassh_pending_approvals 0", "# TYPE leassh_executions_total counter"} {
		if !strings.Contains(body, want) {
			t.Errorf("manque %q dans:\n%s", want, body)
		}
	}
	if strings.Contains(body, "echo hello") || strings.Contains(body, "hello") && strings.Contains(body, "reason") {
		t.Fatal("la sortie ou la commande ne doit pas apparaître dans les métriques")
	}
	for _, path := range []string{"/health", "/healthz"} {
		if resp, _ := e.raw0(path); resp.StatusCode != 200 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
}

func TestMetricsTokenProtection(t *testing.T) {
	e := newEnv(t)
	// SEC-02: sécurisé par défaut, tant qu'aucun jeton n'est défini /metrics est fermé.
	if resp, _ := e.raw0("/metrics"); resp.StatusCode != 401 {
		t.Fatalf("sans jeton défini, /metrics doit être fermé: %d", resp.StatusCode)
	}
	cfg := e.must("GET", "/settings/general", nil, 200)
	cfg["metrics_token"] = "m3trics-token"
	out := e.must("PUT", "/settings/general", cfg, 200)
	if out["has_metrics_token"] != true || out["metrics_token"] != nil {
		t.Fatalf("le jeton ne doit jamais être renvoyé: %v", out)
	}
	if resp, _ := e.raw0("/metrics"); resp.StatusCode != 401 {
		t.Fatalf("sans jeton: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", e.ts.URL+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer m3trics-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("avec jeton: %v %v", resp, err)
	}
	resp.Body.Close()
	req.Header.Set("Authorization", "Bearer faux")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 401 {
		t.Fatalf("mauvais jeton: %d", resp.StatusCode)
	}
}

// Régression: un template sans placeholder renvoyait `"placeholders": null`, ce qui faisait planter l'éditeur.
func TestLintReturnsEmptyListsNotNull(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/commands/lint", "/proposals/lint"} {
		body := map[string]any{"template": "uptime", "risk": "read"}
		if path == "/proposals/lint" {
			body = map[string]any{"slug": "a.b", "name": "A", "description": "Une description suffisante.", "template": "uptime", "risk": "read"}
		}
		r, out := e.call("POST", path, body)
		if r.StatusCode != 200 {
			t.Fatalf("%s: %d", path, r.StatusCode)
		}
		if ph, ok := out["placeholders"].([]any); !ok || len(ph) != 0 {
			t.Errorf("%s: placeholders = %#v (liste vide attendue, pas null)", path, out["placeholders"])
		}
		lint := out["lint"].(map[string]any)
		if _, ok := lint["errors"].([]any); !ok {
			t.Errorf("%s: errors = %#v", path, lint["errors"])
		}
		if _, ok := lint["warnings"].([]any); !ok {
			t.Errorf("%s: warnings = %#v", path, lint["warnings"])
		}
	}
}

// Une clé révoquée est retirée de toutes les règles (elle n'y apparaît plus nulle part).
func TestRevokeRemovesKeyFromRules(t *testing.T) {
	e := newEnv(t)
	sg := e.must("POST", "/server-groups", map[string]any{"slug": "s", "name": "S"}, 201)
	cg := e.must("POST", "/command-groups", map[string]any{"slug": "c", "name": "C"}, 201)
	k1 := e.must("POST", "/keys", map[string]any{"name": "N1"}, 201)["key"].(map[string]any)
	k2 := e.must("POST", "/keys", map[string]any{"name": "N2"}, 201)["key"].(map[string]any)
	rule := e.must("POST", "/rules", map[string]any{"name": "r", "server_group_ids": []any{sg["id"]}, "command_group_ids": []any{cg["id"]}, "profile": "RO", "key_ids": []string{str(k1, "id"), str(k2, "id")}}, 201)
	e.must("POST", "/keys/"+str(k1, "id")+"/revoke", nil, 200)
	rules := e.must("GET", "/rules", nil, 200)["items"].([]any)
	ids := rules[0].(map[string]any)["key_ids"].([]any)
	if rules[0].(map[string]any)["id"] != rule["id"] || len(ids) != 1 || ids[0] != k2["id"] {
		t.Fatalf("clés de la règle après révocation: %v", ids)
	}
	for _, k := range e.must("GET", "/keys", nil, 200)["items"].([]any) {
		m := k.(map[string]any)
		if m["id"] == k1["id"] && len(m["rule_ids"].([]any)) != 0 {
			t.Fatalf("la clé révoquée garde des règles: %v", m["rule_ids"])
		}
	}
}

func TestServerSSHUserField(t *testing.T) {
	e := newEnv(t)
	id := e.must("POST", "/identities", map[string]any{"name": "i", "username": "ia", "mode": "generate"}, 201)["identity"].(map[string]any)
	base := map[string]any{"slug": "resa", "name": "Resa", "host": "10.0.0.1", "port": 2222, "identity_id": id["id"]}
	for _, bad := range []string{"x\ny", "a\tb", "nul\x00", strings.Repeat("a", 129)} {
		b := map[string]any{"slug": "x", "name": "X", "host": "h", "identity_id": id["id"], "ssh_user": bad}
		if r, _ := e.call("POST", "/servers", b); r.StatusCode != 400 {
			t.Errorf("ssh_user %q accepté (%d)", bad, r.StatusCode)
		}
	}
	base["ssh_user"] = "ia:reservations-dev - LXC ct145" // nom exact de la cible Warpgate, espaces compris
	srv := e.must("POST", "/servers", base, 201)
	if srv["ssh_user"] != "ia:reservations-dev - LXC ct145" {
		t.Fatalf("création: %v", srv)
	}
	base["ssh_user"] = "ia:reduc"
	if up := e.must("PUT", "/servers/"+str(srv, "id"), base, 200); up["ssh_user"] != "ia:reduc" {
		t.Fatalf("mise à jour: %v", up)
	}
	base["ssh_user"] = ""
	if up := e.must("PUT", "/servers/"+str(srv, "id"), base, 200); up["ssh_user"] != "" {
		t.Fatalf("retour à l'utilisateur de l'identité: %v", up)
	}
}

// « Tester la connexion » réussi efface l'anomalie « injoignable » du tableau de bord.
func TestConnectionTestClearsAnomaly(t *testing.T) {
	e := newEnv(t)
	ident := e.must("POST", "/identities", map[string]any{"name": "i", "username": "u", "mode": "import", "private_key": string(e.ssh.ClientPEM)}, 201)["identity"].(map[string]any)
	srv := e.must("POST", "/servers", map[string]any{"slug": "web-01", "name": "Web", "host": "127.0.0.1", "port": e.ssh.Port, "identity_id": ident["id"]}, 201)
	e.must("POST", "/servers/"+str(srv, "id")+"/host-key", map[string]any{"fingerprint": e.ssh.Fingerprint}, 200)
	e.st.DB.Exec(`UPDATE servers SET last_ssh_error_at=?`, store.FormatTime(time.Now().Add(-2*time.Second))) // échec récent, antérieur au test à venir
	an := e.must("GET", "/dashboard", nil, 200)["stats"].(map[string]any)["servers_in_anomaly"].([]any)
	if len(an) != 1 || an[0].(map[string]any)["reason"] != "ssh_unreachable" {
		t.Fatalf("anomalie attendue: %v", an)
	}
	if tc := e.must("POST", "/servers/"+str(srv, "id")+"/test-connection", nil, 200); tc["auth_ok"] != true {
		t.Fatalf("test de connexion: %v", tc)
	}
	if an := e.must("GET", "/dashboard", nil, 200)["stats"].(map[string]any)["servers_in_anomaly"].([]any); len(an) != 0 {
		t.Fatalf("anomalie résiduelle: %v", an)
	}
}
