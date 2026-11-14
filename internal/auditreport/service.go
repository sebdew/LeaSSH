package auditreport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/crypto"
	"github.com/sebdew/LeaSSH/internal/store"
)

const (
	tagLabel      = "audit-report"
	maxReportDays = 400
	// MaxVerifyUpload: taille maximale d'un fichier JSON soumis à la vérification.
	MaxVerifyUpload = 32 << 20
)

// Erreurs métier traduites par l'API.
var (
	ErrAIDisabled   = errors.New("la synthèse IA est désactivée: activez-la d'abord dans les réglages de l'audit")
	ErrAIIncomplete = errors.New("une URL et un modèle sont requis pour activer la synthèse IA")
)

// PeriodError: période de rapport invalide.
type PeriodError struct{ Msg string }

func (e *PeriodError) Error() string { return e.Msg }

// InvalidURLError: URL du service d'IA invalide.
type InvalidURLError struct{ Msg string }

func (e *InvalidURLError) Error() string { return e.Msg }

// InvalidFileError: fichier à vérifier illisible ou non conforme.
type InvalidFileError struct{ Msg string }

func (e *InvalidFileError) Error() string { return e.Msg }

// AIError: échec de l'appel au service d'IA (passerelle en amont).
type AIError struct{ Err error }

func (e *AIError) Error() string { return e.Err.Error() }
func (e *AIError) Unwrap() error { return e.Err }

// Settings: réglages généraux dont dépend l'instantané (satisfait par settings.Service).
type Settings interface {
	RetentionDays() int
	ApprovalTTL() time.Duration
	SessionTTL() time.Duration
	BaseURL() string
}

// Service: génération, signature, consultation et synthèse des rapports d'audit.
type Service struct {
	st        *store.Store
	box       *crypto.Box
	version   string
	set       Settings
	defaults  AIDefaults
	transport http.RoundTripper
}

// NewService: transport (facultatif) mesure les appels sortants vers le service d'IA.
func NewService(st *store.Store, box *crypto.Box, version string, set Settings, defaults AIDefaults, transport http.RoundTripper) *Service {
	return &Service{st: st, box: box, version: version, set: set, defaults: defaults, transport: transport}
}

func parseDay(v string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(v))
	if err != nil {
		return t, &PeriodError{Msg: "dates attendues au format AAAA-MM-JJ"}
	}
	return t, nil
}

// resolvePeriod: fin = aujourd'hui (UTC) et début = fin − 29 jours par défaut; la fin n'est jamais dans le futur.
func resolvePeriod(from, to string, today time.Time) (time.Time, time.Time, error) {
	end := today
	var err error
	if strings.TrimSpace(to) != "" {
		if end, err = parseDay(to); err != nil {
			return end, end, err
		}
	}
	start := end.AddDate(0, 0, -29)
	if strings.TrimSpace(from) != "" {
		if start, err = parseDay(from); err != nil {
			return start, end, err
		}
	}
	switch {
	case start.After(end):
		return start, end, &PeriodError{Msg: "la date de début doit précéder la date de fin"}
	case end.After(today):
		return start, end, &PeriodError{Msg: "la date de fin ne peut pas être dans le futur"}
	case end.Sub(start) > maxReportDays*24*time.Hour:
		return start, end, &PeriodError{Msg: fmt.Sprintf("période limitée à %d jours", maxReportDays)}
	}
	return start, end, nil
}

func (s *Service) List(ctx context.Context) ([]store.AuditReportMeta, error) {
	items, err := store.ListAuditReports(ctx, s.st.DB)
	if items == nil {
		items = []store.AuditReportMeta{}
	}
	return items, err
}

func (s *Service) Get(ctx context.Context, id string) (store.AuditReport, error) {
	return store.AuditReportByID(ctx, s.st.DB, id)
}

// Generate calcule l'instantané, le signe et l'enregistre; l'événement d'audit est écrit dans la même transaction.
func (s *Service) Generate(ctx context.Context, actor store.User, ip, fromDay, toDay string) (store.AuditReportMeta, error) {
	from, to, err := resolvePeriod(fromDay, toDay, time.Now().UTC())
	if err != nil {
		return store.AuditReportMeta{}, err
	}
	var meta store.AuditReportMeta
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		snap, err := Build(ctx, tx, Params{Now: time.Now(), From: from, To: to, LeasshVersion: s.version,
			RetentionDays: s.set.RetentionDays(), ApprovalTTL: s.set.ApprovalTTL(), SessionTTL: s.set.SessionTTL(), BaseURL: s.set.BaseURL()})
		if err != nil {
			return audit.Event{}, err
		}
		raw, err := Marshal(snap)
		if err != nil {
			return audit.Event{}, err
		}
		h, m, l := Counts(snap.Findings)
		rep := store.AuditReport{CreatedBy: actor.ID, CreatedByName: actor.Username, PeriodFrom: snap.Period.From, PeriodTo: snap.Period.To, LeasshVersion: s.version,
			SchemaVersion: snap.SchemaVersion, SHA256: Digest(raw), Snapshot: string(raw), High: h, Medium: m, Low: l}
		if rep, err = store.InsertAuditReport(ctx, tx, rep); err != nil {
			return audit.Event{}, err
		}
		rep.HMAC = Tag(s.box.Derive(tagLabel), rep.SHA256, rep.ID, rep.CreatedAt)
		if err = store.SetAuditReportHMAC(ctx, tx, rep.ID, rep.HMAC); err != nil {
			return audit.Event{}, err
		}
		meta = store.AuditReportMeta{AuditReport: rep}
		return audit.Event{Action: "audit_report.generate", TargetType: "audit_report", TargetID: rep.ID, TargetName: rep.PeriodFrom + " → " + rep.PeriodTo,
			Diff: map[string]any{"after": map[string]any{"period_from": rep.PeriodFrom, "period_to": rep.PeriodTo, "sha256": rep.SHA256, "findings_high": h, "findings_medium": m, "findings_low": l,
				"integrity_ok": snap.Integrity.Executions.Verified.OK && snap.Integrity.Admin.Verified.OK}}}, nil
	})
	return meta, err
}

