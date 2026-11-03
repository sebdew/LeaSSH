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
	r.Render(&b, []Gauge{{Name: "leash_servers", Help: "Serveurs par état de clé d'hôte.", Samples: []Sample{{Labels: map[string]string{"host_key_status": `pi"nned`}, Value: 2}}}})
	out := b.String()
	for _, want := range []string{
		`leash_executions_total{status="succeeded"} 2`, `leash_executions_total{status="denied"} 1`,
		`leash_execution_duration_seconds_bucket{le="0.1"} 1`, `leash_execution_duration_seconds_bucket{le="5"} 2`,
		`leash_execution_duration_seconds_bucket{le="+Inf"} 3`, `leash_execution_duration_seconds_count 3`,
		`leash_ssh_errors_total{kind="connect"} 1`, `leash_servers{host_key_status="pi\"nned"} 2`, "# TYPE leash_servers gauge",
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
