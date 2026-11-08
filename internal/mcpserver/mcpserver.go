// Package mcpserver expose les 5 outils MCP en HTTP streamable (mode sans état) sur /mcp.
// L'authentification est faite à chaque requête; l'agent ne peut jamais rien modifier d'autre que ses propres demandes.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/leassh-dev/leassh/internal/access"
	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/catalog"
	"github.com/leassh-dev/leassh/internal/executions"
	"github.com/leassh-dev/leassh/internal/proposals"
	"github.com/leassh-dev/leassh/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Config struct {
	Store      *store.Store
	Access     *access.Service
	Executions *executions.Service
	Proposals  *proposals.Service
	ClientIP   func(*http.Request) string
	Version    string
	RateLimit  int // requêtes par minute et par clé (défaut 60)
	Log        *slog.Logger
}

type Server struct {
	cfg     Config
	limiter *rateLimiter
	authLog *rateLimiter // limite le bruit d'audit des échecs d'authentification
}

func New(cfg Config) *Server {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 60
	}
	return &Server{cfg: cfg, limiter: newRateLimiter(cfg.RateLimit, time.Minute), authLog: newRateLimiter(20, time.Minute)}
}

type ctxKey struct{}

type callCtx struct {
	key       store.MCPKey
	ip        string
	userAgent string
	sessionID string
}

// Handler renvoie le handler HTTP de /mcp.
func (s *Server) Handler() http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		cc, _ := r.Context().Value(ctxKey{}).(callCtx)
		return s.newMCPServer(cc)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := s.cfg.ClientIP(r)
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || token == "" {
			s.authFailed(r, ip, "", "en-tête Authorization absent ou sans « Bearer »")
			unauthorized(w)
			return
		}
		key, err := s.cfg.Access.Authenticate(r.Context(), token)
		if err != nil {
			reason := "jeton inconnu (clé supprimée ou jeton erroné)"
			if !strings.HasPrefix(token, "lsh_") {
				reason = "le jeton ne commence pas par « lsh_ »: vérifier que le client envoie bien la clé MCP et non une autre valeur (URL, clé API…)"
			}
			s.authFailed(r, ip, token, reason)
			unauthorized(w)
			return
		}
		if !s.limiter.allow(key.ID) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"rate_limited"}`, http.StatusTooManyRequests)
			return
		}
		cc := callCtx{key: key, ip: ip, userAgent: r.UserAgent(), sessionID: r.Header.Get("Mcp-Session-Id")}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, cc)))
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="leassh"`)
	http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
}

// authFailed journalise l'échec (préfixe du jeton seulement, jamais le jeton entier).
func (s *Server) authFailed(r *http.Request, ip, token, reason string) {
	if !s.authLog.allow(ip) {
		return
	}
	prefix := token
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	_ = audit.RecordStandalone(r.Context(), s.cfg.Store, audit.Event{ActorType: "system", ActorName: "mcp", Action: "key.auth_failed",
		TargetType: "key", TargetName: prefix, IP: ip, Diff: map[string]any{"reason": reason, "user_agent": r.UserAgent()}})
}

// ---- Outils ----

type serverOut struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

type listServersOut struct {
	Servers  []serverOut `json:"servers"`
	NextStep string      `json:"next_step,omitempty"`
}

type listCommandsIn struct {
	Server string `json:"server" jsonschema:"slug du serveur, tel que renvoyé par list_servers"`
}

type commandOut struct {
	Slug             string                      `json:"slug"`
	Name             string                      `json:"name"`
	Description      string                      `json:"description"`
	Risk             catalog.Risk                `json:"risk"`
	RequiresApproval bool                        `json:"requires_approval"`
	Params           map[string]catalog.ParamDef `json:"params"`
}

type listCommandsOut struct {
	Server   string       `json:"server"`
	Commands []commandOut `json:"commands"`
	NextStep string       `json:"next_step,omitempty"`
}

type runIn struct {
	Server  string         `json:"server" jsonschema:"slug du serveur cible (list_servers)"`
	Command string         `json:"command" jsonschema:"slug de la commande du catalogue (list_commands)"`
	Params  map[string]any `json:"params,omitempty" jsonschema:"valeurs des paramètres déclarés par la commande; aucun autre paramètre n'est accepté"`
	Reason  string         `json:"reason" jsonschema:"raison de l'exécution, 1 à 500 caractères (ex. identifiant d'alarme); journalisée et montrée au validateur"`
}

type runOut struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	DurationMS  *int64 `json:"duration_ms,omitempty"`
	Message     string `json:"message,omitempty"`
}

