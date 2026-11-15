package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func release(t *testing.T, body string, code int) (*Checker, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") != "leassh-update-check" {
			t.Errorf("User-Agent: %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	c := New(true, "v0.1.0", nil)
	c.URL = ts.URL
	return c, &hits
}

const relOK = `{"tag_name":"v0.2.0","html_url":"https://github.com/sebdew/LeaSSH/releases/tag/v0.2.0"}`

func TestNewerReleaseIsReported(t *testing.T) {
	c, _ := release(t, relOK, 200)
	c.Refresh(context.Background())
	s := c.Status()
	if !s.Available || s.Latest != "v0.2.0" || s.URL != "https://github.com/sebdew/LeaSSH/releases/tag/v0.2.0" {
		t.Fatalf("statut: %+v", s)
	}
}

func TestNoUpdateWhenCurrentOrNewer(t *testing.T) {
	for _, cur := range []string{"v0.2.0", "v0.3.0", "v0.2.0-5-gabcdef", "dev", ""} {
		c, _ := release(t, relOK, 200)
		c.current = cur
		c.Refresh(context.Background())
		if c.Status().Available {
			t.Fatalf("version %q: mise à jour signalée à tort", cur)
		}
	}
}

func TestBuildAfterOlderTagIsOutdated(t *testing.T) {
	c, _ := release(t, relOK, 200)
	c.current = "v0.1.0-51-g77aec0c" // build intermédiaire après v0.1.0
	c.Refresh(context.Background())
	if !c.Status().Available {
		t.Fatal("un build postérieur à v0.1.0 doit voir v0.2.0")
	}
}

func TestDisabledNeverCallsOut(t *testing.T) {
	c, hits := release(t, relOK, 200)
	c.enabled = false
	c.Refresh(context.Background())
	if hits.Load() != 0 || c.Status().Available || c.Status().Enabled {
		t.Fatalf("vérification désactivée mais appel émis (%d)", hits.Load())
	}
}

func TestBadAnswersAreIgnored(t *testing.T) {
	cases := map[string]struct {
		body string
		code int
	}{
		"erreur HTTP":     {relOK, 500},
		"JSON invalide":   {"<html>", 200},
		"tag non SemVer":  {`{"tag_name":"latest","html_url":"https://github.com/sebdew/LeaSSH/releases"}`, 200},
		"pré-version":     {`{"tag_name":"v9.0.0-rc1","html_url":"https://github.com/sebdew/LeaSSH/releases/tag/v9.0.0-rc1"}`, 200},
		"lien hors dépôt": {`{"tag_name":"v0.2.0","html_url":"javascript:alert(1)"}`, 200},
		"lien autre hôte": {`{"tag_name":"v0.2.0","html_url":"https://evil.example/sebdew/LeaSSH"}`, 200},
		"réponse énorme":  {`{"tag_name":"v0.2.0","html_url":"https://github.com/sebdew/LeaSSH/","x":"` + strings.Repeat("a", 2<<20) + `"}`, 200},
	}
	for name, tc := range cases {
		c, _ := release(t, tc.body, tc.code)
		c.Refresh(context.Background())
		if c.Status().Available {
			t.Fatalf("%s: mise à jour signalée à tort: %+v", name, c.Status())
		}
	}
}
