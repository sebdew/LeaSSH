package store

import "context"

// MetricsSnapshot: états agrégés exposés sur /metrics (ARC-03: le SQL reste dans store).
type MetricsSnapshot struct {
	PendingApprovals int
	PendingProposals int
	ServersByHostKey map[string]int // host_key_status -> nombre
	KeysByState      map[string]int // active | disabled | expired -> nombre
}

func MetricsState(ctx context.Context, q Q) (MetricsSnapshot, error) {
	m := MetricsSnapshot{}
	var err error
	if m.PendingApprovals, err = scalar(ctx, q, `SELECT count(*) FROM executions WHERE status='pending_approval'`); err != nil {
		return m, err
	}
	if m.PendingProposals, err = scalar(ctx, q, `SELECT count(*) FROM proposals WHERE status='pending'`); err != nil {
		return m, err
	}
	if m.ServersByHostKey, err = countsBy(ctx, q, `SELECT host_key_status,count(*) FROM servers GROUP BY host_key_status`); err != nil {
		return m, err
	}
	m.KeysByState, err = countsBy(ctx, q, `SELECT CASE WHEN disabled=1 THEN 'disabled' WHEN expires_at IS NOT NULL AND expires_at<strftime('%Y-%m-%dT%H:%M:%fZ','now') THEN 'expired' ELSE 'active' END,count(*) FROM mcp_keys GROUP BY 1`)
	return m, err
}

func countsBy(ctx context.Context, q Q, query string) (map[string]int, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, wrapErr("métriques", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, wrapErr("métriques", err)
		}
		out[k] = n
	}
	return out, wrapErr("métriques", rows.Err())
}
