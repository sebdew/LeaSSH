package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// AuditEvent: ligne du journal d'administration.
type AuditEvent struct {
	ID         string          `json:"id"`
	TS         string          `json:"ts"`
	ActorType  string          `json:"actor_type"`
	ActorID    string          `json:"actor_id"`
	ActorName  string          `json:"actor_name"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	TargetName string          `json:"target_name"`
	Diff       json.RawMessage `json:"diff"`
	IP         string          `json:"ip"`
	Seq        int64           `json:"seq"`
}

type AuditFilter struct {
	From, To   string
	ActorType  string
	ActorID    string
	Action     string // préfixe (« server. » couvre server.create…) ou valeur exacte
	TargetType string
	TargetID   string
	Text       string // acteur, cible ou action
	Limit      int
	Before     string
}

func (f AuditFilter) where() (string, []any) {
	var w []string
	var a []any
	eq := func(col, v string) {
		if v != "" {
			w = append(w, col+"=?")
			a = append(a, v)
		}
	}
	eq("actor_type", f.ActorType)
	eq("actor_id", f.ActorID)
	eq("target_type", f.TargetType)
	eq("target_id", f.TargetID)
	if f.Action != "" {
		w = append(w, `action LIKE ? ESCAPE '\'`)
		a = append(a, escapeLike(f.Action)+"%")
	}
	if f.From != "" {
		w = append(w, "ts>=?")
		a = append(a, f.From)
	}
	if f.To != "" {
		w = append(w, "ts<=?")
		a = append(a, f.To)
	}
	if f.Text != "" {
		like := "%" + escapeLike(f.Text) + "%"
		w = append(w, `(actor_name LIKE ? ESCAPE '\' OR target_name LIKE ? ESCAPE '\' OR action LIKE ? ESCAPE '\')`)
		a = append(a, like, like, like)
	}
	if f.Before != "" {
		w = append(w, "id<?")
		a = append(a, f.Before)
	}
	if len(w) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(w, " AND "), a
}

func ListAudit(ctx context.Context, q Q, f AuditFilter) ([]AuditEvent, error) {
	if f.Limit <= 0 || f.Limit > 501 {
		f.Limit = 50
	}
	where, args := f.where()
	rows, err := q.QueryContext(ctx, `SELECT id,ts,actor_type,actor_id,actor_name,action,target_type,target_id,target_name,diff,ip,seq FROM audit_events`+where+
		fmt.Sprintf(" ORDER BY id DESC LIMIT %d", f.Limit), args...)
	if err != nil {
		return nil, wrapErr("liste audit", err)
	}
	return collect(rows, func(r *sql.Rows) (AuditEvent, error) {
		var e AuditEvent
		var diff string
		err := r.Scan(&e.ID, &e.TS, &e.ActorType, &e.ActorID, &e.ActorName, &e.Action, &e.TargetType, &e.TargetID, &e.TargetName, &diff, &e.IP, &e.Seq)
		e.Diff = json.RawMessage(diff)
		return e, err
	})
}

// ---- Intégrité (chaînes de hash) ----

