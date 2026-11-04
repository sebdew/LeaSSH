package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/auditreport"
	"github.com/leassh-dev/leassh/internal/store"
)

const (
	auditTagLabel   = "audit-report"
	maxReportDays   = 400
	maxVerifyUpload = 32 << 20
)

type reportInput struct {
	From string `json:"from"` // AAAA-MM-JJ (UTC), inclus
	To   string `json:"to"`
}

func parseDay(v string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(v))
	if err != nil {
		return t, bad("invalid_period", "dates attendues au format AAAA-MM-JJ")
	}
	return t, nil
}

func (s *Server) listAuditReports(w http.ResponseWriter, r *http.Request) {
	items, err := store.ListAuditReports(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	if items == nil {
		items = []store.AuditReportMeta{}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// createAuditReport calcule l'instantané, le signe et l'enregistre; l'événement d'audit est écrit dans la même transaction.
func (s *Server) createAuditReport(w http.ResponseWriter, r *http.Request) {
	var in reportInput
	if !decode(w, r, &in) {
		return
	}
	today := time.Now().UTC()
	to := today
	var err error
	if strings.TrimSpace(in.To) != "" {
		if to, err = parseDay(in.To); err != nil {
			s.fail(w, err)
			return
		}
	}
	from := to.AddDate(0, 0, -29)
	if strings.TrimSpace(in.From) != "" {
		if from, err = parseDay(in.From); err != nil {
			s.fail(w, err)
			return
		}
	}
	switch {
	case from.After(to):
		s.fail(w, bad("invalid_period", "la date de début doit précéder la date de fin"))
		return
	case to.After(today):
		s.fail(w, bad("invalid_period", "la date de fin ne peut pas être dans le futur"))
		return
	case to.Sub(from) > maxReportDays*24*time.Hour:
		s.fail(w, bad("invalid_period", fmt.Sprintf("période limitée à %d jours", maxReportDays)))
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		snap, err := auditreport.Build(ctx, tx, auditreport.Params{Now: time.Now(), From: from, To: to, LeasshVersion: s.version,
			RetentionDays: s.cfg.LogRetentionDays, ApprovalTTL: s.cfg.ApprovalTTL, SessionTTL: s.cfg.SessionTTL, BaseURL: s.cfg.BaseURL})
		if err != nil {
			return outcome{}, err
		}
		raw, err := auditreport.Marshal(snap)
		if err != nil {
			return outcome{}, err
		}
		h, m, l := auditreport.Counts(snap.Findings)
		rep := store.AuditReport{CreatedBy: u.ID, CreatedByName: u.Username, PeriodFrom: snap.Period.From, PeriodTo: snap.Period.To, LeasshVersion: s.version,
			SchemaVersion: snap.SchemaVersion, SHA256: auditreport.Digest(raw), Snapshot: string(raw), High: h, Medium: m, Low: l}
		if rep, err = store.InsertAuditReport(ctx, tx, rep); err != nil {
			return outcome{}, err
		}
		rep.HMAC = auditreport.Tag(s.box.Derive(auditTagLabel), rep.SHA256, rep.ID, rep.CreatedAt)
		if _, err = tx.ExecContext(ctx, `UPDATE audit_reports SET hmac=? WHERE id=?`, rep.HMAC, rep.ID); err != nil {
			return outcome{}, err
		}
		meta := store.AuditReportMeta{AuditReport: rep}
		return outcome{201, meta, &audit.Event{Action: "audit_report.generate", TargetType: "audit_report", TargetID: rep.ID, TargetName: rep.PeriodFrom + " → " + rep.PeriodTo,
			Diff: map[string]any{"after": map[string]any{"period_from": rep.PeriodFrom, "period_to": rep.PeriodTo, "sha256": rep.SHA256, "findings_high": h, "findings_medium": m, "findings_low": l,
				"integrity_ok": snap.Integrity.Executions.Verified.OK && snap.Integrity.Admin.Verified.OK}}}}, nil
	})
}

func (s *Server) loadReport(w http.ResponseWriter, r *http.Request) (store.AuditReport, bool) {
	rep, err := store.AuditReportByID(r.Context(), s.st.DB, id(r))
	if err != nil {
		s.fail(w, err)
		return rep, false
	}
	return rep, true
}

func (s *Server) getAuditReport(w http.ResponseWriter, r *http.Request) {
	rep, ok := s.loadReport(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"report": store.AuditReportMeta{AuditReport: rep, HasSummary: rep.Summary != ""},
		"snapshot": json.RawMessage(rep.Snapshot), "summary": rep.Summary})
}

func reportFileName(rep store.AuditReport, ext string) string {
	id := rep.ID
	if len(id) > 8 {
		id = id[len(id)-8:]
	}
	return fmt.Sprintf("leassh-audit-%s_%s-%s.%s", rep.PeriodFrom, rep.PeriodTo, strings.ToLower(id), ext)
}

func (s *Server) htmlAuditReport(w http.ResponseWriter, r *http.Request) {
	rep, ok := s.loadReport(w, r)
	if !ok {
		return
	}
	page, err := auditreport.RenderHTML(rep)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+reportFileName(rep, "html")+`"`)
	}
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}

