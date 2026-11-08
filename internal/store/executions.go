package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

// Execution est une ligne du journal des exécutions (SPEC §4.9).
type Execution struct {
	ID              string  `json:"id"`
	KeyID           string  `json:"key_id"`
	KeyName         string  `json:"key_name"`
	ActorUserID     string  `json:"actor_user_id"`
	ServerID        string  `json:"server_id"`
	ServerSlug      string  `json:"server_slug"`
	CommandID       string  `json:"command_id"`
	CommandSlug     string  `json:"command_slug"`
	CommandVersion  int     `json:"command_version"`
	CommandHash     string  `json:"command_hash"`
	Risk            string  `json:"risk"`
	Params          string  `json:"params"` // JSON
	Rendered        *string `json:"rendered"`
	Reason          string  `json:"reason"`
	RuleID          string  `json:"rule_id"`
	RuleName        string  `json:"rule_name"`
	Status          string  `json:"status"`
	DenialReason    string  `json:"denial_reason"`
	ExitCode        *int    `json:"exit_code"`
	Stdout          string  `json:"stdout"`
	Stderr          string  `json:"stderr"`
	Truncated       bool    `json:"output_truncated"`
	DurationMS      *int64  `json:"duration_ms"`
	RequestedAt     string  `json:"requested_at"`
	StartedAt       string  `json:"started_at"`
	FinishedAt      string  `json:"finished_at"`
	DecidedBy       string  `json:"decided_by"`
	DecidedByName   string  `json:"decided_by_name"`
	DecidedAt       string  `json:"decided_at"`
	DecisionComment string  `json:"decision_comment"`
	ClientIP        string  `json:"client_ip"`
	MCPSessionID    string  `json:"mcp_session_id"`
	UserAgent       string  `json:"user_agent"`
	Seq             int64   `json:"seq"`
}

const execCols = `id,key_id,key_name,actor_user_id,server_id,server_slug,command_id,command_slug,command_version,command_hash,risk,params,rendered,reason,rule_id,rule_name,status,denial_reason,exit_code,stdout,stderr,output_truncated,duration_ms,requested_at,coalesce(started_at,''),coalesce(finished_at,''),decided_by,decided_by_name,coalesce(decided_at,''),decision_comment,client_ip,mcp_session_id,user_agent,seq`

func scanExec(r scanner) (Execution, error) {
	var e Execution
	err := r.Scan(&e.ID, &e.KeyID, &e.KeyName, &e.ActorUserID, &e.ServerID, &e.ServerSlug, &e.CommandID, &e.CommandSlug, &e.CommandVersion, &e.CommandHash,
		&e.Risk, &e.Params, &e.Rendered, &e.Reason, &e.RuleID, &e.RuleName, &e.Status, &e.DenialReason, &e.ExitCode, &e.Stdout, &e.Stderr, &e.Truncated,
		&e.DurationMS, &e.RequestedAt, &e.StartedAt, &e.FinishedAt, &e.DecidedBy, &e.DecidedByName, &e.DecidedAt, &e.DecisionComment, &e.ClientIP, &e.MCPSessionID, &e.UserAgent, &e.Seq)
	return e, err
}

