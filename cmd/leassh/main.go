// Commande leassh: serveur MCP donnant aux agents IA un accès SSH limité à un catalogue de commandes.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sebdew/LeaSSH/internal/access"
	"github.com/sebdew/LeaSSH/internal/api"
	"github.com/sebdew/LeaSSH/internal/auditreport"
	"github.com/sebdew/LeaSSH/internal/config"
	"github.com/sebdew/LeaSSH/internal/crypto"
	"github.com/sebdew/LeaSSH/internal/executions"
	"github.com/sebdew/LeaSSH/internal/mcpserver"
	"github.com/sebdew/LeaSSH/internal/metrics"
	"github.com/sebdew/LeaSSH/internal/packs"
	"github.com/sebdew/LeaSSH/internal/proposals"
	"github.com/sebdew/LeaSSH/internal/provisioning"
	"github.com/sebdew/LeaSSH/internal/provisioning/warpgate"
	"github.com/sebdew/LeaSSH/internal/retention"
	"github.com/sebdew/LeaSSH/internal/rotation"
	"github.com/sebdew/LeaSSH/internal/settings"
	"github.com/sebdew/LeaSSH/internal/sshexec"
	"github.com/sebdew/LeaSSH/internal/store"
	"github.com/sebdew/LeaSSH/internal/syslog"
	"github.com/sebdew/LeaSSH/internal/updatecheck"
	"github.com/sebdew/LeaSSH/internal/webhook"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "leassh:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "version":
		fmt.Println(version)
		return nil
	case "healthcheck":
		return healthcheck()
	case "backup":
		if len(args) != 2 {
			return errors.New("usage: leassh backup <fichier-de-destination>")
		}
		dir := os.Getenv("LEASSH_DATA_DIR")
		if dir == "" {
			dir = "/data"
		}
		if err := store.Backup(context.Background(), dir, args[1]); err != nil {
			return err
		}
		fmt.Println("sauvegarde écrite dans", args[1], "— conservez aussi LEASSH_MASTER_KEY, sans elle les secrets sont irrécupérables")
		return nil
	case "rotate-master-key":
		return rotateMasterKey()
	case "reset-password":
		return resetPassword(args[1:])
	case "enable-local-login":
		return enableLocalLogin()
	case "serve":
		cfg, err := config.LoadEnv()
		if err != nil {
			return err
		}
		level := new(slog.LevelVar)
		log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
		st, err := store.Open(context.Background(), cfg.DataDir)
		if err != nil {
			return err
		}
		defer st.Close()
		set, err := settings.New(context.Background(), st, level)
		if err != nil {
			return err
		}
		return serve(cfg, set, st, log)
	default:
		return fmt.Errorf("sous-commande inconnue %q (serve, backup, rotate-master-key, reset-password, enable-local-login, healthcheck, version)", cmd)
	}
}