type getIn struct {
	ExecutionID string `json:"execution_id" jsonschema:"identifiant renvoyé par run_command"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"0 à 30: attendre jusqu'à ce que le statut change (utile pour une validation humaine)"`
}

type getOut struct {
	runOut
	Server          string `json:"server"`
	Command         string `json:"command"`
	Rendered        string `json:"rendered,omitempty"`
	DecidedBy       string `json:"decided_by,omitempty"`
	DecisionComment string `json:"decision_comment,omitempty"`
}

type proposeIn struct {
	Slug          string                      `json:"slug" jsonschema:"identifiant de la commande, forme famille.nom en minuscules (ex. mysql.replica_status)"`
	Name          string                      `json:"name" jsonschema:"libellé court"`
	Description   string                      `json:"description" jsonschema:"quand l'utiliser, ce qu'elle renvoie, effets de bord"`
	Template      string                      `json:"template" jsonschema:"commande avec placeholders {{nom}}; chaque placeholder est un mot autonome, jamais entre guillemets; pas de ; & | $() backtick > <"`
	Params        map[string]catalog.ParamDef `json:"params,omitempty" jsonschema:"définition typée de chaque placeholder (enum, int, identifier, service, path, regex)"`
	Risk          catalog.Risk                `json:"risk" jsonschema:"read, write ou destructive"`
	Justification string                      `json:"justification" jsonschema:"pourquoi cette commande est nécessaire"`
}

type proposeOut struct {
	ProposalID string             `json:"proposal_id"`
	Status     string             `json:"status"`
	Lint       catalog.LintResult `json:"lint"`
}

// toolError: résultat d'erreur MCP (isError) avec un message destiné à l'agent.
func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	case float64, bool, json.Number:
		return fmt.Sprint(x)
	}
	return ""
}

