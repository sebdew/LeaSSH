package catalog

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func i64(n int64) *int64 { return &n }

// Charges malveillantes (SPEC §15.3, CLAUDE.md « Tests obligatoires »).
func payloads(marker string) []string {
	p := []string{
		"; id", "; rm -rf /", "$(id)", "`id`", "| id", "&& id", "|| id", "& id", "\n id", "\r\n id",
		"-rf", "--help", "-", "--", "../", "../../etc/passwd", "/var/log/../../etc/passwd", "/etc/passwd",
		"'", "\"", "\\", "'; id; '", "\"; id; \"", "a'b", "a\"b", "a\\'b", "'$(id)'", "\x00", "a\x00b",
		"é", "日本語", "🙂", "a b", "a\tb", "${IFS}id", "$HOME", "$((1+1))", "*", "?", "~", "~root", "!!",
		"> /tmp/x", "< /etc/passwd", "{a,b}", "#", "a#b", "%s", "%n", "\x1b[31m", "\x7f", "",
		" ", "  ", strings.Repeat("A", 5000), strings.Repeat("'", 300), strings.Repeat("a", 129),
		"; touch " + marker, "$(touch " + marker + ")", "`touch " + marker + "`", "| touch " + marker,
		"&& touch " + marker, "\n touch " + marker, "'; touch " + marker + "; '",
		"/var/log/x; touch " + marker, "/var/log/$(touch " + marker + ")", "abc; touch " + marker,
		"1; touch " + marker, "99999999999999999999", "1e3", "0x10", "-1", "+5", "5.5", "5 ", " 5",
		"nginx; touch " + marker, "nginx.service", "-nginx", "nginx\n", "@", "a@b",
	}
	return p
}

type typeCase struct {
	name string
	def  ParamDef
}

func typeCases() []typeCase {
	return []typeCase{
		{"enum", ParamDef{Type: TypeEnum, Values: []string{"info", "warning", "err"}}},
		{"enum-quote", ParamDef{Type: TypeEnum, Values: []string{"it's", "a b", "$x"}}},
		{"int", ParamDef{Type: TypeInt, Min: i64(1), Max: i64(1000)}},
		{"int-neg", ParamDef{Type: TypeInt, Min: i64(-5), Max: i64(5)}},
		{"identifier", ParamDef{Type: TypeIdentifier}},
		{"identifier-short", ParamDef{Type: TypeIdentifier, MaxLength: 8}},
		{"service", ParamDef{Type: TypeService}},
		{"path", ParamDef{Type: TypePath, AllowedPrefixes: []string{"/var/log"}}},
		{"path-multi", ParamDef{Type: TypePath, AllowedPrefixes: []string{"/var/log/", "/etc/nginx"}}},
		{"regex-strict", ParamDef{Type: TypeRegex, Pattern: `[a-z]{1,10}`}},
		{"regex-permissive", ParamDef{Type: TypeRegex, Pattern: `.+`}}, // pire cas: l'échappement seul doit protéger
		{"regex-permissive-any", ParamDef{Type: TypeRegex, Pattern: `[\s\S]*`}},
	}
}

// Les valeurs acceptées sont rendues puis exécutées dans un vrai `sh -c` pour prouver
// qu'aucune n'est interprétée: la sortie doit être exactement la valeur et aucun fichier-témoin ne doit exister.
func TestInjectionCorpus(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh indisponible")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "PWNED")
	total, accepted := 0, 0
	for _, tc := range typeCases() {
		for _, pl := range payloads(marker) {
			total++
			defs := map[string]ParamDef{"x": tc.def}
			out, err := Render("printf '%s' {{x}}", defs, map[string]any{"x": pl})
			if err != nil {
				if _, ok := err.(*ParamError); !ok {
					t.Fatalf("[%s] %q: erreur non typée: %v", tc.name, pl, err)
				}
				continue // refusé = inoffensif
			}
			accepted++
			got, runErr := exec.Command("sh", "-c", out).CombinedOutput()
			if runErr != nil {
				t.Fatalf("[%s] %q: exécution échouée (%s): %v", tc.name, pl, out, runErr)
			}
			if string(got) != pl {
				t.Fatalf("[%s] %q: sortie %q ≠ valeur (rendu: %s)", tc.name, pl, got, out)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("[%s] %q: commande injectée exécutée! (rendu: %s)", tc.name, pl, out)
			}
		}
	}
	if total < 300 {
		t.Fatalf("corpus trop petit: %d combinaisons (≥ 300 requis)", total)
	}
	if accepted == 0 {
		t.Fatal("aucune valeur acceptée: le test ne prouve rien")
	}
	t.Logf("%d combinaisons type × charge, %d acceptées et neutralisées, %d refusées", total, accepted, total-accepted)
}