// ChainResult: résultat de la vérification d'une chaîne. La première ligne restante sert d'ancre
// (la rétention peut avoir purgé les précédentes); toute altération, suppression au milieu ou trou de séquence est détecté.
// Limite connue: la troncature de la fin de chaîne n'est pas détectable sans ancrage externe.
type ChainResult struct {
	Checked int    `json:"checked"`
	OK      bool   `json:"ok"`
	BadSeq  int64  `json:"bad_seq,omitempty"`
	BadID   string `json:"bad_id,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type chainRow struct {
	id, prev, hash string
	seq            int64
	fields         func(prev string) []string
}

// verifyChain lit par pages de 1000 (une seule connexion SQLite: on ne la garde jamais longtemps).
func verifyChain(ctx context.Context, q Q, next func(ctx context.Context, q Q, afterSeq int64) ([]chainRow, error)) (ChainResult, error) {
	res := ChainResult{OK: true}
	var lastSeq int64
	var lastHash string
	first := true
	for {
		rows, err := next(ctx, q, lastSeq)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			return res, nil
		}
		for _, r := range rows {
			bad := func(reason string) (ChainResult, error) {
				return ChainResult{Checked: res.Checked, OK: false, BadSeq: r.seq, BadID: r.id, Reason: reason}, nil
			}
			switch {
			case first && r.seq == 1 && r.prev != "":
				return bad("la première ligne a un prédécesseur")
			case !first && r.seq != lastSeq+1:
				return bad("trou dans la séquence (ligne supprimée)")
			case !first && r.prev != lastHash:
				return bad("chaînage rompu")
			}
			if chainHash(r.prev, r.fields(r.prev)...) != r.hash {
				return bad("contenu altéré")
			}
			first, lastSeq, lastHash = false, r.seq, r.hash
			res.Checked++
		}
	}
}

func VerifyExecutions(ctx context.Context, q Q) (ChainResult, error) {
	return verifyChain(ctx, q, func(ctx context.Context, q Q, after int64) ([]chainRow, error) {
		rows, err := q.QueryContext(ctx, `SELECT `+execCols+`,prev_hash,row_hash FROM executions WHERE seq>? ORDER BY seq LIMIT 1000`, after)
		if err != nil {
			return nil, wrapErr("lecture exécutions", err)
		}
		return collect(rows, func(r *sql.Rows) (chainRow, error) {
			var e Execution
			var prev, hash string
			err := r.Scan(&e.ID, &e.KeyID, &e.KeyName, &e.ActorUserID, &e.ServerID, &e.ServerSlug, &e.CommandID, &e.CommandSlug, &e.CommandVersion, &e.CommandHash,
				&e.Risk, &e.Params, &e.Rendered, &e.Reason, &e.RuleID, &e.RuleName, &e.Status, &e.DenialReason, &e.ExitCode, &e.Stdout, &e.Stderr, &e.Truncated,
				&e.DurationMS, &e.RequestedAt, &e.StartedAt, &e.FinishedAt, &e.DecidedBy, &e.DecidedByName, &e.DecidedAt, &e.DecisionComment, &e.ClientIP, &e.MCPSessionID, &e.UserAgent, &e.Seq,
				&prev, &hash)
			return chainRow{id: e.ID, seq: e.Seq, prev: prev, hash: hash, fields: func(string) []string { return execChainFields(e) }}, err
		})
	})
}

// AuditChainHash / AuditChainFields doivent rester identiques à audit.Record (écriture) — cf. audit.Verify.
func VerifyAudit(ctx context.Context, q Q, hashFields func(id, ts string, seq int64, actorType, actorID, actorName, action, targetType, targetID, targetName, diff, ip string) []string) (ChainResult, error) {
	return verifyChain(ctx, q, func(ctx context.Context, q Q, after int64) ([]chainRow, error) {
		rows, err := q.QueryContext(ctx, `SELECT id,ts,seq,actor_type,actor_id,actor_name,action,target_type,target_id,target_name,diff,ip,prev_hash,row_hash FROM audit_events WHERE seq>? ORDER BY seq LIMIT 1000`, after)
		if err != nil {
			return nil, wrapErr("lecture audit", err)
		}
		return collect(rows, func(r *sql.Rows) (chainRow, error) {
			var id, ts, at, aid, an, act, tt, tid, tn, diff, ip, prev, hash string
			var seq int64
			err := r.Scan(&id, &ts, &seq, &at, &aid, &an, &act, &tt, &tid, &tn, &diff, &ip, &prev, &hash)
			return chainRow{id: id, seq: seq, prev: prev, hash: hash,
				fields: func(string) []string { return hashFields(id, ts, seq, at, aid, an, act, tt, tid, tn, diff, ip) }}, err
		})
	})
}

// ---- Rétention ----

// PurgeBefore supprime les lignes de journal antérieures à cutoff (hors demandes en attente ou en cours).
func PurgeBefore(ctx context.Context, tx Q, cutoff string) (execs, audits int64, err error) {
	r, err := tx.ExecContext(ctx, `DELETE FROM executions WHERE requested_at<? AND status NOT IN ('pending_approval','running')`, cutoff)
	if err != nil {
		return 0, 0, wrapErr("purge exécutions", err)
	}
	execs, _ = r.RowsAffected()
	r, err = tx.ExecContext(ctx, `DELETE FROM audit_events WHERE ts<?`, cutoff)
	if err != nil {
		return execs, 0, wrapErr("purge audit", err)
	}
	audits, _ = r.RowsAffected()
	return execs, audits, nil
}
