// Package commands: administration des commandes du catalogue (ARC-03). Valide, passe le linter et écrit
// l'événement d'audit dans la même transaction que la mutation.
package commands

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/catalog"
	"github.com/sebdew/LeaSSH/internal/store"
)

var (
	ErrInvalidName        = errors.New("nom requis (120 caractères max)")
	ErrInvalidDescription = errors.New("description trop longue (4000 caractères max)")
	ErrInvalidStatus      = errors.New("statut invalide")
	ErrInvalidTimeout     = errors.New("délai entre 1 et 600 secondes")
	ErrInvalidMaxOutput   = errors.New("sortie max entre 1 et 4096 Ko")
)

// LintError: le gabarit est refusé par le linter; Result porte le détail à afficher.
type LintError struct{ Result catalog.LintResult }

func (e *LintError) Error() string { return e.Result.Errors[0].Message }

type Service struct{ st *store.Store }

func New(st *store.Store) *Service { return &Service{st: st} }

// Input: contenu d'une commande. Statut, délai et sortie max vides prennent leur valeur par défaut.
type Input struct {
	Slug, Name, Description string
	Risk                    catalog.Risk
	Template                string
	Params                  map[string]catalog.ParamDef
	TimeoutS, MaxOutputKB   int
	Status                  string
}

// validate normalise l'entrée, vérifie les bornes puis passe le linter (mode administrateur).
func (in *Input) validate() (catalog.LintResult, error) {
	in.Slug, in.Name = strings.TrimSpace(in.Slug), strings.TrimSpace(in.Name)
	if err := catalog.CheckSlug(in.Slug); err != nil {
		return catalog.LintResult{}, err
	}
	switch {
	case in.Name == "" || len(in.Name) > 120:
		return catalog.LintResult{}, ErrInvalidName
	case len(in.Description) > 4000:
		return catalog.LintResult{}, ErrInvalidDescription
	}
	if in.Status == "" {
		in.Status = "enabled"
	}
	if in.Status != "enabled" && in.Status != "disabled" && in.Status != "pending" {
		return catalog.LintResult{}, ErrInvalidStatus
	}
	if in.TimeoutS == 0 {
		in.TimeoutS = 30
	}
	if in.TimeoutS < 1 || in.TimeoutS > 600 {
		return catalog.LintResult{}, ErrInvalidTimeout
	}
	if in.MaxOutputKB == 0 {
		in.MaxOutputKB = 64
	}
	if in.MaxOutputKB < 1 || in.MaxOutputKB > 4096 {
		return catalog.LintResult{}, ErrInvalidMaxOutput
	}
	if in.Params == nil {
		in.Params = map[string]catalog.ParamDef{}
	}
	res := catalog.Lint(in.Template, in.Params, in.Risk, catalog.ModeAdmin)
	if !res.OK() {
		return res, &LintError{Result: res}
	}
	return res, nil
}

func (in Input) command() store.Command {
	return store.Command{Slug: in.Slug, Name: in.Name, Description: in.Description, Risk: in.Risk, Template: in.Template, Params: in.Params,
		TimeoutS: in.TimeoutS, MaxOutputKB: in.MaxOutputKB, Status: in.Status}
}

func (s *Service) List(ctx context.Context) ([]store.Command, error) {
	return store.ListCommands(ctx, s.st.DB)
}

func (s *Service) Get(ctx context.Context, id string) (store.Command, error) {
	return store.CommandByID(ctx, s.st.DB, id)
}

func (s *Service) Create(ctx context.Context, actor store.User, ip string, in Input) (store.Command, catalog.LintResult, error) {
	lint, err := in.validate()
	if err != nil {
		return store.Command{}, lint, err
	}
	var c store.Command
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		c = in.command()
		c.Source, c.CreatedBy = "manual", actor.ID
		var err error
		if c, err = store.InsertCommand(ctx, tx, c); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "command.create", TargetType: "command", TargetID: c.ID, TargetName: c.Slug, Diff: map[string]any{"after": c}}, nil
	})
	return c, lint, err
}

func (s *Service) Update(ctx context.Context, actor store.User, ip, id string, in Input) (store.Command, catalog.LintResult, error) {
	lint, err := in.validate()
	if err != nil {
		return store.Command{}, lint, err
	}
	var c store.Command
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		upd := in.command()
		upd.ID = id
		nc, old, err := store.UpdateCommand(ctx, tx, upd)
		if err != nil {
			return audit.Event{}, err
		}
		c = nc
		return audit.Event{Action: "command.update", TargetType: "command", TargetID: c.ID, TargetName: c.Slug, Diff: map[string]any{"before": old, "after": c}}, nil
	})
	return c, lint, err
}

func (s *Service) Delete(ctx context.Context, actor store.User, ip, id string) error {
	return audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.CommandByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		if err := store.DeleteCommand(ctx, tx, old.ID); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "command.delete", TargetType: "command", TargetID: old.ID, TargetName: old.Slug, Diff: map[string]any{"before": old}}, nil
	})
}
