package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

type Risk string

const (
	RiskRead        Risk = "read"
	RiskWrite       Risk = "write"
	RiskDestructive Risk = "destructive"
)

func (r Risk) Valid() bool { return r == RiskRead || r == RiskWrite || r == RiskDestructive }

const maxTemplateLen = 4096

var placeholderRe = regexp.MustCompile(`\{\{([a-z][a-z0-9_]*)\}\}`)

// Placeholder: occurrence {{nom}} dans un template.
type Placeholder struct {
	Name       string
	Start, End int // offsets dans le template
}

// Placeholders renvoie les occurrences dans l'ordre.
func Placeholders(tpl string) []Placeholder {
	var out []Placeholder
	for _, m := range placeholderRe.FindAllStringSubmatchIndex(tpl, -1) {
		out = append(out, Placeholder{Name: tpl[m[2]:m[3]], Start: m[0], End: m[1]})
	}
	return out
}

// PlaceholderNames: noms uniques triés.
func PlaceholderNames(tpl string) []string {
	seen := map[string]bool{}
	names := []string{} // jamais nil: la valeur part en JSON (null casserait l'éditeur)
	for _, p := range Placeholders(tpl) {
		if !seen[p.Name] {
			seen[p.Name] = true
			names = append(names, p.Name)
		}
	}
	sort.Strings(names)
	return names
}

// ShellQuote échappe en POSIX: guillemets simples, `'` → `'\”`.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Hash: SHA-256 de (template + params + risk). Les clés de map sont triées par encoding/json.
func Hash(tpl string, params map[string]ParamDef, risk Risk) string {
	if params == nil {
		params = map[string]ParamDef{}
	}
	b, _ := json.Marshal(struct {
		T string              `json:"template"`
		P map[string]ParamDef `json:"params"`
		R Risk                `json:"risk"`
	}{tpl, params, risk})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// structureErrors vérifie que chaque placeholder est un mot shell autonome (SPEC §6.1).
// Ces contrôles sont bloquants dans tous les cas: ils garantissent que l'échappement POSIX est sûr.
func structureErrors(tpl string) []Issue {
	var out []Issue
	phs := Placeholders(tpl)
	if f := strings.Fields(tpl); len(f) > 0 && strings.Contains(f[0], "{{") {
		out = append(out, Issue{Code: "first_word_placeholder", Message: "le premier mot du template ne peut pas être un placeholder"})
	}
	// {{ ou }} résiduels hors placeholders valides
	rest := placeholderRe.ReplaceAllString(tpl, "")
	if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
		out = append(out, Issue{Code: "malformed_placeholder", Message: "placeholder mal formé: utiliser {{nom}} avec [a-z][a-z0-9_]*"})
	}
	// état de guillemets sur le texte littéral, placeholders neutralisés
	inS, inD, esc := false, false, false
	pi := 0
	for i := 0; i < len(tpl); i++ {
		if pi < len(phs) && i == phs[pi].Start {
			ph := phs[pi]
			if inS || inD {
				out = append(out, Issue{Code: "placeholder_quoted", Param: ph.Name, Message: fmt.Sprintf("{{%s}} ne doit pas être entre guillemets (l'échappement est ajouté automatiquement)", ph.Name)})
			}
			if !autonomousBefore(tpl, ph.Start) {
				out = append(out, Issue{Code: "placeholder_not_standalone", Param: ph.Name, Message: fmt.Sprintf("{{%s}} doit être un mot shell autonome (seule exception: --option={{%s}})", ph.Name, ph.Name)})
			}
			if ph.End < len(tpl) && tpl[ph.End] != ' ' && tpl[ph.End] != '\t' {
				out = append(out, Issue{Code: "placeholder_not_standalone", Param: ph.Name, Message: fmt.Sprintf("{{%s}} ne doit pas être suivi de texte collé", ph.Name)})
			}
			i = ph.End - 1
			pi++
			continue
		}
		c := tpl[i]
		switch {
		case esc:
			esc = false
		case inS:
			if c == '\'' {
				inS = false
			}
		case c == '\\':
			esc = true
		case inD:
			if c == '"' {
				inD = false
			}
		case c == '\'':
			inS = true
		case c == '"':
			inD = true
		}
	}
	return out
}

var prefixOK = regexp.MustCompile(`^[A-Za-z0-9_.@:+,/-]+=$`)

func autonomousBefore(tpl string, start int) bool {
	if start == 0 {
		return true
	}
	if c := tpl[start-1]; c == ' ' || c == '\t' {
		return true
	}
	// forme --option=valeur: le mot précédent doit être un préfixe littéral simple finissant par «=»
	ws := strings.LastIndexAny(tpl[:start], " \t") + 1
	return prefixOK.MatchString(tpl[ws:start])
}