func serve(cfg *config.Config, set *settings.Service, st *store.Store, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	box, err := crypto.NewBox(cfg.MasterKey)
	if err != nil {
		return err
	}
	pool := sshexec.NewPool(60*time.Second, 2)
	defer pool.Close()
	acc := access.New(st)
	exec := executions.New(ctx, st, acc, box, pool, log, set.ApprovalTTL)
	reg := metrics.New()
	st.Observe = func(d time.Duration, err error) { reg.Outbound("database", d, err) }
	exec.Metrics = reg
	props := proposals.New(st)
	pk := packs.New(st)
	hook := webhook.New(st, box, set.BaseURL, log)
	hook.SetTransport(reg.Transport("webhook", nil))
	sl := syslog.New(st, box, version, log)
	if err := sl.Reload(ctx); err != nil {
		log.Error("configuration syslog invalide, export désactivé", "err", err)
	}
	exec.Notify, exec.Observe = hook.OnExecution, sl.OnExecution
	props.Notify = func(event string, p store.Proposal) { hook.OnProposal(event, p); sl.OnProposal(event, p) }
	if err := exec.ReconcileRunning(ctx); err != nil {
		return err
	}
	go hook.Run(ctx)
	upd := updatecheck.New(cfg.UpdateCheck, version, reg.Transport("update", nil))
	go upd.Run(ctx)
	go sl.Run(ctx)
	provisioning.Register(warpgate.Type())
	prov := provisioning.New(st, box, log)
	prov.Invalidate = pool.Invalidate
	go prov.Run(ctx)
	go exec.ExpireLoop(ctx, 30*time.Second)
	go retention.Loop(ctx, st, set.RetentionDays, log)

	srv, err := api.New(set, st, log, webHandler())
	if err != nil {
		return err
	}
	mcp := mcpserver.New(mcpserver.Config{Store: st, Access: acc, Executions: exec, Proposals: props, ClientIP: srv.ClientIP, Version: version, Log: log})
	if err := configureSetupToken(context.Background(), srv, cfg, st); err != nil {
		return err
	}
	srv.Attach(api.Services{Box: box, Pool: pool, Acc: acc, Exec: exec, Props: props, Packs: pk, Hook: hook, Syslog: sl, Prov: prov, MCP: mcp.Handler(), Metrics: reg, Version: version, Updates: upd,
		AIDefaults: auditreport.AIDefaults{URL: cfg.LLMBaseURL, Model: cfg.LLMModel, APIKey: cfg.LLMAPIKey}})

	// Les flux longs (SSE du journal, attentes de get_execution) se terminent à l'arrêt: sans cela Shutdown attendrait le délai complet.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	hs := &http.Server{Addr: cfg.Listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context { return baseCtx }}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	log.Info("leassh démarré", "listen", cfg.Listen, "version", version)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	cancelBase()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = hs.Shutdown(shutdown)
	exec.Wait()
	return err
}

// healthcheck interroge /health en local (l'image distroless n'a ni shell ni curl).
func healthcheck() error {
	addr := os.Getenv("LEASSH_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/health") //nolint:gosec // adresse locale issue de LEASSH_LISTEN, pas d'entrée utilisateur
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: HTTP %d", resp.StatusCode)
	}
	return nil
}

// rotateMasterKey: LEASSH_MASTER_KEY = ancienne clé, LEASSH_NEW_MASTER_KEY = nouvelle clé. À lancer serveur arrêté, après sauvegarde.
func rotateMasterKey() error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	raw := os.Getenv("LEASSH_NEW_MASTER_KEY")
	newKey, err := base64.StdEncoding.DecodeString(raw)
	if raw == "" || err != nil || len(newKey) != 32 {
		return errors.New("LEASSH_NEW_MASTER_KEY doit contenir la nouvelle clé (32 octets en base64, ex. `openssl rand -base64 32`)")
	}
	if bytes.Equal(newKey, cfg.MasterKey) {
		return errors.New("la nouvelle clé est identique à l'ancienne")
	}
	oldBox, err := crypto.NewBox(cfg.MasterKey)
	if err != nil {
		return err
	}
	newBox, err := crypto.NewBox(newKey)
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := rotation.Rotate(ctx, st, oldBox, newBox)
	if err != nil {
		return fmt.Errorf("rotation annulée, rien n'a été modifié: %w", err)
	}
	fmt.Printf("%d secret(s) rechiffré(s). Remplacez LEASSH_MASTER_KEY par la nouvelle clé avant de redémarrer Leassh.\n", n)
	return nil
}

// configureSetupToken arme le jeton d'initialisation (SEC-01). Sur une instance déjà initialisée, aucun jeton n'est nécessaire.
// Sans LEASSH_SETUP_TOKEN, un jeton aléatoire est généré et affiché une seule fois sur la sortie d'erreur
// (volontairement hors du logger structuré, dont les lignes peuvent être transmises à un syslog).
func configureSetupToken(ctx context.Context, srv *api.Server, cfg *config.Config, st *store.Store) error {
	n, err := st.UserCount(ctx)
	if err != nil {
		return fmt.Errorf("lecture des utilisateurs: %w", err)
	}
	if n > 0 {
		return nil
	}
	tok := cfg.SetupToken
	if tok == "" {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("génération du jeton d'initialisation: %w", err)
		}
		tok = base64.RawURLEncoding.EncodeToString(b)
		fmt.Fprintf(os.Stderr, "Instance non initialisée. Jeton d'initialisation (usage unique, valable jusqu'au redémarrage) : %s\n", tok)
	}
	srv.SetSetupToken(tok)
	return nil
}
