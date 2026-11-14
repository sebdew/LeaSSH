package accounts

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

// Erreurs de session: l'API les traduit en 401 sans révéler la cause exacte au-delà du message.
var (
	ErrSessionExpired     = errors.New("session expirée")
	ErrAccountUnavailable = errors.New("compte indisponible")
)

// HashSession: seul le haché de l'identifiant de session est stocké en base (le cookie porte la valeur en clair).
func HashSession(cookie string) string { return crypto.HashToken(cookie) }

func randToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("jeton aléatoire: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Initialized indique si l'instance a déjà un compte (écran d'initialisation sinon).
func (s *Service) Initialized(ctx context.Context) (bool, error) {
	n, err := s.st.UserCount(ctx)
	if err != nil {
		return false, fmt.Errorf("comptes: %w", err)
	}
	return n > 0, nil
}

// OpenSession crée une session et renvoie la valeur du cookie et le jeton CSRF associé.
func (s *Service) OpenSession(ctx context.Context, u store.User, ip string, ttl time.Duration) (cookie, csrf string, err error) {
	if cookie, err = randToken(); err != nil {
		return "", "", err
	}
	if csrf, err = randToken(); err != nil {
		return "", "", err
	}
	if err = s.st.CreateSession(ctx, HashSession(cookie), u.ID, csrf, ip, ttl); err != nil {
		return "", "", fmt.Errorf("création de session: %w", err)
	}
	return cookie, csrf, nil
}

// LookupSession retrouve l'utilisateur et le jeton CSRF d'une session valide (non expirée, non inactive, compte actif).
func (s *Service) LookupSession(ctx context.Context, cookie string, idle time.Duration) (store.User, string, error) {
	se, err := s.st.SessionByHash(ctx, HashSession(cookie), idle)
	if err != nil {
		return store.User{}, "", ErrSessionExpired
	}
	u, err := s.st.UserByID(ctx, se.UserID)
	if err != nil || u.Disabled {
		return store.User{}, "", ErrAccountUnavailable
	}
	return u, se.CSRF, nil
}

// CloseSession supprime la session (déconnexion).
func (s *Service) CloseSession(ctx context.Context, cookie string) error {
	return s.st.DeleteSession(ctx, HashSession(cookie))
}

// Limiter compte les échecs en base (SQLite) pour que les verrouillages survivent à un redémarrage (SEC-04).
// scope sépare les compteurs par compte et par adresse.
type Limiter struct {
	st    *store.Store
	scope string
	max   int
	win   time.Duration
}

func NewLimiter(st *store.Store, scope string, max int, win time.Duration) *Limiter {
	return &Limiter{st: st, scope: scope, max: max, win: win}
}

// Blocked: en cas d'erreur de base, on refuse (fail closed) plutôt que de laisser passer des essais illimités.
func (l *Limiter) Blocked(k string) bool {
	n, err := store.CountLoginFailures(context.Background(), l.st.DB, l.scope, k, time.Now().Add(-l.win))
	return err != nil || n >= l.max
}

func (l *Limiter) Fail(k string) {
	now := time.Now()
	_ = store.AddLoginFailure(context.Background(), l.st.DB, l.scope, k, now, now.Add(-l.win))
}

func (l *Limiter) Reset(k string) {
	_ = store.ResetLoginFailures(context.Background(), l.st.DB, l.scope, k)
}