func chainHash(prev string, fields ...string) string {
	h := sha256.New()
	h.Write([]byte(prev))
	for _, f := range fields {
		fmt.Fprintf(h, "\x1f%d:%s", len(f), f)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// execChainFields: champs immuables de la demande couverts par la chaîne d'intégrité.
func execChainFields(e Execution) []string {
	return []string{e.ID, fmt.Sprint(e.Seq), e.KeyID, e.ActorUserID, e.ServerSlug, e.CommandSlug, e.Params, e.Reason, e.RequestedAt}
}

func InsertExecution(ctx context.Context, tx Q, e Execution) (Execution, error) {
	var seq sql.NullInt64
	var prev sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT seq,row_hash FROM executions ORDER BY seq DESC LIMIT 1`).Scan(&seq, &prev); err != nil && err != sql.ErrNoRows {
		return e, wrapErr("dernière exécution", err)
	}
	e.ID, e.Seq = NewID(), seq.Int64+1
	if e.RequestedAt == "" {
		e.RequestedAt = Now()
	}
	if e.Params == "" {
		e.Params = "{}"
	}
	hash := chainHash(prev.String, execChainFields(e)...)
	_, err := tx.ExecContext(ctx, `INSERT INTO executions (id,key_id,key_name,actor_user_id,server_id,server_slug,command_id,command_slug,command_version,command_hash,risk,params,rendered,reason,rule_id,rule_name,status,denial_reason,exit_code,stdout,stderr,output_truncated,duration_ms,requested_at,started_at,finished_at,decided_by,decided_by_name,decided_at,decision_comment,client_ip,mcp_session_id,user_agent,seq,prev_hash,row_hash) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.KeyID, e.KeyName, e.ActorUserID, e.ServerID, e.ServerSlug, e.CommandID, e.CommandSlug, e.CommandVersion, e.CommandHash,
		e.Risk, e.Params, e.Rendered, e.Reason, e.RuleID, e.RuleName, e.Status, e.DenialReason, e.ExitCode, e.Stdout, e.Stderr, e.Truncated,
		e.DurationMS, e.RequestedAt, nullStr(e.StartedAt), nullStr(e.FinishedAt), e.DecidedBy, e.DecidedByName, nullStr(e.DecidedAt), e.DecisionComment,
		e.ClientIP, e.MCPSessionID, e.UserAgent, e.Seq, prev.String, hash)
	return e, wrapErr("insertion exécution", err)
}

// SaveExecutionState persiste les champs mutables (statut, résultat, décision).
func SaveExecutionState(ctx context.Context, q Q, e Execution) error {
	_, err := q.ExecContext(ctx, `UPDATE executions SET rendered=?,rule_id=?,rule_name=?,status=?,denial_reason=?,exit_code=?,stdout=?,stderr=?,output_truncated=?,duration_ms=?,
		started_at=?,finished_at=?,decided_by=?,decided_by_name=?,decided_at=?,decision_comment=? WHERE id=?`,
		e.Rendered, e.RuleID, e.RuleName, e.Status, e.DenialReason, e.ExitCode, e.Stdout, e.Stderr, e.Truncated, e.DurationMS,
		nullStr(e.StartedAt), nullStr(e.FinishedAt), e.DecidedBy, e.DecidedByName, nullStr(e.DecidedAt), e.DecisionComment, e.ID)
	return wrapErr("mise à jour exécution", err)
}

// TransitionExecution fait passer atomiquement le statut de `from` à `to`; renvoie false si déjà changé.
func TransitionExecution(ctx context.Context, q Q, id, from, to string) (bool, error) {
	res, err := q.ExecContext(ctx, `UPDATE executions SET status=? WHERE id=? AND status=?`, to, id, from)
	if err != nil {
		return false, wrapErr("transition", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func ExecutionByID(ctx context.Context, q Q, id string) (Execution, error) {
	e, err := scanExec(q.QueryRowContext(ctx, `SELECT `+execCols+` FROM executions WHERE id=?`, id))
	return e, wrapErr("exécution", err)
}

type ExecFilter struct {
	Statuses      []string
	KeyID         string
	ServerSlug    string
	ServerGroupID string
	CommandSlug   string
	Risk          string
	DecidedBy     string // identifiant de l'utilisateur validateur
	Text          string // recherche dans la raison, la commande rendue et le slug
	From, To      string // bornes sur requested_at (RFC 3339 millisecondes, UTC)
	Limit         int
	Before        string // curseur: id ULID; renvoie les plus anciennes
	Oldest        bool   // tri croissant (écran Validations)
}

// escapeLike neutralise % _ \ pour une recherche littérale.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (f ExecFilter) where() (string, []any) {
	var w []string
	var a []any
	eq := func(col, v string) {
		if v != "" {
			w = append(w, col+"=?")
			a = append(a, v)
		}
	}
	if len(f.Statuses) > 0 {
		w = append(w, "status IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",")+")")
		for _, st := range f.Statuses {
			a = append(a, st)
		}
	}
	eq("key_id", f.KeyID)
	eq("server_slug", f.ServerSlug)
	eq("command_slug", f.CommandSlug)
	eq("risk", f.Risk)
	eq("decided_by", f.DecidedBy)
	if f.ServerGroupID != "" {
		w = append(w, "server_id IN (SELECT server_id FROM server_group_members WHERE group_id=?)")
		a = append(a, f.ServerGroupID)
	}
	if f.From != "" {
		w = append(w, "requested_at>=?")
		a = append(a, f.From)
	}
	if f.To != "" {
		w = append(w, "requested_at<=?")
		a = append(a, f.To)
	}
	if f.Text != "" {
		like := "%" + escapeLike(f.Text) + "%"
		w = append(w, `(reason LIKE ? ESCAPE '\' OR rendered LIKE ? ESCAPE '\' OR command_slug LIKE ? ESCAPE '\')`)
		a = append(a, like, like, like)
	}
	if f.Before != "" {
		if f.Oldest {
			w = append(w, "id>?")
		} else {
			w = append(w, "id<?")
		}
		a = append(a, f.Before)
	}
	if len(w) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(w, " AND "), a
}

func ListExecutions(ctx context.Context, q Q, f ExecFilter) ([]Execution, error) {
	if f.Limit <= 0 || f.Limit > 501 {
		f.Limit = 50
	}
	where, args := f.where()
	order := " ORDER BY id DESC"
	if f.Oldest {
		order = " ORDER BY id ASC"
	}
	rows, err := q.QueryContext(ctx, `SELECT `+execCols+` FROM executions`+where+order+fmt.Sprintf(" LIMIT %d", f.Limit), args...)
	if err != nil {
		return nil, wrapErr("liste exécutions", err)
	}
	return collect(rows, func(r *sql.Rows) (Execution, error) { return scanExec(r) })
}

// ExpirePending marque comme expirées les demandes en attente plus anciennes que cutoff; renvoie leurs identifiants.
func ExpirePending(ctx context.Context, q Q, cutoff string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `UPDATE executions SET status='expired',finished_at=? WHERE status='pending_approval' AND requested_at<? RETURNING id`, Now(), cutoff)
	if err != nil {
		return nil, wrapErr("expiration", err)
	}
	return collect(rows, func(r *sql.Rows) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
}

// InterruptRunning passe en erreur (raison « interrupted ») les exécutions « running » démarrées avant cutoff;
// renvoie leurs identifiants. Appelée au démarrage: aucune de ces exécutions n'a plus de processus pour la terminer.
func InterruptRunning(ctx context.Context, q Q, cutoff string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `UPDATE executions SET status='error',denial_reason='interrupted',stderr='leassh: exécution interrompue par un redémarrage',finished_at=?
		WHERE status='running' AND (started_at IS NULL OR started_at<?) RETURNING id`, Now(), cutoff)
	if err != nil {
		return nil, wrapErr("réconciliation", err)
	}
	return collect(rows, func(r *sql.Rows) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
}
