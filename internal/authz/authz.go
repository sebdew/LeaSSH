// Package authz: fonction unique d'évaluation des droits (SPEC §5).
// Utilisée à l'identique par l'exécution, les outils MCP et le simulateur d'accès.
package authz

import (
	"time"

	"github.com/leash-dev/leash/internal/catalog"
)

type Profile string

const (
	RO Profile = "RO"
	RW Profile = "RW"
)

// Key: clé MCP (état utile à l'autorisation).
type Key struct {
	ID        string
	Disabled  bool
	ExpiresAt *time.Time
}

type Server struct {
	ID            string
	Enabled       bool
	HostKeyStatus string // unknown | pinned | mismatch
}

type Command struct {
	ID     string
	Status string // pending | enabled | disabled
	Risk   catalog.Risk
}

type Rule struct {
	ID                    string
	Name                  string
	Enabled               bool
	Profile               Profile
	WriteRequiresApproval bool
	KeyIDs                map[string]bool
	ServerIDs             map[string]bool // membres du groupe de serveurs
	CommandIDs            map[string]bool // membres du groupe de commandes
}

// Denial: raisons internes, journalisées dans denial_reason mais jamais détaillées à l'agent (invariant 8).
const (
	DenyKeyInactive    = "key_inactive"
	DenyKeyExpired     = "key_expired"
	DenyServerDisabled = "server_disabled"
	DenyHostKey        = "host_key_not_pinned"
	DenyCommandStatus  = "command_not_enabled"
	DenyNoRule         = "no_matching_rule"
	DenyProfile        = "profile_forbids_risk"
)

type Decision struct {
	Allowed          bool
	RequiresApproval bool
	Rule             *Rule  // règle retenue (la moins contraignante)
	Reason           string // raison du refus (vide si autorisé)
}

// RiskAllowed: RO → read seulement; RW → tout.
func RiskAllowed(p Profile, r catalog.Risk) bool {
	switch p {
	case RO:
		return r == catalog.RiskRead
	case RW:
		return r.Valid()
	}
	return false
}

// NeedsApproval: destructive toujours; write si la règle l'exige; read jamais.
func NeedsApproval(r *Rule, risk catalog.Risk) bool {
	switch risk {
	case catalog.RiskDestructive:
		return true
	case catalog.RiskWrite:
		return r.WriteRequiresApproval
	}
	return false
}

// Evaluate décide si la clé peut exécuter cmd sur srv. Parmi plusieurs règles valides on retient
// la moins contraignante: sans validation de préférence, puis à égalité l'identifiant le plus petit (déterministe).
func Evaluate(now time.Time, k Key, srv Server, cmd Command, rules []Rule) Decision {
	if k.Disabled {
		return Decision{Reason: DenyKeyInactive}
	}
	if k.ExpiresAt != nil && !now.Before(*k.ExpiresAt) {
		return Decision{Reason: DenyKeyExpired}
	}
	if !srv.Enabled {
		return Decision{Reason: DenyServerDisabled}
	}
	if srv.HostKeyStatus != "pinned" {
		return Decision{Reason: DenyHostKey}
	}
	if cmd.Status != "enabled" {
		return Decision{Reason: DenyCommandStatus}
	}
	var best *Rule
	bestApproval := true
	matched := false
	for i := range rules {
		r := &rules[i]
		if !r.Enabled || !r.KeyIDs[k.ID] || !r.ServerIDs[srv.ID] || !r.CommandIDs[cmd.ID] {
			continue
		}
		matched = true
		if !RiskAllowed(r.Profile, cmd.Risk) {
			continue
		}
		ap := NeedsApproval(r, cmd.Risk)
		if best == nil || (bestApproval && !ap) || (ap == bestApproval && r.ID < best.ID) {
			best, bestApproval = r, ap
		}
	}
	if best == nil {
		if matched {
			return Decision{Reason: DenyProfile}
		}
		return Decision{Reason: DenyNoRule}
	}
	return Decision{Allowed: true, RequiresApproval: bestApproval, Rule: best}
}
