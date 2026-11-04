package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func rawGet(e *env, path string) (*http.Response, string) {
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

func rawPost(e *env, path, body string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/v1"+path, strings.NewReader(body))
	req.Header.Set("X-CSRF-Token", e.csrf)
	resp, err := e.c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestAuditReportLifecycle(t *testing.T) {
	e := newEnv(t)
	e.must("POST", "/identities", map[string]any{"name": "secret-ident", "username": "u", "mode": "import", "private_key": string(e.ssh.ClientPEM)}, 201)
	today := time.Now().UTC().Format("2006-01-02")

	// validations de période
	e.must("POST", "/audit-reports", map[string]any{"from": "2026-13-01"}, 400)
	e.must("POST", "/audit-reports", map[string]any{"from": today, "to": "2000-01-01"}, 400)
	e.must("POST", "/audit-reports", map[string]any{"from": "2000-01-01", "to": today}, 400) // plus de 400 jours
	e.must("POST", "/audit-reports", map[string]any{"to": time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")}, 400)

	rep := e.must("POST", "/audit-reports", map[string]any{}, 201) // période par défaut: 30 jours
	id := str(rep, "id")
	if id == "" || len(str(rep, "sha256")) != 64 || strings.Contains(toJSON(rep), "snapshot") {
		t.Fatalf("rapport créé: %v", rep)
	}
	items := e.must("GET", "/audit-reports", nil, 200)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("historique: %v", items)
	}
	full := e.must("GET", "/audit-reports/"+id, nil, 200)
	snap := full["snapshot"].(map[string]any)
	if snap["kind"] != "leassh-audit-report" || snap["leassh_version"] != "test" || snap["controls"] == nil {
		t.Fatalf("instantané: %v", snap)
	}

	// HTML autonome et JSON signé
	resp, page := rawGet(e, "/audit-reports/"+id+"/html")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") || !strings.Contains(page, "aide à l'audit") || strings.Contains(page, "<script") {
		t.Fatalf("html: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if dl, _ := rawGet(e, "/audit-reports/"+id+"/html?download=1"); !strings.Contains(dl.Header.Get("Content-Disposition"), "attachment") {
		t.Fatal("téléchargement HTML sans Content-Disposition")
	}
	resp, file := rawGet(e, "/audit-reports/"+id+"/json")
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), ".json") || !strings.Contains(file, `"digest": "`+str(rep, "sha256")+`"`) {
		t.Fatalf("json: %d", resp.StatusCode)
	}
	if strings.Contains(file, "PRIVATE KEY") || strings.Contains(file, "private_key") {
		t.Fatal("un secret figure dans le rapport")
	}

	// vérification: intact / falsifié / étranger
	_, ok := rawPost(e, "/audit-reports/verify", file)
	if !strings.Contains(ok, `"digest_ok":true`) || !strings.Contains(ok, `"tag_ok":true`) {
		t.Fatalf("vérification d'un fichier intact: %s", ok)
	}
	forged := strings.Replace(file, `"integrity_chains`, `"integrity_chains`, 1)
	forged = strings.Replace(forged, `"checked": `, `"checked": 9`, 1)
	if forged == file {
		t.Fatal("falsification de test sans effet")
	}
	_, bad := rawPost(e, "/audit-reports/verify", forged)
	if !strings.Contains(bad, `"digest_ok":false`) {
		t.Fatalf("un fichier falsifié doit être rejeté: %s", bad)
	}
	if r, _ := rawPost(e, "/audit-reports/verify", `{"hello":1}`); r.StatusCode != 400 {
		t.Fatalf("fichier étranger: %d", r.StatusCode)
	}
	e.must("GET", "/audit-reports/inconnu", nil, 404)

	// l'événement d'audit existe, sans contenu du rapport
	a := toJSON(e.must("GET", "/audit?action=audit_report.generate", nil, 200))
	if !strings.Contains(a, "audit_report.generate") || strings.Contains(a, "snapshot") {
		t.Fatalf("audit: %s", a)
	}

	// authentification requise
	if r, _ := do(t, client(), "GET", e.ts.URL+"/api/v1/audit-reports", nil, nil); r.StatusCode != 401 {
		t.Fatalf("accès anonyme: %d", r.StatusCode)
	}
}

func TestAuditSummaryViaAI(t *testing.T) {
	e := newEnv(t)
	var gotAuth, gotBody string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotAuth, gotBody = r.Header.Get("Authorization"), string(b)
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(`{"model":"modele-test","choices":[{"message":{"content":"Résumé\n\nPoints d'attention\n- x"}}]}`))
	}))
	defer llm.Close()

	// données sensibles qui ne doivent JAMAIS partir
	e.must("POST", "/identities", map[string]any{"name": "identité-confidentielle", "username": "u", "mode": "import", "private_key": string(e.ssh.ClientPEM)}, 201)
	e.must("POST", "/keys", map[string]any{"name": "clé-confidentielle", "rule_ids": []string{}}, 201)
	rep := e.must("POST", "/audit-reports", map[string]any{}, 201)
	id := str(rep, "id")

	// désactivée par défaut
	if cfg := e.must("GET", "/settings/audit-ai", nil, 200)["config"].(map[string]any); cfg["enabled"] != false {
		t.Fatalf("la synthèse IA doit être désactivée par défaut: %v", cfg)
	}
	if r, _ := e.call("POST", "/audit-reports/"+id+"/summary", nil); r.StatusCode != 409 {
		t.Fatalf("synthèse sans activation: %d", r.StatusCode)
	}
	e.must("PUT", "/settings/audit-ai", map[string]any{"enabled": true, "url": "", "model": ""}, 400) // incomplet
	e.must("PUT", "/settings/audit-ai", map[string]any{"enabled": true, "url": "ftp://x", "model": "m"}, 400)
	put := e.must("PUT", "/settings/audit-ai", map[string]any{"enabled": true, "url": llm.URL + "/v1", "model": "modele-test", "api_key": "sk-tres-secret"}, 200)
	if strings.Contains(toJSON(put), "sk-tres-secret") || put["config"].(map[string]any)["has_key"] != true {
		t.Fatalf("la clé d'API ne doit jamais être renvoyée: %v", put)
	}
	if strings.Contains(toJSON(e.must("GET", "/settings/audit-ai", nil, 200)), "sk-tres-secret") {
		t.Fatal("clé d'API renvoyée par GET")
	}

	out := e.must("POST", "/audit-reports/"+id+"/summary", nil, 200)
	if out["model"] != "modele-test" || !strings.Contains(str(out, "summary"), "Points d'attention") {
		t.Fatalf("synthèse: %v", out)
	}
	if gotAuth != "Bearer sk-tres-secret" {
		t.Fatalf("en-tête d'autorisation: %q", gotAuth)
	}
	for _, leak := range []string{"identité-confidentielle", "clé-confidentielle", "127.0.0.1", "lsh_", "PRIVATE KEY"} {
		if strings.Contains(strings.ToLower(gotBody), strings.ToLower(leak)) {
			t.Errorf("%q a été envoyé à l'IA: %s", leak, gotBody)
		}
	}
	if !strings.Contains(gotBody, "requests") || strings.Contains(gotBody, "snapshot") {
		t.Fatalf("seuls des agrégats doivent partir: %s", gotBody)
	}
	// la synthèse s'affiche à part dans le HTML et ne change ni l'empreinte ni le JSON signé
	_, page := rawGet(e, "/audit-reports/"+id+"/html")
	if !strings.Contains(page, "Synthèse rédigée par une IA — à relire") || !strings.Contains(page, "modele-test") {
		t.Fatal("la synthèse doit figurer dans le HTML, étiquetée")
	}
	if again := e.must("GET", "/audit-reports/"+id, nil, 200)["report"].(map[string]any); again["sha256"] != rep["sha256"] {
		t.Fatal("la synthèse IA ne doit pas modifier l'empreinte des faits")
	}
	_, file := rawGet(e, "/audit-reports/"+id+"/json")
	if strings.Contains(file, "Points d'attention") {
		t.Fatal("la synthèse IA ne doit pas être dans le JSON signé")
	}
	if !strings.Contains(toJSON(e.must("GET", "/audit?action=audit_report.summary", nil, 200)), "audit_report.summary") {
		t.Fatal("événement audit_report.summary absent")
	}
	if strings.Contains(toJSON(e.must("GET", "/audit", nil, 200)), "sk-tres-secret") {
		t.Fatal("la clé d'API ne doit apparaître dans aucun diff d'audit")
	}

	// modèle injoignable: erreur claire, rien d'enregistré
	llm.Close()
	if r, body := e.call("POST", "/audit-reports/"+id+"/summary", nil); r.StatusCode != 502 || !strings.Contains(toJSON(body), "ai_error") {
		t.Fatalf("modèle injoignable: %d %v", r.StatusCode, body)
	}
}

