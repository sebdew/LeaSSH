package store

import (
	"context"
	"sort"
	"time"
)

// HourBucket: activité d'une heure (UTC), par issue. Le client agrège en jours et en heatmap dans son fuseau.
type HourBucket struct {
	Hour   string `json:"hour"` // 2026-11-02T14
	OK     int    `json:"ok"`
	KO     int    `json:"ko"`
	Denied int    `json:"denied"`
	Other  int    `json:"other"`
}

type TopItem struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Failed  int    `json:"failed"`
	AvgMs   int64  `json:"avg_ms"`
	Servers int    `json:"servers,omitempty"` // serveurs distincts (commandes) ou commandes distinctes (clés)
}

// Insights: statistiques d'usage sur une période, pour le tableau de bord.
type Insights struct {
	Days           int            `json:"days"`
	Requests       int            `json:"requests"`
	Succeeded      int            `json:"succeeded"`
	Failed         int            `json:"failed"` // failed + timeout + error
	Denied         int            `json:"denied"`
	Executed       int            `json:"executed"` // exécutions réellement lancées (durée connue)
	AvgMs          int64          `json:"avg_ms"`
	P50Ms          int64          `json:"p50_ms"`
	P95Ms          int64          `json:"p95_ms"`
	MaxMs          int64          `json:"max_ms"`
	Approvals      int            `json:"approvals"`            // demandes tranchées par un humain
	AvgApprovalSec int64          `json:"avg_approval_seconds"` // délai moyen de décision
	Hourly         []HourBucket   `json:"hourly"`
	TopCommands    []TopItem      `json:"top_commands"`
	TopServers     []TopItem      `json:"top_servers"`
	TopKeys        []TopItem      `json:"top_keys"`
	Slowest        []TopItem      `json:"slowest_commands"`
	DenialReasons  []TopItem      `json:"denial_reasons"`
	Risk           map[string]int `json:"risk"`
}

const execOutcome = `CASE WHEN status='succeeded' THEN 'ok' WHEN status IN ('failed','timeout','error') THEN 'ko' WHEN status='denied' THEN 'denied' ELSE 'other' END`

