package auditreport

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/leassh-dev/leassh/internal/store"
)

//go:embed report.html.tmpl
var reportTmpl string

var labels = map[string]string{
	"succeeded": "Réussie", "failed": "En échec", "denied": "Refusée", "pending_approval": "En attente", "rejected": "Rejetée", "expired": "Expirée", "running": "En cours", "timeout": "Délai dépassé", "error": "Erreur",
	"read": "Lecture", "write": "Écriture", "destructive": "Destructive",
	"high": "Élevée", "medium": "Moyenne", "low": "Faible", "info": "Information",
	"points": "Points d'attention", "mineurs": "Points mineurs", "elements": "Éléments de preuve",
	"active": "Active", "disabled": "Désactivée", "pinned": "Épinglée", "unknown": "Inconnue", "mismatch": "Divergente",
	"enabled": "Activée", "pending": "En attente", "manual": "Manuelle", "pack": "Pack", "proposal": "Proposition",
	"admin": "Administrateur", "operator": "Opérateur", "viewer": "Lecteur",
	"user.login_failed": "Échecs de connexion à l'interface", "key.auth_failed": "Jetons MCP refusés", "host_key.mismatch": "Divergences de clé d'hôte",
	"no_matching_rule": "Aucune règle ne l'autorise", "profile_forbids_risk": "Profil lecture seule face à une écriture", "key_inactive": "Clé désactivée ou révoquée", "key_expired": "Clé expirée",
	"server_disabled": "Serveur désactivé", "host_key_not_pinned": "Clé d'hôte non épinglée", "command_not_enabled": "Commande non activée", "reason_invalid": "Motif manquant ou invalide",
	"target_not_found": "Serveur ou commande introuvable", "param_invalid": "Paramètre invalide", "ssh_cooldown": "Pause après échec d'authentification SSH", "hash_mismatch": "Commande modifiée depuis la demande",
	"revalidation_failed": "Droits perdus avant l'exécution", "actor_forbidden": "Décision non autorisée", "host_key_mismatch": "Clé d'hôte divergente", "identity_error": "Identité SSH inutilisable",
	"render_failed": "Rendu de la commande impossible", "ssh_error": "Erreur SSH",
}

func label(k string) string {
	if v, ok := labels[k]; ok {
		return v
	}
	return k
}

func fmtTS(s string) string {
	if s == "" {
		return "—"
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		return s
	}
	return t.UTC().Format("02/01/2006 15:04") + " UTC"
}

func fmtDay(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("02/01/2006")
}

func fmtDur(sec int64) string {
	switch {
	case sec <= 0:
		return "—"
	case sec < 60:
		return fmt.Sprintf("%d s", sec)
	case sec < 3600:
		return fmt.Sprintf("%d min %02d s", sec/60, sec%60)
	default:
		return fmt.Sprintf("%d h %02d min", sec/3600, (sec%3600)/60)
	}
}

func pct(n, total int) string {
	if total <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f %%", float64(n)*100/float64(total))
}

func yesno(b bool) string {
	if b {
		return "oui"
	}
	return "non"
}

var funcs = template.FuncMap{
	"label": label, "ts": fmtTS, "day": fmtDay, "dur": fmtDur, "pct": pct, "yesno": yesno,
	"join": func(s []string) string { return strings.Join(s, ", ") },
	"sub":  func(a, b int) int { return a - b },
	"short": func(s string) string {
		if len(s) > 16 {
			return s[:16] + "…"
		}
		return s
	},
	"paras": func(s string) []string {
		var out []string
		for _, p := range strings.Split(strings.TrimSpace(s), "\n\n") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	},
}

var tmpl = template.Must(template.New("report").Funcs(funcs).Parse(reportTmpl))

type view struct {
	Snapshot     Snapshot
	Report       store.AuditReport
	Summary      string
	SummaryModel string
	SummaryAt    string
	High, Medium int
	Low          int
}

// RenderHTML produit la page autonome (CSS intégré, aucun script, aucune ressource externe), imprimable en PDF depuis le navigateur.
func RenderHTML(r store.AuditReport) ([]byte, error) {
	var snap Snapshot
	if err := json.Unmarshal([]byte(r.Snapshot), &snap); err != nil {
		return nil, fmt.Errorf("lecture du rapport: %w", err)
	}
	h, m, l := Counts(snap.Findings)
	v := view{Snapshot: snap, Report: r, Summary: r.Summary, SummaryModel: r.SummaryModel, SummaryAt: r.SummaryAt, High: h, Medium: m, Low: l}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, v); err != nil {
		return nil, fmt.Errorf("rendu du rapport: %w", err)
	}
	return b.Bytes(), nil
}
