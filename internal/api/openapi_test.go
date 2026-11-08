package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/leassh-dev/leassh/internal/settings"
	"github.com/leassh-dev/leassh/internal/store"
)

const specPath = "../../docs/openapi.json"

// ARC-04: la spécification OpenAPI est commitée, valide, et décrit exactement les routes du routeur.
func TestOpenAPISpecIsValid(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("lecture de %s: %v", specPath, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("spécification invalide: %v", err)
	}
}

func TestOpenAPIMatchesRoutes(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("lecture de %s: %v", specPath, err)
	}
	documented := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			documented[method+" "+path] = true
		}
	}

	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set, err := settings.New(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(set, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]bool{}
	err = chi.Walk(srv.Handler().(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		if route == "/mcp" { // transport MCP: décrit par le protocole MCP, pas par cette spécification
			return nil
		}
		routes[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var missing, extra []string
	for r := range routes {
		if !documented[r] {
			missing = append(missing, r)
		}
	}
	for d := range documented {
		if !routes[d] {
			extra = append(extra, d)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("routes absentes de %s:\n  %s", specPath, strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("opérations documentées mais sans route:\n  %s", strings.Join(extra, "\n  "))
	}
}
