package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const wgTargets = `[
 {"description":"Proxmox 01","group":{"id":"g1","name":"Hyperviseurs"},"id":"t1","kind":"Ssh","name":"pve01 - SSH root"},
 {"description":"Dev résa","group":{"id":"g2","name":"Web Sites"},"id":"t2","kind":"Ssh","name":"reservations-dev - LXC ct145"},
 {"description":"sans groupe","id":"t3","kind":"Ssh","name":"seul"}
]`

func list(e *env, path string) []any {
	e.t.Helper()
	return e.must("GET", path, nil, 200)["items"].([]any)
}

func find(items []any, key, val string) map[string]any {
	for _, it := range items {
		if m := it.(map[string]any); str(m, key) == val {
			return m
		}
	}
	return nil
}

func TestProvisioningLifecycleAndLocks(t *testing.T) {
	e := newEnv(t)
	wg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Warpgate-Token") != "jeton-secret" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(wgTargets))
	}))
	defer wg.Close()

	ident := e.must("POST", "/identities", map[string]any{"name": "wg", "username": "ia", "mode": "import", "private_key": string(e.ssh.ClientPEM)}, 201)["identity"].(map[string]any)
	cg := e.must("POST", "/command-groups", map[string]any{"slug": "diag", "name": "Diag"}, 201)
	sg := e.must("POST", "/server-groups", map[string]any{"slug": "autre", "name": "Autre"}, 201)
	rule := e.must("POST", "/rules", map[string]any{"name": "défaut", "server_group_ids": []any{sg["id"]}, "command_group_ids": []any{cg["id"]}, "profile": "RO"}, 201)

	// types disponibles, sans secret
	types := list(e, "/provisioners/types")
	if find(types, "id", "warpgate") == nil {
		t.Fatal("type warpgate absent")
	}

	body := map[string]any{"name": "bastion", "type": "warpgate", "identity_id": ident["id"], "interval_min": 0, "rule_ids": []any{rule["id"]},
		"config": map[string]any{"url": wg.URL, "ssh_host": "10.0.0.9", "ssh_port": 2222}, "secret": "jeton-secret"}
	e.must("POST", "/provisioners", map[string]any{"name": "mauvais", "type": "inconnu", "identity_id": ident["id"]}, 400)
	p := e.must("POST", "/provisioners", body, 201)
	if strings.Contains(toJSON(p), "jeton-secret") || p["has_secret"] != true {
		t.Fatalf("le secret ne doit jamais être renvoyé: %v", p)
	}
	pid := str(p, "id")

	// aperçu: rien n'est écrit
	prev := e.must("POST", "/provisioners/"+pid+"/preview", nil, 200)
	if len(prev["changes"].([]any)) == 0 || len(list(e, "/servers")) != 0 {
		t.Fatalf("aperçu: %v", prev)
	}
	e.must("POST", "/provisioners/"+pid+"/sync", nil, 200)
	servers := list(e, "/servers")
	if len(servers) != 3 {
		t.Fatalf("serveurs: %d", len(servers))
	}
	pve := find(servers, "name", "pve01 - SSH root")
	if pve["managed_by"] != pid || pve["ssh_user"] != "ia:pve01 - SSH root" || pve["description"] != "Proxmox 01" || pve["host"] != "10.0.0.9" {
		t.Fatalf("serveur synchronisé: %v", pve)
	}

	// verrous
	e.must("PUT", "/servers/"+str(pve, "id"), map[string]any{"name": "autre nom", "slug": str(pve, "slug"), "host": "10.0.0.9", "port": 2222, "identity_id": ident["id"], "ssh_user": str(pve, "ssh_user"), "description": "Proxmox 01", "enabled": true}, 409)
	e.must("DELETE", "/servers/"+str(pve, "id"), nil, 409)
	// l'utilisateur SSH, lui, se modifie, et devient un choix de l'admin
	custom := e.must("PUT", "/servers/"+str(pve, "id"), map[string]any{"name": "pve01 - SSH root", "slug": str(pve, "slug"), "host": "10.0.0.9", "port": 2222, "identity_id": ident["id"], "ssh_user": "root:pve01", "description": "Proxmox 01", "enabled": true}, 200)
	if custom["ssh_user"] != "root:pve01" || custom["ssh_user_custom"] != true {
		t.Fatalf("utilisateur SSH: %v", custom)
	}
	pve["ssh_user"] = "root:pve01"
	// « suivre la synchro » rend la main: le drapeau tombe, la valeur sera recalculée par la prochaine synchro
	back := e.must("PUT", "/servers/"+str(pve, "id"), map[string]any{"name": "pve01 - SSH root", "slug": str(pve, "slug"), "host": "10.0.0.9", "port": 2222, "identity_id": ident["id"], "ssh_user": "ignoré", "ssh_user_follow_sync": true, "description": "Proxmox 01", "enabled": true}, 200)
	if back["ssh_user_custom"] != false || back["ssh_user"] != "root:pve01" {
		t.Fatalf("suivre la synchro: %v", back)
	}
	e.must("POST", "/provisioners/"+pid+"/sync", nil, 200)
	if got := find(list(e, "/servers"), "id", str(pve, "id")); got["ssh_user"] != "ia:pve01 - SSH root" {
		t.Fatalf("la synchro devait recalculer l'utilisateur: %v", got["ssh_user"])
	}
	e.must("PUT", "/servers/"+str(pve, "id"), map[string]any{"name": "pve01 - SSH root", "slug": str(pve, "slug"), "host": "10.0.0.9", "port": 2222, "identity_id": ident["id"], "ssh_user": "root:pve01", "description": "Proxmox 01", "enabled": true}, 200)
	e.must("PUT", "/servers/"+str(pve, "id"), map[string]any{"name": "pve01 - SSH root", "slug": str(pve, "slug"), "host": "10.0.0.9", "port": 2222, "identity_id": ident["id"], "ssh_user": "root:pve01", "description": "Proxmox 01", "enabled": true, "tags": []string{"hyperviseur"}}, 200)

	groups := list(e, "/server-groups")
	hyp := find(groups, "name", "Hyperviseurs")
	if hyp == nil || hyp["managed_by"] != pid {
		t.Fatalf("groupe synchronisé: %v", hyp)
	}
	e.must("PUT", "/server-groups/"+str(hyp, "id"), map[string]any{"slug": "x", "name": "x"}, 409)
	e.must("DELETE", "/server-groups/"+str(hyp, "id"), nil, 409)
	// retirer tous les membres ne retire pas ceux de la synchro
	m := e.must("PUT", "/server-groups/"+str(hyp, "id")+"/members", map[string]any{"ids": []string{}}, 200)
	if len(m["member_ids"].([]any)) != 1 {
		t.Fatalf("membre synchronisé retiré: %v", m)
	}
	// la règle par défaut a reçu le groupe synchronisé, sans perdre l'existant
	r := find(list(e, "/rules"), "id", str(rule, "id"))
	if len(r["server_group_ids"].([]any)) != 3 {
		t.Fatalf("règle par défaut: %v", r["server_group_ids"])
	}

	// jeton révoqué côté source: l'erreur est claire et l'état de synchro la retient
	e.must("PUT", "/provisioners/"+pid, map[string]any{"name": "bastion", "type": "warpgate", "identity_id": ident["id"], "config": body["config"], "secret": "mauvais"}, 200)
	rr, out := e.call("POST", "/provisioners/"+pid+"/sync", nil)
	if rr.StatusCode != 502 || !strings.Contains(toJSON(out), "refuse le jeton") {
		t.Fatalf("jeton refusé: %d %v", rr.StatusCode, out)
	}
	if st := find(list(e, "/provisioners"), "id", pid); st["last_sync_status"] != "error" {
		t.Fatalf("état: %v", st)
	}

	// suppression: tout est rendu à la gestion manuelle, rien n'est supprimé
	e.must("DELETE", "/provisioners/"+pid, nil, 204)
	if pve = find(list(e, "/servers"), "name", "pve01 - SSH root"); pve == nil || pve["managed_by"] != "" {
		t.Fatalf("serveur après suppression: %v", pve)
	}
	e.must("DELETE", "/servers/"+str(pve, "id"), nil, 204)
	// audit présent
	if !strings.Contains(toJSON(list(e, "/audit")), "provisioner.sync") {
		t.Fatal("événement provisioner.sync absent du journal d'administration")
	}
}