// FileName: nom de fichier de l'export d'un rapport.
func FileName(rep store.AuditReport, ext string) string {
	id := rep.ID
	if len(id) > 8 {
		id = id[len(id)-8:]
	}
	return fmt.Sprintf("leassh-audit-%s_%s-%s.%s", rep.PeriodFrom, rep.PeriodTo, strings.ToLower(id), ext)
}

// Verify contrôle un fichier JSON exporté (intégrité et signature).
func (s *Service) Verify(file []byte) (VerifyResult, error) {
	res, err := Verify(file, s.box.Derive(tagLabel))
	if err != nil {
		return VerifyResult{}, &InvalidFileError{Msg: err.Error()}
	}
	return res, nil
}

// Summarize demande à l'IA configurée une synthèse rédigée à partir des seuls agrégats de l'instantané.
// L'appel réseau se fait hors transaction; la synthèse est ensuite enregistrée avec son événement d'audit.
func (s *Service) Summarize(ctx context.Context, actor store.User, ip, id, lang string) (text, model string, err error) {
	rep, err := store.AuditReportByID(ctx, s.st.DB, id)
	if err != nil {
		return "", "", err
	}
	cfg, key, err := LoadAIConfig(ctx, s.st.DB, s.box)
	if err != nil {
		return "", "", err
	}
	cfg, key = s.defaults.Apply(cfg, key)
	if !cfg.Enabled {
		return "", "", ErrAIDisabled
	}
	var snap Snapshot
	if err := json.Unmarshal([]byte(rep.Snapshot), &snap); err != nil {
		return "", "", fmt.Errorf("instantané du rapport: %w", err)
	}
	if text, model, err = Summarize(ctx, cfg, key, BuildAIInput(snap), lang, WithTransport(s.transport)); err != nil {
		return "", "", &AIError{Err: err}
	}
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		if err := store.SetAuditReportSummary(ctx, tx, rep.ID, text, model); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "audit_report.summary", TargetType: "audit_report", TargetID: rep.ID, TargetName: rep.PeriodFrom + " → " + rep.PeriodTo,
			Diff: map[string]any{"after": map[string]any{"model": model, "ai_endpoint": cfg.URL, "had_summary": rep.Summary != ""}}}, nil
	})
	return text, model, err
}

// AIConfig renvoie la configuration de la synthèse IA (la clé d'API n'est jamais renvoyée, seulement HasKey).
func (s *Service) AIConfig(ctx context.Context) (AIConfig, error) {
	cfg, key, err := LoadAIConfig(ctx, s.st.DB, s.box)
	if err != nil {
		return AIConfig{}, err
	}
	cfg, _ = s.defaults.Apply(cfg, key)
	return cfg, nil
}

// AIUpdate: nouvelle configuration. Une clé d'API vide conserve la clé existante.
type AIUpdate struct {
	Enabled bool
	URL     string
	Model   string
	APIKey  string
}

// UpdateAI valide et enregistre la configuration (clé d'API chiffrée, jamais dans l'audit).
func (s *Service) UpdateAI(ctx context.Context, actor store.User, ip string, in AIUpdate) (AIConfig, error) {
	in.URL, in.Model, in.APIKey = strings.TrimSpace(in.URL), strings.TrimSpace(in.Model), strings.TrimSpace(in.APIKey)
	if in.URL != "" {
		if err := ValidateAIURL(in.URL); err != nil {
			return AIConfig{}, &InvalidURLError{Msg: err.Error()}
		}
	}
	var cfg AIConfig
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, _, err := LoadAIConfig(ctx, tx, s.box)
		if err != nil {
			return audit.Event{}, err
		}
		if in.Enabled && (in.URL == "" || in.Model == "") {
			return audit.Event{}, ErrAIIncomplete
		}
		cfg = AIConfig{Enabled: in.Enabled, URL: in.URL, Model: in.Model}
		if err := SaveAIConfig(ctx, tx, s.box, cfg, in.APIKey); err != nil {
			return audit.Event{}, err
		}
		cfg.HasKey = old.HasKey || in.APIKey != ""
		return audit.Event{Action: "audit_ai.update", TargetType: "audit_ai", TargetName: in.URL,
			Diff: map[string]any{"before": map[string]any{"enabled": old.Enabled, "url": old.URL, "model": old.Model},
				"after": map[string]any{"enabled": in.Enabled, "url": in.URL, "model": in.Model, "api_key_changed": in.APIKey != ""}}}, nil
	})
	return cfg, err
}
