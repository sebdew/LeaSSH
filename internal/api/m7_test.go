package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sebdew/LeaSSH/internal/catalog"
	"github.com/sebdew/LeaSSH/internal/proposals"
	"github.com/sebdew/LeaSSH/internal/store"
	"github.com/sebdew/LeaSSH/internal/webhook"
)

const pipedPack = `
pack:
  id: piped
  name: Avec pipe
  version: 2.0.0
commands:
  - id: piped.top
    name: Top mémoire
    description: Processus triés par mémoire.
    risk: read
    template: ps aux | sort -k4 -nr
    params: {}
`

func TestPacksAPI(t *testing.T) {
	e := newEnv(t)
	l := e.must("GET", "/packs", nil, 200)
	if b := l["bundled"].([]any); len(b) != 9 {
		t.Fatalf("packs fournis: %v", l)
	}
	pv := e.must("POST", "/packs/preview", map[string]any{"bundled": "mysql"}, 200)
	if pv["trusted"] != true || pv["preview"].(map[string]any)["counts"].(map[string]any)["new"].(float64) < 5 {
		t.Fatalf("aperçu: %v", pv)
	}
	e.must("POST", "/packs/import", map[string]any{"bundled": "inconnu"}, 404)
	imp := e.must("POST", "/packs/import", map[string]any{"bundled": "mysql", "create_group": true}, 200)
	if imp["created"].(float64) < 5 || imp["group_id"] == "" {
		t.Fatalf("import: %v", imp)
	}
	again := e.must("POST", "/packs/preview", map[string]any{"bundled": "mysql"}, 200)["preview"].(map[string]any)
	if again["counts"].(map[string]any)["new"].(float64) != 0 || again["installed_version"] != "1.0.0" {
		t.Fatalf("réaperçu: %v", again)
	}
	if cmds := e.must("GET", "/commands", nil, 200)["items"].([]any); len(cmds) < 5 || cmds[0].(map[string]any)["source"] != "pack" {
		t.Fatalf("commandes importées: %d", len(cmds))
	}

	// fichier importé: les avertissements doivent être acceptés
	pp := e.must("POST", "/packs/preview", map[string]any{"yaml": pipedPack}, 200)
	if pp["trusted"] != false || pp["preview"].(map[string]any)["warnings"].(float64) == 0 {
		t.Fatalf("avertissements attendus: %v", pp)
	}
	r, body := e.call("POST", "/packs/import", map[string]any{"yaml": pipedPack})
	if r.StatusCode != 400 || body["error"].(map[string]any)["code"] != "import_blocked" {
		t.Fatalf("import sans acceptation: %d %v", r.StatusCode, body)
	}
	e.must("POST", "/packs/import", map[string]any{"yaml": pipedPack, "allow_warnings": true}, 200)
	e.must("POST", "/packs/preview", map[string]any{"yaml": "pack: ["}, 400)

	// export d'un groupe
	groups := e.must("GET", "/command-groups", nil, 200)["items"].([]any)
	resp, y := e.raw("/packs/export?pack_id=mysql-copy&name=Copie&group=" + str(groups[0].(map[string]any), "id"))
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), "mysql-copy.yaml") || !strings.Contains(y, "id: mysql-copy") || !strings.Contains(y, "mysql.processlist") {
		t.Fatalf("export: %d %s", resp.StatusCode, y[:min(200, len(y))])
	}
	if resp, _ := e.raw("/packs/export?ids=inconnu"); resp.StatusCode != 404 {
		t.Fatalf("export vide: %d", resp.StatusCode)
	}
	var n int
	e.st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action='pack.import'`).Scan(&n)
	if n != 2 {
		t.Fatalf("audit pack.import: %d", n)
	}
}

type hookRecv struct {
	mu   sync.Mutex
	hits []struct {
		body []byte
		sig  string
	}
}

func (h *hookRecv) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	h.hits = append(h.hits, struct {
		body []byte
		sig  string
	}{b, r.Header.Get("X-Leassh-Signature")})
	h.mu.Unlock()
	w.WriteHeader(200)
}

func (h *hookRecv) wait(n int) bool {
	for i := 0; i < 200; i++ {
		h.mu.Lock()
		ok := len(h.hits) >= n
		h.mu.Unlock()
		if ok {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestWebhookSettingsAndEvents(t *testing.T) {
	e := newEnv(t)
	rc := &hookRecv{}
	ts := httptest.NewServer(rc)
	defer ts.Close()

	e.must("PUT", "/settings/webhook", map[string]any{"url": "ftp://x", "enabled": false}, 400)
	e.must("POST", "/settings/webhook/test", nil, 400) // non configuré
	e.must("PUT", "/settings/webhook", map[string]any{"url": ts.URL, "secret": "s3cret-hmac", "enabled": true}, 200)
	cfg := e.must("GET", "/settings/webhook", nil, 200)
	if c := cfg["config"].(map[string]any); c["has_secret"] != true || c["url"] != ts.URL || strings.Contains(toJSON(cfg), "s3cret-hmac") {
		t.Fatalf("config: %v", cfg)
	}
	// le secret reste si on renvoie un champ vide
	e.must("PUT", "/settings/webhook", map[string]any{"url": ts.URL, "secret": "", "enabled": true}, 200)

	res := e.must("POST", "/settings/webhook/test", nil, 200)
	if res["ok"] != true || !rc.wait(1) {
		t.Fatalf("test d'envoi: %v", res)
	}
	if rc.hits[0].sig != webhook.Sign("s3cret-hmac", rc.hits[0].body) {
		t.Fatal("signature invalide")
	}

	// événement réel: une proposition d'agent déclenche proposal.created
	var key store.MCPKey
	key.ID, key.Name, key.CanPropose = "k1", "Agent N1", true
	e.st.DB.Exec(`INSERT INTO mcp_keys (id,name,prefix,hash,created_at,can_propose) VALUES ('k1','Agent N1','lsh_aaaa','h','2026-01-01T00:00:00.000Z',1)`)
	if _, _, err := e.props.Create(context.Background(), key, proposals.Payload{Slug: "a.b", Name: "AB", Description: "Une commande de test.", Template: "uptime", Risk: catalog.RiskRead}, "test"); err != nil {
		t.Fatal(err)
	}
	if !rc.wait(2) {
		t.Fatal("proposal.created non reçu")
	}
	var env map[string]any
	json.Unmarshal(rc.hits[1].body, &env)
	if env["event"] != "proposal.created" || env["url"] != "https://leassh.example.com/propositions/"+env["data"].(map[string]any)["id"].(string) {
		t.Fatalf("enveloppe: %s", rc.hits[1].body)
	}
	if d := e.must("GET", "/settings/webhook/deliveries", nil, 200)["items"].([]any); len(d) < 2 {
		t.Fatalf("livraisons: %v", d)
	}
	rows, _ := e.st.DB.Query(`SELECT diff FROM audit_events WHERE action='webhook.update'`)
	defer rows.Close()
	for rows.Next() {
		var d string
		rows.Scan(&d)
		if strings.Contains(d, "s3cret-hmac") {
			t.Fatalf("secret dans l'audit: %s", d)
		}
	}
}

func TestProposalsAPI(t *testing.T) {
	e := newEnv(t)
	e.st.DB.Exec(`INSERT INTO mcp_keys (id,name,prefix,hash,created_at,can_propose) VALUES ('k1','Agent N1','lsh_aaaa','h','2026-01-01T00:00:00.000Z',1)`)
	key := store.MCPKey{ID: "k1", Name: "Agent N1", CanPropose: true}
	good, _, _ := e.props.Create(context.Background(), key, proposals.Payload{Slug: "mysql.replica_status", Name: "Réplication", Description: "État de la réplication MySQL.", Template: `mysql --batch -e "SHOW REPLICA STATUS"`, Risk: catalog.RiskRead}, "diag")
	bad, _, _ := e.props.Create(context.Background(), key, proposals.Payload{Slug: "x.evil", Name: "Evil", Description: "Commande dangereuse.", Template: "uptime; rm -rf /", Risk: catalog.RiskRead}, "x")

	list := e.must("GET", "/proposals", nil, 200)["items"].([]any)
	if len(list) != 2 {
		t.Fatalf("liste: %v", list)
	}
	p := e.must("GET", "/proposals/"+bad.ID, nil, 200)
	if p["status"] != "invalid" || p["payload"].(map[string]any)["template"] != "uptime; rm -rf /" || len(p["lint"].(map[string]any)["errors"].([]any)) == 0 {
		t.Fatalf("détail (payload et lint doivent être des objets JSON): %v", p)
	}
	// linter strict de l'éditeur
	lint := e.must("POST", "/proposals/lint", map[string]any{"slug": "x.evil", "name": "E", "description": "Description suffisante.", "template": "uptime; id", "risk": "read"}, 200)
	if len(lint["lint"].(map[string]any)["errors"].([]any)) == 0 {
		t.Fatalf("lint strict: %v", lint)
	}
	// approbation d'une proposition invalide: refusée; corrigée: acceptée avec groupe
	e.must("POST", "/proposals/"+bad.ID+"/approve", map[string]any{}, 400)
	cg := e.must("POST", "/command-groups", map[string]any{"slug": "diag", "name": "Diag"}, 201)
	fixed := map[string]any{"slug": "x.evil", "name": "Uptime", "description": "Charge du serveur.", "template": "uptime", "risk": "read"}
	ok := e.must("POST", "/proposals/"+bad.ID+"/approve", map[string]any{"edited": fixed, "group_ids": []string{str(cg, "id")}, "comment": "corrigé"}, 200)
	if cmd := ok["command"].(map[string]any); cmd["source"] != "proposal" || cmd["status"] != "enabled" {
		t.Fatalf("commande créée: %v", cmd)
	}
	if g := e.must("GET", "/command-groups", nil, 200)["items"].([]any)[0].(map[string]any); len(g["member_ids"].([]any)) != 1 {
		t.Fatalf("groupe: %v", g)
	}
	e.must("POST", "/proposals/"+bad.ID+"/approve", map[string]any{}, 409)
	e.must("POST", "/proposals/"+good.ID+"/reject", map[string]any{"comment": "doublon"}, 204)
	if e.must("GET", "/proposals/"+good.ID, nil, 200)["status"] != "rejected" {
		t.Fatal("rejet")
	}
}
