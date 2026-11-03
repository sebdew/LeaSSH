package auditreport

// Control: une ligne de la correspondance indicative entre ce que LeaSSH établit et un contrôle de référentiel.
// Statut: « points » = au moins un constat de gravité moyenne ou haute; « mineurs » = constats faibles seulement;
// « elements » = éléments de preuve disponibles, sans constat. Ce n'est JAMAIS un verdict de conformité.
type Control struct {
	Framework string   `json:"framework"` // ISO 27001:2022 | HDS
	Key       string   `json:"key"`       // clé de rattachement des constats
	ID        string   `json:"id"`        // numéro du contrôle (vide pour les thèmes HDS)
	Title     string   `json:"title"`
	Evidence  []string `json:"evidence"`
	Findings  []string `json:"findings"` // codes des constats liés
	Status    string   `json:"status"`
}

type controlDef struct {
	framework, key, id, title string
	evidence                  []string
}

// Contrôles de l'annexe A de l'ISO/IEC 27001:2022 pertinents pour un outil d'accès SSH médié.
// Les thèmes HDS sont donnés sans numérotation: le référentiel HDS s'appuie sur l'ISO 27001 et ajoute des exigences propres à
// l'hébergement de données de santé; ce rapport n'en cite aucune clause précise.
var controlDefs = []controlDef{
	{"ISO 27001:2022", "ISO 5.15", "5.15", "Contrôle d'accès", []string{"Règles d'accès par clé, groupe de serveurs, groupe de commandes et profil (RO/RW)", "Simulateur d'accès: une seule fonction d'autorisation pour l'interface, MCP et exécution"}},
	{"ISO 27001:2022", "ISO 5.16", "5.16", "Gestion des identités", []string{"Inventaire des comptes d'administration, rôles, dernière connexion", "Inventaire des clés MCP, des identités SSH et de leur méthode d'authentification"}},
	{"ISO 27001:2022", "ISO 5.17", "5.17", "Informations d'authentification", []string{"Jetons MCP stockés sous forme de hash, mots de passe en argon2id, secrets chiffrés (AES-256-GCM)", "Expiration des clés MCP"}},
	{"ISO 27001:2022", "ISO 5.18", "5.18", "Droits d'accès", []string{"Liste des clés avec leurs règles et leur dernière utilisation (revue des droits)", "Clés désactivées, expirées ou révoquées"}},
	{"ISO 27001:2022", "ISO 5.28", "5.28", "Collecte de preuves", []string{"Journal des exécutions et journal d'administration chaînés par hash SHA-256", "Vérification d'intégrité et tête de chaîne dans ce rapport"}},
	{"ISO 27001:2022", "ISO 5.33", "5.33", "Protection des enregistrements", []string{"Rétention configurée des journaux", "Intégrité vérifiée des deux journaux, purges tracées (logs.purge)"}},
	{"ISO 27001:2022", "ISO 8.2", "8.2", "Droits d'accès privilégiés", []string{"Commandes d'écriture et destructives, validation humaine obligatoire pour les destructives", "Exécutions destructives de la période avec décideur et délai", "Règles RW avec ou sans validation"}},
	{"ISO 27001:2022", "ISO 8.3", "8.3", "Restriction d'accès à l'information", []string{"Catalogue de commandes validées, paramètres typés (aucun texte libre), échappement systématique", "Cloisonnement par groupes de serveurs et de commandes"}},
	{"ISO 27001:2022", "ISO 8.5", "8.5", "Authentification sécurisée", []string{"Authentification des agents par jeton (hash, comparaison à temps constant), pause après échecs", "Échecs d'authentification de la période, HTTPS de l'interface"}},
	{"ISO 27001:2022", "ISO 8.9", "8.9", "Gestion de la configuration", []string{"Inventaire des serveurs, groupes, commandes et règles", "Clés d'hôte épinglées, serveurs synchronisés et leur source"}},
	{"ISO 27001:2022", "ISO 8.15", "8.15", "Journalisation", []string{"Toute demande d'exécution est journalisée, y compris les refus (motif, clé, serveur, commande, décideur)", "Journal d'administration de toutes les modifications de configuration"}},
	{"ISO 27001:2022", "ISO 8.16", "8.16", "Activités de surveillance", []string{"Tableau de bord, indicateurs /metrics, webhook signé pour les alertes", "Refus, échecs et événements de sécurité de la période"}},
	{"ISO 27001:2022", "ISO 8.24", "8.24", "Utilisation de la cryptographie", []string{"Secrets chiffrés avec une clé maître, rotation possible", "Clés d'hôte SSH épinglées (divergence = accès bloqué), canal SSH exec uniquement"}},
	{"ISO 27001:2022", "ISO 8.32", "8.32", "Gestion des changements", []string{"Journal d'administration des changements de la période avec auteur et horodatage", "Propositions de commandes soumises à revue humaine, packs importés avec aperçu"}},
	{"HDS", "HDS traçabilité", "", "Traçabilité des accès et des actions sur les systèmes", []string{"Journal des exécutions et journal d'administration, horodatés et attribués (clé, acteur)", "Chaînes de hash vérifiées"}},
	{"HDS", "HDS habilitations", "", "Gestion des habilitations et des accès à privilèges", []string{"Règles d'accès, revue des clés et des comptes, validation humaine des actions à risque"}},
	{"HDS", "HDS conservation", "", "Conservation et intégrité des traces", []string{"Rétention configurée, intégrité vérifiée, tête de chaîne conservée"}},
	{"HDS", "HDS cloisonnement", "", "Cloisonnement des accès aux systèmes d'information", []string{"Groupes de serveurs et de commandes, règles par clé, serveurs non listés invisibles pour une clé"}},
}

// MapControls rattache les constats aux contrôles par leur clé et calcule le statut de chaque ligne.
func MapControls(s Snapshot) []Control {
	out := make([]Control, 0, len(controlDefs))
	for _, d := range controlDefs {
		c := Control{Framework: d.framework, Key: d.key, ID: d.id, Title: d.title, Evidence: d.evidence, Findings: []string{}, Status: "elements"}
		for _, f := range s.Findings {
			for _, k := range f.Controls {
				if k != d.key {
					continue
				}
				c.Findings = append(c.Findings, f.Code)
				switch {
				case f.Severity == High || f.Severity == Medium:
					c.Status = "points"
				case f.Severity == Low && c.Status != "points":
					c.Status = "mineurs"
				}
			}
		}
		out = append(out, c)
	}
	return out
}
