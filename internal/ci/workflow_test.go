// Package ci vérifie que la chaîne qualité décrite par la constitution
// (principes 5, 7, 9 et 10) existe bien dans le dépôt.
package ci

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const workflowPath = "../../.gitea/workflows/ci.yml"

// Chaque contrôle exigé par le ticket #3 doit apparaître dans une commande ou une action du workflow.
var requiredChecks = map[string]string{
	"make lint":   "lint",
	"make test":   "tests",
	"gitleaks":    "détection de secrets (principe 5)",
	"trivy":       "scan de vulnérabilités (principe 7)",
	"govulncheck": "vulnérabilités Go",
	"npm audit":   "vulnérabilités npm",
}

func TestWorkflowCIExisteEtLanceLesControles(t *testing.T) {
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("le workflow CI est absent (%s) : %v", workflowPath, err)
	}

	var wf struct {
		On   any `yaml:"on"`
		Jobs map[string]struct {
			Steps []struct {
				Uses string `yaml:"uses"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("workflow invalide : %v", err)
	}
	if wf.On == nil {
		t.Error("le workflow n'a aucun déclencheur (on:)")
	}

	var all strings.Builder
	for _, job := range wf.Jobs {
		for _, s := range job.Steps {
			all.WriteString(s.Uses + " " + s.Run + "\n")
		}
	}
	for needle, why := range requiredChecks {
		if !strings.Contains(all.String(), needle) {
			t.Errorf("contrôle manquant dans la CI : %q (%s)", needle, why)
		}
	}

	// Le workflow doit tourner sur les merge requests ET sur main.
	for _, trigger := range []string{"pull_request", "push"} {
		if !strings.Contains(string(raw), trigger) {
			t.Errorf("déclencheur %q absent", trigger)
		}
	}
}