// parseRunArgs lit les arguments de run_command sans jamais échouer: ce qui manque ou a un mauvais type devient
// une valeur vide, que executions.Run refuse et journalise. `params` peut arriver en objet ou en texte JSON.
func parseRunArgs(raw json.RawMessage) runIn {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	in := runIn{Server: str(m["server"]), Command: str(m["command"]), Reason: str(m["reason"])}
	switch p := m["params"].(type) {
	case nil:
	case map[string]any:
		in.Params = p
	case string:
		if t := strings.TrimSpace(p); t != "" {
			var obj map[string]any
			if json.Unmarshal([]byte(t), &obj) == nil {
				in.Params = obj
			} else {
				in.Params = map[string]any{"params": p} // texte non JSON: refusé comme paramètre inconnu
			}
		}
	default:
		in.Params = map[string]any{"params": fmt.Sprint(p)}
	}
	return in
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func (s *Server) newMCPServer(cc callCtx) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "leassh", Version: s.cfg.Version}, &mcp.ServerOptions{
		Instructions: "Leassh donne un accès contrôlé à des serveurs. Tu ne tapes jamais de commande: tu choisis dans un catalogue. " +
			"Flux: list_servers → list_commands(server) → run_command(server, command, params, reason). " +
			"Si requires_approval est vrai, run_command renvoie pending_approval: un humain doit valider; suis l'état avec get_execution (wait_seconds).",
	})
	srv.AddReceivingMiddleware(s.logCalls(cc))
	touch := func(ctx context.Context) {
		_ = store.TouchKey(context.WithoutCancel(ctx), s.cfg.Store.DB, cc.key.ID, cc.ip)
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_servers",
		Description: "Liste les serveurs sur lesquels cette clé peut lancer au moins une commande. " +
			"À appeler en premier. Chaque serveur a un `slug` (à utiliser dans les autres outils) et une `description` du rôle du serveur.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listServersOut, error) {
		touch(ctx)
		snap, err := s.cfg.Access.Load(ctx)
		if err != nil {
			return nil, listServersOut{}, errors.New("erreur interne")
		}
		out := listServersOut{Servers: []serverOut{}, NextStep: "Étape suivante : appelle list_commands avec {\"server\": \"<slug>\"} pour voir les commandes autorisées sur un serveur, puis run_command pour en lancer une."}
		for _, sv := range snap.ServersFor(time.Now(), cc.key) {
			out.Servers = append(out.Servers, serverOut{Slug: sv.Slug, Name: sv.Name, Description: sv.Description, Tags: sv.Tags})
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_commands",
		Description: "Liste les commandes que cette clé peut lancer sur un serveur, avec leur description, leur niveau de risque " +
			"(read, write, destructive), leurs paramètres typés et `requires_approval` (true = validation humaine avant exécution). " +
			"Seules ces commandes existent pour toi: il est impossible d'en lancer une autre.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCommandsIn) (*mcp.CallToolResult, listCommandsOut, error) {
		touch(ctx)
		snap, err := s.cfg.Access.Load(ctx)
		if err != nil {
			return nil, listCommandsOut{}, errors.New("erreur interne")
		}
		sv, ok := snap.ServerBySlug(in.Server)
		out := listCommandsOut{Server: in.Server, Commands: []commandOut{}, NextStep: "Étape suivante : appelle run_command avec server, command, params (selon les paramètres ci-dessus) et reason (pourquoi tu lances cette commande)."}
		if !ok {
			return nil, out, errors.New(executions.GenericDenial)
		}
		entries := snap.CommandsFor(time.Now(), cc.key, sv)
		if len(entries) == 0 {
			return nil, out, errors.New(executions.GenericDenial)
		}
		for _, e := range entries {
			out.Commands = append(out.Commands, commandOut{Slug: e.Command.Slug, Name: e.Command.Name, Description: e.Command.Description,
				Risk: e.Command.Risk, RequiresApproval: e.RequiresApproval, Params: e.Command.Params})
		}
		return nil, out, nil
	})

	// run_command est enregistré en bas niveau: le SDK ne valide pas les arguments avant le handler.
	// Ainsi toute demande, même mal formée (raison absente, params en texte…), est journalisée comme refus (invariant 6)
	// au lieu d'être rejetée en silence avant d'atteindre executions.Run.
	srv.AddTool(&mcp.Tool{
		Name: "run_command",
		Description: "Exécute une commande du catalogue sur un serveur. Les valeurs de `params` sont validées selon leur type puis échappées; " +
			"un paramètre invalide est refusé avec la raison. `reason` est obligatoire et journalisé. " +
			"Résultat: soit l'exécution terminée (status succeeded/failed/timeout, avec stdout, stderr, exit_code), soit `pending_approval` " +
			"si un humain doit valider: dans ce cas appelle get_execution avec wait_seconds pour suivre la décision. " +
			"Un refus d'accès renvoie une erreur générique volontairement.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"server":  {Type: "string", Description: "slug du serveur cible (list_servers)"},
				"command": {Type: "string", Description: "slug de la commande du catalogue (list_commands)"},
				"params":  {Type: "object", Description: "valeurs des paramètres déclarés par la commande; aucun autre paramètre n'est accepté"},
				"reason":  {Type: "string", Description: "raison de l'exécution, 1 à 500 caractères (ex. identifiant d'alarme); journalisée et montrée au validateur"},
			},
			Required: []string{"server", "command", "reason"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		touch(ctx)
		in := parseRunArgs(req.Params.Arguments)
		k := cc.key
		o, err := s.cfg.Executions.Run(ctx, executions.Request{
			Actor:  executions.Actor{Key: &k, IP: cc.ip, SessionID: cc.sessionID, UserAgent: cc.userAgent},
			Server: in.Server, Command: in.Command, Params: in.Params, Reason: in.Reason,
		})
		if err != nil {
			s.logf("run_command: erreur interne", "key", k.Prefix, "err", err)
			return toolError("erreur interne"), nil
		}
		if o.Exec.Status == executions.StDenied {
			return toolError(o.DenialMessage), nil
		}
		out := toRun(o.Exec)
		b, _ := json.Marshal(out)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, StructuredContent: out}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_execution",
		Description: "Renvoie le statut et le résultat d'une exécution lancée par cette clé (jamais celles des autres clés). " +
			"Statuts: pending_approval (en attente d'un humain), running, succeeded, failed, timeout, rejected, expired, error. " +
			"`wait_seconds` (0-30) attend un changement de statut avant de répondre.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, getOut, error) {
		touch(ctx)
		e, err := store.ExecutionByID(ctx, s.cfg.Store.DB, in.ExecutionID)
		if err != nil || e.KeyID != cc.key.ID {
			return nil, getOut{}, errors.New("exécution introuvable")
		}
		wait := in.WaitSeconds
		if wait < 0 || wait > 30 {
			return nil, getOut{}, errors.New("wait_seconds doit être compris entre 0 et 30")
		}
		if wait > 0 && (e.Status == executions.StPending || e.Status == executions.StRunning) {
			if e, err = s.cfg.Executions.WaitChange(ctx, e.ID, e.Status, time.Duration(wait)*time.Second); err != nil && ctx.Err() == nil {
				return nil, getOut{}, errors.New("erreur interne")
			}
		}
		out := getOut{runOut: toRun(e), Server: e.ServerSlug, Command: e.CommandSlug, DecidedBy: e.DecidedByName, DecisionComment: e.DecisionComment}
		if e.Rendered != nil {
			out.Rendered = *e.Rendered
		}
		if e.Status == executions.StDenied {
			out.Message = executions.GenericDenial
		}
		return nil, out, nil
	})

	if cc.key.CanPropose {
		mcp.AddTool(srv, &mcp.Tool{
			Name: "propose_command",
			Description: "Propose l'ajout d'une nouvelle commande au catalogue quand aucune commande existante ne convient. " +
				"Rien n'est exécutable avant la validation d'un administrateur. Un linter refuse les templates dangereux " +
				"(métacaractères shell, placeholder entre guillemets ou collé à du texte, interpréteurs comme sh/python/sudo). " +
				"Chaque valeur variable doit être un placeholder `{{nom}}` déclaré dans `params` avec un type strict. Limite: 10 propositions en attente.",
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in proposeIn) (*mcp.CallToolResult, proposeOut, error) {
			touch(ctx)
			p, res, err := s.cfg.Proposals.Create(ctx, cc.key, proposals.Payload{Slug: in.Slug, Name: in.Name, Description: in.Description,
				Template: in.Template, Params: in.Params, Risk: in.Risk}, in.Justification)
			switch {
			case errors.Is(err, proposals.ErrCannotPropose), errors.Is(err, proposals.ErrTooMany):
				return nil, proposeOut{}, err
			case err != nil:
				return nil, proposeOut{}, errors.New("erreur interne")
			}
			return nil, proposeOut{ProposalID: p.ID, Status: p.Status, Lint: res}, nil
		})
	}
	return srv
}

