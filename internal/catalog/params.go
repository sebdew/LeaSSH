// Package catalog: commandes, paramètres typés, rendu de templates et linter (SPEC §6).
package catalog

import (
	"encoding/json"
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Types de paramètres. Il n'existe volontairement aucun type « texte libre ».
const (
	TypeEnum       = "enum"
	TypeInt        = "int"
	TypeIdentifier = "identifier"
	TypeService    = "service"
	TypePath       = "path"
	TypeRegex      = "regex"
)

var (
	identifierRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)
	serviceRe    = regexp.MustCompile(`^[A-Za-z0-9@._:-]{1,128}$`)
	paramNameRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

const (
	defaultIdentifierMax = 64
	defaultRegexMax      = 128
	maxPathLen           = 1024
)

// ParamDef décrit un paramètre d'une commande (champ JSON `params`).
type ParamDef struct {
	Type            string   `json:"type" yaml:"type"`
	Description     string   `json:"description,omitempty" yaml:"description,omitempty"`
	Required        *bool    `json:"required,omitempty" yaml:"required,omitempty"`
	Default         any      `json:"default,omitempty" yaml:"default,omitempty"`
	Values          []string `json:"values,omitempty" yaml:"values,omitempty"`
	Min             *int64   `json:"min,omitempty" yaml:"min,omitempty"`
	Max             *int64   `json:"max,omitempty" yaml:"max,omitempty"`
	MaxLength       int      `json:"max_length,omitempty" yaml:"max_length,omitempty"`
	AllowedPrefixes []string `json:"allowed_prefixes,omitempty" yaml:"allowed_prefixes,omitempty"`
	Pattern         string   `json:"pattern,omitempty" yaml:"pattern,omitempty"`
}

// IsRequired: un paramètre est obligatoire par défaut.
func (p ParamDef) IsRequired() bool { return p.Required == nil || *p.Required }

// ParamError est une erreur de validation d'un paramètre; Denial() donne la raison journalisée.
type ParamError struct {
	Name string
	Msg  string
}

func (e *ParamError) Error() string  { return fmt.Sprintf("paramètre `%s`: %s", e.Name, e.Msg) }
func (e *ParamError) Denial() string { return "param_invalid:" + e.Name }

func perr(name, format string, a ...any) *ParamError {
	return &ParamError{Name: name, Msg: fmt.Sprintf(format, a...)}
}

// stringify convertit une valeur JSON en chaîne; seuls string et nombres entiers sont acceptés.
func stringify(name string, v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case json.Number:
		return x.String(), nil
	case float64:
		if x != math.Trunc(x) || math.Abs(x) > 1<<53 {
			return "", perr(name, "valeur numérique non entière")
		}
		return strconv.FormatInt(int64(x), 10), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	default:
		return "", perr(name, "type de valeur non supporté (%T)", v)
	}
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// Validate vérifie une valeur selon la définition et renvoie sa forme canonique (chaîne).
func (d ParamDef) Validate(name string, v any) (string, error) {
	s, err := stringify(name, v)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(s) || hasControl(s) { // inclut l'octet nul et les retours à la ligne
		return "", perr(name, "caractères de contrôle ou UTF-8 invalide")
	}
	if s == "" {
		return "", perr(name, "valeur vide")
	}
	switch d.Type {
	case TypeEnum:
		for _, ok := range d.Values {
			if s == ok {
				return s, nil
			}
		}
		return "", perr(name, "doit être l'une des valeurs: %s", strings.Join(d.Values, ", "))
	case TypeInt:
		if d.Min == nil || d.Max == nil {
			return "", perr(name, "définition invalide: bornes manquantes")
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != s {
			return "", perr(name, "doit être un entier (reçu : %.40s)", s)
		}
		if n < *d.Min || n > *d.Max {
			return "", perr(name, "doit être compris entre %d et %d (reçu : %d)", *d.Min, *d.Max, n)
		}
		return s, nil
	case TypeIdentifier:
		max := d.MaxLength
		if max <= 0 || max > defaultIdentifierMax {
			max = defaultIdentifierMax
		}
		if len(s) > max || !identifierRe.MatchString(s) || s[0] == '-' {
			return "", perr(name, "identifiant invalide (caractères A-Z a-z 0-9 . _ @ -, %d max, sans « - » initial)", max)
		}
		return s, nil
	case TypeService:
		if !serviceRe.MatchString(s) || s[0] == '-' {
			return "", perr(name, "nom de service invalide")
		}
		return s, nil
	case TypePath:
		if len(d.AllowedPrefixes) == 0 {
			return "", perr(name, "définition invalide: allowed_prefixes manquant")
		}
		if len(s) > maxPathLen || !strings.HasPrefix(s, "/") || path.Clean(s) != s || containsDotDot(s) {
			return "", perr(name, "doit être un chemin absolu normalisé, sans « .. »")
		}
		for _, pre := range d.AllowedPrefixes {
			pre = strings.TrimRight(pre, "/")
			if s == pre || strings.HasPrefix(s, pre+"/") {
				return s, nil
			}
		}
		return "", perr(name, "chemin hors des préfixes autorisés (%s)", strings.Join(d.AllowedPrefixes, ", "))
	case TypeRegex:
		max := d.MaxLength
		if max <= 0 {
			max = defaultRegexMax
		}
		if len(s) > max {
			return "", perr(name, "trop long (%d max)", max)
		}
		re, err := d.compile()
		if err != nil {
			return "", perr(name, "définition invalide: %v", err)
		}
		if s[0] == '-' || !re.MatchString(s) {
			return "", perr(name, "ne correspond pas au format attendu")
		}
		return s, nil
	}
	return "", perr(name, "type inconnu %q", d.Type)
}

func containsDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// compile ancre automatiquement le motif.
func (d ParamDef) compile() (*regexp.Regexp, error) {
	return regexp.Compile(`^(?:` + d.Pattern + `)$`)
}

// Check valide la définition elle-même (utilisé par le linter).
func (d ParamDef) Check(name string) []string {
	var errs []string
	if !paramNameRe.MatchString(name) {
		errs = append(errs, fmt.Sprintf("nom de paramètre invalide: %q", name))
	}
	switch d.Type {
	case TypeEnum:
		if len(d.Values) == 0 {
			errs = append(errs, "enum: `values` ne peut pas être vide")
		}
	case TypeInt:
		if d.Min == nil || d.Max == nil {
			errs = append(errs, "int: `min` et `max` sont obligatoires")
		} else if *d.Min > *d.Max {
			errs = append(errs, "int: min > max")
		}
	case TypePath:
		if len(d.AllowedPrefixes) == 0 {
			errs = append(errs, "path: `allowed_prefixes` est obligatoire")
		}
		for _, p := range d.AllowedPrefixes {
			if !strings.HasPrefix(p, "/") || containsDotDot(p) || path.Clean(p) != strings.TrimRight(p, "/") && p != "/" {
				errs = append(errs, fmt.Sprintf("path: préfixe invalide %q (absolu, normalisé)", p))
			}
			if p == "/" {
				errs = append(errs, "path: le préfixe « / » autoriserait tout le système de fichiers")
			}
		}
	case TypeRegex:
		if d.Pattern == "" {
			errs = append(errs, "regex: `pattern` est obligatoire")
		} else if _, err := d.compile(); err != nil {
			errs = append(errs, fmt.Sprintf("regex: motif invalide: %v", err))
		}
	case TypeIdentifier, TypeService:
	default:
		errs = append(errs, fmt.Sprintf("type de paramètre inconnu: %q", d.Type))
		return errs
	}
	if d.Default != nil && len(errs) == 0 {
		if _, err := d.Validate(name, d.Default); err != nil {
			errs = append(errs, "valeur par défaut invalide: "+err.Error())
		}
	}
	return errs
}
