// Package audit: journal d'administration avec chaîne d'intégrité (SPEC §10).
package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/leash-dev/leash/internal/store"
)

type Event struct {
	ActorType  string // user | key | system
	ActorID    string
	ActorName  string
	Action     string
	TargetType string
	TargetID   string
	TargetName string
	Diff       any
	IP         string
}

var secretKey = regexp.MustCompile(`(?i)(password|passwd|secret|private_key|token|api_key|hash)`)

// Mask remplace récursivement les valeurs des clés sensibles (invariant 7).
func Mask(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if secretKey.MatchString(k) {
				out[k] = "***"
			} else {
				out[k] = Mask(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = Mask(val)
		}
		return out
	}
	return v
}

// normalize passe par JSON pour que Mask voie des map[string]any (structs comprises).
func normalize(v any) any {
	if v == nil {
		return map[string]any{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"_error": "diff non sérialisable"}
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return Mask(out)
}

func rowHash(prev string, fields ...string) string {
	h := sha256.New()
	h.Write([]byte(prev))
	for _, f := range fields {
		fmt.Fprintf(h, "\x1f%d:%s", len(f), f)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func eventFields(id, ts string, seq int64, e Event, diff string) []string {
	return []string{id, ts, fmt.Sprint(seq), e.ActorType, e.ActorID, e.ActorName, e.Action,
		e.TargetType, e.TargetID, e.TargetName, diff, e.IP}
}

// Record écrit l'événement dans la transaction de l'appelant (la mutation et son audit sont atomiques).
func Record(ctx context.Context, tx *sql.Tx, e Event) error {
	var seq sql.NullInt64
	var prev sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT seq, row_hash FROM audit_events ORDER BY seq DESC LIMIT 1`).Scan(&seq, &prev)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("dernier audit: %w", err)
	}
	next := seq.Int64 + 1
	diff, _ := json.Marshal(normalize(e.Diff))
	id, ts := store.NewID(), store.Now()
	hash := rowHash(prev.String, eventFields(id, ts, next, e, string(diff))...)
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events
		(id,ts,actor_type,actor_id,actor_name,action,target_type,target_id,target_name,diff,ip,seq,prev_hash,row_hash)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, ts, e.ActorType, e.ActorID, e.ActorName, e.Action, e.TargetType, e.TargetID, e.TargetName, string(diff), e.IP, next, prev.String, hash)
	if err != nil {
		return fmt.Errorf("insertion audit: %w", err)
	}
	return nil
}

// RecordStandalone écrit un événement dans sa propre transaction.
func RecordStandalone(ctx context.Context, s *store.Store, e Event) error {
	return s.Tx(ctx, func(tx *sql.Tx) error { return Record(ctx, tx, e) })
}

// Verify contrôle la chaîne d'intégrité du journal d'administration.
func Verify(ctx context.Context, q store.Q) (store.ChainResult, error) {
	return store.VerifyAudit(ctx, q, func(id, ts string, seq int64, at, aid, an, act, tt, tid, tn, diff, ip string) []string {
		return eventFields(id, ts, seq, Event{ActorType: at, ActorID: aid, ActorName: an, Action: act, TargetType: tt, TargetID: tid, TargetName: tn, IP: ip}, diff)
	})
}
