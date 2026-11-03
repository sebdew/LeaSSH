package store

import (
	"context"
	"database/sql"
)

// SyslogConfig: configuration de l'export syslog (table à ligne unique, SPEC §11bis).
type SyslogConfig struct {
	Enabled          bool
	Protocol         string // udp | tcp | tls
	Host             string
	Port             int
	MessageFormat    string // text | json | cef
	Framing          string // lf | octet (TCP et TLS)
	Facility         string
	AppName          string
	Hostname         string
	MinSeverity      int    // 0 (emerg) … 7 (debug)
	Types            string // liste séparée par des virgules: execution,proposal,audit
	TLSCAPEM         string
	TLSServerName    string
	TLSSkipVerify    bool
	TLSClientCertPEM string
	TLSClientKeyEnc  []byte
	AuditCursor      sql.NullInt64 // dernier seq d'audit livré; NULL = pas encore initialisé
}

func GetSyslogConfig(ctx context.Context, q Q) (SyslogConfig, error) {
	var c SyslogConfig
	err := q.QueryRowContext(ctx, `SELECT enabled,protocol,host,port,message_format,framing,facility,app_name,hostname,min_severity,types,
		tls_ca_pem,tls_server_name,tls_skip_verify,tls_client_cert_pem,tls_client_key_enc,audit_cursor FROM syslog_config WHERE id=1`).
		Scan(&c.Enabled, &c.Protocol, &c.Host, &c.Port, &c.MessageFormat, &c.Framing, &c.Facility, &c.AppName, &c.Hostname, &c.MinSeverity, &c.Types,
			&c.TLSCAPEM, &c.TLSServerName, &c.TLSSkipVerify, &c.TLSClientCertPEM, &c.TLSClientKeyEnc, &c.AuditCursor)
	return c, wrapErr("configuration syslog", err)
}

func SaveSyslogConfig(ctx context.Context, q Q, c SyslogConfig) error {
	_, err := q.ExecContext(ctx, `UPDATE syslog_config SET enabled=?,protocol=?,host=?,port=?,message_format=?,framing=?,facility=?,app_name=?,hostname=?,
		min_severity=?,types=?,tls_ca_pem=?,tls_server_name=?,tls_skip_verify=?,tls_client_cert_pem=?,tls_client_key_enc=?,audit_cursor=? WHERE id=1`,
		c.Enabled, c.Protocol, c.Host, c.Port, c.MessageFormat, c.Framing, c.Facility, c.AppName, c.Hostname,
		c.MinSeverity, c.Types, c.TLSCAPEM, c.TLSServerName, c.TLSSkipVerify, c.TLSClientCertPEM, c.TLSClientKeyEnc, c.AuditCursor)
	return wrapErr("configuration syslog", err)
}

// SetSyslogAuditCursor avance le curseur de livraison des événements d'audit (jamais en arrière).
func SetSyslogAuditCursor(ctx context.Context, q Q, seq int64) error {
	_, err := q.ExecContext(ctx, `UPDATE syslog_config SET audit_cursor=? WHERE id=1 AND (audit_cursor IS NULL OR audit_cursor<?)`, seq, seq)
	return wrapErr("curseur syslog", err)
}

// MaxAuditSeq renvoie le dernier numéro de séquence du journal d'administration (0 si vide).
func MaxAuditSeq(ctx context.Context, q Q) (int64, error) {
	var n sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT max(seq) FROM audit_events`).Scan(&n)
	return n.Int64, wrapErr("dernier audit", err)
}

// AuditAfter liste les événements d'audit de seq > after, du plus ancien au plus récent.
func AuditAfter(ctx context.Context, q Q, after int64, limit int) ([]AuditEvent, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,ts,actor_type,actor_id,actor_name,action,target_type,target_id,target_name,diff,ip,seq
		FROM audit_events WHERE seq>? ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, wrapErr("audit", err)
	}
	return collect(rows, func(r *sql.Rows) (AuditEvent, error) {
		var e AuditEvent
		var diff string
		err := r.Scan(&e.ID, &e.TS, &e.ActorType, &e.ActorID, &e.ActorName, &e.Action, &e.TargetType, &e.TargetID, &e.TargetName, &diff, &e.IP, &e.Seq)
		e.Diff = []byte(diff)
		return e, err
	})
}
