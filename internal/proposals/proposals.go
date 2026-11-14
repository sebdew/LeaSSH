// Package proposals: propositions de commandes par les agents, linter, approbation par un admin.
package proposals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/catalog"
	"github.com/leassh-dev/leassh/internal/store"
)

const MaxOpenPerKey = 10

var (
	ErrCannotPropose = errors.New("cette clé n'est pas autorisée à proposer des commandes")
	ErrTooMany       = errors.New("trop de propositions en attente pour cette clé (10 maximum)")
	ErrNotPending    = errors.New("proposition déjà traitée")
	ErrLintFailed    = errors.New("la proposition ne passe pas le linter")
)

var slugRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}(\.[a-z0-9][a-z0-9_-]{0,47}){1,3}$`)

type Payload struct {
	Slug        string                      `json:"slug"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Template    string                      `json:"template"`
	Params      map[string]catalog.ParamDef `json:"params"`
	Risk        catalog.Risk                `json:"risk"`
}

type Service struct {
	st     *store.Store
	Notify func(event string, p store.Proposal)
}

func New(st *store.Store) *Service {
	return &Service{st: st, Notify: func(string, store.Proposal) {}}
}

// List renvoie les propositions (filtre de statut optionnel).
func (s *Service) List(ctx context.Context, status string) ([]store.Proposal, error) {
	return store.ListProposals(ctx, s.st.DB, status)
}

// Get renvoie une proposition.
func (s *Service) Get(ctx context.Context, id string) (store.Proposal, error) {
	return store.ProposalByID(ctx, s.st.DB, id)
}

// CheckDraft applique le linter strict à un brouillon (éditeur « Modifier puis approuver »).
func (s *Service) CheckDraft(ctx context.Context, p Payload) catalog.LintResult {
	return s.Check(ctx, s.st.DB, p, "")
}

// Check applique le linter en mode proposition + les contrôles propres aux propositions.
func (s *Service) Check(ctx context.Context, q store.Q, p Payload, ignoreSlugOf string) catalog.LintResult {
	res := catalog.Lint(p.Template, p.Params, p.Risk, catalog.ModeProposal)
	add := func(code, msg string) { res.Errors = append(res.Errors, catalog.Issue{Code: code, Message: msg}) }
	if !slugRe.MatchString(p.Slug) {
		add("slug_invalid", "slug invalide: attendu `famille.nom` en minuscules (ex. mysql.replica_status)")
	} else if c, err := store.CommandBySlug(ctx, q, p.Slug); err == nil && c.ID != ignoreSlugOf {
		add("slug_taken", "ce slug existe déjà dans le catalogue")
	}
	if p.Name == "" || len(p.Name) > 120 {
		add("name_invalid", "nom requis (120 caractères max)")
	}
	if len(p.Description) < 10 || len(p.Description) > 2000 {
		add("description_invalid", "description requise (10 à 2000 caractères): quand l'utiliser, ce qu'elle renvoie")
	}
	return res
}

// Create enregistre la proposition: pending si le lint passe, invalid sinon.
func (s *Service) Create(ctx context.Context, key store.MCPKey, p Payload, justification string) (store.Proposal, catalog.LintResult, error) {
	if !key.CanPropose || key.Disabled {
		return store.Proposal{}, catalog.LintResult{}, ErrCannotPropose
	}
	n, err := store.CountOpenProposals(ctx, s.st.DB, key.ID)
	if err != nil {
		return store.Proposal{}, catalog.LintResult{}, fmt.Errorf("compte des propositions: %w", err)
	}
	if n >= MaxOpenPerKey {
		return store.Proposal{}, catalog.LintResult{}, ErrTooMany
	}
	if p.Params == nil {
		p.Params = map[string]catalog.ParamDef{}
	}
	res := s.Check(ctx, s.st.DB, p, "")
	status := "pending"
	if !res.OK() {
		status = "invalid"
	}
	payload, _ := json.Marshal(p)
	lint, _ := json.Marshal(res)
	var out store.Proposal
	err = s.st.Tx(ctx, func(tx *sql.Tx) (err error) {
		out, err = store.InsertProposal(ctx, tx, store.Proposal{KeyID: key.ID, KeyName: key.Name, Payload: string(payload), Justification: justification, Lint: string(lint), Status: status})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "key", ActorID: key.ID, ActorName: key.Name, Action: "proposal.create",
			TargetType: "proposal", TargetID: out.ID, TargetName: p.Slug, Diff: map[string]any{"status": status}})
	})
	if err != nil {
		return out, res, err
	}
	s.Notify("proposal.created", out)
	return out, res, nil
}

// Approve crée la commande à partir de la proposition (éventuellement corrigée par l'admin).
// Une proposition ne peut être approuvée que si le payload final passe le linter strict.
func (s *Service) Approve(ctx context.Context, id string, reviewer store.User, edited *Payload, groupIDs []string, comment string) (store.Command, error) {
	var cmd store.Command
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		prop, err := store.ProposalByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if prop.Status != "pending" && prop.Status != "invalid" {
			return ErrNotPending
		}
		var p Payload
		if edited != nil {
			p = *edited
		} else if err := json.Unmarshal([]byte(prop.Payload), &p); err != nil {
			return fmt.Errorf("payload illisible: %w", err)
		}
		if p.Params == nil {
			p.Params = map[string]catalog.ParamDef{}
		}
		if res := s.Check(ctx, tx, p, ""); !res.OK() {
			return fmt.Errorf("%w: %s", ErrLintFailed, res.Errors[0].Message)
		}
		cmd, err = store.InsertCommand(ctx, tx, store.Command{Slug: p.Slug, Name: p.Name, Description: p.Description, Risk: p.Risk, Template: p.Template,
			Params: p.Params, Status: "enabled", Source: "proposal", CreatedBy: reviewer.ID, ApprovedBy: reviewer.ID, ApprovedAt: store.Now()})
		if err != nil {
			return err
		}
		for _, gid := range groupIDs {
			g, err := store.GroupByID(ctx, tx, store.CommandGroups, gid)
			if err != nil {
				return err
			}
			if err := store.SetGroupMembers(ctx, tx, store.CommandGroups, gid, append(g.MemberIDs, cmd.ID)); err != nil {
				return err
			}
		}
		if err := store.ReviewProposal(ctx, tx, id, "approved", reviewer.ID, comment, cmd.ID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: reviewer.ID, ActorName: reviewer.Username, Action: "proposal.approve",
			TargetType: "command", TargetID: cmd.ID, TargetName: cmd.Slug, Diff: map[string]any{"proposal": id, "risk": cmd.Risk, "template": cmd.Template}})
	})
	return cmd, err
}

func (s *Service) Reject(ctx context.Context, id string, reviewer store.User, comment string) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		prop, err := store.ProposalByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if prop.Status != "pending" && prop.Status != "invalid" {
			return ErrNotPending
		}
		if err := store.ReviewProposal(ctx, tx, id, "rejected", reviewer.ID, comment, ""); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: reviewer.ID, ActorName: reviewer.Username, Action: "proposal.reject",
			TargetType: "proposal", TargetID: id, TargetName: prop.KeyName, Diff: map[string]any{"comment": comment}})
	})
}
