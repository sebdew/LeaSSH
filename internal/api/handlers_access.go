package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

type keyInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ExpiresAt   string   `json:"expires_at"`
	CanPropose  *bool    `json:"can_propose"`
	Disabled    bool     `json:"disabled"`
	RuleIDs     []string `json:"rule_ids"`
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	items, err := store.ListKeys(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items}) // le hash est en json:"-"
}

func (in *keyInput) validate() (string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 120 {
		return "", bad("invalid_name", "nom requis (120 caractères max)")
	}
	exp, err := parseExpiry(in.ExpiresAt)
	if err != nil {
		return "", err
	}
	if exp != "" {
		if t, _ := store.ParseTime(exp); !t.After(time.Now()) {
			return "", bad("invalid_expiry", "la date d'expiration est dans le passé")
		}
	}
	return exp, nil
}

// createKey: le jeton complet n'est renvoyé qu'ici, une seule fois; seul son hash est stocké.
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var in keyInput
	if !decode(w, r, &in) {
		return
	}
	exp, err := in.validate()
	if err != nil {
		s.fail(w, err)
		return
	}
	token, err := crypto.NewToken()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		k, err := store.InsertKey(ctx, tx, store.MCPKey{Name: in.Name, Description: in.Description, Prefix: crypto.Prefix(token), Hash: crypto.HashToken(token),
			ExpiresAt: exp, CanPropose: in.CanPropose == nil || *in.CanPropose, CreatedBy: u.ID, RuleIDs: in.RuleIDs})
		if err != nil {
			return outcome{}, err
		}
		return outcome{201, map[string]any{"key": k, "token": token}, &audit.Event{Action: "key.create", TargetType: "key", TargetID: k.ID, TargetName: k.Name,
			Diff: map[string]any{"after": map[string]any{"name": k.Name, "prefix": k.Prefix, "expires_at": k.ExpiresAt, "can_propose": k.CanPropose, "rule_ids": k.RuleIDs}}}}, nil
	})
}

func (s *Server) updateKey(w http.ResponseWriter, r *http.Request) {
	var in keyInput
	if !decode(w, r, &in) {
		return
	}
	exp, err := in.validate()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, err := store.KeyByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		k := old
		k.Name, k.Description, k.ExpiresAt, k.Disabled = in.Name, in.Description, exp, in.Disabled
		if in.CanPropose != nil {
			k.CanPropose = *in.CanPropose
		}
		if in.RuleIDs != nil {
			k.RuleIDs = in.RuleIDs
		}
		if err := store.UpdateKey(ctx, tx, k); err != nil {
			return outcome{}, err
		}
		return outcome{200, k, &audit.Event{Action: "key.update", TargetType: "key", TargetID: k.ID, TargetName: k.Name, Diff: map[string]any{"before": old, "after": k}}}, nil
	})
}

// revokeKey désactive la clé: effet immédiat (la clé est relue à chaque requête).
func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		k, err := store.KeyByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		k.Disabled, k.RuleIDs = true, []string{} // une clé révoquée sort de toutes les règles
		if err := store.UpdateKey(ctx, tx, k); err != nil {
			return outcome{}, err
		}
		return outcome{200, k, &audit.Event{Action: "key.revoke", TargetType: "key", TargetID: k.ID, TargetName: k.Name}}, nil
	})
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, err := store.KeyByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		if err := store.DeleteKey(ctx, tx, old.ID); err != nil {
			return outcome{}, err
		}
		return outcome{Event: &audit.Event{Action: "key.delete", TargetType: "key", TargetID: old.ID, TargetName: old.Name}}, nil
	})
}

type ruleInput struct {
	Name                  string   `json:"name"`
	ServerGroupIDs        []string `json:"server_group_ids"`
	CommandGroupIDs       []string `json:"command_group_ids"`
	Profile               string   `json:"profile"`
	WriteRequiresApproval *bool    `json:"write_requires_approval"`
	Enabled               *bool    `json:"enabled"`
	KeyIDs                []string `json:"key_ids"`
}

func (in ruleInput) toRule() (store.AccessRule, error) {
	if strings.TrimSpace(in.Name) == "" {
		return store.AccessRule{}, bad("invalid_name", "nom requis")
	}
	if in.Profile != "RO" && in.Profile != "RW" {
		return store.AccessRule{}, bad("invalid_profile", "profil: RO ou RW")
	}
	if len(in.ServerGroupIDs) == 0 || len(in.CommandGroupIDs) == 0 {
		return store.AccessRule{}, bad("invalid_rule", "au moins un groupe de serveurs et un groupe de commandes requis")
	}
	return store.AccessRule{Name: strings.TrimSpace(in.Name), ServerGroupIDs: in.ServerGroupIDs, CommandGroupIDs: in.CommandGroupIDs, Profile: in.Profile,
		WriteRequiresApproval: in.WriteRequiresApproval == nil || *in.WriteRequiresApproval, Enabled: in.Enabled == nil || *in.Enabled, KeyIDs: in.KeyIDs}, nil
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	items, err := store.ListRules(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var in ruleInput
	if !decode(w, r, &in) {
		return
	}
	rule, err := in.toRule()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		rule, err := store.InsertRule(ctx, tx, rule)
		if err != nil {
			return outcome{}, err
		}
		return outcome{201, rule, &audit.Event{Action: "rule.create", TargetType: "rule", TargetID: rule.ID, TargetName: rule.Name, Diff: map[string]any{"after": rule}}}, nil
	})
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	var in ruleInput
	if !decode(w, r, &in) {
		return
	}
	rule, err := in.toRule()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, err := store.RuleByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		rule.ID = old.ID
		if err := store.UpdateRule(ctx, tx, rule); err != nil {
			return outcome{}, err
		}
		return outcome{200, rule, &audit.Event{Action: "rule.update", TargetType: "rule", TargetID: rule.ID, TargetName: rule.Name, Diff: map[string]any{"before": old, "after": rule}}}, nil
	})
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, err := store.RuleByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		if err := store.DeleteRule(ctx, tx, old.ID); err != nil {
			return outcome{}, err
		}
		return outcome{Event: &audit.Event{Action: "rule.delete", TargetType: "rule", TargetID: old.ID, TargetName: old.Name, Diff: map[string]any{"before": old}}}, nil
	})
}

// simulate: même fonction d'évaluation que l'exécution et les outils MCP (SPEC §5).
func (s *Server) simulate(w http.ResponseWriter, r *http.Request) {
	key, err := store.KeyByID(r.Context(), s.st.DB, r.URL.Query().Get("key"))
	if err != nil {
		s.fail(w, err)
		return
	}
	snap, err := s.acc.Load(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"key": key.Name, "cells": snap.Matrix(time.Now(), key, r.URL.Query().Get("server"))})
}
