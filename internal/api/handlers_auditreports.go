package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/leassh-dev/leassh/internal/auditreport"
	"github.com/leassh-dev/leassh/internal/store"
)

type reportInput struct {
	From string `json:"from"` // AAAA-MM-JJ (UTC), inclus
	To   string `json:"to"`
}

// reportErr traduit les erreurs métier des rapports d'audit en erreurs d'API.
func reportErr(err error) error {
	var (
		pe *auditreport.PeriodError
		iu *auditreport.InvalidURLError
		fe *auditreport.InvalidFileError
		ae *auditreport.AIError
	)
	switch {
	case errors.As(err, &pe):
		return bad("invalid_period", pe.Msg)
	case errors.As(err, &iu):
		return bad("invalid_url", iu.Msg)
	case errors.As(err, &fe):
		return bad("invalid_file", fe.Msg)
	case errors.Is(err, auditreport.ErrAIIncomplete):
		return bad("incomplete", err.Error())
	case errors.Is(err, auditreport.ErrAIDisabled):
		return &apiError{Status: 409, Code: "ai_disabled", Message: err.Error()}
	case errors.As(err, &ae):
		return &apiError{Status: 502, Code: "ai_error", Message: ae.Error()}
	}
	return err
}

func (s *Server) listAuditReports(w http.ResponseWriter, r *http.Request) {
	items, err := s.auditrep.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// createAuditReport calcule l'instantané, le signe et l'enregistre; l'événement d'audit est écrit dans la même transaction.
func (s *Server) createAuditReport(w http.ResponseWriter, r *http.Request) {
	var in reportInput
	if !decode(w, r, &in) {
		return
	}
	meta, err := s.auditrep.Generate(r.Context(), currentUser(r), s.clientIP(r), in.From, in.To)
	if err != nil {
		s.fail(w, reportErr(err))
		return
	}
	writeJSON(w, 201, meta)
}

func (s *Server) loadReport(w http.ResponseWriter, r *http.Request) (store.AuditReport, bool) {
	rep, err := s.auditrep.Get(r.Context(), id(r))
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
		w.Header().Set("Content-Disposition", `attachment; filename="`+auditreport.FileName(rep, "html")+`"`)
	}
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page) //nolint:gosec // page produite par html/template (échappement automatique)
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
	w.Header().Set("Content-Disposition", `attachment; filename="`+auditreport.FileName(rep, "json")+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// verifyAuditReport contrôle un fichier JSON exporté (corps brut de la requête).
func (s *Server) verifyAuditReport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, auditreport.MaxVerifyUpload))
	if err != nil {
		s.fail(w, bad("too_large", "fichier illisible ou trop volumineux"))
		return
	}
	res, err := s.auditrep.Verify(body)
	if err != nil {
		s.fail(w, reportErr(err))
		return
	}
	writeJSON(w, 200, res)
}

// summarizeAuditReport demande à l'IA configurée une synthèse rédigée à partir des seuls agrégats de l'instantané.
func (s *Server) summarizeAuditReport(w http.ResponseWriter, r *http.Request) {
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
	text, model, err := s.auditrep.Summarize(r.Context(), currentUser(r), s.clientIP(r), id(r), lang)
	if err != nil {
		s.fail(w, reportErr(err))
		return
	}
	writeJSON(w, 200, map[string]any{"summary": text, "model": model})
}

func (s *Server) getAuditAI(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.auditrep.AIConfig(r.Context())
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
	cfg, err := s.auditrep.UpdateAI(r.Context(), currentUser(r), s.clientIP(r), auditreport.AIUpdate{Enabled: in.Enabled, URL: in.URL, Model: in.Model, APIKey: in.APIKey})
	if err != nil {
		s.fail(w, reportErr(err))
		return
	}
	writeJSON(w, 200, map[string]any{"config": cfg})
}
