// Package journal: lecture du journal d'exécution et du journal d'administration, et vérification de leur chaîne
// d'intégrité (ARC-03). Les routes HTTP passent par ce service; l'écriture reste dans executions et audit.
package journal

import (
	"context"
	"fmt"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/store"
)

type Service struct{ st *store.Store }

func New(st *store.Store) *Service { return &Service{st: st} }

func (s *Service) ListExecutions(ctx context.Context, f store.ExecFilter) ([]store.Execution, error) {
	return store.ListExecutions(ctx, s.st.DB, f)
}

func (s *Service) Execution(ctx context.Context, id string) (store.Execution, error) {
	return store.ExecutionByID(ctx, s.st.DB, id)
}

func (s *Service) ListAudit(ctx context.Context, f store.AuditFilter) ([]store.AuditEvent, error) {
	return store.ListAudit(ctx, s.st.DB, f)
}

// Integrity: résultat des deux vérifications de chaîne.
type Integrity struct {
	OK         bool              `json:"ok"`
	Executions store.ChainResult `json:"executions"`
	Audit      store.ChainResult `json:"audit"`
}

// VerifyIntegrity contrôle la chaîne des exécutions et celle de l'audit.
func (s *Service) VerifyIntegrity(ctx context.Context) (Integrity, error) {
	ex, err := store.VerifyExecutions(ctx, s.st.DB)
	if err != nil {
		return Integrity{}, fmt.Errorf("chaîne des exécutions: %w", err)
	}
	au, err := audit.Verify(ctx, s.st.DB)
	if err != nil {
		return Integrity{}, fmt.Errorf("chaîne d'audit: %w", err)
	}
	return Integrity{OK: ex.OK && au.OK, Executions: ex, Audit: au}, nil
}
