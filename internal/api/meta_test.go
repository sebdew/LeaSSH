package api

import "testing"

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