func top(ctx context.Context, q Q, query string, args ...any) ([]TopItem, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("classement", err)
	}
	defer rows.Close()
	out := []TopItem{}
	for rows.Next() {
		var t TopItem
		var avg *float64
		if err := rows.Scan(&t.Name, &t.Count, &t.Failed, &avg, &t.Servers); err != nil {
			return nil, err
		}
		if avg != nil {
			t.AvgMs = int64(*avg + 0.5)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetInsights calcule les statistiques des `days` derniers jours (index sur requested_at).
func GetInsights(ctx context.Context, q Q, now time.Time, days int) (Insights, error) {
	in := Insights{Days: days, Hourly: []HourBucket{}, Risk: map[string]int{"read": 0, "write": 0, "destructive": 0}}
	since := FormatTime(now.AddDate(0, 0, -days))

	rows, err := q.QueryContext(ctx, `SELECT substr(requested_at,1,13), `+execOutcome+`, count(*) FROM executions WHERE requested_at>=? GROUP BY 1,2 ORDER BY 1`, since)
	if err != nil {
		return in, wrapErr("activité", err)
	}
	pos := map[string]int{}
	for rows.Next() {
		var h, o string
		var n int
		if err := rows.Scan(&h, &o, &n); err != nil {
			rows.Close()
			return in, err
		}
		i, ok := pos[h]
		if !ok {
			i = len(in.Hourly)
			pos[h] = i
			in.Hourly = append(in.Hourly, HourBucket{Hour: h})
		}
		b := &in.Hourly[i]
		switch o {
		case "ok":
			b.OK, in.Succeeded = b.OK+n, in.Succeeded+n
		case "ko":
			b.KO, in.Failed = b.KO+n, in.Failed+n
		case "denied":
			b.Denied, in.Denied = b.Denied+n, in.Denied+n
		default:
			b.Other += n
		}
		in.Requests += n
	}
	rows.Close()

	// durées des exécutions réellement lancées
	drows, err := q.QueryContext(ctx, `SELECT duration_ms FROM executions WHERE requested_at>=? AND duration_ms IS NOT NULL AND status IN ('succeeded','failed','timeout') ORDER BY duration_ms LIMIT 500000`, since)
	if err != nil {
		return in, wrapErr("durées", err)
	}
	var ds []int64
	var sum int64
	for drows.Next() {
		var d int64
		if err := drows.Scan(&d); err != nil {
			drows.Close()
			return in, err
		}
		ds = append(ds, d)
		sum += d
	}
	drows.Close()
	if n := len(ds); n > 0 {
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		in.Executed, in.AvgMs, in.P50Ms, in.MaxMs = n, sum/int64(n), ds[n*50/100], ds[n-1]
		in.P95Ms = ds[min(n-1, n*95/100)]
	}

	var avgApproval *float64
	if err := q.QueryRowContext(ctx, `SELECT count(*), avg((julianday(decided_at)-julianday(requested_at))*86400.0) FROM executions
		WHERE requested_at>=? AND decided_by<>'' AND decided_at IS NOT NULL`, since).Scan(&in.Approvals, &avgApproval); err != nil {
		return in, wrapErr("validations", err)
	}
	if avgApproval != nil {
		in.AvgApprovalSec = int64(*avgApproval + 0.5)
	}

	if in.TopCommands, err = top(ctx, q, `SELECT command_slug,count(*),sum(status IN ('failed','timeout','error')),avg(duration_ms),count(DISTINCT server_slug)
		FROM executions WHERE requested_at>=? AND command_slug<>'' GROUP BY command_slug ORDER BY count(*) DESC, command_slug LIMIT 10`, since); err != nil {
		return in, err
	}
	if in.TopServers, err = top(ctx, q, `SELECT server_slug,count(*),sum(status IN ('failed','timeout','error')),avg(duration_ms),count(DISTINCT command_slug)
		FROM executions WHERE requested_at>=? AND server_slug<>'' GROUP BY server_slug ORDER BY count(*) DESC, server_slug LIMIT 10`, since); err != nil {
		return in, err
	}
	if in.TopKeys, err = top(ctx, q, `SELECT key_name,count(*),sum(status IN ('failed','timeout','error')),avg(duration_ms),count(DISTINCT command_slug)
		FROM executions WHERE requested_at>=? AND key_name<>'' GROUP BY key_name ORDER BY count(*) DESC, key_name LIMIT 10`, since); err != nil {
		return in, err
	}
	if in.Slowest, err = top(ctx, q, `SELECT command_slug,count(*),sum(status IN ('failed','timeout','error')),avg(duration_ms),count(DISTINCT server_slug)
		FROM executions WHERE requested_at>=? AND command_slug<>'' AND duration_ms IS NOT NULL AND status IN ('succeeded','failed','timeout')
		GROUP BY command_slug HAVING count(*)>=3 ORDER BY avg(duration_ms) DESC LIMIT 5`, since); err != nil {
		return in, err
	}
	if in.DenialReasons, err = top(ctx, q, `SELECT substr(denial_reason,1,instr(denial_reason||':',':')-1),count(*),0,NULL,0
		FROM executions WHERE requested_at>=? AND status='denied' AND denial_reason<>'' GROUP BY 1 ORDER BY count(*) DESC LIMIT 5`, since); err != nil {
		return in, err
	}
	rrows, err := q.QueryContext(ctx, `SELECT risk,count(*) FROM executions WHERE requested_at>=? AND risk IN ('read','write','destructive') AND status NOT IN ('denied') GROUP BY risk`, since)
	if err != nil {
		return in, wrapErr("risques", err)
	}
	defer rrows.Close()
	for rrows.Next() {
		var r string
		var n int
		if err := rrows.Scan(&r, &n); err != nil {
			return in, err
		}
		in.Risk[r] = n
	}
	return in, rrows.Err()
}
