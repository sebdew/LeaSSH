package store

import (
	"context"
	"database/sql"
	"errors"
)

// AuditReport: ligne de la table audit_reports. Snapshot est le JSON des faits (immuable); Summary est facultatif et hors empreinte.
type AuditReport struct {
	ID            string `json:"id"`
	CreatedAt     string `json:"created_at"`
	CreatedBy     string `json:"created_by"`
	CreatedByName string `json:"created_by_name"`
	PeriodFrom    string `json:"period_from"`
	PeriodTo      string `json:"period_to"`
	LeashVersion  string `json:"leash_version"`
	SchemaVersion int    `json:"schema_version"`
	SHA256        string `json:"sha256"`
	HMAC          string `json:"-"`
	Snapshot      string `json:"-"`
	High          int    `json:"findings_high"`
	Medium        int    `json:"findings_medium"`
	Low           int    `json:"findings_low"`
	Summary       string `json:"-"`
	SummaryModel  string `json:"summary_model"`
	SummaryAt     string `json:"summary_at"`
}

const reportMeta = `id,created_at,created_by,created_by_name,period_from,period_to,leash_version,schema_version,sha256,hmac,findings_high,findings_medium,findings_low,summary_model,summary_at,summary<>''`

// AuditReportMeta: ligne d'historique (sans l'instantané).
type AuditReportMeta struct {
	AuditReport
	HasSummary bool `json:"has_summary"`
}

func scanReportMeta(r scanner) (AuditReportMeta, error) {
	var m AuditReportMeta
	err := r.Scan(&m.ID, &m.CreatedAt, &m.CreatedBy, &m.CreatedByName, &m.PeriodFrom, &m.PeriodTo, &m.LeashVersion, &m.SchemaVersion, &m.SHA256, &m.HMAC,
		&m.High, &m.Medium, &m.Low, &m.SummaryModel, &m.SummaryAt, &m.HasSummary)
	return m, err
}

func InsertAuditReport(ctx context.Context, tx Q, r AuditReport) (AuditReport, error) {
	r.ID, r.CreatedAt = NewID(), Now()
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_reports (id,created_at,created_by,created_by_name,period_from,period_to,leash_version,schema_version,sha256,hmac,snapshot,findings_high,findings_medium,findings_low)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.CreatedAt, r.CreatedBy, r.CreatedByName, r.PeriodFrom, r.PeriodTo, r.LeashVersion, r.SchemaVersion, r.SHA256, r.HMAC, r.Snapshot, r.High, r.Medium, r.Low)
	return r, wrapErr("création du rapport d'audit", err)
}

func ListAuditReports(ctx context.Context, q Q) ([]AuditReportMeta, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+reportMeta+` FROM audit_reports ORDER BY created_at DESC, id DESC LIMIT 500`)
	if err != nil {
		return nil, wrapErr("rapports d'audit", err)
	}
	return collect(rows, func(r *sql.Rows) (AuditReportMeta, error) { return scanReportMeta(r) })
}