// Render valide les valeurs et produit la commande exacte à envoyer (SPEC §6.3 étapes 1-2).
// La vérification de hash (étape 3) est faite par l'appelant (executions.Run).
func Render(tpl string, defs map[string]ParamDef, values map[string]any) (string, error) {
	if len(tpl) > maxTemplateLen {
		return "", fmt.Errorf("template trop long")
	}
	if iss := structureErrors(tpl); len(iss) > 0 {
		return "", &ParamError{Name: iss[0].Param, Msg: "template invalide: " + iss[0].Message}
	}
	for k := range values {
		if _, ok := defs[k]; !ok {
			return "", perr(k, "paramètre inconnu")
		}
	}
	resolved := map[string]string{}
	for _, name := range PlaceholderNames(tpl) {
		def, ok := defs[name]
		if !ok {
			return "", perr(name, "paramètre non déclaré")
		}
		v, given := values[name]
		if !given || v == nil {
			if def.Default != nil {
				v = def.Default
			} else if def.IsRequired() {
				return "", perr(name, "obligatoire")
			} else {
				return "", perr(name, "paramètre optionnel sans valeur par défaut non supporté dans le template")
			}
		}
		s, err := def.Validate(name, v)
		if err != nil {
			return "", err
		}
		resolved[name] = ShellQuote(s)
	}
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		return resolved[m[2:len(m)-2]]
	}), nil
}

// ---- Linter ----

type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param,omitempty"`
}

type LintResult struct {
	Errors   []Issue `json:"errors"`
	Warnings []Issue `json:"warnings"`
}

func (r LintResult) OK() bool { return len(r.Errors) == 0 }

// Mode du linter: une proposition est strictement bloquée, un admin reçoit des avertissements
// pour les métacaractères et interpréteurs. Les erreurs de structure restent bloquantes dans les deux cas.
type Mode int

const (
	ModeProposal Mode = iota
	ModeAdmin
)

var interpreters = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "perl": true, "ruby": true, "node": true, "php": true,
	"eval": true, "exec": true, "xargs": true, "env": true, "nohup": true, "sudo": true, "su": true,
}

func isInterpreter(word string) bool {
	b := path.Base(word)
	return interpreters[b] || strings.HasPrefix(b, "python")
}

var metaTokens = []string{";", "&", "|", "`", "$(", "${", ">", "<", "\n", "\r"}

// Lint applique les règles du SPEC §6.4.
func Lint(tpl string, defs map[string]ParamDef, risk Risk, mode Mode) LintResult {
	res := LintResult{Errors: []Issue{}, Warnings: []Issue{}}
	soft := func(i Issue) { // erreur pour une proposition, avertissement pour un admin
		if mode == ModeProposal {
			res.Errors = append(res.Errors, i)
		} else {
			res.Warnings = append(res.Warnings, i)
		}
	}
	if !risk.Valid() {
		res.Errors = append(res.Errors, Issue{Code: "risk_missing", Message: "le niveau de risque est obligatoire (read, write, destructive)"})
	}
	if strings.TrimSpace(tpl) == "" {
		res.Errors = append(res.Errors, Issue{Code: "template_empty", Message: "template vide"})
		return res
	}
	if len(tpl) > maxTemplateLen {
		res.Errors = append(res.Errors, Issue{Code: "template_too_long", Message: fmt.Sprintf("template trop long (%d max)", maxTemplateLen)})
	}
	res.Errors = append(res.Errors, structureErrors(tpl)...)

	// premier mot
	if first := strings.Fields(tpl)[0]; !strings.Contains(first, "{{") && isInterpreter(first) {
		soft(Issue{Code: "interpreter", Message: fmt.Sprintf("le premier mot %q est un interpréteur/lanceur de commandes", first)})
	}

	// métacaractères hors placeholders
	literal := placeholderRe.ReplaceAllString(tpl, "")
	for _, m := range metaTokens {
		if strings.Contains(literal, m) {
			label := m
			if m == "\n" || m == "\r" {
				label = "retour à la ligne"
			}
			code := "shell_metachar"
			if m == "|" {
				code = "pipe"
			}
			soft(Issue{Code: code, Message: fmt.Sprintf("métacaractère shell hors placeholder: %s", label)})
		}
	}

	// cohérence placeholders / params
	used := map[string]bool{}
	for _, n := range PlaceholderNames(tpl) {
		used[n] = true
		if _, ok := defs[n]; !ok {
			res.Errors = append(res.Errors, Issue{Code: "placeholder_undeclared", Param: n, Message: fmt.Sprintf("placeholder {{%s}} non déclaré dans params", n)})
		}
	}
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !used[n] {
			res.Errors = append(res.Errors, Issue{Code: "param_unused", Param: n, Message: fmt.Sprintf("paramètre %q déclaré mais absent du template", n)})
		}
		for _, e := range defs[n].Check(n) {
			res.Errors = append(res.Errors, Issue{Code: "param_definition", Param: n, Message: e})
		}
	}
	return res
}
