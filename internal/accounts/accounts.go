// Package accounts: cas d'usage liés aux comptes (ARC-03). Les routes HTTP appellent ce service,
// qui porte les règles métier et passe par le paquet store pour les données.
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

// MinPassword: longueur minimale d'un mot de passe local.
const MinPassword = 10

var (
	ErrInvalidUsername    = errors.New("nom d'utilisateur requis (64 caractères max)")
	ErrWeakPassword       = errors.New("le mot de passe doit faire au moins 10 caractères")
	ErrAlreadyInitialized = errors.New("l'instance est déjà initialisée")
)

type Service struct{ st *store.Store }

func New(st *store.Store) *Service { return &Service{st: st} }

// InitAdminInput: données de l'initialisation (premier compte administrateur).
type InitAdminInput struct {
	Username, DisplayName, Password, IP string
}

// InitializeAdmin crée le premier administrateur. Le test « aucun utilisateur » et l'insertion
// partagent la même transaction: deux appels concurrents ne produisent pas deux administrateurs.
func (s *Service) InitializeAdmin(ctx context.Context, in InitAdminInput) (store.User, error) {
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" || len(in.Username) > 64 {
		return store.User{}, ErrInvalidUsername
	}
	if len(in.Password) < MinPassword {
		return store.User{}, ErrWeakPassword
	}
	hash, err := crypto.HashPassword(in.Password)
	if err != nil {
		return store.User{}, fmt.Errorf("hachage du mot de passe: %w", err)
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	var u store.User
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		n, err := store.CountUsers(ctx, tx)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrAlreadyInitialized
		}
		if u, err = store.InsertUser(ctx, tx, store.User{Username: in.Username, DisplayName: in.DisplayName, Role: "admin", PasswordHash: hash}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "system", ActorName: "setup", Action: "user.create",
			TargetType: "user", TargetID: u.ID, TargetName: u.Username,
			Diff: map[string]any{"after": map[string]any{"username": u.Username, "role": u.Role}}, IP: in.IP})
	})
	return u, err
}
