package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sebdew/LeaSSH/internal/store"
)

func (e *env) insertExec(x store.Execution) store.Execution {
	e.t.Helper()
	var out store.Execution
	if err := e.st.Tx(context.Background(), func(tx *sql.Tx) (err error) { out, err = store.InsertExecution(context.Background(), tx, x); return }); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) raw(path string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/v1"+path, nil)
	resp, err := e.c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestJournalFiltersAndPagination(t *testing.T) {
	e := newEnv(t)
	r := "uptime"
	for i, st := range []string{"succeeded", "denied", "failed", "succeeded"} {
		e.insertExec(store.Execution{KeyID: "k", KeyName: "N1", ServerSlug: "web-01", CommandSlug: "system.uptime", Risk: "read", Status: st, Reason: "alarme " + string(rune('A'+i)), Rendered: &r})
	}
	out := e.must("GET", "/executions?status=succeeded,failed", nil, 200)
	if len(out["items"].([]any)) != 3 {
		t.Fatalf("statuts: %v", out)
	}
	out = e.must("GET", "/executions?limit=2", nil, 200)
	if len(out["items"].([]any)) != 2 || out["next_cursor"] == "" {
		t.Fatalf("pagination: %v", out)
	}
	out2 := e.must("GET", "/executions?limit=2&cursor="+str(out, "next_cursor"), nil, 200)
	if len(out2["items"].([]any)) != 2 || out2["next_cursor"] != "" {
		t.Fatalf("page 2: %v", out2)
	}
	if n := len(e.must("GET", "/executions?period=1h&q=alarme+C", nil, 200)["items"].([]any)); n != 1 {
		t.Fatalf("texte + période: %d", n)
	}
	e.must("GET", "/executions?status=nope", nil, 400)
	e.must("GET", "/executions?period=3y", nil, 400)
	e.must("GET", "/executions?risk=huge", nil, 400)
	e.must("GET", "/executions?from=hier", nil, 400)
}

func TestExportCSVNeutralizesFormulasAndNDJSON(t *testing.T) {
	e := newEnv(t)
	r := "echo '=cmd|calc'"
	e.insertExec(store.Execution{KeyName: "N1", ServerSlug: "web-01", CommandSlug: "c.d", Risk: "read", Status: "succeeded", Reason: "=HYPERLINK(\"http://evil\")", Rendered: &r, Stdout: "secret-out"})
	e.insertExec(store.Execution{KeyName: "N1", ServerSlug: "web-01", CommandSlug: "c.d", Risk: "read", Status: "denied", Reason: "+1+1"})

	resp, body := e.raw("/executions/export?format=csv")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") || !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("en-têtes: %v", resp.Header)
	}
	recs, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil || len(recs) != 3 {
		t.Fatalf("csv: %v %v", err, body)
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[h] = i
	}
	for _, row := range recs[1:] {
		for _, cell := range row {
			if cell != "" && strings.ContainsRune("=+-@", rune(cell[0])) {
				t.Fatalf("cellule dangereuse non neutralisée: %q", cell)
			}
		}
	}
	if recs[2][col["reason"]] != `'=HYPERLINK("http://evil")` {
		t.Fatalf("raison: %q", recs[2][col["reason"]])
	}
	if strings.Contains(body, "secret-out") {
		t.Fatal("la sortie ne doit pas figurer dans le CSV")
	}

	_, nd := e.raw("/executions/export?format=ndjson&status=denied")
	lines := strings.Split(strings.TrimSpace(nd), "\n")
	var one map[string]any
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &one) != nil || one["status"] != "denied" {
		t.Fatalf("ndjson filtré: %q", nd)
	}
	if resp, _ := e.raw("/executions/export?format=xml"); resp.StatusCode != 400 {
		t.Fatalf("format invalide: %d", resp.StatusCode)
	}
}

func TestExportPagesThroughAllRows(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < exportPage+25; i++ {
		e.insertExec(store.Execution{ServerSlug: "s", CommandSlug: "c", Status: "denied"})
	}
	_, nd := e.raw("/executions/export?format=ndjson")
	if n := len(strings.Split(strings.TrimSpace(nd), "\n")); n != exportPage+25 {
		t.Fatalf("lignes exportées: %d", n)
	}
}

func TestSSEStreamsExecutionChanges(t *testing.T) {
	e := newEnv(t)
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/v1/executions/stream", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := e.c.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE: %d %v", resp.StatusCode, resp.Header)
	}
	br := bufio.NewReader(resp.Body)
	if l, _ := br.ReadString('\n'); !strings.HasPrefix(l, "retry:") {
		t.Fatalf("préambule: %q", l)
	}
	br.ReadString('\n')
	x := e.insertExec(store.Execution{KeyName: "N1", ServerSlug: "web-01", CommandSlug: "c.d", Status: "succeeded", Stdout: "SECRET-OUT"})
	e.exec.Broker.Publish(x.ID)
	var evt, data string
	for data == "" {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("lecture SSE: %v", err)
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			evt = strings.TrimSpace(line[7:])
		case strings.HasPrefix(line, "data: "):
			data = line[6:]
		}
	}
	var got map[string]any
	if evt != "execution" || json.Unmarshal([]byte(data), &got) != nil || got["id"] != x.ID || got["status"] != "succeeded" {
		t.Fatalf("événement: %s %s", evt, data)
	}
	if strings.Contains(data, "SECRET-OUT") {
		t.Fatal("les sorties ne doivent pas transiter par le flux")
	}
}

