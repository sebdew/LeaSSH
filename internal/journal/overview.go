package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/leassh-dev/leassh/internal/store"
)

// ErrInvalidPeriod: la période des statistiques d'usage n'est pas l'une de celles proposées.
var ErrInvalidPeriod = errors.New("période: 7, 30 ou 90 jours")

// ChecklistItem: une étape de la checklist d'accueil, cochée automatiquement d'après l'état de la configuration.
type ChecklistItem struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
	Href string `json:"href"`
}

// Overview: contenu du tableau de bord (checklist, chiffres des dernières 24 h, activité récente).
type Overview struct {
	Checklist []ChecklistItem      `json:"checklist"`
	Stats     store.DashboardStats `json:"stats"`
	Recent    []store.Execution    `json:"recent"`
}

// Dashboard calcule le tableau de bord; les sorties des exécutions récentes sont retirées (elles restent dans le journal).
func (s *Service) Dashboard(ctx context.Context, now time.Time) (Overview, error) {
	st, err := store.Dashboard(ctx, s.st.DB, now)
	if err != nil {
		return Overview{}, fmt.Errorf("tableau de bord: %w", err)
	}
	c := st.Counts
	recent, err := store.ListExecutions(ctx, s.st.DB, store.ExecFilter{Limit: 10})
	if err != nil {
		return Overview{}, fmt.Errorf("activité récente: %w", err)
	}
	for i := range recent {
		recent[i].Stdout, recent[i].Stderr = "", ""
	}
	return Overview{
		Checklist: []ChecklistItem{
			{"identity", c["identities"] > 0, "/identites"},
			{"server", c["pinned_servers"] > 0, "/serveurs"},
			{"pack", c["commands"] > 0, "/packs"},
			{"rule", c["rules"] > 0, "/regles"},
			{"key", c["keys_with_rules"] > 0, "/cles"},
			{"agent", c["used_keys"] > 0, "/cles"},
		},
		Stats: st, Recent: recent,
	}, nil
}

// Insights: usage sur 7, 30 ou 90 jours (durées, classements, activité horaire).
func (s *Service) Insights(ctx context.Context, now time.Time, days int) (store.Insights, error) {
	switch days {
	case 7, 30, 90:
	default:
		return store.Insights{}, ErrInvalidPeriod
	}
	return store.GetInsights(ctx, s.st.DB, now, days)
}

// MetricsState: état agrégé exposé par /metrics.
func (s *Service) MetricsState(ctx context.Context) (store.MetricsSnapshot, error) {
	return store.MetricsState(ctx, s.st.DB)
}
