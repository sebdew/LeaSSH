package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/leassh-dev/leassh/internal/metrics"
	"github.com/leassh-dev/leassh/internal/store"
)

type checklistItem struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
	Href string `json:"href"`
}

// dashboard: checklist d'accueil (cases cochées automatiquement), chiffres des dernières 24 h, activité récente.
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	st, err := store.Dashboard(r.Context(), s.st.DB, time.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	c := st.Counts
	checklist := []checklistItem{
		{"identity", c["identities"] > 0, "/identites"},
		{"server", c["pinned_servers"] > 0, "/serveurs"},
		{"pack", c["commands"] > 0, "/packs"},
		{"rule", c["rules"] > 0, "/regles"},
		{"key", c["keys_with_rules"] > 0, "/cles"},
		{"agent", c["used_keys"] > 0, "/cles"},
	}
	recent, err := store.ListExecutions(r.Context(), s.st.DB, store.ExecFilter{Limit: 10})
	if err != nil {
		s.fail(w, err)
		return
	}
	for i := range recent {
		recent[i].Stdout, recent[i].Stderr = "", ""
	}
	writeJSON(w, 200, map[string]any{"checklist": checklist, "stats": st, "recent": recent})
}

// insights: usage sur 7, 30 ou 90 jours (durées, classements, activité horaire).
func (s *Server) insights(w http.ResponseWriter, r *http.Request) {
	days := 30
	switch r.URL.Query().Get("days") {
	case "7":
		days = 7
	case "90":
		days = 90
	case "", "30":
	default:
		s.fail(w, bad("invalid_period", "période: 7, 30 ou 90 jours"))
		return
	}
	in, err := store.GetInsights(r.Context(), s.st.DB, time.Now(), days)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, in)
}

// metricsHandler: format Prometheus. Fermé tant qu'aucun jeton n'est défini dans Réglages → Général.
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if !s.set.CheckMetricsToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="leassh-metrics"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	m, err := store.MetricsState(r.Context(), s.st.DB)
	if err != nil {
		s.log.Error("metrics", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	labelled := func(name, help, label string, counts map[string]int) metrics.Gauge {
		g := metrics.Gauge{Name: name, Help: help}
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			g.Samples = append(g.Samples, metrics.Sample{Labels: map[string]string{label: k}, Value: float64(counts[k])})
		}
		return g
	}
	gauges := []metrics.Gauge{
		{Name: "leassh_pending_approvals", Help: "Exécutions en attente de validation humaine.", Samples: []metrics.Sample{{Value: float64(m.PendingApprovals)}}},
		{Name: "leassh_pending_proposals", Help: "Propositions de commandes en attente de revue.", Samples: []metrics.Sample{{Value: float64(m.PendingProposals)}}},
		labelled("leassh_servers", "Serveurs par état de clé d'hôte.", "host_key_status", m.ServersByHostKey),
		labelled("leassh_mcp_keys", "Clés MCP par état.", "state", m.KeysByState),
	}
	gauges = append(gauges, metrics.Gauge{Name: "leassh_build_info", Help: "Version de Leassh.", Samples: []metrics.Sample{{Labels: map[string]string{"version": s.version}, Value: 1}}})
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.metrics.Render(w, gauges)
}
