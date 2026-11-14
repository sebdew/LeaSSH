package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/crypto"
	"github.com/sebdew/LeaSSH/internal/store"
)

var (
	ErrInvalidKeyName  = errors.New("nom requis (120 caractères max)")
	ErrInvalidExpiry   = errors.New("date d'expiration invalide (RFC 3339 ou AAAA-MM-JJ)")
	ErrExpiryPast      = errors.New("la date d'expiration est dans le passé")
	ErrInvalidRuleName = errors.New("nom requis")
	ErrInvalidProfile  = errors.New("profil: RO ou RW")
	ErrInvalidRule     = errors.New("au moins un groupe de serveurs et un groupe de commandes requis")
)

// KeyInput: création ou modification d'une clé MCP. À la modification, CanPropose et RuleIDs absents (nil) sont inchangés.
type KeyInput struct {
	Name, Description, ExpiresAt string
	CanPropose                   *bool
	Disabled                     bool
	RuleIDs                      []string
}

// validate normalise le nom et convertit l'expiration (RFC 3339 ou AAAA-MM-JJ, futur) au format de stockage.
func (in *KeyInput) validate() (string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 120 {
		return "", ErrInvalidKeyName
	}
	v := strings.TrimSpace(in.ExpiresAt)
	if v == "" {
		return "", nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			if !t.After(time.Now()) {
				return "", ErrExpiryPast
			}
			return store.FormatTime(t), nil
		}
	}
	return "", ErrInvalidExpiry
}

func (s *Service) ListKeys(ctx context.Context) ([]store.MCPKey, error) {
	return store.ListKeys(ctx, s.st.DB)
}

// CreateKey: le jeton complet n'est renvoyé qu'ici, une seule fois; seul son hash est stocké.
func (s *Service) CreateKey(ctx context.Context, actor store.User, ip string, in KeyInput) (store.MCPKey, string, error) {
	exp, err := in.validate()
	if err != nil {
		return store.MCPKey{}, "", err
	}
	token, err := crypto.NewToken()
	if err != nil {
		return store.MCPKey{}, "", fmt.Errorf("génération du jeton: %w", err)
	}
	var k store.MCPKey
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		var err error
		k, err = store.InsertKey(ctx, tx, store.MCPKey{Name: in.Name, Description: in.Description, Prefix: crypto.Prefix(token), Hash: crypto.HashToken(token),
			ExpiresAt: exp, CanPropose: in.CanPropose == nil || *in.CanPropose, CreatedBy: actor.ID, RuleIDs: in.RuleIDs})
		if err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "key.create", TargetType: "key", TargetID: k.ID, TargetName: k.Name,
			Diff: map[string]any{"after": map[string]any{"name": k.Name, "prefix": k.Prefix, "expires_at": k.ExpiresAt, "can_propose": k.CanPropose, "rule_ids": k.RuleIDs}}}, nil
	})
	if err != nil {
		return store.MCPKey{}, "", err
	}
	return k, token, nil
}

func (s *Service) UpdateKey(ctx context.Context, actor store.User, ip, id string, in KeyInput) (store.MCPKey, error) {
	exp, err := in.validate()
	if err != nil {
		return store.MCPKey{}, err
	}
	var k store.MCPKey
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.KeyByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		k = old
		k.Name, k.Description, k.ExpiresAt, k.Disabled = in.Name, in.Description, exp, in.Disabled
		if in.CanPropose != nil {
			k.CanPropose = *in.CanPropose
		}
		if in.RuleIDs != nil {
			k.RuleIDs = in.RuleIDs
		}
		if err := store.UpdateKey(ctx, tx, k); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "key.update", TargetType: "key", TargetID: k.ID, TargetName: k.Name, Diff: map[string]any{"before": old, "after": k}}, nil
	})
	return k, err
}