func toRun(e store.Execution) runOut {
	o := runOut{ExecutionID: e.ID, Status: e.Status, ExitCode: e.ExitCode, Stdout: e.Stdout, Stderr: e.Stderr, Truncated: e.Truncated, DurationMS: e.DurationMS}
	if e.Status == executions.StPending {
		o.Message = "Cette commande nécessite une validation humaine. Utilisez get_execution pour suivre son statut."
	}
	if e.Status == executions.StRejected {
		o.Message = "Refusée par un validateur."
	}
	return o
}

// ---- limiteur de débit (fenêtre glissante) ----

type rateLimiter struct {
	mu   sync.Mutex
	max  int
	win  time.Duration
	hits map[string][]time.Time
}

func newRateLimiter(max int, win time.Duration) *rateLimiter {
	return &rateLimiter{max: max, win: win, hits: map[string][]time.Time{}}
}

func (l *rateLimiter) allow(k string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	keep := l.hits[k][:0]
	for _, t := range l.hits[k] {
		if now.Sub(t) < l.win {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.max {
		l.hits[k] = keep
		return false
	}
	l.hits[k] = append(keep, now)
	return true
}

func (s *Server) logf(msg string, args ...any) { s.cfg.Log.Warn(msg, args...) }

// logCalls consigne chaque appel d'outil (outil, clé, résultat) dans les logs applicatifs: sans cela un refus du SDK
// (arguments invalides, outil inconnu) ne laisse aucune trace côté serveur.
func (s *Server) logCalls(cc callCtx) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if method != "tools/call" {
				if err != nil {
					s.cfg.Log.Warn("requête MCP en erreur", "method", method, "key", cc.key.Prefix, "ip", cc.ip, "err", err)
				} else {
					s.cfg.Log.Info("requête MCP", "method", method, "key", cc.key.Prefix, "ip", cc.ip)
				}
				return res, err
			}
			// Jamais le contenu des arguments (reason, params): seulement leur taille (SEC-06).
			// Le détail complet est dans le journal d'exécution, qui est protégé.
			tool, argsLen := "", 0
			if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
				tool, argsLen = p.Name, len(p.Arguments)
			}
			attrs := []any{"tool", tool, "key", cc.key.Prefix, "ip", cc.ip, "args_len", argsLen}
			if r, ok := res.(*mcp.CallToolResult); ok && r != nil && r.IsError {
				// Le message de refus peut citer une valeur fournie par l'agent: on ne le logue pas.
				s.cfg.Log.Info("appel MCP refusé", attrs...)
				return res, err
			}
			if err != nil {
				s.cfg.Log.Warn("appel MCP en erreur", append(attrs, "err", err)...)
				return res, err
			}
			s.cfg.Log.Info("appel MCP", attrs...)
			return res, err
		}
	}
}
