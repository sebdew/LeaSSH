package store

import (
	"context"
	"time"
)

// DashboardStats: chiffres de la page d'accueil, calculés en quelques requêtes indexées.
type DashboardStats struct {
	Executions24h    map[string]int `json:"executions_24h"` // succeeded / failed / denied (dont rejected) / pending / other
	ActiveAgents24h  int            `json:"active_agents_24h"`
	PendingApprovals int            `json:"pending_approvals"`
	PendingProposals int            `json:"pending_proposals"`
	Counts           map[string]int `json:"counts"` // identities, servers, pinned_servers, commands, groups, rules, keys, used_keys
	Anomalies        []Anomaly      `json:"servers_in_anomaly"`
}

type Anomaly struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Reason string `json:"reason"` // host_key_mismatch | host_key_unknown | ssh_unreachable
}

func scalar(ctx context.Context, q Q, query string, args ...any) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, wrapErr("statistiques", err)
}

func Dashboard(ctx context.Context, q Q, now time.Time) (DashboardStats, error) {
	st := DashboardStats{Executions24h: map[string]int{"succeeded": 0, "failed": 0, "denied": 0, "pending": 0, "other": 0}, Counts: map[string]int{}, Anomalies: []Anomaly{}}
	since := FormatTime(now.Add(-24 * time.Hour))

	rows, err := q.QueryContext(ctx, `SELECT status,count(*) FROM executions WHERE requested_at>=? GROUP BY status`, since)
	if err != nil {
		return st, wrapErr("statistiques", err)
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			return st, err
		}
		switch s {
		case "succeeded", "failed", "denied":
			st.Executions24h[s] += n
		case "rejected": // refusée par l'approbateur: un refus comme les autres
			st.Executions24h["denied"] += n
		case "pending_approval":
			st.Executions24h["pending"] += n
		default:
			st.Executions24h["other"] += n
		}
	}
	rows.Close()

	if st.ActiveAgents24h, err = scalar(ctx, q, `SELECT count(DISTINCT key_id) FROM executions WHERE requested_at>=? AND key_id<>''`, since); err != nil {
		return st, err
	}

	for _, c := range []struct {
		key   string
		query string
	}{
		{"identities", `SELECT count(*) FROM identities`},
		{"servers", `SELECT count(*) FROM servers`},
		{"pinned_servers", `SELECT count(*) FROM servers WHERE host_key_status='pinned'`},
		{"commands", `SELECT count(*) FROM commands`},
		{"groups", `SELECT (SELECT count(*) FROM server_groups)+(SELECT count(*) FROM command_groups)`},
		{"rules", `SELECT count(*) FROM access_rules`},
		{"keys", `SELECT count(*) FROM mcp_keys`},
		{"keys_with_rules", `SELECT count(DISTINCT key_id) FROM access_rule_keys`},
		{"used_keys", `SELECT count(*) FROM mcp_keys WHERE last_used_at IS NOT NULL`},
	} {
		if st.Counts[c.key], err = scalar(ctx, q, c.query); err != nil {
			return st, err
		}
	}
	if st.PendingApprovals, err = scalar(ctx, q, `SELECT count(*) FROM executions WHERE status='pending_approval'`); err != nil {
		return st, err
	}
	if st.PendingProposals, err = scalar(ctx, q, `SELECT count(*) FROM proposals WHERE status='pending'`); err != nil {
		return st, err
	}

	// serveurs en anomalie: clé d'hôte non validée, ou dernier échec SSH (24 h) plus récent que le dernier succès
	arows, err := q.QueryContext(ctx, `SELECT s.slug,s.name,
		CASE WHEN s.host_key_status='mismatch' THEN 'host_key_mismatch'
		     WHEN s.host_key_status='unknown' THEN 'host_key_unknown'
		     ELSE 'ssh_unreachable' END
		FROM servers s WHERE s.enabled=1 AND (s.host_key_status<>'pinned' OR
			(s.last_ssh_error_at<>'' AND s.last_ssh_error_at>=? AND s.last_ssh_error_at>s.last_ssh_ok_at))
		ORDER BY s.slug`, since)
	if err != nil {
		return st, wrapErr("anomalies", err)
	}
	defer arows.Close()
	for arows.Next() {
		var a Anomaly
		if err := arows.Scan(&a.Slug, &a.Name, &a.Reason); err != nil {
			return st, err
		}
		st.Anomalies = append(st.Anomalies, a)
	}
	return st, arows.Err()
}
