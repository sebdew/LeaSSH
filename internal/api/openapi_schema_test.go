package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/sebdew/LeaSSH/internal/access"
	"github.com/sebdew/LeaSSH/internal/auditreport"
	"github.com/sebdew/LeaSSH/internal/catalog"
	"github.com/sebdew/LeaSSH/internal/infra"
	"github.com/sebdew/LeaSSH/internal/journal"
	"github.com/sebdew/LeaSSH/internal/packs"
	"github.com/sebdew/LeaSSH/internal/proposals"
	"github.com/sebdew/LeaSSH/internal/provisioning"
	"github.com/sebdew/LeaSSH/internal/store"
	"github.com/sebdew/LeaSSH/internal/syslog"
	"github.com/sebdew/LeaSSH/internal/webhook"
)

// schemaTypes associe chaque schéma de docs/openapi.json au type Go qui le produit ou le consomme. Le test
// TestOpenAPISchemasMatchGoTypes compare leurs champs JSON: ajouter un champ à un type sans mettre à jour la
// spécification fait échouer le test (ARC-04).
var schemaTypes = map[string]reflect.Type{
	// corps de requête
	"SetupRequest":        reflect.TypeFor[setupInput](),
	"CommandInput":        reflect.TypeFor[commandInput](),
	"LintRequest":         reflect.TypeFor[lintInput](),
	"PreviewRequest":      reflect.TypeFor[previewInput](),
	"TestRunRequest":      reflect.TypeFor[testInput](),
	"GroupInput":          reflect.TypeFor[groupInput](),
	"KeyInput":            reflect.TypeFor[keyInput](),
	"RuleInput":           reflect.TypeFor[ruleInput](),
	"IdentityInput":       reflect.TypeFor[identityInput](),
	"ServerInput":         reflect.TypeFor[serverInput](),
	"UserInput":           reflect.TypeFor[userInput](),
	"ProvisionerInput":    reflect.TypeFor[provisionerInput](),
	"WebhookInput":        reflect.TypeFor[webhookInput](),
	"GeneralSettingsPut":  reflect.TypeFor[generalInput](),
	"OIDCSettingsPut":     reflect.TypeFor[oidcInput](),
	"ReportInput":         reflect.TypeFor[reportInput](),
	"AuditAIInput":        reflect.TypeFor[auditAIInput](),
	"DecisionInput":       reflect.TypeFor[decisionInput](),
	"ApproveProposal":     reflect.TypeFor[approveProposalInput](),
	"PackImportRequest":   reflect.TypeFor[importInput](),
	"PackSource":          reflect.TypeFor[packs.Source](),
	"SyslogSettingsInput": reflect.TypeFor[syslog.Input](),
	"ProposalPayload":     reflect.TypeFor[proposals.Payload](),
	// ressources
	"Command":          reflect.TypeFor[store.Command](),
	"Server":           reflect.TypeFor[store.Server](),
	"Identity":         reflect.TypeFor[store.Identity](),
	"MCPKey":           reflect.TypeFor[store.MCPKey](),
	"AccessRule":       reflect.TypeFor[store.AccessRule](),
	"Group":            reflect.TypeFor[store.Group](),
	"User":             reflect.TypeFor[userDTO](),
	"Execution":        reflect.TypeFor[store.Execution](),
	"Proposal":         reflect.TypeFor[proposalDTO](),
	"Provisioner":      reflect.TypeFor[store.Provisioner](),
	"AuditEvent":       reflect.TypeFor[store.AuditEvent](),
	"AuditReport":      reflect.TypeFor[store.AuditReportMeta](),
	"InstalledPack":    reflect.TypeFor[store.Pack](),
	"BundledPack":      reflect.TypeFor[packs.BundledInfo](),
	"WebhookDelivery":  reflect.TypeFor[store.WebhookDelivery](),
	"WebhookConfig":    reflect.TypeFor[webhook.Config](),
	"WebhookResult":    reflect.TypeFor[webhook.Result](),
	"SyslogConfig":     reflect.TypeFor[syslog.Config](),
	"SyslogStatus":     reflect.TypeFor[syslog.Status](),
	"SyslogTest":       reflect.TypeFor[syslog.TestResult](),
	"AIConfig":         reflect.TypeFor[auditreport.AIConfig](),
	"ReportVerify":     reflect.TypeFor[auditreport.VerifyResult](),
	"Integrity":        reflect.TypeFor[journal.Integrity](),
	"ChecklistItem":    reflect.TypeFor[journal.ChecklistItem](),
	"DashboardStats":   reflect.TypeFor[store.DashboardStats](),
	"Insights":         reflect.TypeFor[store.Insights](),
	"ConnectionTest":   reflect.TypeFor[infra.ConnectionTest](),
	"SourceTest":       reflect.TypeFor[provisioning.SourceTest](),
	"SyncReport":       reflect.TypeFor[provisioning.Report](),
	"AccessCell":       reflect.TypeFor[access.Cell](),
	"ProvisionerType":  reflect.TypeFor[provisioning.Type](),
	"ProvisionerField": reflect.TypeFor[provisioning.Field](),
	"SyncChange":       reflect.TypeFor[provisioning.Change](),
	"PackPreview":      reflect.TypeFor[packs.Preview](),
	"PackImportResult": reflect.TypeFor[packs.Result](),
	"LintResult":       reflect.TypeFor[catalog.LintResult](),
	"GeneralSettings":  reflect.TypeFor[generalView](),
	"OIDCSettings":     reflect.TypeFor[oidcView](),
}

