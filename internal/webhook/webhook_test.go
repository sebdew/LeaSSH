package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

type recv struct {
	mu   sync.Mutex
	hits []hit
	fail int // nombre de réponses 500 avant succès
}
type hit struct {
	body   []byte
	header http.Header
}

func (r *recv) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.hits = append(r.hits, hit{b, req.Header.Clone()})
	n := len(r.hits)
	r.mu.Unlock()
	if n <= r.fail {
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(204)
}

func newD(t *testing.T) (*Dispatcher, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := crypto.NewBox(bytes.Repeat([]byte{3}, 32))
	d := New(st, box, func() string { return "https://leassh.example.com/" }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.Backoff = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	return d, st
}

func TestSignatureIsVerifiable(t *testing.T) {
	d, _ := newD(t)
	rc := &recv{}
	ts := httptest.NewServer(rc)
	defer ts.Close()
	res := d.Send(context.Background(), ts.URL, "s3cret", "execution.finished", map[string]any{"id": "E1"}, "https://x/y", 3)
	if !res.OK || res.Attempts != 1 {
		t.Fatalf("envoi: %+v", res)
	}
	h := rc.hits[0]
	if got := h.header.Get("X-Leassh-Signature"); got != Sign("s3cret", h.body) || !strings.HasPrefix(got, "sha256=") || Sign("autre", h.body) == got {
		t.Fatalf("signature: %s", got)
	}
	if h.header.Get("X-Leassh-Event") != "execution.finished" || h.header.Get("Content-Type") != "application/json" {
		t.Fatalf("en-têtes: %v", h.header)
	}
	var env map[string]any
	json.Unmarshal(h.body, &env)
	if env["event"] != "execution.finished" || env["url"] != "https://x/y" || env["id"] == "" || env["data"].(map[string]any)["id"] != "E1" {
		t.Fatalf("enveloppe: %s", h.body)
	}
}

func TestRetriesWithBackoffThenGivesUp(t *testing.T) {
	d, _ := newD(t)
	rc := &recv{fail: 2}
	ts := httptest.NewServer(rc)
	defer ts.Close()
	if res := d.Send(context.Background(), ts.URL, "s", "e", nil, "", 3); !res.OK || res.Attempts != 3 {
		t.Fatalf("succès à la 3e tentative: %+v", res)
	}
	rc2 := &recv{fail: 99}
	ts2 := httptest.NewServer(rc2)
	defer ts2.Close()
	res := d.Send(context.Background(), ts2.URL, "s", "e", nil, "", 3)
	if res.OK || res.Attempts != 3 || res.Status != 500 || len(rc2.hits) != 3 {
		t.Fatalf("abandon après 3 essais: %+v (%d requêtes)", res, len(rc2.hits))
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	d, _ := newD(t)
	target := &recv{}
	tsT := httptest.NewServer(target)
	defer tsT.Close()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, tsT.URL, http.StatusFound) }))
	defer ts.Close()
	if res := d.Send(context.Background(), ts.URL, "s", "e", nil, "", 1); res.OK || res.Status != 302 || len(target.hits) != 0 {
		t.Fatalf("redirection suivie: %+v", res)
	}
}

func TestQueueDeliversAndRecordsOutcome(t *testing.T) {
	d, st := newD(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc := &recv{}
	ts := httptest.NewServer(rc)
	defer ts.Close()
	// désactivé / sans config: rien n'est envoyé
	go d.Run(ctx)
	d.Enqueue("execution.finished", map[string]any{"id": "x"}, "")
	time.Sleep(100 * time.Millisecond)
	if len(rc.hits) != 0 {
		t.Fatal("envoi alors que le webhook n'est pas configuré")
	}
	if err := d.Save(ctx, st.DB, ts.URL, "s3cret", false, true); err != nil {
		t.Fatal(err)
	}
	cfg, secret, _ := d.Config(ctx)
	if !cfg.Enabled || !cfg.HasSecret || secret != "s3cret" || cfg.URL != ts.URL {
		t.Fatalf("config: %+v", cfg)
	}
	var stored string
	st.DB.QueryRow(`SELECT value FROM settings WHERE key='webhook.secret_enc'`).Scan(&stored)
	if strings.Contains(stored, "s3cret") {
		t.Fatal("secret stocké en clair")
	}
	// un secret vide conserve l'ancien
	d.Save(ctx, st.DB, ts.URL, "", false, true)
	if _, s2, _ := d.Config(ctx); s2 != "s3cret" {
		t.Fatal("secret écrasé par une valeur vide")
	}
	d.OnExecution("execution.pending_approval", store.Execution{ID: "E1", KeyName: "N1", ServerSlug: "web-01", CommandSlug: "x.y", Stdout: "TOP-SECRET-OUT"})
	d.OnProposal("proposal.created", store.Proposal{ID: "P1", KeyName: "N1", Payload: `{"slug":"a.b","name":"A","risk":"read"}`, Status: "pending"})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rc.mu.Lock()
		n := len(rc.hits)
		rc.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(rc.hits) != 2 {
		t.Fatalf("%d livraisons", len(rc.hits))
	}
	if bytes.Contains(rc.hits[0].body, []byte("TOP-SECRET-OUT")) {
		t.Fatal("la sortie ne doit pas partir dans le webhook")
	}
	var e1, e2 map[string]any
	json.Unmarshal(rc.hits[0].body, &e1)
	json.Unmarshal(rc.hits[1].body, &e2)
	if e1["url"] != "https://leassh.example.com/validations" || e2["url"] != "https://leassh.example.com/propositions/P1" {
		t.Fatalf("liens: %v %v", e1["url"], e2["url"])
	}
	time.Sleep(50 * time.Millisecond)
	ds, _ := store.ListDeliveries(ctx, st.DB, 10)
	if len(ds) != 2 || !ds[0].OK || ds[0].Attempts != 1 {
		t.Fatalf("livraisons journalisées: %+v", ds)
	}
}

func TestValidateURL(t *testing.T) {
	for _, bad := range []string{"", "ftp://x", "javascript:alert(1)", "http://", "//host/x", "file:///etc/passwd"} {
		if ValidateURL(bad) == nil {
			t.Errorf("%q accepté", bad)
		}
	}
	for _, ok := range []string{"https://ntfy.example.com/topic", "http://127.0.0.1:9000/hook"} {
		if err := ValidateURL(ok); err != nil {
			t.Errorf("%q refusé", ok)
		}
	}
}
