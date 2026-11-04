package warpgate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leassh-dev/leassh/internal/provisioning"
)

const sample = `[
 {"description":"[phpipam-sync] Proxmox Server 01 - Gîte","external_host":null,"group":{"color":null,"id":"g1","name":"Hyperviseurs"},"id":"t1","kind":"Ssh","name":"pve01 - SSH root"},
 {"description":"Outils (dev)","group":{"id":"g2","name":"Web Sites"},"id":"t2","kind":"Ssh","name":"reservations-dev - LXC ct145"},
 {"description":"sans groupe","group":null,"id":"t3","kind":"Ssh","name":"seul"},
 {"description":"web","id":"t4","kind":"Http","name":"interface web"},
 {"description":"base","id":"t5","kind":"MySql","name":"bdd"}
]`

func cfg(url string) map[string]any {
	return map[string]any{"url": url, "ssh_host": "192.168.6.163", "ssh_port": float64(2222), "host_key_fingerprint": "SHA256:pin"}
}

func TestDiscover(t *testing.T) {
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken = r.URL.Path, r.Header.Get("X-Warpgate-Token")
		w.Write([]byte(sample))
	}))
	defer srv.Close()
	c, err := New(cfg(srv.URL+"/"), "secret-token", provisioning.Env{User: "ia"})
	if err != nil {
		t.Fatal(err)
	}
	ms, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/@warpgate/api/targets" || gotToken != "secret-token" {
		t.Fatalf("requête: %s %q", gotPath, gotToken)
	}
	if len(ms) != 3 { // les cibles non SSH sont ignorées
		t.Fatalf("machines: %+v", ms)
	}
	m := ms[0]
	if m.ExternalID != "t1" || m.Name != "pve01 - SSH root" || m.Description != "[phpipam-sync] Proxmox Server 01 - Gîte" || m.GroupID != "g1" || m.GroupName != "Hyperviseurs" ||
		m.Host != "192.168.6.163" || m.Port != 2222 || m.SSHUser != "ia:pve01 - SSH root" || m.HostKeyFingerp != "SHA256:pin" {
		t.Fatalf("machine: %+v", m)
	}
	if ms[2].GroupID != "" {
		t.Fatalf("machine sans groupe: %+v", ms[2])
	}
	if h, p := c.(provisioning.Bastion).Endpoint(); h != "192.168.6.163" || p != 2222 {
		t.Fatal("endpoint")
	}
}

func TestDiscoverErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"jeton refusé": {401, "", "refuse le jeton"},
		"page HTML":    {200, "<html>login</html>", "illisible"},
		"erreur 500":   {500, "", "HTTP 500"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer srv.Close()
			c, _ := New(cfg(srv.URL), "t", provisioning.Env{User: "ia"})
			if _, err := c.Discover(context.Background()); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("erreur: %v", err)
			}
		})
	}
}

func TestNewValidates(t *testing.T) {
	for name, c := range map[string]map[string]any{
		"url":  {"url": "ftp://x", "ssh_host": "h"},
		"host": {"url": "https://x"},
		"port": {"url": "https://x", "ssh_host": "h", "ssh_port": float64(70000)},
	} {
		if _, err := New(c, "t", provisioning.Env{}); err == nil {
			t.Errorf("%s: configuration invalide acceptée", name)
		}
	}
	if _, err := New(cfg("https://x"), " ", provisioning.Env{}); err == nil {
		t.Error("jeton vide accepté")
	}
}