func TestLanguageErrorsAndAISummary(t *testing.T) {
	e := newEnv(t)
	hdr := func(lang string) map[string]string {
		h := map[string]string{"X-CSRF-Token": e.csrf}
		if lang != "" {
			h["Accept-Language"] = lang
		}
		return h
	}
	msg := func(lang, method, path string, body any) (int, string, string) {
		r, out := do(t, e.c, method, e.ts.URL+"/api/v1"+path, body, hdr(lang))
		er, _ := out["error"].(map[string]any)
		return r.StatusCode, str(er, "code"), str(er, "message")
	}

	// le code ne change jamais; le message suit Accept-Language, le français reste la valeur par défaut
	for _, tc := range []struct{ lang, want string }{{"", "identifiants incorrects"}, {"fr", "identifiants incorrects"}, {"en-GB,en;q=0.9", "invalid credentials"}} {
		r, out := do(t, client(), "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "admin", Password: "faux"}, map[string]string{"Accept-Language": tc.lang})
		er := out["error"].(map[string]any)
		if r.StatusCode != 401 || er["code"] != "invalid_credentials" || er["message"] != tc.want {
			t.Errorf("lang=%q: %d %v", tc.lang, r.StatusCode, er)
		}
	}
	if code, c, m := msg("en", "GET", "/servers/inconnu", nil); code != 404 || c != "not_found" || m != "resource not found" {
		t.Errorf("404: %d %s %s", code, c, m)
	}
	if _, c, m := msg("en", "PUT", "/settings/oidc", map[string]any{"enabled": true}); c != "invalid_oidc" || m != "issuer and client ID are required to enable SSO" {
		t.Errorf("validation: %s %s", c, m)
	}
	// partie dynamique conservée; message inconnu laissé tel quel
	if _, _, m := msg("en", "GET", "/executions?period=1y", nil); m == "" || strings.Contains(m, "période") {
		t.Errorf("période: %q", m)
	}
	// les réponses de succès ne sont pas touchées
	if code, _, _ := msg("en", "GET", "/servers", nil); code != 200 {
		t.Errorf("succès: %d", code)
	}

	// la synthèse IA est rédigée dans la langue demandée
	var system string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		system = req.Messages[0].Content
		w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer llm.Close()
	e.must("PUT", "/settings/audit-ai", map[string]any{"enabled": true, "url": llm.URL + "/v1", "model": "m"}, 200)
	id := str(e.must("POST", "/audit-reports", map[string]any{}, 201), "id")
	for _, tc := range []struct {
		body map[string]any
		lang string
		want string
	}{{nil, "", "Écris en français"}, {map[string]any{"lang": "en"}, "", "Write in English"}, {nil, "en", "Write in English"}, {map[string]any{"lang": "fr"}, "en", "Écris en français"}} {
		if r, out := do(t, e.c, "POST", e.ts.URL+"/api/v1/audit-reports/"+id+"/summary", tc.body, hdr(tc.lang)); r.StatusCode != 200 {
			t.Fatalf("synthèse: %d %v", r.StatusCode, out)
		}
		if !strings.Contains(system, tc.want) {
			t.Errorf("body=%v lang=%q: prompt sans %q", tc.body, tc.lang, tc.want)
		}
	}
	if code, _, m := msg("en", "POST", "/audit-reports/"+id+"/summary", map[string]any{"bogus": 1}); code != 400 || m != "invalid JSON body" {
		t.Errorf("corps inconnu: %d %q", code, m)
	}
}
