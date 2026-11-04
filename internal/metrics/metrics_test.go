package metrics

import (
	"bytes"
	"strings"
	"testing"
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
