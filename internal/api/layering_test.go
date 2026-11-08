package api

import (
	"os"
	"path/filepath"
	"regexp"
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
