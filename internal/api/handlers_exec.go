package api

import (
	"encoding/json"
	"errors"
	"github.com/sebdew/LeaSSH/internal/catalog"
	"net/http"

	"github.com/sebdew/LeaSSH/internal/executions"
	"github.com/sebdew/LeaSSH/internal/proposals"
	"github.com/sebdew/LeaSSH/internal/store"
)

func (s *Server) getExecution(w http.ResponseWriter, r *http.Request) {
	e, err := s.journal.Execution(r.Context(), id(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, e)
}

type decisionInput struct {
	Decision    string `json:"decision"` // approve | reject
	Comment     string `json:"comment"`
	ConfirmSlug string `json:"confirm_slug"`
}

func (s *Server) decideExecution(w http.ResponseWriter, r *http.Request) {
	var in decisionInput
	if !decode(w, r, &in) {
		return
	}
	if in.Decision != "approve" && in.Decision != "reject" {
		s.fail(w, bad("invalid_decision", "décision: approve ou reject"))
		return
	}
	e, err := s.exec.Decide(r.Context(), id(r), currentUser(r), in.Decision == "approve", in.Comment, in.ConfirmSlug)
	switch {
	case errors.Is(err, executions.ErrNotPending):
		writeErr(w, 409, "not_pending", err.Error())
	case errors.Is(err, executions.ErrConfirm):
		writeErr(w, 400, "confirm_required", err.Error())
	case errors.Is(err, executions.ErrNeedComment):
		writeErr(w, 400, "comment_required", err.Error())
	case errors.Is(err, executions.ErrForbidden):
		writeErr(w, 403, "forbidden", err.Error())
	case err != nil:
		s.fail(w, err)
	default:
		writeJSON(w, 200, e)
	}
}

// proposalDTO expose payload et lint comme JSON (et non comme chaînes).
type proposalDTO struct {
	store.Proposal
	Payload json.RawMessage `json:"payload"`
	Lint    json.RawMessage `json:"lint"`
}

func toDTO(p store.Proposal) proposalDTO {
	return proposalDTO{Proposal: p, Payload: json.RawMessage(p.Payload), Lint: json.RawMessage(p.Lint)}
}

func (s *Server) listProposals(w http.ResponseWriter, r *http.Request) {
	items, err := s.props.List(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]proposalDTO, len(items))
	for i, p := range items {
		out[i] = toDTO(p)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

func (s *Server) getProposal(w http.ResponseWriter, r *http.Request) {
	p, err := s.props.Get(r.Context(), id(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, toDTO(p))
}

// lintProposal: linter strict (mode proposition) pour l'éditeur « Modifier puis approuver ».
func (s *Server) lintProposal(w http.ResponseWriter, r *http.Request) {
	var in proposals.Payload
	if !decode(w, r, &in) {
		return
	}
	writeJSON(w, 200, map[string]any{"lint": s.props.CheckDraft(r.Context(), in), "placeholders": catalog.PlaceholderNames(in.Template)})
}

type approveProposalInput struct {
	Edited   *proposals.Payload `json:"edited"`
	GroupIDs []string           `json:"group_ids"`
	Comment  string             `json:"comment"`
}

func (s *Server) approveProposal(w http.ResponseWriter, r *http.Request) {
	var in approveProposalInput
	if !decode(w, r, &in) {
		return
	}
	cmd, err := s.props.Approve(r.Context(), id(r), currentUser(r), in.Edited, in.GroupIDs, in.Comment)
	switch {
	case errors.Is(err, proposals.ErrNotPending):
		writeErr(w, 409, "not_pending", err.Error())
	case errors.Is(err, proposals.ErrLintFailed):
		writeErr(w, 400, "lint_failed", err.Error())
	case err != nil:
		s.fail(w, err)
	default:
		writeJSON(w, 200, map[string]any{"command": cmd})
	}
}

func (s *Server) rejectProposal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Comment string `json:"comment"`
	}
	if !decode(w, r, &in) {
		return
	}
	err := s.props.Reject(r.Context(), id(r), currentUser(r), in.Comment)
	switch {
	case errors.Is(err, proposals.ErrNotPending):
		writeErr(w, 409, "not_pending", err.Error())
	case err != nil:
		s.fail(w, err)
	default:
		w.WriteHeader(204)
	}
}
