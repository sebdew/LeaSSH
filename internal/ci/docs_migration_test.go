package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// #65 : la documentation utilisateur vit dans docs/fr et docs/en (plus dans le wiki).

// pagesUtilisateur : pages attendues dans chaque langue (même nom de fichier que la page du wiki, sans préfixe).
var pagesFR = []string{
	"README", "presentation", "installation", "premiers-pas", "manuel-utilisateur", "connexion",
	"tableau-de-bord", "journal", "validations", "propositions", "serveurs", "identites-ssh",
	"provisioning", "commandes", "packs", "regles-d-acces", "cles-mcp", "simulateur", "audit",
	"reglages", "agents-mcp", "securite", "sso", "api", "depannage", "structure-du-depot", "contribuer",
}

var pagesEN = []string{
	"README", "overview", "installation", "getting-started", "user-manual", "sign-in",
	"dashboard", "log", "approvals", "proposals", "servers", "ssh-identities",
	"provisioning", "commands", "packs", "access-rules", "mcp-keys", "simulator", "audit",
	"settings", "mcp-agents", "security", "sso", "operations", "architecture", "api-reference",
	"troubleshooting", "repository-layout", "contributing",
}

func TestDocumentationUtilisateurDansDocs(t *testing.T) {
	for lang, pages := range map[string][]string{"fr": pagesFR, "en": pagesEN} {
		for _, p := range pages {
			lire(t, filepath.Join("docs", lang, p+".md"))
		}
	}
	for _, lang := range []string{"fr", "en"} {
		m, _ := filepath.Glob(filepath.Join(racine, "docs/images", lang, "*.png"))
		if len(m) < 30 {
			t.Errorf("docs/images/%s : captures d'écran attendues (%d trouvées)", lang, len(m))
		}
	}
}

// Le wiki est désactivé : plus aucune référence dans le dépôt (l'historique CHANGELOG et les revues sont conservés tels quels).
func TestAucuneReferenceAuWiki(t *testing.T) {
	fichiers := []string{"README.md", "README.en.md", "CONTRIBUTING.md", "CLAUDE.md", "Dockerfile", "docker-compose.yml", "docs/openapi.json"}
	_ = filepath.WalkDir(filepath.Join(racine, "docs"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") || strings.Contains(p, "/revues/") {
			return nil
		}
		r, _ := filepath.Rel(racine, p)
		fichiers = append(fichiers, r)
		return nil
	})
	re := regexp.MustCompile(`(?i)wiki`)
	for _, f := range fichiers {
		for i, l := range strings.Split(lire(t, f), "\n") {
			if re.MatchString(l) {
				t.Errorf("%s:%d : référence au wiki à retirer : %s", f, i+1, strings.TrimSpace(l))
			}
		}
	}
}

// Liens relatifs et images des pages utilisateur : la cible existe.
func TestLiensEtImagesDesPagesUtilisateur(t *testing.T) {
	lien := regexp.MustCompile(`!?\[[^\]]*\]\(([^)#\s]+)(#[^)]*)?\)`)
	for _, lang := range []string{"fr", "en"} {
		m, _ := filepath.Glob(filepath.Join(racine, "docs", lang, "*.md"))
		for _, p := range m {
			r, _ := filepath.Rel(racine, p)
			for _, l := range lien.FindAllStringSubmatch(lire(t, r), -1) {
				c := l[1]
				if strings.Contains(c, "://") || strings.HasPrefix(c, "mailto:") {
					continue
				}
				if strings.HasPrefix(c, "/") {
					t.Errorf("%s : lien absolu (non portable) %s", r, c)
					continue
				}
				if _, err := os.Stat(filepath.Join(racine, filepath.Dir(r), c)); err != nil {
					t.Errorf("%s : cible introuvable %s", r, c)
				}
			}
		}
	}
}
