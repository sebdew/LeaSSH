package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Ces tests vérifient la documentation et la traçabilité exigées par la constitution
// (principes 3, 6, 8, 10 et 11) : ticket #11, constats DOC-01 à DOC-09.

const racine = "../.."

func lire(t *testing.T, chemin string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(racine, chemin))
	if err != nil {
		t.Fatalf("document attendu absent (%s) : %v", chemin, err)
	}
	return string(b)
}

func TestREADMEDeclareLeNiveauEtRenvoieVersLesStandards(t *testing.T) { // DOC-01
	readme := lire(t, "README.md")
	if !regexp.MustCompile(`(?i)niveau\s*:\s*\**N[123]`).MatchString(readme) {
		t.Error("le README doit déclarer le niveau du projet (« Niveau : N3 »)")
	}
	if !strings.Contains(readme, "docs/standards/") {
		t.Error("le README doit renvoyer vers docs/standards/")
	}
}

func TestREADMERenvoieVersLaDocumentationEtOffreUneVersionAnglaise(t *testing.T) {
	readme := lire(t, "README.md")
	if !strings.Contains(readme, "docs/fr/") || !strings.Contains(readme, "docs/en/") {
		t.Error("le README doit renvoyer vers docs/fr/ et docs/en/")
	}
	if !strings.Contains(readme, "README.en.md") {
		t.Error("le README doit proposer la version anglaise")
	}
	en := lire(t, "README.en.md")
	if !regexp.MustCompile(`(?i)level\s*:\s*\**N[123]`).MatchString(en) {
		t.Error("README.en.md doit déclarer le niveau (« Level: N3 »)")
	}
}

func TestADRRetroactifs(t *testing.T) { // DOC-02
	for motif, objet := range map[string]string{
		"docs/adr/*stack*":       "choix de la stack",
		"docs/adr/*binaire*":     "binaire unique",
		"docs/adr/*authent*":     "authentification",
		"docs/adr/*integrite*":   "chaîne d'intégrité",
		"docs/adr/*synthese*ia*": "synthèse IA configurable en base",
	} {
		if m, _ := filepath.Glob(filepath.Join(racine, motif)); len(m) == 0 {
			t.Errorf("ADR manquant : %s (%s)", objet, motif)
		}
	}
}

func TestSpecificationsAPosteriori(t *testing.T) { // DOC-03
	for _, f := range []string{"sso", "rapport-audit", "provisioning"} {
		for _, doc := range []string{"spec.md", "plan.md", "tasks.md"} {
			lire(t, filepath.Join("docs/specs", f, doc))
		}
	}
}

func TestChangelogEtTraçabilite(t *testing.T) { // DOC-04, DOC-05
	c := lire(t, "CHANGELOG.md")
	if !regexp.MustCompile(`## \[0\.1\.0\]`).MatchString(c) {
		t.Error("CHANGELOG.md doit contenir l'entrée de la version 0.1.0")
	}
	if !strings.Contains(lire(t, "CONTRIBUTING.md"), "merge request") {
		t.Error("CONTRIBUTING.md doit décrire le flux ticket, branche, merge request")
	}
}

func TestArchitectureEtExploitation(t *testing.T) { // DOC-06
	a := lire(t, "docs/architecture.md")
	if !strings.Contains(a, "```mermaid") {
		t.Error("docs/architecture.md doit contenir un schéma Mermaid")
	}
	for _, mot := range []string{"conservation", "Frontières de confiance"} {
		if !strings.Contains(a, mot) {
			t.Errorf("docs/architecture.md doit traiter : %s", mot)
		}
	}
	e := lire(t, "docs/exploitation.md")
	for _, mot := range []string{"Retour arrière", "Test de restauration"} {
		if !strings.Contains(e, mot) {
			t.Errorf("docs/exploitation.md doit contenir la section : %s", mot)
		}
	}
}

