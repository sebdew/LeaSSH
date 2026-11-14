package syslog

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/sebdew/LeaSSH/internal/store"
)

// Catégories filtrables.
var Types = []string{"execution", "proposal", "audit"}

const (
	sevCrit    = 2
	sevErr     = 3
	sevWarning = 4
	sevNotice  = 5
	sevInfo    = 6
)

func parseTS(s string) time.Time {
	if t, err := store.ParseTime(s); err == nil {
		return t
	}
	return time.Now()
}

// FromExecution: résumé d'une exécution. Ni stdout, ni stderr: seulement leur taille.
func FromExecution(event string, e store.Execution) Event {
	sev := sevInfo
	switch event {
	case "execution.denied":
		sev = sevWarning
	case "execution.pending_approval", "execution.expired", "execution.rejected":
		sev = sevNotice
	case "execution.finished":
		switch e.Status {
		case "failed", "timeout":
			sev = sevWarning
		case "error":
			sev = sevErr
		}
	}
	agent := e.KeyName
	if agent == "" {
		agent = e.DecidedByName
	}
	rendered := ""
	if e.Rendered != nil {
		rendered = *e.Rendered
	}
	f := []Field{{"id", e.ID}, {"status", e.Status}, {"agent", agent}, {"server", e.ServerSlug}, {"command", e.CommandSlug}, {"risk", e.Risk},
		{"denial_reason", e.DenialReason}, {"rule", e.RuleName}, {"decided_by", e.DecidedByName}, {"client_ip", e.ClientIP},
		{"reason", e.Reason}, {"rendered", rendered}}
	if e.ExitCode != nil {
		f = append(f, Field{"exit_code", *e.ExitCode})
	}
	if e.DurationMS != nil {
		f = append(f, Field{"duration_ms", *e.DurationMS})
	}
	if e.Stdout != "" || e.Stderr != "" {
		f = append(f, Field{"stdout_bytes", len(e.Stdout)}, Field{"stderr_bytes", len(e.Stderr)}, Field{"output_truncated", e.Truncated})
	}
	return Event{Time: time.Now(), Type: event, Severity: sev, Fields: f}
}

func FromProposal(event string, p store.Proposal) Event {
	var payload struct{ Slug, Name, Risk string }
	_ = json.Unmarshal([]byte(p.Payload), &payload)
	return Event{Time: parseTS(p.CreatedAt), Type: event, Severity: sevNotice, Fields: []Field{{"id", p.ID}, {"agent", p.KeyName}, {"slug", payload.Slug},
		{"name", payload.Name}, {"risk", payload.Risk}, {"status", p.Status}, {"justification", p.Justification}}}
}

// FromAudit: événement d'administration. Le diff est déjà masqué à l'écriture (audit.Mask); Redact s'applique en plus au texte.
func FromAudit(a store.AuditEvent) Event {
	sev := sevInfo
	switch {
	case a.Action == "host_key.mismatch":
		sev = sevCrit
	case strings.Contains(a.Action, "delete"), strings.Contains(a.Action, "reject"), strings.Contains(a.Action, "revoke"),
		strings.Contains(a.Action, "rotate"), strings.Contains(a.Action, "disable"):
		sev = sevNotice
	}
	diff := strings.TrimSpace(string(a.Diff))
	if diff == "{}" || diff == "null" {
		diff = ""
	}
	return Event{Time: parseTS(a.TS), Type: "audit." + a.Action, Severity: sev, AuditSeq: a.Seq, Fields: []Field{{"seq", a.Seq}, {"id", a.ID}, {"actor_type", a.ActorType},
		{"actor", a.ActorName}, {"action", a.Action}, {"target_type", a.TargetType}, {"target_id", a.TargetID}, {"target", a.TargetName}, {"ip", a.IP},
		{"diff", json.RawMessage(diff)}}}
}
