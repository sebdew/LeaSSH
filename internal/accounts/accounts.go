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

// MaxUsername: longueur maximale (en octets) d'un nom d'utilisateur, aussi appliquée à la connexion (SEC-09).
const MaxUsername = 64

var (
	ErrInvalidUsername    = errors.New("nom d'utilisateur requis (64 caractères max)")
	ErrWeakPassword       = errors.New("le mot de passe doit faire au moins 10 caractères")
	ErrAlreadyInitialized = errors.New("l'instance est déjà initialisée")

	ErrInvalidCredentials      = errors.New("identifiants incorrects")
	ErrLocalLoginDisabled      = errors.New("la connexion par mot de passe est désactivée: utilisez le SSO")
	ErrLocalPasswordAdminsOnly = errors.New("la connexion par mot de passe est réservée aux administrateurs: utilisez le SSO")
)

// dummyHash égalise le temps de réponse quand l'utilisateur n'existe pas.
var dummyHash, _ = crypto.HashPassword("dummy-password-for-timing")

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
	if in.Username == "" || len(in.Username) > MaxUsername {
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

// LoginInput: identifiants présentés à la connexion locale et adresse du client (pour l'audit).
type LoginInput struct {
	Username, Password, IP string
}

// Authenticate applique la politique de connexion par mot de passe: SSO obligatoire (refus avant toute vérification),
// compte local actif, mot de passe correct, mot de passe local réservé aux administrateurs si le réglage est posé.
// Chaque refus est journalisé (la réussite l'est par CompleteLogin). Le plafonnement des tentatives reste à la charge de l'appelant (il dépend de l'IP).
func (s *Service) Authenticate(ctx context.Context, in LoginInput) (store.User, error) {
	oc, ocErr := store.GetOIDCConfig(ctx, s.st.DB)
	ssoOn := ocErr == nil && oc.Enabled
	deny := func(u store.User, reason string) {
		name := in.Username
		if u.ID != "" {
			name = u.Username // nom canonique du compte connu
		}
		ev := audit.Event{ActorType: "system", ActorName: name, Action: "user.login_failed",
			TargetType: "user", TargetID: u.ID, TargetName: name, IP: in.IP}
		if reason != "" {
			ev.Diff = map[string]any{"reason": reason}
		}
		_ = audit.RecordStandalone(ctx, s.st, ev)
	}
	if ssoOn && oc.LocalLoginDisabled {
		deny(store.User{}, "local_login_disabled")
		return store.User{}, ErrLocalLoginDisabled
	}
	u, err := s.st.UserByUsername(ctx, in.Username)
	ok := false
	if err == nil {
		// Un compte SSO n'a pas de mot de passe local.
		ok = crypto.CheckPassword(in.Password, u.PasswordHash) && !u.Disabled && u.AuthProvider == "local"
	} else {
		crypto.CheckPassword(in.Password, dummyHash)
	}
	if !ok {
		deny(store.User{}, "")
		return store.User{}, ErrInvalidCredentials
	}
	if ssoOn && oc.LocalPasswordAdminsOnly && u.Role != "admin" {
		deny(u, "local_password_disabled")
		return store.User{}, ErrLocalPasswordAdminsOnly
	}
	return u, nil
}

// CompleteLogin consigne une connexion réussie, une fois la session créée par l'appelant.
func (s *Service) CompleteLogin(ctx context.Context, u store.User, ip string) {
	_ = s.st.TouchLogin(ctx, u.ID)
	_ = audit.RecordStandalone(ctx, s.st, audit.Event{ActorType: "user", ActorID: u.ID, ActorName: u.Username, Action: "user.login",
		TargetType: "user", TargetID: u.ID, TargetName: u.Username, IP: ip})
}
