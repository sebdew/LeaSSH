package api

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/executions"
	"github.com/leassh-dev/leassh/internal/store"
	"github.com/leassh-dev/leassh/internal/syslog/syslogtest"
)

func TestSyslogSettingsAdminOnly(t *testing.T) {
	e := newEnv(t)
	hash, _ := crypto.HashPassword("mot-de-passe-viewer")
	if err := e.st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := store.InsertUser(context.Background(), tx, store.User{Username: "viewer", Role: "viewer", PasswordHash: hash})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c := client()
	_, out := do(t, c, "POST", e.ts.URL+"/api/v1/auth/login", credentials{Username: "viewer", Password: "mot-de-passe-viewer"}, nil)
	h := map[string]string{"X-CSRF-Token": str(out, "csrf_token")}
	for _, call := range [][2]string{{"GET", "/settings/syslog"}, {"PUT", "/settings/syslog"}, {"POST", "/settings/syslog/test"}} {
		if r, _ := do(t, c, call[0], e.ts.URL+"/api/v1"+call[1], map[string]any{}, h); r.StatusCode != 403 {
			t.Errorf("%s %s: %d (403 attendu)", call[0], call[1], r.StatusCode)
		}
	}
}

func TestSyslogSettingsAndForwarding(t *testing.T) {
	e := newEnv(t)
	srv := syslogtest.New(t, "tls")
	crt, key, cas := syslogtest.ClientCert(t)
	srv.ClientCAs = cas
	srv.Stop()
	srv.Start()

	got := e.must("GET", "/settings/syslog", nil, 200)
	if c := got["config"].(map[string]any); c["enabled"] != false || c["protocol"] != "udp" || c["facility"] != "local0" {
		t.Fatalf("défauts: %v", c)
	}
	e.must("POST", "/settings/syslog/test", nil, 400) // pas d'hôte
	e.must("PUT", "/settings/syslog", map[string]any{"enabled": true}, 400)
	e.must("PUT", "/settings/syslog", map[string]any{"host": "x", "protocol": "smtp"}, 400)

	cfg := map[string]any{"enabled": true, "protocol": "tls", "host": srv.Host, "port": srv.Port, "message_format": "json", "facility": "local4", "app_name": "leassh",
		"min_severity": "info", "types": []string{"execution", "audit"}, "tls_ca_pem": srv.CAPEM, "tls_client_cert_pem": crt, "tls_client_key_pem": key}
	out := e.must("PUT", "/settings/syslog", cfg, 200)
	if c := out["config"].(map[string]any); c["has_tls_client_key"] != true || strings.Contains(toJSON(out), "PRIVATE KEY") {
		t.Fatalf("config: %v", c)
	}
	if strings.Contains(toJSON(e.must("GET", "/settings/syslog", nil, 200)), "PRIVATE KEY") {
		t.Fatal("clé privée renvoyée par l'API")
	}
	if res := e.must("POST", "/settings/syslog/test", nil, 200); res["ok"] != true {
		t.Fatalf("test: %v", res)
	}
	if m := srv.WaitFor(1)[0]; !strings.Contains(m, `"event":"syslog.test"`) {
		t.Fatalf("message de test: %s", m)
	}

	// une demande refusée (serveur inconnu) est journalisée ET envoyée en syslog, avec son contexte
	adminCtx := context.Background()
	if _, err := e.exec.Run(adminCtx, executions.Request{Actor: executions.Actor{User: &store.User{ID: "u", Username: "sd", Role: "admin"}, IP: "10.1.2.3"}, Server: "nope", Command: "x", Reason: "essai"}); err != nil {
		t.Fatal(err)
	}
	var denied string
	for i := 0; i < 100 && denied == ""; i++ {
		for _, m := range srv.Messages() {
			if strings.Contains(m, `"event":"execution.denied"`) {
				denied = m
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if denied == "" || !strings.Contains(denied, `"server":"nope"`) || !strings.Contains(denied, `"client_ip":"10.1.2.3"`) {
		t.Fatalf("refus non envoyé: %q", srv.Messages())
	}

	// la modification et le test sont audités, sans secret
	rows, err := e.st.DB.Query(`SELECT action, diff FROM audit_events WHERE action LIKE 'syslog.%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actions := map[string]int{}
	for rows.Next() {
		var a, d string
		rows.Scan(&a, &d)
		actions[a]++
		if strings.Contains(d, "PRIVATE KEY") || strings.Contains(d, "BEGIN") {
			t.Fatalf("secret dans l'audit: %s", d)
		}
	}
	if actions["syslog.update"] != 1 || actions["syslog.test"] != 1 {
		t.Fatalf("audit: %v", actions)
	}
	// la modification elle-même est transmise (événement d'administration) sur le même canal
	var seen bool
	for i := 0; i < 100 && !seen; i++ {
		for _, m := range srv.Messages() {
			seen = seen || strings.Contains(m, `"event":"audit.syslog.test"`)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !seen {
		t.Fatalf("audit.syslog.test non reçu: %q", srv.Messages())
	}

	// désactivation + effacement de la clé
	e.must("PUT", "/settings/syslog", map[string]any{"enabled": false, "protocol": "tls", "host": srv.Host, "port": srv.Port, "clear_tls_client_key": true, "tls_client_cert_pem": ""}, 200)
	if c := e.must("GET", "/settings/syslog", nil, 200)["config"].(map[string]any); c["has_tls_client_key"] != false || c["enabled"] != false {
		t.Fatalf("config: %v", c)
	}
}