// jsonFields liste les noms JSON d'un type (champs anonymes aplatis, comme encoding/json) et ceux qui sont obligatoires.
func jsonFields(t reflect.Type) (all, required map[string]bool) {
	all, required = map[string]bool{}, map[string]bool{}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" || (!f.IsExported() && !f.Anonymous) {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			sub := f.Type
			for sub.Kind() == reflect.Pointer {
				sub = sub.Elem()
			}
			if sub.Kind() == reflect.Struct {
				a, r := jsonFields(sub)
				for k := range a {
					all[k] = true
				}
				for k := range r {
					required[k] = true
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		all[name] = true
		if !strings.Contains(opts, "omitempty") && f.Type.Kind() != reflect.Pointer {
			required[name] = true
		}
	}
	return all, required
}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("lecture de %s: %v", specPath, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("spécification invalide: %v", err)
	}
	return doc
}

// ARC-04: les corps JSON sont décrits par des schémas par ressource, plus par un « objet quelconque ».
func TestOpenAPIBodiesAreTyped(t *testing.T) {
	doc := loadSpec(t)
	var generic []string
	isGeneric := func(c openapi3.Content) bool {
		mt := c.Get("application/json")
		return mt != nil && mt.Schema != nil && mt.Schema.Ref == "#/components/schemas/Object"
	}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op.RequestBody != nil && op.RequestBody.Value != nil && isGeneric(op.RequestBody.Value.Content) {
				generic = append(generic, "requête "+method+" "+path)
			}
			for code, r := range op.Responses.Map() {
				if r.Value != nil && isGeneric(r.Value.Content) {
					generic = append(generic, "réponse "+code+" "+method+" "+path)
				}
			}
		}
	}
	sort.Strings(generic)
	if len(generic) > 0 {
		t.Errorf("%d corps décrits par le schéma générique Object:\n  %s", len(generic), strings.Join(generic, "\n  "))
	}
	if _, ok := doc.Components.Schemas["Object"]; ok {
		t.Error("le schéma générique Object ne doit plus exister")
	}
}

// ARC-04: les champs d'un schéma sont ceux du type Go correspondant (pas de dérive silencieuse).
func TestOpenAPISchemasMatchGoTypes(t *testing.T) {
	doc := loadSpec(t)
	for name, typ := range schemaTypes {
		ref, ok := doc.Components.Schemas[name]
		if !ok || ref.Value == nil {
			t.Errorf("schéma %s absent de la spécification", name)
			continue
		}
		all, required := jsonFields(typ)
		got := map[string]bool{}
		for p := range ref.Value.Properties {
			got[p] = true
		}
		for f := range all {
			if !got[f] {
				t.Errorf("%s: le champ %q du type Go %s manque dans la spécification", name, f, typ)
			}
		}
		for f := range got {
			if !all[f] {
				t.Errorf("%s: le champ %q de la spécification n'existe pas dans le type Go %s", name, f, typ)
			}
		}
		for _, f := range ref.Value.Required {
			if !all[f] {
				t.Errorf("%s: %q est obligatoire dans la spécification mais absent du type Go", name, f)
			}
		}
		_ = required
	}
}

var (
	contractOnce   sync.Once
	contractRouter routers.Router
	contractErr    error
)