func TestVersionsAlignees(t *testing.T) { // DOC-07
	gomod := lire(t, "go.mod")
	m := regexp.MustCompile(`(?m)^go (\d+\.\d+)`).FindStringSubmatch(gomod)
	if m == nil {
		t.Fatal("version de Go introuvable dans go.mod")
	}
	for _, f := range []string{"README.md", "CLAUDE.md"} {
		if !strings.Contains(lire(t, f), "Go ≥ "+m[1]) {
			t.Errorf("%s doit annoncer « Go ≥ %s » comme go.mod", f, m[1])
		}
	}
	if strings.Contains(lire(t, "CLAUDE.md"), "requêtes `sqlc`") {
		t.Error("CLAUDE.md annonce sqlc, abandonné par l'ADR 0003")
	}
}

func TestStandardsVersionnes(t *testing.T) { // DOC-08
	for _, f := range []string{"constitution.md", "checklist-revue.md", "guide-demarrage.md"} {
		lire(t, filepath.Join("docs/standards", f))
	}
}

// Les liens relatifs des documents du dépôt doivent pointer vers un fichier existant.
func TestLiensRelatifsValides(t *testing.T) {
	lien := regexp.MustCompile(`\]\(([^)#\s]+)(#[^)]*)?\)`)
	fichiers := []string{"README.md", "README.en.md", "CHANGELOG.md", "CONTRIBUTING.md", "CLAUDE.md",
		"docs/architecture.md", "docs/exploitation.md", "docs/README.md", "docs/specs/README.md", "docs/specs/SPEC.md"}
	for _, motif := range []string{"docs/adr/*.md", "docs/specs/*/*.md", "docs/fr/*.md", "docs/en/*.md"} {
		m, _ := filepath.Glob(filepath.Join(racine, motif))
		for _, p := range m {
			r, _ := filepath.Rel(racine, p)
			fichiers = append(fichiers, r)
		}
	}
	for _, f := range fichiers {
		for _, m := range lien.FindAllStringSubmatch(lire(t, f), -1) {
			cible := m[1]
			if strings.Contains(cible, "://") || strings.HasPrefix(cible, "mailto:") || strings.HasPrefix(cible, "/") {
				continue
			}
			if _, err := os.Stat(filepath.Join(racine, filepath.Dir(f), cible)); err != nil {
				t.Errorf("%s : lien relatif cassé vers %s", f, cible)
			}
		}
	}
}

// DOC : le README du dossier docs explique la structure du dépôt et renvoie vers la documentation utilisateur.
func TestREADMEDocsExpliqueLaStructure(t *testing.T) {
	d := lire(t, "docs/README.md")
	for _, mot := range []string{"Structure du dépôt", "Repository layout", "fr/README.md", "en/README.md", "specs/SPEC.md", "adr/"} {
		if !strings.Contains(d, mot) {
			t.Errorf("docs/README.md doit contenir : %s", mot)
		}
	}
	for _, f := range []string{"docs/INSTALL.md", "specs/README.md"} {
		if _, err := os.Stat(filepath.Join(racine, f)); err == nil && f == "docs/INSTALL.md" {
			t.Errorf("%s est obsolète (migré vers docs/fr et docs/en)", f)
		}
	}
}

// DOC-13 : références périmées dans le dépôt.
func TestReferencesDocumentairesAJour(t *testing.T) {
	if strings.Contains(lire(t, "docs/openapi.json"), "docs/SPEC.md") {
		t.Error("docs/openapi.json cite docs/SPEC.md, déplacé en docs/specs/SPEC.md")
	}
	for _, f := range []string{"README.md", "README.en.md"} {
		for _, l := range strings.Split(lire(t, f), "\n") {
			if strings.Contains(l, "webhook") && regexp.MustCompile(`(?i)signé HMAC|HMAC-signed`).MatchString(l) {
				t.Errorf("%s : le secret de signature du webhook est facultatif, ne pas annoncer « signé » sans réserve : %s", f, l)
			}
		}
	}
}

