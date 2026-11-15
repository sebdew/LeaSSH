package api

import (
	"testing"

	"github.com/sebdew/LeaSSH/internal/updatecheck"
)

// Le ticket #43 : l'interface doit connaître la version qui tourne, son auteur et le dépôt du projet.
func TestMetaExposesVersionAuthorAndRepository(t *testing.T) {
	e := newEnv(t)
	m := e.must("GET", "/meta", nil, 200)
	if m["version"] != "test" {
		t.Fatalf("version absente ou fausse: %v", m)
	}
	if m["author"] != "Sébastien Dewarlez" {
		t.Fatalf("auteur absent ou faux: %v", m)
	}
	if m["repository_url"] != "https://github.com/sebdew/LeaSSH" {
		t.Fatalf("dépôt absent ou faux: %v", m)
	}
}

// /meta reste réservé aux sessions authentifiées : la version n'est pas divulguée à un anonyme.
func TestMetaRequiresSession(t *testing.T) {
	ts, _ := newTest(t)
	if r, _ := do(t, client(), "GET", ts.URL+"/api/v1/meta", nil, nil); r.StatusCode != 401 {
		t.Fatalf("meta sans session: %d", r.StatusCode)
	}
}

// Sans vérificateur attaché (ou désactivé), /meta annonce qu'aucune mise à jour n'est connue.
func TestMetaReportsUpdate(t *testing.T) {
	e := newEnv(t)
	m := e.must("GET", "/meta", nil, 200)
	if m["update_available"] != false {
		t.Fatalf("update_available: %v", m)
	}
	e.srv.updates = fakeUpdates{updatecheck.Status{Enabled: true, Available: true, Latest: "v9.9.9", URL: "https://github.com/sebdew/LeaSSH/releases/tag/v9.9.9"}}
	m = e.must("GET", "/meta", nil, 200)
	if m["update_available"] != true || m["latest_version"] != "v9.9.9" || m["release_url"] != "https://github.com/sebdew/LeaSSH/releases/tag/v9.9.9" {
		t.Fatalf("meta: %v", m)
	}
}

type fakeUpdates struct{ s updatecheck.Status }

func (f fakeUpdates) Status() updatecheck.Status { return f.s }
