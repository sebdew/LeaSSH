package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ARC-03: les routes appellent des services ou le paquet store; aucune requête SQL n'est écrite dans internal/api
// (constitution, principe 4: pas de règle métier dans une route ni dans une requête SQL).
func TestNoSQLInAPILayer(t *testing.T) {
	sql := regexp.MustCompile(`\.(QueryContext|ExecContext|QueryRowContext|PrepareContext)\(`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if filepath.Ext(f) != ".go" || len(f) > 8 && f[len(f)-8:] == "_test.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, loc := range sql.FindAllIndex(src, -1) {
			t.Errorf("%s: requête SQL dans la couche API (offset %d): déplacer vers internal/store ou un service", f, loc[0])
		}
	}
}

// ARC-03: les handlers n'appellent plus `store` directement, ils passent par des services. La migration se fait par
// tranches: ce « cliquet » plafonne, par fichier, le nombre d'appels directs (s.st.X, store.Fonction(…)). Un plafond ne
// peut que baisser; un fichier absent de la table doit être à zéro. Quand une tranche est migrée, baisser le plafond.
func TestHandlersCallServicesNotStore(t *testing.T) {
	ceiling := map[string]int{
		"api.go": 8, "handlers_access.go": 0, "handlers_auditreports.go": 9, "handlers_catalog.go": 20,
		"handlers_dashboard.go": 8, "handlers_exec.go": 7, "handlers_infra.go": 27, "handlers_journal.go": 2, // store.FormatTime (formatage pur)
		"handlers_oidc.go": 0, "handlers_packs.go": 8, "handlers_provisioning.go": 12, "handlers_settings.go": 1,
		"handlers_syslog.go": 2, "handlers_users.go": 0, "helpers.go": 2, "routes.go": 1,
	}
	call := regexp.MustCompile(`\bs\.st\.[A-Z]|\bstore\.[A-Z]\w*\(`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(call.FindAll(src, -1)); n > ceiling[f] {
			t.Errorf("%s: %d appels directs à store (plafond %d): passer par un service (internal/accounts, …)", f, n, ceiling[f])
		}
	}
}