func TestAuditListFiltersAndIntegrity(t *testing.T) {
	e := newEnv(t)
	e.must("POST", "/server-groups", map[string]any{"slug": "g1", "name": "G1"}, 201)
	e.must("POST", "/command-groups", map[string]any{"slug": "g2", "name": "G2"}, 201)
	all := e.must("GET", "/audit", nil, 200)["items"].([]any)
	if len(all) < 3 {
		t.Fatalf("audit: %v", all)
	}
	grp := e.must("GET", "/audit?action=server_group.", nil, 200)["items"].([]any)
	if len(grp) != 1 || grp[0].(map[string]any)["target_name"] != "g1" || grp[0].(map[string]any)["diff"] == nil {
		t.Fatalf("filtre action + diff: %v", grp)
	}
	if n := len(e.must("GET", "/audit?q=g2", nil, 200)["items"].([]any)); n != 1 {
		t.Fatalf("texte: %d", n)
	}
	_, nd := e.raw("/audit/export?format=ndjson&action=command_group.")
	if !strings.Contains(nd, `"command_group.create"`) || strings.Contains(nd, "server_group.create") {
		t.Fatalf("export audit: %s", nd)
	}

	v := e.must("POST", "/integrity/verify", nil, 200)
	if v["ok"] != true || v["audit"].(map[string]any)["checked"].(float64) < 3 {
		t.Fatalf("intégrité saine: %v", v)
	}
	e.insertExec(store.Execution{ServerSlug: "s", CommandSlug: "c", Status: "denied", Reason: "original"})
	e.st.DB.Exec(`UPDATE executions SET reason='falsifiée'`)
	e.st.DB.Exec(`UPDATE audit_events SET target_name='x' WHERE action='command_group.create'`)
	v = e.must("POST", "/integrity/verify", nil, 200)
	if v["ok"] != false || v["executions"].(map[string]any)["ok"] != false || v["audit"].(map[string]any)["ok"] != false {
		t.Fatalf("altération non détectée: %v", v)
	}
}

func TestUsersLifecycleAndGuards(t *testing.T) {
	e := newEnv(t)
	me := e.must("GET", "/auth/me", nil, 200)
	e.must("POST", "/users", map[string]any{"username": "bob", "password": "court", "role": "viewer"}, 400)
	e.must("POST", "/users", map[string]any{"username": "bob", "password": "mot-de-passe-de-bob", "role": "root"}, 400)
	bob := e.must("POST", "/users", map[string]any{"username": "bob", "password": "mot-de-passe-de-bob", "role": "approver"}, 201)
	if _, leak := bob["password_hash"]; leak {
		t.Fatal("fuite du hash")
	}
	e.must("POST", "/users", map[string]any{"username": "bob", "password": "mot-de-passe-de-bob", "role": "viewer"}, 409)

	// connexion de bob puis désactivation: sa session tombe immédiatement
	bc := client()
	_, bl := do(t, bc, "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "bob", Password: "mot-de-passe-de-bob"}, nil)
	if r, _ := do(t, bc, "GET", e.ts.URL+"/api/v1/auth/me", nil, nil); r.StatusCode != 200 || bl["role"] != "approver" {
		t.Fatalf("login bob: %v", bl)
	}
	e.must("PUT", "/users/"+str(bob, "id"), map[string]any{"display_name": "Bob", "role": "approver", "disabled": true}, 200)
	if r, _ := do(t, bc, "GET", e.ts.URL+"/api/v1/auth/me", nil, nil); r.StatusCode != 401 {
		t.Fatalf("session d'un compte désactivé: %d", r.StatusCode)
	}
	if r, _ := do(t, client(), "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "bob", Password: "mot-de-passe-de-bob"}, nil); r.StatusCode != 401 {
		t.Fatalf("login d'un compte désactivé: %d", r.StatusCode)
	}

	// garde-fous
	e.must("PUT", "/users/"+str(me, "id"), map[string]any{"role": "admin", "disabled": true}, 400)
	e.must("PUT", "/users/"+str(me, "id"), map[string]any{"role": "viewer"}, 400) // dernier admin
	e.must("DELETE", "/users/"+str(me, "id"), nil, 400)

	// changement de mot de passe: les autres sessions sont fermées, la courante survit
	other := client()
	do(t, other, "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "admin", Password: "un-mot-de-passe-solide"}, nil)
	e.must("POST", "/auth/password", map[string]any{"current": "faux", "new": "nouveau-mot-de-passe-1"}, 403)
	e.must("POST", "/auth/password", map[string]any{"current": "un-mot-de-passe-solide", "new": "nouveau-mot-de-passe-1"}, 204)
	if r, _ := do(t, other, "GET", e.ts.URL+"/api/v1/auth/me", nil, nil); r.StatusCode != 401 {
		t.Fatalf("autre session après changement de mot de passe: %d", r.StatusCode)
	}
	e.must("GET", "/auth/me", nil, 200)
	e.must("PUT", "/auth/theme", map[string]any{"theme": "dark"}, 204)
	e.must("PUT", "/auth/theme", map[string]any{"theme": "neon"}, 400)
	if e.must("GET", "/auth/me", nil, 200)["theme"] != "dark" {
		t.Fatal("thème non mémorisé")
	}
	e.must("DELETE", "/users/"+str(bob, "id"), nil, 204)

	// audit sans secret
	rows, _ := e.st.DB.Query(`SELECT diff FROM audit_events WHERE action LIKE 'user.%'`)
	defer rows.Close()
	for rows.Next() {
		var d string
		rows.Scan(&d)
		if strings.Contains(d, "mot-de-passe") || strings.Contains(d, "$argon2") {
			t.Fatalf("secret dans l'audit: %s", d)
		}
	}
}

// raw0 lit une route hors /api/v1.
func (e *env) raw0(path string) (*http.Response, string) {
	e.t.Helper()
	resp, err := e.c.Get(e.ts.URL + path)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}
