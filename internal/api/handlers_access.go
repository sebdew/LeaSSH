package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/leassh-dev/leassh/internal/access"
)

type keyInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ExpiresAt   string   `json:"expires_at"`
	CanPropose  *bool    `json:"can_propose"`
	Disabled    bool     `json:"disabled"`
	RuleIDs     []string `json:"rule_ids"`
}

func (in keyInput) toService() access.KeyInput {
	return access.KeyInput{Name: in.Name, Description: in.Description, ExpiresAt: in.ExpiresAt, CanPropose: in.CanPropose, Disabled: in.Disabled, RuleIDs: in.RuleIDs}
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

func (in ruleInput) toService() access.RuleInput {
	return access.RuleInput{Name: in.Name, ServerGroupIDs: in.ServerGroupIDs, CommandGroupIDs: in.CommandGroupIDs, Profile: in.Profile,
		WriteRequiresApproval: in.WriteRequiresApproval, Enabled: in.Enabled, KeyIDs: in.KeyIDs}
}

// accessErr traduit les erreurs de validation du service en erreurs d'API.
func accessErr(err error) error {
	switch {
	case errors.Is(err, access.ErrInvalidKeyName), errors.Is(err, access.ErrInvalidRuleName):
		return bad("invalid_name", err.Error())
	case errors.Is(err, access.ErrInvalidExpiry), errors.Is(err, access.ErrExpiryPast):
		return bad("invalid_expiry", err.Error())
	case errors.Is(err, access.ErrInvalidProfile):
		return bad("invalid_profile", err.Error())
	case errors.Is(err, access.ErrInvalidRule):
		return bad("invalid_rule", err.Error())
	}
	return err
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	items, err := s.acc.ListKeys(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items}) // le hash est en json:"-"
}

// createKey: le jeton complet n'est renvoyé qu'ici, une seule fois; seul son hash est stocké.
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var in keyInput
	if !decode(w, r, &in) {
		return
	}
	k, token, err := s.acc.CreateKey(r.Context(), currentUser(r), s.clientIP(r), in.toService())
	if err != nil {
		s.fail(w, accessErr(err))
		return
	}
	writeJSON(w, 201, map[string]any{"key": k, "token": token})
}

func (s *Server) updateKey(w http.ResponseWriter, r *http.Request) {
	var in keyInput
	if !decode(w, r, &in) {
		return
	}
	k, err := s.acc.UpdateKey(r.Context(), currentUser(r), s.clientIP(r), id(r), in.toService())
	if err != nil {
		s.fail(w, accessErr(err))
		return
	}
	writeJSON(w, 200, k)
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	k, err := s.acc.RevokeKey(r.Context(), currentUser(r), s.clientIP(r), id(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, k)
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	if err := s.acc.DeleteKey(r.Context(), currentUser(r), s.clientIP(r), id(r)); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	items, err := s.acc.ListRules(r.Context())
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
	rule, err := s.acc.CreateRule(r.Context(), currentUser(r), s.clientIP(r), in.toService())
	if err != nil {
		s.fail(w, accessErr(err))
		return
	}
	writeJSON(w, 201, rule)
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	var in ruleInput
	if !decode(w, r, &in) {
		return
	}
	rule, err := s.acc.UpdateRule(r.Context(), currentUser(r), s.clientIP(r), id(r), in.toService())
	if err != nil {
		s.fail(w, accessErr(err))
		return
	}
	writeJSON(w, 200, rule)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.acc.DeleteRule(r.Context(), currentUser(r), s.clientIP(r), id(r)); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

// simulate: même fonction d'évaluation que l'exécution et les outils MCP (SPEC §5).
func (s *Server) simulate(w http.ResponseWriter, r *http.Request) {
	name, cells, err := s.acc.Simulate(r.Context(), r.URL.Query().Get("key"), r.URL.Query().Get("server"), time.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"key": name, "cells": cells})
}
