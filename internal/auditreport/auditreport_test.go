package auditreport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
	"github.com/leassh-dev/leassh/internal/testkit"
)

var now = time.Now().UTC().Truncate(time.Second)

// at: horodatage « il y a d jours à h heures » (UTC).
func at(d, h int) string {
	t := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC).AddDate(0, 0, -d)
	return store.FormatTime(t)
}

func params(retention int) Params {
	return Params{Now: now, From: now.AddDate(0, 0, -30), To: now, LeasshVersion: "9.9.9",
		RetentionDays: retention, ApprovalTTL: 30 * time.Minute, SessionTTL: 12 * time.Hour, BaseURL: "https://leassh.example.com"}
}

// seed ajoute de l'activité réaliste à l'instance de test et renvoie le noyau.
func seed(t *testing.T) *testkit.Kit {
	t.Helper()
	k := testkit.New(t, "RW", false, time.Minute)
	ctx := k.Ctx
	dur := func(n int64) *int64 { return &n }
	rendered := "pfctl -F states"
	exec := func(e store.Execution) {
		if err := k.St.Tx(ctx, func(tx *sql.Tx) error { _, err := store.InsertExecution(ctx, tx, e); return err }); err != nil {
			t.Fatal(err)
		}
	}
	base := store.Execution{KeyID: k.Key.ID, KeyName: "Agent N1", ServerID: k.Server.ID, ServerSlug: "web-01", CommandSlug: "t.read", Risk: "read", Status: "succeeded", DurationMS: dur(120), RequestedAt: at(10, 10)}
	exec(base)
	f := base
	f.Status, f.RequestedAt = "failed", at(9, 10)
	exec(f)
	d := base
	d.Status, d.DenialReason, d.DurationMS, d.RequestedAt = "denied", "no_matching_rule", nil, at(8, 10)
	exec(d)
	w := base // écriture exécutée sans validation humaine
	w.CommandSlug, w.Risk, w.RequestedAt = "t.write", "write", at(7, 10)
	exec(w)
	des := base // destructive validée par un humain après 90 s
	des.CommandSlug, des.Risk, des.Rendered, des.Reason, des.DecidedBy, des.DecidedByName, des.DecidedAt, des.RequestedAt = "t.destroy", "destructive", &rendered, "incident #42", "u1", "alice", at(6, 10)[:14]+"01:30.000Z", at(6, 10)
	exec(des)
	out := base // hors période: ne doit jamais apparaître
	out.RequestedAt, out.CommandSlug = at(70, 10), "t.outside"
	exec(out)
	for _, ev := range []audit.Event{
		{ActorType: "user", ActorName: "alice", Action: "rule.create", TargetType: "rule", TargetName: "N1"},
		{ActorType: "user", ActorName: "alice", Action: "key.create", TargetType: "key", TargetName: "Agent N1"},
		{ActorType: "key", ActorName: "Agent N1", Action: "key.auth_failed"},
	} {
		if err := k.St.Tx(ctx, func(tx *sql.Tx) error { return audit.Record(ctx, tx, ev) }); err != nil {
			t.Fatal(err)
		}
	}
	// noms distinctifs, pour vérifier plus loin qu'aucun ne part vers une IA
	for _, q := range []string{`UPDATE access_rules SET name='Règle secrète N1'`, `UPDATE users SET username='bob-admin'`} {
		if _, err := k.St.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	return k
}

func TestSnapshotFactsAreComputedFromTheDatabase(t *testing.T) {
	k := seed(t)
	s, err := Build(k.Ctx, k.St.DB, params(365))
	if err != nil {
		t.Fatal(err)
	}
	a := s.Activity
	if a.Requests != 5 || a.Succeeded != 3 || a.Failed != 1 || a.Denied != 1 {
		t.Fatalf("activité: %+v", a)
	}
	if a.WriteNoApproval != 1 || a.DestructiveTot != 1 || len(a.Destructive) != 1 || a.Destructive[0].DecidedBy != "alice" || a.Destructive[0].ApprovalSec != 90 || a.Destructive[0].Rendered != "pfctl -F states" {
		t.Fatalf("écritures et destructives: %+v", a)
	}
	if a.Approvals.Approved != 1 || a.Approvals.AvgDelaySec != 90 || a.Approvals.Deciders[0].Name != "alice" {
		t.Fatalf("validations: %+v", a.Approvals)
	}
	if len(a.DenialReasons) != 1 || a.DenialReasons[0].Name != "no_matching_rule" || a.DistinctKeys != 1 {
		t.Fatalf("refus: %+v", a.DenialReasons)
	}
	if len(a.Security) != 1 || a.Security[0].Name != "key.auth_failed" {
		t.Fatalf("événements de sécurité: %+v", a.Security)
	}
	if s.Changes.Total != 3 || len(s.Changes.Listed) != 3 || s.Changes.Truncated {
		t.Fatalf("changements: %+v", s.Changes)
	}
	in := s.Integrity
	if !in.Executions.Verified.OK || !in.Admin.Verified.OK || in.Executions.Info.Count != 6 || in.Executions.Info.HeadHash == "" || in.Admin.Info.MaxSeq != 3 {
		t.Fatalf("intégrité: %+v", in)
	}
	if s.Scope.Servers.Total != 1 || s.Scope.Servers.Pinned != 1 || s.Scope.Keys.Total != 1 || s.Scope.Rules.ReadWrite != 1 || s.Scope.Commands.ByRisk["destructive"] != 1 || s.Scope.Users.Admins != 1 {
		t.Fatalf("périmètre: %+v", s.Scope)
	}
	if s.Period.Days != 31 || s.GeneratedAt != store.FormatTime(now) || s.Period.To != now.Format("2006-01-02") {
		t.Fatalf("période: %+v", s.Period)
	}
}

func TestSnapshotIsDeterministic(t *testing.T) {
	k := seed(t)
	a, _ := Build(k.Ctx, k.St.DB, params(365))
	b, _ := Build(k.Ctx, k.St.DB, params(365))
	ja, _ := Marshal(a)
	jb, _ := Marshal(b)
	if !bytes.Equal(ja, jb) || Digest(ja) != Digest(jb) {
		t.Fatal("deux instantanés du même état doivent être identiques à l'octet près")
	}
	var round Snapshot
	if err := json.Unmarshal(ja, &round); err != nil {
		t.Fatal(err)
	}
}

func codes(fs []Finding) map[string]Finding {
	m := map[string]Finding{}
	for _, f := range fs {
		m[f.Code] = f
	}
	return m
}

func TestFindingsRules(t *testing.T) {
	k := seed(t)
	s, err := Build(k.Ctx, k.St.DB, params(90)) // rétention trop courte
	if err != nil {
		t.Fatal(err)
	}
	f := codes(s.Findings)
	// la règle du kit est RW sans validation; la clé n'a jamais servi et n'expire pas; un seul admin; pas de webhook
	for _, want := range []string{"rule_rw_no_approval", "key_no_expiry", "retention_short", "single_admin", "webhook_disabled", "write_executed_no_approval"} {
		if _, ok := f[want]; !ok {
			t.Errorf("constat attendu absent: %s (obtenu %v)", want, s.Findings)
		}
	}
	if f["rule_rw_no_approval"].Severity != High || f["retention_short"].Severity != Medium {
		t.Fatalf("gravités: %+v %+v", f["rule_rw_no_approval"], f["retention_short"])
	}
	if _, ok := f["integrity_failed"]; ok {
		t.Fatal("pas de rupture d'intégrité attendue")
	}
	if s.Findings[0].Severity != High { // tri: gravité d'abord
		t.Fatalf("tri: %+v", s.Findings[0])
	}
	// base saine à 365 jours: la rétention n'est plus signalée
	ok, _ := Build(k.Ctx, k.St.DB, params(365))
	if _, bad := codes(ok.Findings)["retention_short"]; bad {
		t.Fatal("rétention de 365 jours signalée à tort")
	}
}

func TestIntegrityFailureIsReportedAsFinding(t *testing.T) {
	k := seed(t)
	if _, err := k.St.DB.ExecContext(k.Ctx, `UPDATE executions SET reason='truqué' WHERE seq=2`); err != nil {
		t.Fatal(err)
	}
	s, _ := Build(k.Ctx, k.St.DB, params(365))
	f := codes(s.Findings)["integrity_failed"]
	if s.Integrity.Executions.Verified.OK || f.Severity != High || len(f.Items) != 1 {
		t.Fatalf("la falsification doit être détectée: %+v %+v", s.Integrity.Executions.Verified, f)
	}
	for _, c := range s.Controls {
		if c.Key == "ISO 8.15" && c.Status != "points" {
			t.Fatalf("le contrôle 8.15 doit passer en « points d'attention »: %+v", c)
		}
	}
}

func TestStaleKeysAndAdmins(t *testing.T) {
	k := seed(t)
	old := "2025-01-01T00:00:00.000Z"
	k.St.DB.ExecContext(k.Ctx, `UPDATE mcp_keys SET created_at=?, last_used_at=?`, old, old)
	k.St.DB.ExecContext(k.Ctx, `UPDATE users SET created_at=?, last_login_at=?`, old, old)
	s, _ := Build(k.Ctx, k.St.DB, params(365))
	f := codes(s.Findings)
	if f["key_unused_90d"].Count != 1 || f["admin_inactive"].Count != 1 || s.Scope.Keys.Unused != 1 {
		t.Fatalf("inactivité: %+v", s.Findings)
	}
}

func TestControlMappingStatuses(t *testing.T) {
	k := seed(t)
	s, _ := Build(k.Ctx, k.St.DB, params(365))
	by := map[string]Control{}
	for _, c := range s.Controls {
		by[c.Key] = c
	}
	if len(s.Controls) != len(controlDefs) || by["ISO 5.15"].Status != "points" { // règle RW sans validation
		t.Fatalf("5.15: %+v", by["ISO 5.15"])
	}
	if by["ISO 8.3"].Status != "elements" || len(by["ISO 8.3"].Evidence) == 0 {
		t.Fatalf("8.3: %+v", by["ISO 8.3"])
	}
	fr := map[string]bool{}
	for _, c := range s.Controls {
		fr[c.Framework] = true
		if c.Framework == "HDS" && c.ID != "" {
			t.Fatalf("aucun numéro de clause HDS ne doit être cité: %+v", c)
		}
	}
	if !fr["HDS"] || !fr["ISO 27001:2022"] {
		t.Fatalf("référentiels: %v", fr)
	}
}

func box(t *testing.T, b byte) *crypto.Box {
	t.Helper()
	bx, err := crypto.NewBox(bytes.Repeat([]byte{b}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return bx
}

func TestSignatureAndVerification(t *testing.T) {
	k := seed(t)
	s, _ := Build(k.Ctx, k.St.DB, params(365))
	raw, _ := Marshal(s)
	bx := box(t, 7)
	rep := store.AuditReport{ID: "R1", CreatedAt: s.GeneratedAt, LeasshVersion: "9.9.9", SHA256: Digest(raw), Snapshot: string(raw)}
	rep.HMAC = Tag(bx.Derive("audit-report"), rep.SHA256, rep.ID, rep.CreatedAt)
	file, _ := json.MarshalIndent(NewEnvelope(rep), "", "  ") // même indenté, le contenu se re-compacte à l'identique

	res, err := Verify(file, bx.Derive("audit-report"))
	if err != nil || !res.DigestOK || !res.TagOK {
		t.Fatalf("un fichier intact doit être reconnu: %+v %v", res, err)
	}
	if other, _ := Verify(file, box(t, 8).Derive("audit-report")); !other.DigestOK || other.TagOK {
		t.Fatalf("une autre instance reconnaît le contenu mais pas l'étiquette: %+v", other)
	}
	tampered := strings.Replace(string(file), `"requests": 5`, `"requests": 4`, 1)
	if tampered == string(file) {
		t.Fatal("la falsification de test n'a rien changé")
	}
	if bad, _ := Verify([]byte(tampered), bx.Derive("audit-report")); bad.DigestOK || bad.TagOK {
		t.Fatalf("un contenu modifié doit être rejeté: %+v", bad)
	}
	if _, err := Verify([]byte(`{"x":1}`), bx.Derive("audit-report")); err == nil {
		t.Fatal("un fichier étranger doit être refusé")
	}
}

func TestRenderHTMLIsSelfContainedAndComplete(t *testing.T) {
	k := seed(t)
	s, _ := Build(k.Ctx, k.St.DB, params(90))
	raw, _ := Marshal(s)
	rep := store.AuditReport{ID: "R1", CreatedAt: s.GeneratedAt, CreatedByName: "alice", SHA256: Digest(raw), Snapshot: string(raw)}
	html, err := RenderHTML(rep)
	if err != nil {
		t.Fatal(err)
	}
	h := string(html)
	for _, want := range []string{"Rapport d'audit", "aide à l'audit", "ISO 27001", "5.15", "HDS", "Règles en lecture/écriture sans validation humaine", rep.SHA256, "incident #42", "pfctl -F states", "intègres", "Éléments de preuve"} {
		if !strings.Contains(h, want) {
			t.Errorf("le rapport HTML ne contient pas %q", want)
		}
	}
	if strings.Contains(h, "<script") || strings.Contains(h, "http://") || strings.Contains(h, "src=") || strings.Contains(h, "<link") {
		t.Fatal("le rapport doit être autonome: ni script ni ressource externe")
	}
	if strings.Contains(h, "Synthèse rédigée par une IA") {
		t.Fatal("pas de synthèse IA tant qu'elle n'est pas demandée")
	}
	rep.Summary, rep.SummaryModel, rep.SummaryAt = "Résumé: <b>x</b>\n\nPoints", "gpt-test", "2026-11-03T12:00:00.000Z"
	with, _ := RenderHTML(rep)
	if !strings.Contains(string(with), "Synthèse rédigée par une IA — à relire") || strings.Contains(string(with), "<b>x</b>") {
		t.Fatal("synthèse IA absente, ou non échappée")
	}
}

// Le contenu qui quitte l'instance vers une IA ne doit contenir AUCUN nom: ni serveur, clé, utilisateur, commande, motif ni commande rendue.
func TestAIInputContainsNoNamesOrOutput(t *testing.T) {
	k := seed(t)
	s, _ := Build(k.Ctx, k.St.DB, params(90))
	b, _ := json.Marshal(BuildAIInput(s))
	sent := string(b)
	for _, secret := range []string{"web-01", "Agent N1", "alice", "bob-admin", "t.read", "t.destroy", "incident #42", "pfctl", "Règle secrète", k.Key.Prefix, "127.0.0.1", "leassh.example.com", "Frontal"} {
		if strings.Contains(sent, secret) {
			t.Errorf("la donnée %q ne doit pas être envoyée à l'IA: %s", secret, sent)
		}
	}
	if !strings.Contains(sent, `"requests":5`) || !strings.Contains(sent, `"read_write_without_approval":1`) {
		t.Fatalf("les agrégats doivent y figurer: %s", sent)
	}
}

func TestAIConfigSecretIsEncryptedAndKeptWhenBlank(t *testing.T) {
	k := seed(t)
	bx := box(t, 3)
	if err := SaveAIConfig(k.Ctx, k.St.DB, bx, AIConfig{Enabled: true, URL: "http://litellm:4000/v1", Model: "m"}, "sk-secret"); err != nil {
		t.Fatal(err)
	}
	raw, _ := store.GetSetting(k.Ctx, k.St.DB, KeyAISecret)
	if raw == "" || strings.Contains(raw, "sk-secret") {
		t.Fatalf("clé d'API non chiffrée: %q", raw)
	}
	if err := SaveAIConfig(k.Ctx, k.St.DB, bx, AIConfig{Enabled: true, URL: "http://litellm:4000/v1", Model: "m2"}, ""); err != nil {
		t.Fatal(err)
	}
	c, key, err := LoadAIConfig(k.Ctx, k.St.DB, bx)
	if err != nil || key != "sk-secret" || !c.HasKey || c.Model != "m2" || !c.Enabled {
		t.Fatalf("relecture: %+v %q %v", c, key, err)
	}
	if err := ValidateAIURL("ftp://x"); err == nil {
		t.Fatal("URL non http acceptée")
	}
	ctx := context.Background()
	if _, _, err := Summarize(ctx, AIConfig{}, "", AIInput{}, "fr"); err == nil {
		t.Fatal("la synthèse doit refuser de s'exécuter quand elle est désactivée")
	}
}

func TestAIDefaultsFillOnlyWhatIsNotConfigured(t *testing.T) {
	d := AIDefaults{URL: "http://litellm:4000/v1", Model: "m-env", APIKey: "sk-env"}
	// rien en base: les variables LLM_* servent de valeurs par défaut (la synthèse reste désactivée tant qu'on ne l'active pas)
	c, key := d.Apply(AIConfig{}, "")
	if c.URL != d.URL || c.Model != "m-env" || key != "sk-env" || !c.HasKey || c.Enabled {
		t.Fatalf("défauts non appliqués: %+v %q", c, key)
	}
	// valeurs saisies dans l'interface: elles l'emportent
	c, key = d.Apply(AIConfig{URL: "http://autre/v1", Model: "m-ui", HasKey: true}, "sk-ui")
	if c.URL != "http://autre/v1" || c.Model != "m-ui" || key != "sk-ui" {
		t.Fatalf("les réglages de l'interface doivent l'emporter: %+v %q", c, key)
	}
}
