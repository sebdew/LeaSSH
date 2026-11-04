// Package syslog exporte les exécutions et les événements d'administration vers un serveur syslog
// (UDP, TCP ou TCP+TLS; RFC 5424; message texte, JSON ou CEF), SPEC §11bis.
//
// Invariants: jamais de secret ni de sortie de commande (stdout/stderr) dans les messages; les champs libres
// sont tronqués et passent par Redact; l'envoi ne bloque jamais l'appelant (file mémoire bornée).
package syslog

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Facilities RFC 5424 (index = code).
var facilities = []string{"kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "ntp", "security", "console", "clock",
	"local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"}

// Severities RFC 5424 (index = code).
var severities = []string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}

func Facilities() []string { return append([]string(nil), facilities...) }
func Severities() []string { return append([]string(nil), severities...) }

func indexOf(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}

// Field est un champ ordonné d'un événement.
type Field struct {
	K string
	V any
}

// Event est un événement prêt à être mis en forme (indépendant de la configuration d'envoi).
type Event struct {
	Time     time.Time
	Type     string // execution.denied, proposal.created, audit.server.create…
	Severity int
	Fields   []Field
	AuditSeq int64 // > 0 pour un événement d'audit (curseur de reprise)
}

// Category: premier segment du type (execution | proposal | audit | syslog).
func (e Event) Category() string {
	c, _, _ := strings.Cut(e.Type, ".")
	return c
}

const (
	maxField = 256
	maxDiff  = 1024
)

var secretText = regexp.MustCompile(`(?i)(pass(?:word|wd)?|secret|token|api[_-]?key|private[_-]?key|authorization|bearer)(\s*[=:]\s*|\s+)("[^"]*"|'[^']*'|\S+)`)

// Redact masque les valeurs qui suivent un mot sensible (password=…, token: …) dans un texte libre (invariant 7).
func Redact(s string) string {
	return secretText.ReplaceAllString(s, "${1}${2}***")
}

// clean: texte libre → une seule ligne, sans caractère de contrôle, tronqué, sans secret.
func clean(s string, max int) string {
	s = Redact(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == utf8.RuneError, r < 0x20, r == 0x7f:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > max {
		out = strings.ToValidUTF8(out[:max], "") + "…"
	}
	return out
}

// ---- RFC 5424 ----

var hostname = func() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "-"
	}
	return h
}

// token5424 garde les caractères ASCII imprimables (hors espace) et borne la longueur (champs HOSTNAME, APP-NAME, MSGID).
func token5424(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if r > '!'-1 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > max {
		out = out[:max]
	}
	if out == "" {
		return "-"
	}
	return out
}

// Render produit le message syslog complet (sans cadrage TCP).
func (r *runtime) Render(e Event) []byte {
	pri := indexOf(facilities, r.cfg.Facility)*8 + e.Severity
	var msg string
	switch r.cfg.MessageFormat {
	case "json":
		msg = renderJSON(r.cfg, e)
	case "cef":
		msg = renderCEF(r.version, e)
	default:
		msg = renderText(e)
	}
	host := r.cfg.Hostname
	if host == "" {
		host = hostname()
	}
	return []byte(fmt.Sprintf("<%d>1 %s %s %s %d %s - %s", pri, e.Time.UTC().Format("2006-01-02T15:04:05.000Z"), token5424(host, 255),
		token5424(r.cfg.AppName, 48), os.Getpid(), token5424(e.Type, 32), msg))
}

func severityName(n int) string {
	if n < 0 || n >= len(severities) {
		return "info"
	}
	return severities[n]
}

func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return clean(x, maxField)
	case nil:
		return ""
	case json.RawMessage:
		return clean(string(x), maxDiff)
	default:
		return clean(fmt.Sprint(x), maxField)
	}
}

// ---- texte: clé=valeur ----

func quoteKV(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \"=\\") {
		return s
	}
	return strconv.Quote(s)
}

func renderText(e Event) string {
	var b strings.Builder
	b.WriteString(e.Type)
	for _, f := range e.Fields {
		if s := scalar(f.V); s != "" {
			b.WriteString(" " + f.K + "=" + quoteKV(s))
		}
	}
	return b.String()
}

// ---- JSON ----

func renderJSON(c Config, e Event) string {
	m := map[string]any{"app": c.AppName, "event": e.Type, "category": e.Category(), "severity": severityName(e.Severity), "time": e.Time.UTC().Format(time.RFC3339Nano)}
	for _, f := range e.Fields {
		switch v := f.V.(type) {
		case int, int64, bool:
			m[f.K] = v
		default:
			if s := scalar(v); s != "" {
				m[f.K] = s
			}
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return `{"event":"` + e.Type + `"}`
	}
	return string(b)
}

// ---- CEF ----

var cefMap = map[string]string{"id": "externalId", "agent": "suser", "actor": "suser", "client_ip": "src", "ip": "src", "server": "dvchost", "command": "act", "action": "act", "status": "outcome"}

func cefHeader(s string) string {
	return strings.NewReplacer(`\`, `\\`, `|`, `\|`).Replace(clean(s, maxField))
}

func cefExt(s string) string {
	return strings.NewReplacer(`\`, `\\`, `=`, `\=`).Replace(s)
}

// cefSeverity: syslog 0..7 → CEF 0..10.
func cefSeverity(sev int) int {
	return [...]int{10, 9, 8, 7, 5, 4, 3, 1}[min(max(sev, 0), 7)]
}

func renderCEF(version string, e Event) string {
	var ext []string
	var rest []Field
	ext = append(ext, "rt="+strconv.FormatInt(e.Time.UnixMilli(), 10))
	for _, f := range e.Fields {
		s := scalar(f.V)
		if s == "" {
			continue
		}
		if k, ok := cefMap[f.K]; ok {
			ext = append(ext, k+"="+cefExt(s))
		} else {
			rest = append(rest, Field{f.K, s})
		}
	}
	var over []string
	for i, f := range rest {
		if i < 6 {
			ext = append(ext, fmt.Sprintf("cs%dLabel=%s cs%d=%s", i+1, f.K, i+1, cefExt(f.V.(string))))
		} else {
			over = append(over, f.K+"="+f.V.(string))
		}
	}
	if len(over) > 0 {
		ext = append(ext, "msg="+cefExt(strings.Join(over, " ")))
	}
	return fmt.Sprintf("CEF:0|Leassh|Leassh|%s|%s|%s|%d|%s", cefHeader(version), cefHeader(e.Type), cefHeader(e.Type), cefSeverity(e.Severity), strings.Join(ext, " "))
}