// checkContract valide la réponse d'un appel de test contre docs/openapi.json (statut documenté, type de contenu, corps).
// Tous les tests d'API qui passent par do() vérifient ainsi la spécification sur de vrais échanges. Les erreurs 4xx/5xx
// non listées pour une opération sont ignorées (le format d'erreur commun est vérifié par ailleurs).
func checkContract(t *testing.T, req *http.Request, resp *http.Response, body []byte) {
	t.Helper()
	if !strings.HasPrefix(req.URL.Path, "/api/v1/") {
		return
	}
	contractOnce.Do(func() {
		var doc *openapi3.T
		if doc, contractErr = openapi3.NewLoader().LoadFromFile(specPath); contractErr == nil {
			contractRouter, contractErr = legacy.NewRouter(doc)
		}
	})
	if contractErr != nil {
		t.Fatalf("spécification: %v", contractErr)
	}
	lookup := req.Clone(req.Context()) // serveur « / » de la spécification: on cherche par chemin seul
	lookup.URL, lookup.Host = &url.URL{Path: req.URL.Path}, ""
	route, params, err := contractRouter.FindRoute(lookup)
	if err != nil && resp.StatusCode == http.StatusNotFound {
		return // chemin inexistant: 404 attendu
	}
	if err != nil {
		t.Errorf("%s %s: route absente de la spécification: %v", req.Method, req.URL.Path, err)
		return
	}
	if resp.StatusCode >= 400 && route.Operation.Responses.Status(resp.StatusCode) == nil {
		return
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: lookup, PathParams: params, Route: route},
		Status:                 resp.StatusCode,
		Header:                 resp.Header,
		Body:                   io.NopCloser(bytes.NewReader(body)),
	}
	if err := openapi3filter.ValidateResponse(req.Context(), in); err != nil {
		t.Errorf("%s %s → %d: la réponse ne respecte pas la spécification: %v\n%s", req.Method, req.URL.Path, resp.StatusCode, err, clip(body, 400))
	}
}

func clip(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// Parcours CRUD complet: chaque réponse passe par checkContract (via env.call → do), y compris les 204 des suppressions.
func TestOpenAPIContractCRUD(t *testing.T) {
	e := newEnv(t)
	idn := e.must("POST", "/identities", map[string]any{"name": "lab", "username": "u"}, 201)["identity"].(map[string]any)
	e.must("GET", "/identities", nil, 200)
	srv := e.must("POST", "/servers", map[string]any{"slug": "web-01", "name": "Web", "host": "127.0.0.1", "port": e.ssh.Port, "identity_id": idn["id"]}, 201)
	sid := str(srv, "id")
	e.must("GET", "/servers/"+sid, nil, 200)
	e.must("PUT", "/servers/"+sid, map[string]any{"slug": "web-01", "name": "Web 1", "host": "127.0.0.1", "port": e.ssh.Port, "identity_id": idn["id"]}, 200)

	cmd := e.must("POST", "/commands", map[string]any{"slug": "sys.hello", "name": "Hello", "description": "d", "risk": "read", "template": "echo hello"}, 201)
	cid := str(cmd["command"].(map[string]any), "id")
	e.must("GET", "/commands/"+cid, nil, 200)
	e.must("PUT", "/commands/"+cid, map[string]any{"slug": "sys.hello", "name": "Hello 2", "description": "d", "risk": "read", "template": "echo hello"}, 200)

	sgID := ""
	cgID := ""
	for _, kind := range []string{"server-groups", "command-groups"} {
		member := sid
		if kind == "command-groups" {
			member = cid
		}
		g := e.must("POST", "/"+kind, map[string]any{"slug": "g1", "name": "G1"}, 201)
		gid := str(g, "id")
		e.must("PUT", "/"+kind+"/"+gid, map[string]any{"slug": "g1", "name": "G1 bis", "member_ids": []string{member}}, 200)
		e.must("PUT", "/"+kind+"/"+gid+"/members", map[string]any{"ids": []string{member}}, 200)
		if kind == "server-groups" {
			sgID = gid
		} else {
			cgID = gid
		}
	}

	key := e.must("POST", "/keys", map[string]any{"name": "Agent"}, 201)["key"].(map[string]any)
	kid := str(key, "id")
	e.must("PUT", "/keys/"+kid, map[string]any{"name": "Agent 2"}, 200)
	rule := e.must("POST", "/rules", map[string]any{"name": "R", "profile": "RO", "server_group_ids": []string{sgID}, "command_group_ids": []string{cgID}, "key_ids": []string{kid}}, 201)
	rid := str(rule, "id")
	e.must("PUT", "/rules/"+rid, map[string]any{"name": "R2", "profile": "RO", "server_group_ids": []string{sgID}, "command_group_ids": []string{cgID}, "key_ids": []string{kid}}, 200)
	e.must("DELETE", "/rules/"+rid, nil, 204)
	e.must("DELETE", "/server-groups/"+sgID, nil, 204)
	e.must("DELETE", "/command-groups/"+cgID, nil, 204)
	e.must("DELETE", "/keys/"+kid, nil, 204)

	e.must("GET", "/meta", nil, 200)
	e.must("GET", "/users", nil, 200)
	e.must("DELETE", "/commands/"+cid, nil, 204)
	e.must("DELETE", "/servers/"+sid, nil, 204)
	e.must("DELETE", "/identities/"+str(idn, "id"), nil, 204)
}