func (s *Server) jsonAuditReport(w http.ResponseWriter, r *http.Request) {
	rep, ok := s.loadReport(w, r)
	if !ok {
		return
	}
	out, err := json.MarshalIndent(auditreport.NewEnvelope(rep), "", "  ")
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+reportFileName(rep, "json")+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// verifyAuditReport contrôle un fichier JSON exporté (corps brut de la requête).
func (s *Server) verifyAuditReport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxVerifyUpload))
	if err != nil {
		s.fail(w, bad("too_large", "fichier illisible ou trop volumineux"))
		return
	}
	res, err := auditreport.Verify(body, s.box.Derive(auditTagLabel))
	if err != nil {
		s.fail(w, bad("invalid_file", err.Error()))
		return
	}
	writeJSON(w, 200, res)
}

// summarizeAuditReport demande à l'IA configurée une synthèse rédigée à partir des seuls agrégats de l'instantané.
func (s *Server) summarizeAuditReport(w http.ResponseWriter, r *http.Request) {
	rep, ok := s.loadReport(w, r)
	if !ok {
		return
	}
	cfg, key, err := auditreport.LoadAIConfig(r.Context(), s.st.DB, s.box)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !cfg.Enabled {
		s.fail(w, &apiError{Status: 409, Code: "ai_disabled", Message: "la synthèse IA est désactivée: activez-la d'abord dans les réglages de l'audit"})
		return
	}
	var snap auditreport.Snapshot
	if err := json.Unmarshal([]byte(rep.Snapshot), &snap); err != nil {
		s.fail(w, err)
		return
	}
	lang := "fr"
	if wantsEnglish(r) {
		lang = "en"
	}
	var in struct {
		Lang string `json:"lang"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Lang == "en" || in.Lang == "fr" {
		lang = in.Lang
	}
	text, model, err := auditreport.Summarize(r.Context(), cfg, key, auditreport.BuildAIInput(snap), lang) // appel réseau hors transaction
	if err != nil {
		s.fail(w, &apiError{Status: 502, Code: "ai_error", Message: err.Error()})
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		if err := store.SetAuditReportSummary(ctx, tx, rep.ID, text, model); err != nil {
			return outcome{}, err
		}
		return outcome{200, map[string]any{"summary": text, "model": model}, &audit.Event{Action: "audit_report.summary", TargetType: "audit_report", TargetID: rep.ID, TargetName: rep.PeriodFrom + " → " + rep.PeriodTo,
			Diff: map[string]any{"after": map[string]any{"model": model, "ai_endpoint": cfg.URL, "had_summary": rep.Summary != ""}}}}, nil
	})
}

func (s *Server) getAuditAI(w http.ResponseWriter, r *http.Request) {
	cfg, _, err := auditreport.LoadAIConfig(r.Context(), s.st.DB, s.box)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"config": cfg})
}

type auditAIInput struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
}

func (s *Server) putAuditAI(w http.ResponseWriter, r *http.Request) {
	var in auditAIInput
	if !decode(w, r, &in) {
		return
	}
	in.URL, in.Model, in.APIKey = strings.TrimSpace(in.URL), strings.TrimSpace(in.Model), strings.TrimSpace(in.APIKey)
	if in.URL != "" {
		if err := auditreport.ValidateAIURL(in.URL); err != nil {
			s.fail(w, bad("invalid_url", err.Error()))
			return
		}
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, _, err := auditreport.LoadAIConfig(ctx, tx, s.box)
		if err != nil {
			return outcome{}, err
		}
		if in.Enabled && (in.URL == "" || in.Model == "") {
			return outcome{}, bad("incomplete", "une URL et un modèle sont requis pour activer la synthèse IA")
		}
		cfg := auditreport.AIConfig{Enabled: in.Enabled, URL: in.URL, Model: in.Model}
		if err := auditreport.SaveAIConfig(ctx, tx, s.box, cfg, in.APIKey); err != nil {
			return outcome{}, err
		}
		cfg.HasKey = old.HasKey || in.APIKey != ""
		return outcome{200, map[string]any{"config": cfg}, &audit.Event{Action: "audit_ai.update", TargetType: "audit_ai", TargetName: in.URL,
			Diff: map[string]any{"before": map[string]any{"enabled": old.Enabled, "url": old.URL, "model": old.Model},
				"after": map[string]any{"enabled": in.Enabled, "url": in.URL, "model": in.Model, "api_key_changed": in.APIKey != ""}}}}, nil
	})
}