func TestRuleAcceptsSeveralGroups(t *testing.T) {
	e := newEnv(t)
	s1 := e.must("POST", "/server-groups", map[string]any{"slug": "s1", "name": "S1"}, 201)
	s2 := e.must("POST", "/server-groups", map[string]any{"slug": "s2", "name": "S2"}, 201)
	c1 := e.must("POST", "/command-groups", map[string]any{"slug": "c1", "name": "C1"}, 201)
	c2 := e.must("POST", "/command-groups", map[string]any{"slug": "c2", "name": "C2"}, 201)
	e.must("POST", "/rules", map[string]any{"name": "vide", "server_group_ids": []any{}, "command_group_ids": []any{c1["id"]}, "profile": "RO"}, 400)
	r := e.must("POST", "/rules", map[string]any{"name": "multi", "server_group_ids": []any{s1["id"], s2["id"]}, "command_group_ids": []any{c1["id"], c2["id"]}, "profile": "RO"}, 201)
	got := find(list(e, "/rules"), "id", str(r, "id"))
	if len(got["server_group_ids"].([]any)) != 2 || len(got["command_group_ids"].([]any)) != 2 {
		t.Fatalf("règle: %v", got)
	}
}

func TestDashboardInsightsEndpoint(t *testing.T) {
	e := newEnv(t)
	out := e.must("GET", "/dashboard/insights", nil, 200)
	if out["days"].(float64) != 30 || out["requests"].(float64) != 0 || out["hourly"] == nil || out["top_commands"] == nil {
		t.Fatalf("période par défaut ou listes nulles: %v", out)
	}
	e.must("GET", "/dashboard/insights?days=7", nil, 200)
	e.must("GET", "/dashboard/insights?days=90", nil, 200)
	e.must("GET", "/dashboard/insights?days=1000", nil, 400)
}