// DOC-11, DOC-12, DOC-15 : la documentation utilisateur ne contient ni commande inexécutable, ni URL factice, ni adresse privée.
func TestDocsUtilisateurSansCommandesInexecutablesNiAdressePrivee(t *testing.T) {
	interdits := map[string]*regexp.Regexp{
		"docker exec … leassh <sous-commande> (l'exécutable de l'image est /leassh, hors PATH)": regexp.MustCompile(`docker exec\b[^\n]*?\bleassh leassh `),
		"URL de binaire factice (aucune release publiée)":                                       regexp.MustCompile(`https://…/`),
		"adresse du réseau local (utiliser 192.0.2.0/24)":                                       regexp.MustCompile(`\b(192\.168|10\.\d+|172\.(1[6-9]|2\d|3[01]))\.\d+\.\d+`),
	}
	for _, motif := range []string{"docs/fr/*.md", "docs/en/*.md", "docs/exploitation.md"} {
		m, _ := filepath.Glob(filepath.Join(racine, motif))
		for _, p := range m {
			r, _ := filepath.Rel(racine, p)
			page := lire(t, r)
			for nom, re := range interdits {
				if re.MatchString(page) {
					t.Errorf("%s : %s", r, nom)
				}
			}
		}
	}
}

// DOC-10 : la note SEC-08 doit suivre le niveau du README (N3) et la décision sur les
// adresses privées doit être assumée par un ADR.
func TestNiveauCoherentEtDecisionSEC08(t *testing.T) {
	note := lire(t, "docs/securite-sorties-et-urls.md")
	if regexp.MustCompile(`N2|jamais sur Internet`).MatchString(note) {
		t.Error("docs/securite-sorties-et-urls.md déclare N2 / « jamais sur Internet » alors que le projet est N3")
	}
	m, _ := filepath.Glob(filepath.Join(racine, "docs/adr/*adresses-privees*"))
	if len(m) == 0 {
		t.Fatal("ADR manquant sur les adresses privées des URL sortantes (docs/adr/*adresses-privees*)")
	}
	if !strings.Contains(note, filepath.Base(m[0])) {
		t.Error("la note SEC-08 doit renvoyer vers l'ADR des adresses privées")
	}
	if !strings.Contains(lire(t, "docs/adr/README.md"), filepath.Base(m[0])) {
		t.Error("docs/adr/README.md doit lister l'ADR des adresses privées")
	}
}

// #45 : les fichiers de test restent à côté du code ; ce choix est assumé par un ADR
// et rappelé dans CONTRIBUTING.md pour ne pas relancer la question à chaque revue.
func TestADRTestsACoteDuCode(t *testing.T) {
	m, _ := filepath.Glob(filepath.Join(racine, "docs/adr/*tests-a-cote-du-code*"))
	if len(m) == 0 {
		t.Fatal("ADR manquant sur l'emplacement des fichiers de test (docs/adr/*tests-a-cote-du-code*)")
	}
	nom := filepath.Base(m[0])
	if !strings.Contains(lire(t, "docs/adr/README.md"), nom) {
		t.Error("docs/adr/README.md doit lister l'ADR sur l'emplacement des tests")
	}
	if !strings.Contains(lire(t, "CONTRIBUTING.md"), nom) {
		t.Error("CONTRIBUTING.md doit renvoyer vers l'ADR sur l'emplacement des tests")
	}
}

// DOC-05 (#52) : la revue par agent est consignée en commentaire de merge request ; l'approbation et la fusion restent humaines.
func TestContributingFixeLaTraceDeRevue(t *testing.T) {
	c := lire(t, "CONTRIBUTING.md")
	for _, mot := range []string{"commentaire de la merge request", "ni n'approuve ni ne fusionne"} {
		if !strings.Contains(c, mot) {
			t.Errorf("CONTRIBUTING.md doit préciser : %s", mot)
		}
	}
}
