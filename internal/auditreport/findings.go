package auditreport

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Severity string

const (
	High   Severity = "high"
	Medium Severity = "medium"
	Low    Severity = "low"
	Info   Severity = "info"
)

var severityRank = map[Severity]int{High: 0, Medium: 1, Low: 2, Info: 3}

// Finding: constat automatique, calculé par des règles fixes à partir de l'instantané (jamais par une IA).
type Finding struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Count    int      `json:"count"`
	Items    []string `json:"items"` // noms concernés (20 au maximum)
	Controls []string `json:"controls"`
}

const maxItems = 20

func items(in []string) []string {
	in = sortedNames(in)
	if len(in) > maxItems {
		rest := len(in) - maxItems
		in = append(in[:maxItems], fmt.Sprintf("… et %d autre(s)", rest))
	}
	return in
}

func older(ts string, cutoff string) bool { return ts != "" && ts < cutoff }

// Evaluate applique les règles de constat. L'ordre du résultat est stable: gravité puis code.
func Evaluate(s Snapshot) []Finding {
	var out []Finding
	add := func(f Finding) {
		if f.Items == nil {
			f.Items = []string{}
		}
		out = append(out, f)
	}
	now, _ := time.Parse("2006-01-02T15:04:05.000Z", s.GeneratedAt)
	cutoff := now.AddDate(0, 0, -unusedDays).UTC().Format("2006-01-02T15:04:05.000Z")

	// clés
	var noExp, unused, proposers []string
	for _, k := range s.Scope.Keys.Items {
		if k.State != "active" {
			continue
		}
		if k.ExpiresAt == "" {
			noExp = append(noExp, k.Name)
		}
		if (k.LastUsedAt == "" && older(k.CreatedAt, cutoff)) || older(k.LastUsedAt, cutoff) {
			unused = append(unused, k.Name)
		}
		if k.CanPropose {
			proposers = append(proposers, k.Name)
		}
	}
	if len(noExp) > 0 {
		add(Finding{Code: "key_no_expiry", Severity: Medium, Title: "Clés MCP actives sans date d'expiration", Count: len(noExp), Items: items(noExp),
			Detail: "Une clé sans expiration reste valable indéfiniment si elle fuit. Définir une échéance et la renouveler.", Controls: []string{"ISO 5.17", "ISO 8.5"}})
	}
	if len(unused) > 0 {
		add(Finding{Code: "key_unused_90d", Severity: Medium, Title: fmt.Sprintf("Clés MCP actives inutilisées depuis plus de %d jours", unusedDays), Count: len(unused), Items: items(unused),
			Detail: "Les droits non utilisés devraient être retirés (revue périodique des droits d'accès): désactiver ou révoquer ces clés.", Controls: []string{"ISO 5.18", "HDS habilitations"}})
	}
	if len(proposers) > 0 {
		add(Finding{Code: "key_can_propose", Severity: Info, Title: "Clés autorisées à proposer des commandes", Count: len(proposers), Items: items(proposers),
			Detail: "Ces clés peuvent soumettre de nouvelles commandes à la revue d'un administrateur (aucune n'est utilisable avant approbation humaine).", Controls: []string{"ISO 8.32"}})
	}

	// règles
	var rw []string
	for _, r := range s.Scope.Rules.Items {
		if r.Enabled && r.Profile == "RW" && !r.Approval {
			rw = append(rw, r.Name)
		}
	}
	if len(rw) > 0 {
		add(Finding{Code: "rule_rw_no_approval", Severity: High, Title: "Règles en lecture/écriture sans validation humaine", Count: len(rw), Items: items(rw),
			Detail: "Ces règles laissent des agents exécuter des commandes d'écriture sans approbation. Les commandes destructives exigent toujours une validation, mais pas les écritures.", Controls: []string{"ISO 5.15", "ISO 5.18", "ISO 8.2", "HDS habilitations"}})
	}
	if s.Activity.WriteNoApproval > 0 {
		add(Finding{Code: "write_executed_no_approval", Severity: Low, Title: "Commandes d'écriture exécutées sans validation humaine pendant la période", Count: s.Activity.WriteNoApproval,
			Detail: fmt.Sprintf("%d exécution(s) de commandes d'écriture menées à terme sans décision humaine, autorisées par une règle sans validation.", s.Activity.WriteNoApproval), Controls: []string{"ISO 8.2", "ISO 8.15"}})
	}

	// serveurs
	var mismatch, unknown []string
	for _, sv := range s.Scope.Servers.Items {
		if !sv.Enabled {
			continue
		}
		switch sv.HostKey {
		case "mismatch":
			mismatch = append(mismatch, sv.Slug)
		case "pinned":
		default:
			unknown = append(unknown, sv.Slug)
		}
	}
	if len(mismatch) > 0 {
		add(Finding{Code: "server_hostkey_mismatch", Severity: High, Title: "Serveurs dont la clé d'hôte a changé (accès bloqué)", Count: len(mismatch), Items: items(mismatch),
			Detail: "La clé SSH présentée diffère de celle épinglée: changement légitime à valider, ou usurpation possible. L'accès reste bloqué tant qu'un administrateur n'a pas tranché.", Controls: []string{"ISO 8.9", "ISO 8.24"}})
	}
	if len(unknown) > 0 {
		add(Finding{Code: "server_unpinned", Severity: Medium, Title: "Serveurs actifs sans clé d'hôte épinglée", Count: len(unknown), Items: items(unknown),
			Detail: "Sans clé d'hôte validée, la connexion est refusée: le serveur est inutilisable et la configuration est incomplète. Épingler la clé après vérification de l'empreinte.", Controls: []string{"ISO 8.9", "ISO 8.24"}})
	}

	// utilisateurs
	var inactive []string
	for _, u := range s.Scope.Users.Items {
		if u.Disabled || u.Role != "admin" {
			continue
		}
		if (u.LastLoginAt == "" && older(u.CreatedAt, cutoff)) || older(u.LastLoginAt, cutoff) {
			inactive = append(inactive, u.Username)
		}
	}
	if len(inactive) > 0 {
		add(Finding{Code: "admin_inactive", Severity: Medium, Title: fmt.Sprintf("Comptes administrateurs actifs sans connexion depuis plus de %d jours", unusedDays), Count: len(inactive), Items: items(inactive),
			Detail: "Un compte à privilèges inutilisé est une surface d'attaque: le désactiver ou le supprimer.", Controls: []string{"ISO 5.16", "ISO 5.18", "ISO 8.2", "HDS habilitations"}})
	}
	if s.Scope.Users.Admins == 1 {
		add(Finding{Code: "single_admin", Severity: Low, Title: "Un seul administrateur actif", Count: 1,
			Detail: "Aucune séparation des tâches ni continuité si ce compte est indisponible: prévoir au moins un second administrateur de confiance.", Controls: []string{"ISO 5.16", "ISO 8.2"}})
	}
	if s.Scope.Identities.Password > 0 {
		add(Finding{Code: "identity_password_auth", Severity: Low, Title: "Identités SSH par mot de passe", Count: s.Scope.Identities.Password,
			Detail: "L'authentification par clé est préférable au mot de passe pour les accès automatisés.", Controls: []string{"ISO 5.17", "ISO 8.5"}})
	}

	// commandes proposées par l'IA et approuvées
	var prop []string
	for _, c := range s.Scope.Commands.Privileged {
		if c.Source == "proposal" && c.Status == "enabled" {
			prop = append(prop, c.Slug)
		}
	}
	if n := s.Scope.Commands.BySource["proposal"]; n > 0 {
		add(Finding{Code: "proposal_commands_approved", Severity: Info, Title: "Commandes issues de propositions d'agents et approuvées", Count: n, Items: items(prop),
			Detail: fmt.Sprintf("%d commande(s) du catalogue proviennent d'une proposition d'agent, approuvée par un administrateur (voir le journal d'administration: proposal.approve). Dont %d d'écriture ou destructives (voir la liste).", n, len(prop)), Controls: []string{"ISO 8.32", "ISO 5.15"}})
	}

	// journalisation et intégrité
	var broken []string
	if !s.Integrity.Executions.Verified.OK {
		broken = append(broken, "journal des exécutions")
	}
	if !s.Integrity.Admin.Verified.OK {
		broken = append(broken, "journal d'administration")
	}
	if len(broken) > 0 {
		add(Finding{Code: "integrity_failed", Severity: High, Title: "Rupture d'intégrité d'un journal", Count: len(broken), Items: broken,
			Detail: "La chaîne de hash d'un journal est rompue (contenu altéré, ligne supprimée ou trou de séquence). Les preuves de ce journal ne sont plus fiables.", Controls: []string{"ISO 5.28", "ISO 5.33", "ISO 8.15", "HDS traçabilité", "HDS conservation"}})
	}
	if s.Config.RetentionDays < 365 {
		add(Finding{Code: "retention_short", Severity: Medium, Title: "Rétention des journaux inférieure à 365 jours", Count: 1,
			Detail: fmt.Sprintf("La rétention configurée est de %d jours (Réglages → Général). Repère indicatif: 12 mois; HDS et certaines politiques exigent davantage.", s.Config.RetentionDays), Controls: []string{"ISO 5.33", "ISO 8.15", "HDS traçabilité", "HDS conservation"}})
	}
	if s.Integrity.PurgeEvents > 0 {
		add(Finding{Code: "logs_purged", Severity: Info, Title: "Purges de journaux pendant la période", Count: s.Integrity.PurgeEvents,
			Detail: "La rétention a supprimé des lignes anciennes (événement logs.purge): la première ligne conservée sert d'ancre à la chaîne.", Controls: []string{"ISO 5.33", "ISO 8.15"}})
	}

	// transport et supervision
	if !s.Config.BaseURLHTTPS && !localhost(s.Config.BaseURL) {
		add(Finding{Code: "base_url_http", Severity: Medium, Title: "Interface d'administration servie sans HTTPS", Count: 1,
			Detail: "L'URL publique configurée (" + s.Config.BaseURL + ") n'est pas en HTTPS: mots de passe et jetons transitent en clair. Placer LeaSSH derrière un reverse proxy TLS.", Controls: []string{"ISO 8.5", "ISO 8.24"}})
	}
	if !s.Config.WebhookEnabled {
		add(Finding{Code: "webhook_disabled", Severity: Low, Title: "Aucune alerte externe (webhook) configurée", Count: 1,
			Detail: "Les demandes de validation et propositions ne sont signalées par aucun canal externe; la supervision repose sur la consultation de l'interface ou de /metrics.", Controls: []string{"ISO 8.16"}})
	}
	var perr []string
	for _, p := range s.Scope.Provisioners {
		if p.Enabled && p.LastStatus == "error" {
			perr = append(perr, p.Name)
		}
	}
	if len(perr) > 0 {
		add(Finding{Code: "provisioner_error", Severity: Low, Title: "Synchronisations de provisioning en échec", Count: len(perr), Items: items(perr),
			Detail: "L'inventaire des serveurs peut être obsolète tant que la dernière synchronisation échoue.", Controls: []string{"ISO 8.9"}})
	}
	if sec := securityCount(s, "user.login_failed") + securityCount(s, "key.auth_failed"); sec >= 20 {
		add(Finding{Code: "many_auth_failures", Severity: Low, Title: "Nombreux échecs d'authentification pendant la période", Count: sec,
			Detail: fmt.Sprintf("%d échec(s) de connexion ou de jeton refusé: vérifier s'il s'agit d'une erreur de configuration ou d'une tentative d'intrusion.", sec), Controls: []string{"ISO 8.15", "ISO 8.16", "ISO 8.5"}})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if severityRank[out[i].Severity] != severityRank[out[j].Severity] {
			return severityRank[out[i].Severity] < severityRank[out[j].Severity]
		}
		return out[i].Code < out[j].Code
	})
	if out == nil {
		out = []Finding{}
	}
	return out
}

func securityCount(s Snapshot, name string) int {
	for _, c := range s.Activity.Security {
		if c.Name == name {
			return c.Count
		}
	}
	return 0
}

func localhost(base string) bool {
	b := strings.ToLower(base)
	for _, h := range []string{"//localhost", "//127.0.0.1", "//[::1]"} {
		if strings.Contains(b, h) {
			return true
		}
	}
	return b == ""
}

// Counts renvoie le nombre de constats par gravité (hors information).
func Counts(fs []Finding) (high, medium, low int) {
	for _, f := range fs {
		switch f.Severity {
		case High:
			high++
		case Medium:
			medium++
		case Low:
			low++
		}
	}
	return
}
