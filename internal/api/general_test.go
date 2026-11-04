package api

import "testing"

func TestGeneralSettings(t *testing.T) {
	e := newEnv(t)
	cfg := e.must("GET", "/settings/general", nil, 200)
	if cfg["session_ttl_hours"] != float64(12) || cfg["approval_ttl_minutes"] != float64(30) || cfg["log_retention_days"] != float64(365) || cfg["log_level"] != "info" {
		t.Fatalf("défauts inattendus: %v", cfg)
	}
	cfg["base_url"] = "https://leassh.example.com/"
	cfg["trusted_proxies"] = "10.0.0.0/8, 192.168.1.5"
	cfg["session_ttl_hours"] = 4
	out := e.must("PUT", "/settings/general", cfg, 200)
	if out["base_url"] != "https://leassh.example.com" || out["session_ttl_hours"] != float64(4) {
		t.Fatalf("enregistrement: %v", out)
	}
	// L'URL fixe pilote l'URL de redirection SSO.
	oc := e.must("GET", "/settings/oidc", nil, 200)
	if oc["redirect_url"] != "https://leassh.example.com/api/v1/auth/oidc/callback" {
		t.Fatalf("redirect_url: %v", oc["redirect_url"])
	}
	for name, mod := range map[string]func(m map[string]any){
		"url avec chemin":    func(m map[string]any) { m["base_url"] = "https://x.example.com/app" },
		"url sans schéma":    func(m map[string]any) { m["base_url"] = "leassh.example.com" },
		"proxy invalide":     func(m map[string]any) { m["trusted_proxies"] = "pas-une-ip" },
		"session nulle":      func(m map[string]any) { m["session_ttl_hours"] = 0 },
		"niveau inconnu":     func(m map[string]any) { m["log_level"] = "bavard" },
		"rétention négative": func(m map[string]any) { m["log_retention_days"] = -1 },
	} {
		bad := e.must("GET", "/settings/general", nil, 200)
		mod(bad)
		if r, _ := e.call("PUT", "/settings/general", bad); r.StatusCode != 400 {
			t.Errorf("%s: %d attendu 400", name, r.StatusCode)
		}
	}
}