func AuditReportByID(ctx context.Context, q Q, id string) (AuditReport, error) {
	var r AuditReport
	err := q.QueryRowContext(ctx, `SELECT id,created_at,created_by,created_by_name,period_from,period_to,leash_version,schema_version,sha256,hmac,snapshot,findings_high,findings_medium,findings_low,summary,summary_model,summary_at
		FROM audit_reports WHERE id=?`, id).Scan(&r.ID, &r.CreatedAt, &r.CreatedBy, &r.CreatedByName, &r.PeriodFrom, &r.PeriodTo, &r.LeashVersion, &r.SchemaVersion, &r.SHA256, &r.HMAC,
		&r.Snapshot, &r.High, &r.Medium, &r.Low, &r.Summary, &r.SummaryModel, &r.SummaryAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, wrapErr("rapport d'audit", err)
}

// SetAuditReportSummary enregistre la synthèse IA; l'instantané et son empreinte ne changent jamais.
func SetAuditReportSummary(ctx context.Context, tx Q, id, summary, model string) error {
	res, err := tx.ExecContext(ctx, `UPDATE audit_reports SET summary=?,summary_model=?,summary_at=? WHERE id=?`, summary, model, Now(), id)
	if err != nil {
		return wrapErr("synthèse du rapport", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Requêtes de faits pour le rapport (toutes en lecture, sur Q: utilisables dans une transaction) ----

// ChainInfo décrit une chaîne de hash: plage de séquences et tête.
type ChainInfo struct {
	Count    int    `json:"count"`
	MinSeq   int64  `json:"min_seq"`
	MaxSeq   int64  `json:"max_seq"`
	HeadHash string `json:"head_hash"`
	Oldest   string `json:"oldest"`
	Newest   string `json:"newest"`
}

func GetChainInfo(ctx context.Context, q Q, table string) (ChainInfo, error) {
	tsCol := "ts"
	if table == "executions" {
		tsCol = "requested_at"
	} else if table != "audit_events" {
		return ChainInfo{}, errors.New("table inconnue")
	}
	var ci ChainInfo
	var minS, maxS sql.NullInt64
	var oldest, newest sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT count(*),min(seq),max(seq),min(`+tsCol+`),max(`+tsCol+`) FROM `+table).Scan(&ci.Count, &minS, &maxS, &oldest, &newest); err != nil {
		return ci, wrapErr("chaîne", err)
	}
	ci.MinSeq, ci.MaxSeq, ci.Oldest, ci.Newest = minS.Int64, maxS.Int64, oldest.String, newest.String
	if maxS.Valid {
		if err := q.QueryRowContext(ctx, `SELECT row_hash FROM `+table+` WHERE seq=?`, maxS.Int64).Scan(&ci.HeadHash); err != nil {
			return ci, wrapErr("tête de chaîne", err)
		}
	}
	return ci, nil
}

type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type DestructiveExec struct {
	At          string `json:"at"`
	Key         string `json:"key"`
	Server      string `json:"server"`
	Command     string `json:"command"`
	Risk        string `json:"risk"`
	Status      string `json:"status"`
	Rendered    string `json:"rendered"`
	Reason      string `json:"reason"`
	DecidedBy   string `json:"decided_by"`
	ApprovalSec int64  `json:"approval_seconds"`
}

// PeriodActivity: agrégats d'exécutions sur [from, to] (chaînes d'horodatage normalisées).
type PeriodActivity struct {
	Requests         int
	ByStatus         []NameCount
	ByRisk           []NameCount
	DenialReasons    []NameCount
	ByKey            []NameCount
	FailedByCommand  []NameCount
	DistinctKeys     int
	DistinctServers  int
	Approved         int
	Rejected         int
	Expired          int
	AvgApprovalSec   int64
	MaxApprovalSec   int64
	Deciders         []NameCount
	WriteNoApproval  int // exécutions « write » menées à terme sans décision humaine
	Destructive      []DestructiveExec
	DestructiveTotal int
}

func nameCounts(ctx context.Context, q Q, query string, args ...any) ([]NameCount, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("agrégat", err)
	}
	out, err := collect(rows, func(r *sql.Rows) (NameCount, error) {
		var n NameCount
		return n, r.Scan(&n.Name, &n.Count)
	})
	if out == nil {
		out = []NameCount{}
	}
	return out, err
}

func GetPeriodActivity(ctx context.Context, q Q, from, to string) (PeriodActivity, error) {
	var a PeriodActivity
	var err error
	w := `requested_at>=? AND requested_at<=?`
	if err = q.QueryRowContext(ctx, `SELECT count(*), count(DISTINCT nullif(key_id,'')), count(DISTINCT nullif(server_id,'')) FROM executions WHERE `+w, from, to).Scan(&a.Requests, &a.DistinctKeys, &a.DistinctServers); err != nil {
		return a, wrapErr("activité", err)
	}
	if a.ByStatus, err = nameCounts(ctx, q, `SELECT status,count(*) FROM executions WHERE `+w+` GROUP BY status ORDER BY count(*) DESC, status`, from, to); err != nil {
		return a, err
	}
	if a.ByRisk, err = nameCounts(ctx, q, `SELECT risk,count(*) FROM executions WHERE `+w+` AND risk<>'' GROUP BY risk ORDER BY risk`, from, to); err != nil {
		return a, err
	}
	if a.DenialReasons, err = nameCounts(ctx, q, `SELECT substr(denial_reason,1,instr(denial_reason||':',':')-1),count(*) FROM executions WHERE `+w+` AND status='denied' AND denial_reason<>'' GROUP BY 1 ORDER BY count(*) DESC, 1`, from, to); err != nil {
		return a, err
	}
	if a.ByKey, err = nameCounts(ctx, q, `SELECT key_name,count(*) FROM executions WHERE `+w+` AND key_name<>'' GROUP BY key_name ORDER BY count(*) DESC, key_name LIMIT 50`, from, to); err != nil {
		return a, err
	}
	if a.FailedByCommand, err = nameCounts(ctx, q, `SELECT command_slug,count(*) FROM executions WHERE `+w+` AND status IN ('failed','timeout','error') AND command_slug<>'' GROUP BY command_slug ORDER BY count(*) DESC, command_slug LIMIT 10`, from, to); err != nil {
		return a, err
	}
	var avg *float64
	var max *float64
	if err = q.QueryRowContext(ctx, `SELECT sum(status<>'rejected'), sum(status='rejected'), avg((julianday(decided_at)-julianday(requested_at))*86400.0), max((julianday(decided_at)-julianday(requested_at))*86400.0)
		FROM executions WHERE `+w+` AND decided_by<>'' AND decided_at IS NOT NULL`, from, to).Scan(nullInt{&a.Approved}, nullInt{&a.Rejected}, &avg, &max); err != nil {
		return a, wrapErr("validations", err)
	}
	if avg != nil {
		a.AvgApprovalSec = int64(*avg + 0.5)
	}
	if max != nil {
		a.MaxApprovalSec = int64(*max + 0.5)
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM executions WHERE `+w+` AND status='expired'`, from, to).Scan(&a.Expired); err != nil {
		return a, wrapErr("validations expirées", err)
	}
	if a.Deciders, err = nameCounts(ctx, q, `SELECT decided_by_name,count(*) FROM executions WHERE `+w+` AND decided_by<>'' GROUP BY decided_by_name ORDER BY count(*) DESC, decided_by_name`, from, to); err != nil {
		return a, err
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM executions WHERE `+w+` AND risk='write' AND status IN ('succeeded','failed','timeout') AND decided_by=''`, from, to).Scan(&a.WriteNoApproval); err != nil {
		return a, wrapErr("écritures sans validation", err)
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM executions WHERE `+w+` AND risk='destructive'`, from, to).Scan(&a.DestructiveTotal); err != nil {
		return a, wrapErr("destructives", err)
	}
	rows, err := q.QueryContext(ctx, `SELECT requested_at,key_name,server_slug,command_slug,risk,status,coalesce(rendered,''),substr(reason,1,300),decided_by_name,
		coalesce(round((julianday(decided_at)-julianday(requested_at))*86400.0),0) FROM executions WHERE `+w+` AND risk='destructive' ORDER BY requested_at DESC, id DESC LIMIT 200`, from, to)
	if err != nil {
		return a, wrapErr("exécutions destructives", err)
	}
	a.Destructive, err = collect(rows, func(r *sql.Rows) (DestructiveExec, error) {
		var d DestructiveExec
		return d, r.Scan(&d.At, &d.Key, &d.Server, &d.Command, &d.Risk, &d.Status, &d.Rendered, &d.Reason, &d.DecidedBy, &d.ApprovalSec)
	})
	if a.Destructive == nil {
		a.Destructive = []DestructiveExec{}
	}
	return a, err
}

// nullInt lit un entier éventuellement NULL (somme sur aucune ligne).
type nullInt struct{ p *int }

func (n nullInt) Scan(v any) error {
	switch x := v.(type) {
	case nil:
		*n.p = 0
	case int64:
		*n.p = int(x)
	case float64:
		*n.p = int(x)
	default:
		return errors.New("entier attendu")
	}
	return nil
}

// AdminEvent: ligne du journal d'administration réduite à ce qu'un rapport montre (pas de diff).
type AdminEvent struct {
	TS         string `json:"ts"`
	ActorType  string `json:"actor_type"`
	ActorName  string `json:"actor_name"`
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	TargetName string `json:"target_name"`
	Seq        int64  `json:"seq"`
}

func ListAdminEventsBetween(ctx context.Context, q Q, from, to string, limit int) ([]AdminEvent, error) {
	rows, err := q.QueryContext(ctx, `SELECT ts,actor_type,actor_name,action,target_type,target_name,seq FROM audit_events WHERE ts>=? AND ts<=? ORDER BY seq LIMIT ?`, from, to, limit)
	if err != nil {
		return nil, wrapErr("journal d'administration", err)
	}
	out, err := collect(rows, func(r *sql.Rows) (AdminEvent, error) {
		var e AdminEvent
		return e, r.Scan(&e.TS, &e.ActorType, &e.ActorName, &e.Action, &e.TargetType, &e.TargetName, &e.Seq)
	})
	if out == nil {
		out = []AdminEvent{}
	}
	return out, err
}

func AdminActionCounts(ctx context.Context, q Q, from, to string) ([]NameCount, error) {
	return nameCounts(ctx, q, `SELECT action,count(*) FROM audit_events WHERE ts>=? AND ts<=? GROUP BY action ORDER BY action`, from, to)
}
