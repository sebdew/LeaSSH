package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/store"
)

var (
	validStatuses = map[string]bool{"denied": true, "pending_approval": true, "rejected": true, "expired": true, "running": true, "succeeded": true, "failed": true, "timeout": true, "error": true}
	validRisks    = map[string]bool{"read": true, "write": true, "destructive": true}
	periods       = map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}
)

func parseTime(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return "", bad("invalid_time", "date invalide (RFC 3339 attendu): "+v)
	}
	return store.FormatTime(t), nil
}

// period: raccourcis 1h / 24h / 7d / 30d, sinon bornes from/to explicites.
func parsePeriod(q url.Values) (from, to string, err error) {
	if p := q.Get("period"); p != "" {
		d, ok := periods[p]
		if !ok {
			return "", "", bad("invalid_period", "période inconnue (1h, 24h, 7d, 30d)")
		}
		return store.FormatTime(time.Now().Add(-d)), "", nil
	}
	if from, err = parseTime(q.Get("from")); err != nil {
		return
	}
	to, err = parseTime(q.Get("to"))
	return
}

func execFilter(q url.Values) (store.ExecFilter, error) {
	f := store.ExecFilter{KeyID: q.Get("key_id"), ServerSlug: q.Get("server"), ServerGroupID: q.Get("server_group"), CommandSlug: q.Get("command"),
		Risk: q.Get("risk"), DecidedBy: q.Get("decided_by"), Text: strings.TrimSpace(q.Get("q")), Before: q.Get("cursor")}
	if f.Risk != "" && !validRisks[f.Risk] {
		return f, bad("invalid_risk", "risque invalide")
	}
	for _, st := range strings.Split(q.Get("status"), ",") {
		if st == "" {
			continue
		}
		if !validStatuses[st] {
			return f, bad("invalid_status", "statut invalide: "+st)
		}
		f.Statuses = append(f.Statuses, st)
	}
	var err error
	if f.From, f.To, err = parsePeriod(q); err != nil {
		return f, err
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Oldest = len(f.Statuses) == 1 && f.Statuses[0] == "pending_approval" // écran Validations: la plus ancienne d'abord
	return f, nil
}

func (s *Server) listExecutions(w http.ResponseWriter, r *http.Request) {
	f, err := execFilter(r.URL.Query())
	if err != nil {
		s.fail(w, err)
		return
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	want := f.Limit
	f.Limit++ // une ligne de plus pour savoir s'il existe une page suivante
	items, err := store.ListExecutions(r.Context(), s.st.DB, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	next := ""
	if len(items) > want {
		items = items[:want]
		next = items[want-1].ID
	}
	// la liste ne transporte pas les sorties volumineuses: elles sont dans GET /executions/{id}
	for i := range items {
		items[i].Stdout, items[i].Stderr = "", ""
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
}

// streamExecutions: SSE du journal en direct. Chaque événement est un résumé (sans stdout/stderr).
func (s *Server) streamExecutions(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "no_stream", "streaming indisponible")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	ch, cancel := s.exec.Broker.Subscribe()
	defer cancel()
	fmt.Fprint(w, "retry: 3000\n\n")
	fl.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case id := <-ch:
			e, err := store.ExecutionByID(r.Context(), s.st.DB, id)
			if err != nil {
				continue
			}
			e.Stdout, e.Stderr = "", ""
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: execution\ndata: %s\n\n", b)
		}
		fl.Flush()
	}
}

// csvSafe neutralise l'injection de formules tableur (= + - @ tab CR en tête de cellule).
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func exportHeaders(w http.ResponseWriter, kind, format string) bool {
	name := fmt.Sprintf("leassh-%s-%s", kind, time.Now().UTC().Format("20060102-150405"))
	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
	case "ndjson", "json":
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.ndjson"`)
	default:
		return false
	}
	return true
}

const exportPage = 500

