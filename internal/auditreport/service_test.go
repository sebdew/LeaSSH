package auditreport

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/store"
)

type fakeSettings struct{}

func (fakeSettings) RetentionDays() int         { return 365 }
func (fakeSettings) ApprovalTTL() time.Duration { return 30 * time.Minute }
func (fakeSettings) SessionTTL() time.Duration  { return 12 * time.Hour }
func (fakeSettings) BaseURL() string            { return "https://leassh.example.com" }

func TestResolvePeriod(t *testing.T) {
	today := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	from, to, err := resolvePeriod("", "", today)
	if err != nil || !to.Equal(today) || !from.Equal(today.AddDate(0, 0, -29)) {
		t.Fatalf("défauts: %v %v %v", from, to, err)
	}
	if _, _, err := resolvePeriod("2026-10-01", "2026-10-31", today); err != nil {
		t.Fatalf("période valide: %v", err)
	}
	for name, c := range map[string][2]string{
		"format":  {"14/11/2026", ""},
		"format2": {"", "demain"},
		"ordre":   {"2026-11-10", "2026-11-01"},
		"futur":   {"2026-11-01", "2026-11-20"},
		"trop":    {"2025-01-01", "2026-11-01"},
	} {
		var pe *PeriodError
		if _, _, err := resolvePeriod(c[0], c[1], today); !errors.As(err, &pe) {
			t.Errorf("%s: erreur de période attendue, obtenu %v", name, err)
		}
	}
}

func TestServiceGenerateSignsAuditsAndVerifies(t *testing.T) {
	k := seed(t)
	svc := NewService(k.St, k.Box, "9.9.9", fakeSettings{}, AIDefaults{}, nil)

	var pe *PeriodError
	if _, err := svc.Generate(k.Ctx, k.Admin, "", "2026-13-45", ""); !errors.As(err, &pe) {
		t.Fatalf("période invalide: %v", err)
	}
	meta, err := svc.Generate(k.Ctx, k.Admin, "192.0.2.1", "", "")
	if err != nil || meta.ID == "" || meta.HMAC == "" || meta.SHA256 == "" {
		t.Fatalf("génération: %+v %v", meta, err)
	}
	var n int
	if err := k.St.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action='audit_report.generate' AND target_id=?`, meta.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("événement d'audit: %d %v", n, err)
	}
	items, err := svc.List(k.Ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("liste: %+v %v", items, err)
	}
	rep, err := svc.Get(k.Ctx, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := json.Marshal(NewEnvelope(rep))
	res, err := svc.Verify(file)
	if err != nil || !res.DigestOK || !res.TagOK {
		t.Fatalf("un export intact doit être reconnu par la même instance: %+v %v", res, err)
	}
	var fe *InvalidFileError
	if _, err := svc.Verify([]byte(`{"x":1}`)); !errors.As(err, &fe) {
		t.Fatalf("fichier étranger: %v", err)
	}
	if name := FileName(rep, "json"); !strings.HasPrefix(name, "leassh-audit-") || !strings.HasSuffix(name, ".json") {
		t.Fatalf("nom de fichier: %s", name)
	}
	if _, err := svc.Get(k.Ctx, "absent"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rapport inconnu: %v", err)
	}
}

func TestServiceAIConfigAndSummaryGuards(t *testing.T) {
	k := seed(t)
	svc := NewService(k.St, k.Box, "9.9.9", fakeSettings{}, AIDefaults{}, nil)

	var iu *InvalidURLError
	if _, err := svc.UpdateAI(k.Ctx, k.Admin, "", AIUpdate{URL: "ftp://x"}); !errors.As(err, &iu) {
		t.Fatalf("URL invalide: %v", err)
	}
	if _, err := svc.UpdateAI(k.Ctx, k.Admin, "", AIUpdate{Enabled: true, URL: "http://llm:4000/v1"}); !errors.Is(err, ErrAIIncomplete) {
		t.Fatalf("activation incomplète: %v", err)
	}
	cfg, err := svc.UpdateAI(k.Ctx, k.Admin, "192.0.2.1", AIUpdate{Enabled: true, URL: " http://llm:4000/v1 ", Model: "m", APIKey: "sk-top-secret"})
	if err != nil || !cfg.HasKey || cfg.URL != "http://llm:4000/v1" {
		t.Fatalf("mise à jour: %+v %v", cfg, err)
	}
	var diff string
	if err := k.St.DB.QueryRow(`SELECT diff FROM audit_events WHERE action='audit_ai.update'`).Scan(&diff); err != nil || strings.Contains(diff, "sk-top-secret") {
		t.Fatalf("la clé d'API ne doit pas être auditée: %v %s", err, diff)
	}
	if got, err := svc.AIConfig(k.Ctx); err != nil || !got.HasKey || !got.Enabled {
		t.Fatalf("relecture: %+v %v", got, err)
	}

	// synthèse: désactivée → refus avant tout appel réseau
	if _, err := svc.UpdateAI(k.Ctx, k.Admin, "", AIUpdate{Enabled: false, URL: "http://llm:4000/v1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	meta, err := svc.Generate(k.Ctx, k.Admin, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Summarize(k.Ctx, k.Admin, "", meta.ID, "fr"); !errors.Is(err, ErrAIDisabled) {
		t.Fatalf("synthèse désactivée: %v", err)
	}
	if _, _, err := svc.Summarize(k.Ctx, k.Admin, "", "absent", "fr"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rapport inconnu: %v", err)
	}
}