// Les types stricts doivent refuser toute charge contenant des métacaractères.
func TestStrictTypesRejectMetachars(t *testing.T) {
	strict := []ParamDef{
		{Type: TypeEnum, Values: []string{"info"}},
		{Type: TypeInt, Min: i64(0), Max: i64(100)},
		{Type: TypeIdentifier},
		{Type: TypeService},
		{Type: TypeRegex, Pattern: `[a-z]+`},
	}
	bad := []string{"; id", "$(id)", "`id`", "| id", "&& id", "\n id", "-rf", "--help", "'", "\"", "\\", "\x00", "a b", "*", ">x", "<x"}
	for _, d := range strict {
		for _, b := range bad {
			if _, err := d.Validate("x", b); err == nil {
				t.Errorf("type %s a accepté %q", d.Type, b)
			}
		}
	}
}

func TestPathTraversal(t *testing.T) {
	d := ParamDef{Type: TypePath, AllowedPrefixes: []string{"/var/log"}}
	ok := []string{"/var/log", "/var/log/syslog", "/var/log/nginx/error.log"}
	bad := []string{"var/log/x", "/var/log/../../etc/passwd", "/var/logs/x", "/var/log/", "/var/log//x", "/var/log/./x", "/etc/passwd", "/var/log/..", ""}
	for _, s := range ok {
		if _, err := d.Validate("p", s); err != nil {
			t.Errorf("%q refusé: %v", s, err)
		}
	}
	for _, s := range bad {
		if _, err := d.Validate("p", s); err == nil {
			t.Errorf("%q accepté", s)
		}
	}
}

func TestRenderAndDenial(t *testing.T) {
	defs := map[string]ParamDef{
		"service": {Type: TypeService},
		"lines":   {Type: TypeInt, Min: i64(1), Max: i64(1000), Default: 50, Required: boolp(false)},
	}
	got, err := Render("journalctl -u {{service}} -n {{lines}} --no-pager", defs, map[string]any{"service": "nginx"})
	if err != nil || got != "journalctl -u 'nginx' -n '50' --no-pager" {
		t.Fatalf("rendu: %q %v", got, err)
	}
	_, err = Render("journalctl -u {{service}} -n {{lines}}", defs, map[string]any{"service": "nginx", "lines": float64(50000)})
	pe, ok := err.(*ParamError)
	if !ok || pe.Denial() != "param_invalid:lines" || !strings.Contains(pe.Error(), "entre 1 et 1000") {
		t.Fatalf("refus attendu sur lines: %v", err)
	}
	if _, err = Render("echo {{service}}", defs, map[string]any{"service": "x", "evil": "y"}); err == nil {
		t.Fatal("paramètre inconnu accepté")
	}
	if _, err = Render("echo {{service}}", defs, nil); err == nil {
		t.Fatal("paramètre obligatoire manquant accepté")
	}
	// forme --option={{x}}
	defs["free"] = ParamDef{Type: TypeRegex, Pattern: ".+"}
	got, err = Render("cmd --unit={{free}}", defs, map[string]any{"free": "a'b"})
	if err != nil || got != `cmd --unit='a'\''b'` {
		t.Fatalf("option=valeur: %q %v", got, err)
	}
}

func TestRenderRefusesUnsafeTemplates(t *testing.T) {
	defs := map[string]ParamDef{"x": {Type: TypeService}}
	for _, tpl := range []string{`echo "{{x}}"`, `echo '{{x}}'`, `echo a{{x}}`, `echo {{x}}b`, `echo /var/{{x}}.log`, `{{x}} foo`, `echo {{x}}{{x}}`, `echo "a b" '{{x}}'`} {
		if _, err := Render(tpl, defs, map[string]any{"x": "n"}); err == nil {
			t.Errorf("template dangereux accepté: %s", tpl)
		}
	}
}

func boolp(b bool) *bool { return &b }

func TestHashStable(t *testing.T) {
	defs := map[string]ParamDef{"a": {Type: TypeService}, "b": {Type: TypeIdentifier}}
	h1 := Hash("x {{a}} {{b}}", defs, RiskRead)
	for i := 0; i < 20; i++ {
		if Hash("x {{a}} {{b}}", defs, RiskRead) != h1 {
			t.Fatal("hash instable")
		}
	}
	if h1 == Hash("x {{a}} {{b}}", defs, RiskWrite) || h1 == Hash("x {{a}}  {{b}}", defs, RiskRead) {
		t.Fatal("hash insensible à une modification")
	}
	_ = fmt.Sprint()
}