// RevokeKey désactive la clé et la retire de toutes les règles: effet immédiat (la clé est relue à chaque requête).
func (s *Service) RevokeKey(ctx context.Context, actor store.User, ip, id string) (store.MCPKey, error) {
	var k store.MCPKey
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		var err error
		if k, err = store.KeyByID(ctx, tx, id); err != nil {
			return audit.Event{}, err
		}
		k.Disabled, k.RuleIDs = true, []string{}
		if err := store.UpdateKey(ctx, tx, k); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "key.revoke", TargetType: "key", TargetID: k.ID, TargetName: k.Name}, nil
	})
	return k, err
}

func (s *Service) DeleteKey(ctx context.Context, actor store.User, ip, id string) error {
	return audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.KeyByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		if err := store.DeleteKey(ctx, tx, old.ID); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "key.delete", TargetType: "key", TargetID: old.ID, TargetName: old.Name}, nil
	})
}

// RuleInput: règle d'accès. WriteRequiresApproval et Enabled absents (nil) valent true (défauts sûrs).
type RuleInput struct {
	Name                  string
	ServerGroupIDs        []string
	CommandGroupIDs       []string
	Profile               string
	WriteRequiresApproval *bool
	Enabled               *bool
	KeyIDs                []string
}

func (in RuleInput) toRule() (store.AccessRule, error) {
	switch {
	case strings.TrimSpace(in.Name) == "":
		return store.AccessRule{}, ErrInvalidRuleName
	case in.Profile != "RO" && in.Profile != "RW":
		return store.AccessRule{}, ErrInvalidProfile
	case len(in.ServerGroupIDs) == 0 || len(in.CommandGroupIDs) == 0:
		return store.AccessRule{}, ErrInvalidRule
	}
	return store.AccessRule{Name: strings.TrimSpace(in.Name), ServerGroupIDs: in.ServerGroupIDs, CommandGroupIDs: in.CommandGroupIDs, Profile: in.Profile,
		WriteRequiresApproval: in.WriteRequiresApproval == nil || *in.WriteRequiresApproval, Enabled: in.Enabled == nil || *in.Enabled, KeyIDs: in.KeyIDs}, nil
}

func (s *Service) ListRules(ctx context.Context) ([]store.AccessRule, error) {
	return store.ListRules(ctx, s.st.DB)
}

func (s *Service) CreateRule(ctx context.Context, actor store.User, ip string, in RuleInput) (store.AccessRule, error) {
	rule, err := in.toRule()
	if err != nil {
		return store.AccessRule{}, err
	}
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		var err error
		if rule, err = store.InsertRule(ctx, tx, rule); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "rule.create", TargetType: "rule", TargetID: rule.ID, TargetName: rule.Name, Diff: map[string]any{"after": rule}}, nil
	})
	return rule, err
}

func (s *Service) UpdateRule(ctx context.Context, actor store.User, ip, id string, in RuleInput) (store.AccessRule, error) {
	rule, err := in.toRule()
	if err != nil {
		return store.AccessRule{}, err
	}
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.RuleByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		rule.ID = old.ID
		if err := store.UpdateRule(ctx, tx, rule); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "rule.update", TargetType: "rule", TargetID: rule.ID, TargetName: rule.Name, Diff: map[string]any{"before": old, "after": rule}}, nil
	})
	return rule, err
}

func (s *Service) DeleteRule(ctx context.Context, actor store.User, ip, id string) error {
	return audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.RuleByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		if err := store.DeleteRule(ctx, tx, old.ID); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "rule.delete", TargetType: "rule", TargetID: old.ID, TargetName: old.Name, Diff: map[string]any{"before": old}}, nil
	})
}

// Simulate: même fonction d'évaluation que l'exécution et les outils MCP (SPEC §5). Renvoie le nom de la clé et la matrice.
func (s *Service) Simulate(ctx context.Context, keyID, serverSlug string, now time.Time) (string, []Cell, error) {
	key, err := store.KeyByID(ctx, s.st.DB, keyID)
	if err != nil {
		return "", nil, err
	}
	snap, err := s.Load(ctx)
	if err != nil {
		return "", nil, err
	}
	return key.Name, snap.Matrix(now, key, serverSlug), nil
}
