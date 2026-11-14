// Package groups: administration des groupes de serveurs et de commandes (ARC-03). Les groupes tenus par une synchro
// (managed_by) ne changent ni de nom ni d'identifiant et ne se suppriment pas; leurs membres restent modifiables.
package groups

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/catalog"
	"github.com/leassh-dev/leassh/internal/store"
)

var (
	ErrInvalidName        = errors.New("nom requis")
	ErrManagedLocked      = errors.New("ce groupe est synchronisé: nom et identifiant sont verrouillés (seuls ses membres se modifient, via la liste des membres)")
	ErrManagedUndeletable = errors.New("ce groupe est synchronisé: il ne peut pas être supprimé")
)

type Service struct{ st *store.Store }

func New(st *store.Store) *Service { return &Service{st: st} }

// Input: contenu d'un groupe. MemberIDs absent (nil) à la modification = membres inchangés.
type Input struct {
	Slug, Name, Description string
	MemberIDs               []string
}

func (in *Input) validate() error {
	in.Slug, in.Name = strings.TrimSpace(in.Slug), strings.TrimSpace(in.Name)
	if err := catalog.CheckSlug(in.Slug); err != nil {
		return err
	}
	if in.Name == "" {
		return ErrInvalidName
	}
	return nil
}

func (s *Service) List(ctx context.Context, kind store.GroupKind) ([]store.Group, error) {
	return store.ListGroups(ctx, s.st.DB, kind)
}

func (s *Service) Create(ctx context.Context, actor store.User, ip string, kind store.GroupKind, in Input) (store.Group, error) {
	if err := in.validate(); err != nil {
		return store.Group{}, err
	}
	label := string(kind)
	var g store.Group
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		var err error
		if g, err = store.InsertGroup(ctx, tx, kind, store.Group{Slug: in.Slug, Name: in.Name, Description: in.Description, MemberIDs: in.MemberIDs}); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: label + "_group.create", TargetType: label + "_group", TargetID: g.ID, TargetName: g.Slug, Diff: map[string]any{"after": g}}, nil
	})
	return g, err
}

func (s *Service) Update(ctx context.Context, actor store.User, ip string, kind store.GroupKind, id string, in Input) (store.Group, error) {
	if err := in.validate(); err != nil {
		return store.Group{}, err
	}
	label := string(kind)
	var g store.Group
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.GroupByID(ctx, tx, kind, id)
		if err != nil {
			return audit.Event{}, err
		}
		if old.ManagedBy != "" {
			return audit.Event{}, ErrManagedLocked
		}
		g = store.Group{ID: old.ID, Slug: in.Slug, Name: in.Name, Description: in.Description, MemberIDs: in.MemberIDs}
		if err := store.UpdateGroup(ctx, tx, kind, g); err != nil {
			return audit.Event{}, err
		}
		if g.MemberIDs == nil {
			g.MemberIDs = old.MemberIDs
		}
		return audit.Event{Action: label + "_group.update", TargetType: label + "_group", TargetID: g.ID, TargetName: g.Slug, Diff: map[string]any{"before": old, "after": g}}, nil
	})
	return g, err
}

func (s *Service) Delete(ctx context.Context, actor store.User, ip string, kind store.GroupKind, id string) error {
	label := string(kind)
	return audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.GroupByID(ctx, tx, kind, id)
		if err != nil {
			return audit.Event{}, err
		}
		if old.ManagedBy != "" {
			return audit.Event{}, ErrManagedUndeletable
		}
		if err := store.DeleteGroup(ctx, tx, kind, old.ID); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: label + "_group.delete", TargetType: label + "_group", TargetID: old.ID, TargetName: old.Slug, Diff: map[string]any{"before": old}}, nil
	})
}

// SetMembers remplace les membres et renvoie la liste effective (les membres posés par une synchro restent).
func (s *Service) SetMembers(ctx context.Context, actor store.User, ip string, kind store.GroupKind, id string, ids []string) ([]string, error) {
	label := string(kind)
	effective := ids
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.GroupByID(ctx, tx, kind, id)
		if err != nil {
			return audit.Event{}, err
		}
		if err := store.SetGroupMembers(ctx, tx, kind, old.ID, ids); err != nil {
			return audit.Event{}, err
		}
		if updated, err := store.GroupByID(ctx, tx, kind, old.ID); err == nil {
			effective = updated.MemberIDs
		}
		return audit.Event{Action: label + "_group.members", TargetType: label + "_group", TargetID: old.ID, TargetName: old.Slug,
			Diff: map[string]any{"before": old.MemberIDs, "after": effective}}, nil
	})
	return effective, err
}
