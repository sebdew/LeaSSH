package api

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sebdew/LeaSSH/internal/journal"
	"github.com/sebdew/LeaSSH/internal/metrics"
)

// dashboard: checklist d'accueil (cases cochées automatiquement), chiffres des dernières 24 h, activité récente.
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ov, err := s.journal.Dashboard(r.Context(), time.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"checklist": ov.Checklist, "stats": ov.Stats, "recent": ov.Recent})
}

// insights: usage sur 7, 30 ou 90 jours (durées, classements, activité horaire).
func (s *Server) insights(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		var err error
		if days, err = strconv.Atoi(v); err != nil {
			days = 0
		}
	}
	in, err := s.journal.Insights(r.Context(), time.Now(), days)
	if errors.Is(err, journal.ErrInvalidPeriod) {
		s.fail(w, bad("invalid_period", err.Error()))
		return
	}
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
	m, err := s.journal.MetricsState(r.Context())
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
