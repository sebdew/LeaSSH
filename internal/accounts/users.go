package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

var (
	ErrInvalidRole    = errors.New("rôle: admin, approver ou viewer")
	ErrInvalidTheme   = errors.New("thème: system, light ou dark")
	ErrManagedAccount = errors.New("compte SSO: le rôle vient du mapping des revendications et il n'y a pas de mot de passe local")
	ErrSelfDisable    = errors.New("vous ne pouvez pas désactiver votre propre compte")
	ErrSelfDelete     = errors.New("vous ne pouvez pas supprimer votre propre compte")
	ErrLastAdmin      = errors.New("il doit rester au moins un administrateur actif")
	ErrWrongPassword  = errors.New("mot de passe actuel incorrect")
	ErrSSOAccount     = errors.New("ce compte est géré par le SSO: pas de mot de passe local")
)

var roles = map[string]bool{"admin": true, "approver": true, "viewer": true}

// UserInput: création ou modification d'un compte. Password vide à la modification = inchangé.
type UserInput struct {
	Username, DisplayName, Password, Role string
	Disabled                              bool
}

func (s *Service) ListUsers(ctx context.Context) ([]store.User, error) {
	return store.ListUsers(ctx, s.st.DB)
}

// mutate: transaction + événement d'audit de l'acteur, atomiques (invariant « toute mutation écrit son audit »).
func (s *Service) mutate(ctx context.Context, actor store.User, ip string, fn func(tx *sql.Tx) (audit.Event, error)) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		ev, err := fn(tx)
		if err != nil {
			return err
		}
		ev.ActorType, ev.ActorID, ev.ActorName, ev.IP = "user", actor.ID, actor.Username, ip
		return audit.Record(ctx, tx, ev)
	})
}

func (s *Service) CreateUser(ctx context.Context, actor store.User, ip string, in UserInput) (store.User, error) {
	in.Username = strings.TrimSpace(in.Username)
	switch {
	case in.Username == "" || len(in.Username) > MaxUsername:
		return store.User{}, ErrInvalidUsername
	case !roles[in.Role]:
		return store.User{}, ErrInvalidRole
	case len(in.Password) < MinPassword:
		return store.User{}, ErrWeakPassword
	}
	hash, err := crypto.HashPassword(in.Password)
	if err != nil {
		return store.User{}, fmt.Errorf("hachage du mot de passe: %w", err)
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	var nu store.User
	err = s.mutate(ctx, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		var err error
		if nu, err = store.InsertUser(ctx, tx, store.User{Username: in.Username, DisplayName: in.DisplayName, Role: in.Role, PasswordHash: hash}); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "user.create", TargetType: "user", TargetID: nu.ID, TargetName: nu.Username,
			Diff: map[string]any{"after": map[string]any{"username": nu.Username, "role": nu.Role}}}, nil
	})
	return nu, err
}

// UpdateUser: profil, rôle, désactivation, réinitialisation du mot de passe. Garde-fou: toujours au moins un admin actif.
func (s *Service) UpdateUser(ctx context.Context, actor store.User, ip, id string, in UserInput) (store.User, error) {
	if !roles[in.Role] {
		return store.User{}, ErrInvalidRole
	}
	hash := ""
	if in.Password != "" {
		if len(in.Password) < MinPassword {
			return store.User{}, ErrWeakPassword
		}
		var err error
		if hash, err = crypto.HashPassword(in.Password); err != nil {
			return store.User{}, fmt.Errorf("hachage du mot de passe: %w", err)
		}
	}
	var nu store.User
	err := s.mutate(ctx, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.UserByIDQ(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		if old.AuthProvider != "local" && (hash != "" || in.Role != old.Role) {
			return audit.Event{}, ErrManagedAccount
		}
		nu = old
		nu.DisplayName, nu.Role, nu.Disabled = strings.TrimSpace(in.DisplayName), in.Role, in.Disabled
		if nu.DisplayName == "" {
			nu.DisplayName = old.Username
		}
		if old.ID == actor.ID && nu.Disabled {
			return audit.Event{}, ErrSelfDisable
		}
		if old.Role == "admin" && !old.Disabled && (nu.Role != "admin" || nu.Disabled) {
			if err := keepOneAdmin(ctx, tx); err != nil {
				return audit.Event{}, err
			}
		}
		if err := store.UpdateUser(ctx, tx, nu, hash); err != nil {
			return audit.Event{}, err
		}
		if nu.Disabled || nu.Role != old.Role || hash != "" {
			if err := store.DeleteSessionsOf(ctx, tx, old.ID, ""); err != nil { // effet immédiat
				return audit.Event{}, err
			}
		}
		diff := map[string]any{"before": map[string]any{"display_name": old.DisplayName, "role": old.Role, "disabled": old.Disabled},
			"after": map[string]any{"display_name": nu.DisplayName, "role": nu.Role, "disabled": nu.Disabled, "password_reset": hash != ""}}
		return audit.Event{Action: "user.update", TargetType: "user", TargetID: nu.ID, TargetName: nu.Username, Diff: diff}, nil
	})
	return nu, err
}

func (s *Service) DeleteUser(ctx context.Context, actor store.User, ip, id string) error {
	return s.mutate(ctx, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.UserByIDQ(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		if old.ID == actor.ID {
			return audit.Event{}, ErrSelfDelete
		}
		if old.Role == "admin" && !old.Disabled {
			if err := keepOneAdmin(ctx, tx); err != nil {
				return audit.Event{}, err
			}
		}
		if err := store.DeleteUser(ctx, tx, old.ID); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "user.delete", TargetType: "user", TargetID: old.ID, TargetName: old.Username}, nil
	})
}

// keepOneAdmin refuse l'opération s'il ne reste qu'un administrateur actif.
func keepOneAdmin(ctx context.Context, tx *sql.Tx) error {
	n, err := store.CountEnabledAdmins(ctx, tx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// ChangePassword: l'utilisateur change son propre mot de passe; ses autres sessions (hors keepSession) sont fermées.
func (s *Service) ChangePassword(ctx context.Context, me store.User, ip, current, next, keepSession string) error {
	if me.AuthProvider != "local" {
		return ErrSSOAccount
	}
	if !crypto.CheckPassword(current, me.PasswordHash) {
		return ErrWrongPassword
	}
	if len(next) < MinPassword {
		return ErrWeakPassword
	}
	hash, err := crypto.HashPassword(next)
	if err != nil {
		return fmt.Errorf("hachage du mot de passe: %w", err)
	}
	return s.mutate(ctx, me, ip, func(tx *sql.Tx) (audit.Event, error) {
		if err := store.SetPassword(ctx, tx, me.ID, hash); err != nil {
			return audit.Event{}, err
		}
		if err := store.DeleteSessionsOf(ctx, tx, me.ID, keepSession); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "user.password_change", TargetType: "user", TargetID: me.ID, TargetName: me.Username}, nil
	})
}

// SetTheme: préférence de thème mémorisée côté serveur, par utilisateur (docs/specs/UX.md §4.2).
func (s *Service) SetTheme(ctx context.Context, userID, theme string) error {
	if theme != "system" && theme != "light" && theme != "dark" {
		return ErrInvalidTheme
	}
	return store.SetTheme(ctx, s.st.DB, userID, theme)
}
