package metrics

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRender(t *testing.T) {
	r := New()
	r.Execution("succeeded")
	r.Execution("succeeded")
	r.Execution("denied")
	r.Duration(0.05)
	r.Duration(3)
	r.Duration(1000)
	r.SSHError("connect")
	var b bytes.Buffer
	r.Render(&b, []Gauge{{Name: "leassh_servers", Help: "Serveurs par état de clé d'hôte.", Samples: []Sample{{Labels: map[string]string{"host_key_status": `pi"nned`}, Value: 2}}}})
	out := b.String()
	for _, want := range []string{
		`leassh_executions_total{status="succeeded"} 2`, `leassh_executions_total{status="denied"} 1`,
		`leassh_execution_duration_seconds_bucket{le="0.1"} 1`, `leassh_execution_duration_seconds_bucket{le="5"} 2`,
		`leassh_execution_duration_seconds_bucket{le="+Inf"} 3`, `leassh_execution_duration_seconds_count 3`,
		`leassh_ssh_errors_total{kind="connect"} 1`, `leassh_servers{host_key_status="pi\"nned"} 2`, "# TYPE leassh_servers gauge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("manque %q dans:\n%s", want, out)
		}
	}
}

func TestNilRegistryIsSafe(t *testing.T) {
	var r *Registry
	r.Execution("x")
	r.Duration(1)
	r.SSHError("y")
}

func TestOutboundDurationsAreExposed(t *testing.T) {
	r := New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()
	c := &http.Client{Transport: r.Transport("webhook", nil)}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	r.Outbound("database", 3*time.Millisecond, nil)
	r.Outbound("database", time.Millisecond, errors.New("boom"))
	var b bytes.Buffer
	r.Render(&b, nil)
	out := b.String()
	for _, want := range []string{
		"# TYPE leassh_outbound_duration_seconds histogram",
		`leassh_outbound_duration_seconds_count{result="ok",target="webhook"} 1`,
		`leassh_outbound_duration_seconds_count{result="ok",target="database"} 1`,
		`leassh_outbound_duration_seconds_count{result="error",target="database"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("manque %q dans:\n%s", want, out)
		}
	}
	var nilReg *Registry
	_ = nilReg.Transport("x", nil)
	nilReg.Outbound("x", time.Second, nil)
}