func (s *Server) exportExecutions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := execFilter(q)
	if err != nil {
		s.fail(w, err)
		return
	}
	format := q.Get("format")
	if !exportHeaders(w, "executions", format) {
		s.fail(w, bad("invalid_format", "format: csv ou ndjson"))
		return
	}
	f.Before, f.Limit = "", exportPage
	cw := csv.NewWriter(w)
	enc := json.NewEncoder(w)
	if format == "csv" {
		_ = cw.Write([]string{"id", "requested_at", "agent", "validator", "server", "command", "risk", "status", "exit_code", "duration_ms", "rule", "decided_at", "decision_comment", "reason", "rendered", "params", "denial_reason", "client_ip"})
	}
	for {
		page, err := store.ListExecutions(r.Context(), s.st.DB, f)
		if err != nil || r.Context().Err() != nil {
			return
		}
		for _, e := range page {
			if format == "csv" {
				code, dur, rendered := "", "", ""
				if e.ExitCode != nil {
					code = strconv.Itoa(*e.ExitCode)
				}
				if e.DurationMS != nil {
					dur = strconv.FormatInt(*e.DurationMS, 10)
				}
				if e.Rendered != nil {
					rendered = *e.Rendered
				}
				agent := e.KeyName
				if agent == "" {
					agent = "user:" + e.ActorUserID
				}
				row := []string{e.ID, e.RequestedAt, agent, e.DecidedByName, e.ServerSlug, e.CommandSlug, e.Risk, e.Status, code, dur, e.RuleName, e.DecidedAt, e.DecisionComment, e.Reason, rendered, e.Params, e.DenialReason, e.ClientIP}
				for i := range row {
					row[i] = csvSafe(row[i])
				}
				_ = cw.Write(row)
			} else {
				_ = enc.Encode(e)
			}
		}
		if format == "csv" {
			cw.Flush()
		}
		if f_, ok := w.(http.Flusher); ok {
			f_.Flush()
		}
		if len(page) < exportPage {
			return
		}
		f.Before = page[len(page)-1].ID
	}
}

// ---- journal d'administration ----

func auditFilter(q url.Values) (store.AuditFilter, error) {
	f := store.AuditFilter{ActorType: q.Get("actor_type"), ActorID: q.Get("actor_id"), Action: q.Get("action"), TargetType: q.Get("target_type"),
		TargetID: q.Get("target_id"), Text: strings.TrimSpace(q.Get("q")), Before: q.Get("cursor")}
	var err error
	if f.From, f.To, err = parsePeriod(q); err != nil {
		return f, err
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	return f, nil
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	f, err := auditFilter(r.URL.Query())
	if err != nil {
		s.fail(w, err)
		return
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	want := f.Limit
	f.Limit++
	items, err := store.ListAudit(r.Context(), s.st.DB, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	next := ""
	if len(items) > want {
		items = items[:want]
		next = items[want-1].ID
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
}

func (s *Server) exportAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := auditFilter(q)
	if err != nil {
		s.fail(w, err)
		return
	}
	format := q.Get("format")
	if !exportHeaders(w, "audit", format) {
		s.fail(w, bad("invalid_format", "format: csv ou ndjson"))
		return
	}
	f.Before, f.Limit = "", exportPage
	cw := csv.NewWriter(w)
	enc := json.NewEncoder(w)
	if format == "csv" {
		_ = cw.Write([]string{"id", "ts", "actor_type", "actor", "action", "target_type", "target", "ip", "diff"})
	}
	for {
		page, err := store.ListAudit(r.Context(), s.st.DB, f)
		if err != nil || r.Context().Err() != nil {
			return
		}
		for _, e := range page {
			if format == "csv" {
				row := []string{e.ID, e.TS, e.ActorType, e.ActorName, e.Action, e.TargetType, e.TargetName, e.IP, string(e.Diff)}
				for i := range row {
					row[i] = csvSafe(row[i])
				}
				_ = cw.Write(row)
			} else {
				_ = enc.Encode(e)
			}
		}
		cw.Flush()
		if len(page) < exportPage {
			return
		}
		f.Before = page[len(page)-1].ID
	}
}

// verifyIntegrity: bouton « Vérifier l'intégrité » (Réglages → Journal).
func (s *Server) verifyIntegrity(w http.ResponseWriter, r *http.Request) {
	ex, err := store.VerifyExecutions(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	au, err := audit.Verify(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": ex.OK && au.OK, "executions": ex, "audit": au})
}
