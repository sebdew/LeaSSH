// Package metrics: métriques au format Prometheus, via prometheus/client_golang.
package metrics

import (
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// Registry regroupe les séries de l'application. Toutes les méthodes acceptent un receveur nil (métriques désactivées).
type Registry struct {
	reg       *prometheus.Registry
	execs     *prometheus.CounterVec
	sshErrors *prometheus.CounterVec
	duration  prometheus.Histogram
	outbound  *prometheus.HistogramVec
}

func New() *Registry {
	r := &Registry{
		reg: prometheus.NewRegistry(),
		execs: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "leassh_executions_total",
			Help: "Transitions d'exécutions vers un statut (depuis le démarrage)."}, []string{"status"}),
		sshErrors: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "leassh_ssh_errors_total", Help: "Erreurs SSH par nature."}, []string{"kind"}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "leassh_execution_duration_seconds", Help: "Durée des exécutions SSH terminées.",
			Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}}),
		outbound: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "leassh_outbound_duration_seconds",
			Help:    "Durée des appels sortants (database, webhook, ai, idp), par cible et résultat.",
			Buckets: []float64{0.001, 0.005, 0.025, 0.1, 0.5, 1, 2.5, 5, 10, 30}}, []string{"target", "result"}),
	}
	r.reg.MustRegister(r.execs, r.sshErrors, r.duration, r.outbound)
	return r
}

// Execution compte une transition vers `status` (denied, pending_approval, succeeded, failed, rejected…).
func (r *Registry) Execution(status string) {
	if r != nil {
		r.execs.WithLabelValues(status).Inc()
	}
}

// Duration observe la durée d'une exécution SSH terminée.
func (r *Registry) Duration(seconds float64) {
	if r != nil {
		r.duration.Observe(seconds)
	}
}

// SSHError compte une erreur SSH par nature (connect, auth, host_key_mismatch, channel, other).
func (r *Registry) SSHError(kind string) {
	if r != nil {
		r.sshErrors.WithLabelValues(kind).Inc()
	}
}

// Outbound observe la durée d'un appel sortant vers target (database, webhook, ai, idp); err != nil = échec.
func (r *Registry) Outbound(target string, d time.Duration, err error) {
	if r == nil {
		return
	}
	result := "ok"
	if err != nil {
		result = "error"
	}
	r.outbound.WithLabelValues(target, result).Observe(d.Seconds())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Transport enveloppe next (http.DefaultTransport si nil) et mesure chaque requête sous le nom target.
func (r *Registry) Transport(target string, next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	if r == nil {
		return next
	}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		start := time.Now()
		resp, err := next.RoundTrip(req)
		r.Outbound(target, time.Since(start), err)
		return resp, err
	})
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

// gaugeCollector expose les jauges calculées pour une seule lecture (collecteur sans description: « unchecked »).
type gaugeCollector []Gauge

func (gaugeCollector) Describe(chan<- *prometheus.Desc) {}

func (c gaugeCollector) Collect(ch chan<- prometheus.Metric) {
	for _, g := range c {
		for _, s := range g.Samples {
			names := make([]string, 0, len(s.Labels))
			for k := range s.Labels {
				names = append(names, k)
			}
			sort.Strings(names)
			values := make([]string, len(names))
			for i, k := range names {
				values[i] = s.Labels[k]
			}
			ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(g.Name, g.Help, names, nil), prometheus.GaugeValue, s.Value, values...)
		}
	}
}

// Render écrit toutes les séries au format texte d'exposition, jauges comprises.
func (r *Registry) Render(w io.Writer, gauges []Gauge) {
	if r == nil {
		r = New()
	}
	tmp := prometheus.NewRegistry()
	tmp.MustRegister(gaugeCollector(gauges))
	families, err := prometheus.Gatherers{r.reg, tmp}.Gather()
	if err != nil { // ex. jauge au nom en double: on écrit ce qui est valide plutôt que rien
		families, _ = prometheus.Gatherers{r.reg}.Gather()
	}
	enc := expfmt.NewEncoder(w, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, mf := range families {
		_ = enc.Encode(mf)
	}
}
