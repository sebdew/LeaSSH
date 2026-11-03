// Package access charge l'état de configuration et applique authz.Evaluate.
// Utilisé par les outils MCP, executions.Run et le simulateur d'accès: une seule logique d'évaluation.
package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/leash-dev/leash/internal/authz"
	"github.com/leash-dev/leash/internal/crypto"
	"github.com/leash-dev/leash/internal/store"
)

type Service struct{ st *store.Store }

func New(st *store.Store) *Service { return &Service{st: st} }

// ErrInvalidToken: jeton inconnu ou mal formé.
var ErrInvalidToken = errors.New("clé MCP invalide")

// Authenticate retrouve la clé depuis le jeton complet. Une clé désactivée ou expirée est reconnue
// (pour pouvoir journaliser les refus) mais n'a plus aucun droit: Evaluate la rejette.
func (s *Service) Authenticate(ctx context.Context, token string) (store.MCPKey, error) {
	if len(token) < 8 || token[:4] != crypto.TokenPrefix {
		return store.MCPKey{}, ErrInvalidToken
	}
	h := crypto.HashToken(token)
	k, err := store.KeyByHash(ctx, s.st.DB, h)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.MCPKey{}, ErrInvalidToken
		}
		return store.MCPKey{}, err
	}
	if !crypto.TokenEqual(k.Hash, h) {
		return store.MCPKey{}, ErrInvalidToken
	}
	return k, nil
}

// Snapshot est une vue cohérente de la configuration pour une série d'évaluations.
type Snapshot struct {
	Servers  []store.Server
	Commands []store.Command
	rules    []authz.Rule
	names    map[string]string
}

func (s *Service) Load(ctx context.Context) (*Snapshot, error) {
	servers, err := store.ListServers(ctx, s.st.DB)
	if err != nil {
		return nil, err
	}
	cmds, err := store.ListCommands(ctx, s.st.DB)
	if err != nil {
		return nil, err
	}
	rules, err := store.ListRules(ctx, s.st.DB)
	if err != nil {
		return nil, err
	}
	sgs, err := store.ListGroups(ctx, s.st.DB, store.ServerGroups)
	if err != nil {
		return nil, err
	}
	cgs, err := store.ListGroups(ctx, s.st.DB, store.CommandGroups)
	if err != nil {
		return nil, err
	}
	set := func(ids []string) map[string]bool {
		m := make(map[string]bool, len(ids))
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	sg, cg := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, g := range sgs {
		sg[g.ID] = set(g.MemberIDs)
	}
	for _, g := range cgs {
		cg[g.ID] = set(g.MemberIDs)
	}
	snap := &Snapshot{Servers: servers, Commands: cmds, names: map[string]string{}}
	for _, r := range rules {
		snap.names[r.ID] = r.Name
		snap.rules = append(snap.rules, authz.Rule{
			ID: r.ID, Name: r.Name, Enabled: r.Enabled, Profile: authz.Profile(r.Profile), WriteRequiresApproval: r.WriteRequiresApproval,
			KeyIDs: set(r.KeyIDs), ServerIDs: union(sg, r.ServerGroupIDs), CommandIDs: union(cg, r.CommandGroupIDs),
		})
	}
	return snap, nil
}

// union fusionne les membres de plusieurs groupes.
func union(groups map[string]map[string]bool, ids []string) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		for m := range groups[id] {
			out[m] = true
		}
	}
	return out
}

func toKey(k store.MCPKey) authz.Key {
	ak := authz.Key{ID: k.ID, Disabled: k.Disabled}
	if k.ExpiresAt != "" {
		if t, err := store.ParseTime(k.ExpiresAt); err == nil {
			ak.ExpiresAt = &t
		} else if t, err := time.Parse(time.RFC3339, k.ExpiresAt); err == nil {
			ak.ExpiresAt = &t
		} else {
			// date illisible: on refuse plutôt que d'autoriser
			past := time.Unix(0, 0)
			ak.ExpiresAt = &past
		}
	}
	return ak
}

// Evaluate applique SPEC §5.
func (sn *Snapshot) Evaluate(now time.Time, k store.MCPKey, srv store.Server, cmd store.Command) authz.Decision {
	return authz.Evaluate(now, toKey(k),
		authz.Server{ID: srv.ID, Enabled: srv.Enabled, HostKeyStatus: srv.HostKeyStatus},
		authz.Command{ID: cmd.ID, Status: cmd.Status, Risk: cmd.Risk}, sn.rules)
}

func (sn *Snapshot) ServerBySlug(slug string) (store.Server, bool) {
	for _, s := range sn.Servers {
		if s.Slug == slug {
			return s, true
		}
	}
	return store.Server{}, false
}

func (sn *Snapshot) CommandBySlug(slug string) (store.Command, bool) {
	for _, c := range sn.Commands {
		if c.Slug == slug {
			return c, true
		}
	}
	return store.Command{}, false
}

// Entry: une commande accessible, avec la règle qui l'autorise.
type Entry struct {
	Command          store.Command
	RequiresApproval bool
	RuleID           string
	RuleName         string
}

// CommandsFor liste les commandes autorisées pour la clé sur le serveur.
func (sn *Snapshot) CommandsFor(now time.Time, k store.MCPKey, srv store.Server) []Entry {
	var out []Entry
	for _, c := range sn.Commands {
		if d := sn.Evaluate(now, k, srv, c); d.Allowed {
			out = append(out, Entry{Command: c, RequiresApproval: d.RequiresApproval, RuleID: d.Rule.ID, RuleName: d.Rule.Name})
		}
	}
	return out
}

// ServersFor: serveurs pour lesquels au moins une commande est autorisée.
func (sn *Snapshot) ServersFor(now time.Time, k store.MCPKey) []store.Server {
	var out []store.Server
	for _, s := range sn.Servers {
		if len(sn.CommandsFor(now, k, s)) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// Cell: une case du simulateur d'accès.
type Cell struct {
	Server           string `json:"server"`
	Command          string `json:"command"`
	Allowed          bool   `json:"allowed"`
	RequiresApproval bool   `json:"requires_approval"`
	Rule             string `json:"rule,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

// Matrix évalue toutes les combinaisons serveur × commande (SPEC §9.11).
func (sn *Snapshot) Matrix(now time.Time, k store.MCPKey, serverSlug string) []Cell {
	var cells []Cell
	for _, s := range sn.Servers {
		if serverSlug != "" && s.Slug != serverSlug {
			continue
		}
		for _, c := range sn.Commands {
			d := sn.Evaluate(now, k, s, c)
			cell := Cell{Server: s.Slug, Command: c.Slug, Allowed: d.Allowed, RequiresApproval: d.RequiresApproval, Reason: d.Reason}
			if d.Rule != nil {
				cell.Rule = d.Rule.Name
			}
			if !d.Allowed {
				cell.Reason = fmt.Sprint(d.Reason)
			}
			cells = append(cells, cell)
		}
	}
	return cells
}
