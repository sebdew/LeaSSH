// Package metrics: compteurs et histogramme au format texte Prometheus (sans dépendance externe).
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

var durationBuckets = []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}

type Registry struct {
	mu        sync.Mutex
	execs     map[string]uint64
	sshErrors map[string]uint64
	buckets   []uint64 // non cumulatifs
	sum       float64
	count     uint64
}

func New() *Registry {
	return &Registry{execs: map[string]uint64{}, sshErrors: map[string]uint64{}, buckets: make([]uint64, len(durationBuckets)+1)}
}

// Execution compte une transition vers `status` (denied, pending_approval, succeeded, failed, rejected…).
func (r *Registry) Execution(status string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.execs[status]++
	r.mu.Unlock()
}

// Duration observe la durée d'une exécution SSH terminée.
func (r *Registry) Duration(seconds float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	i := 0
	for i < len(durationBuckets) && seconds > durationBuckets[i] {
		i++
	}
	r.buckets[i]++
	r.sum += seconds
	r.count++
}

// SSHError compte une erreur SSH par nature (connect, auth, host_key_mismatch, channel, other).
func (r *Registry) SSHError(kind string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.sshErrors[kind]++
	r.mu.Unlock()
}

// Sample: une valeur de jauge avec ses libellés.
type Sample struct {
	Labels map[string]string
	Value  float64
}

// Gauge: série calculée à la demande (état courant de la base).
type Gauge struct {
	Name, Help string
	Samples    []Sample
}

func esc(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(s)
}

func labels(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf(`%s="%s"`, k, esc(m[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func sortedKeys(m map[string]uint64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Render écrit toutes les séries (format d'exposition 0.0.4).
func (r *Registry) Render(w io.Writer, gauges []Gauge) {
	if r == nil {
		r = New()
	}
	r.mu.Lock()
	execs, ssh := map[string]uint64{}, map[string]uint64{}
	for k, v := range r.execs {
		execs[k] = v
	}
	for k, v := range r.sshErrors {
		ssh[k] = v
	}
	buckets, sum, count := append([]uint64(nil), r.buckets...), r.sum, r.count
	r.mu.Unlock()

	fmt.Fprintln(w, "# HELP leash_executions_total Transitions d'exécutions vers un statut (depuis le démarrage).")
	fmt.Fprintln(w, "# TYPE leash_executions_total counter")
	for _, k := range sortedKeys(execs) {
		fmt.Fprintf(w, "leash_executions_total{status=\"%s\"} %d\n", esc(k), execs[k])
	}
	fmt.Fprintln(w, "# HELP leash_execution_duration_seconds Durée des exécutions SSH terminées.")
	fmt.Fprintln(w, "# TYPE leash_execution_duration_seconds histogram")
	var cum uint64
	for i, le := range durationBuckets {
		cum += buckets[i]
		fmt.Fprintf(w, "leash_execution_duration_seconds_bucket{le=\"%g\"} %d\n", le, cum)
	}
	cum += buckets[len(durationBuckets)]
	fmt.Fprintf(w, "leash_execution_duration_seconds_bucket{le=\"+Inf\"} %d\n", cum)
	fmt.Fprintf(w, "leash_execution_duration_seconds_sum %g\n", sum)
	fmt.Fprintf(w, "leash_execution_duration_seconds_count %d\n", count)
	fmt.Fprintln(w, "# HELP leash_ssh_errors_total Erreurs SSH par nature.")
	fmt.Fprintln(w, "# TYPE leash_ssh_errors_total counter")
	for _, k := range sortedKeys(ssh) {
		fmt.Fprintf(w, "leash_ssh_errors_total{kind=\"%s\"} %d\n", esc(k), ssh[k])
	}
	for _, g := range gauges {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.Name, g.Help, g.Name)
		for _, s := range g.Samples {
			fmt.Fprintf(w, "%s%s %g\n", g.Name, labels(s.Labels), s.Value)
		}
	}
}
