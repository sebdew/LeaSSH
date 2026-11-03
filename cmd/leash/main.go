// Commande leash: serveur MCP donnant aux agents IA un accès SSH limité à un catalogue de commandes.
package main

import (
	"bytes"
	"context"
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

	"github.com/leash-dev/leash/internal/access"
	"github.com/leash-dev/leash/internal/api"
	"github.com/leash-dev/leash/internal/config"
	"github.com/leash-dev/leash/internal/crypto"
	"github.com/leash-dev/leash/internal/executions"
	"github.com/leash-dev/leash/internal/mcpserver"
	"github.com/leash-dev/leash/internal/metrics"
	"github.com/leash-dev/leash/internal/packs"
	"github.com/leash-dev/leash/internal/proposals"
	"github.com/leash-dev/leash/internal/provisioning"
	"github.com/leash-dev/leash/internal/provisioning/warpgate"
	"github.com/leash-dev/leash/internal/retention"
	"github.com/leash-dev/leash/internal/rotation"
	"github.com/leash-dev/leash/internal/sshexec"
	"github.com/leash-dev/leash/internal/store"
	"github.com/leash-dev/leash/internal/syslog"
	"github.com/leash-dev/leash/internal/webhook"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "leash:", err)
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
			return errors.New("usage: leash backup <fichier-de-destination>")
		}
		dir := os.Getenv("LEASH_DATA_DIR")
		if dir == "" {
			dir = "/data"
		}
		if err := store.Backup(context.Background(), dir, args[1]); err != nil {
			return err
		}
		fmt.Println("sauvegarde écrite dans", args[1], "— conservez aussi LEASH_MASTER_KEY, sans elle les secrets sont irrécupérables")
		return nil
	case "rotate-master-key":
		return rotateMasterKey()
	case "reset-password":
		return resetPassword(args[1:])
	case "serve":
		cfg, err := config.LoadEnv()
		if err != nil {
			return err
		}
		log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
		st, err := store.Open(context.Background(), cfg.DataDir)
		if err != nil {
			return err
		}
		defer st.Close()
		return serve(cfg, st, log)
	default:
		return fmt.Errorf("sous-commande inconnue %q (serve, backup, rotate-master-key, reset-password, healthcheck, version)", cmd)
	}
}

func serve(cfg *config.Config, st *store.Store, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	box, err := crypto.NewBox(cfg.MasterKey)
	if err != nil {
		return err
	}
	pool := sshexec.NewPool(60*time.Second, 2)
	defer pool.Close()
	acc := access.New(st)
	exec := executions.New(ctx, st, acc, box, pool, log, cfg.ApprovalTTL)
	reg := metrics.New()
	exec.Metrics = reg
	props := proposals.New(st)
	pk := packs.New(st)
	hook := webhook.New(st, box, cfg.BaseURL, log)
	sl := syslog.New(st, box, version, log)
	if err := sl.Reload(ctx); err != nil {
		log.Error("configuration syslog invalide, export désactivé", "err", err)
	}
	exec.Notify, exec.Observe = hook.OnExecution, sl.OnExecution
	props.Notify = func(event string, p store.Proposal) { hook.OnProposal(event, p); sl.OnProposal(event, p) }
	go hook.Run(ctx)
	go sl.Run(ctx)
	provisioning.Register(warpgate.Type())
	prov := provisioning.New(st, box, log)
	prov.Invalidate = pool.Invalidate
	go prov.Run(ctx)
	go exec.ExpireLoop(ctx, 30*time.Second)
	go retention.Loop(ctx, st, cfg.LogRetentionDays, log)

	srv, err := api.New(cfg, st, log, webHandler())
	if err != nil {
		return err
	}
	mcp := mcpserver.New(mcpserver.Config{Store: st, Access: acc, Executions: exec, Proposals: props, ClientIP: srv.ClientIP, Version: version, Log: log})
	srv.Attach(api.Services{Box: box, Pool: pool, Acc: acc, Exec: exec, Props: props, Packs: pk, Hook: hook, Syslog: sl, Prov: prov, MCP: mcp.Handler(), Metrics: reg, Version: version})

	// Les flux longs (SSE du journal, attentes de get_execution) se terminent à l'arrêt: sans cela Shutdown attendrait le délai complet.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	hs := &http.Server{Addr: cfg.Listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context { return baseCtx }}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	log.Info("leash démarré", "listen", cfg.Listen, "version", version)
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

// healthcheck interroge /healthz en local (l'image distroless n'a ni shell ni curl).
func healthcheck() error {
	addr := os.Getenv("LEASH_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: HTTP %d", resp.StatusCode)
	}
	return nil
}

// rotateMasterKey: LEASH_MASTER_KEY = ancienne clé, LEASH_NEW_MASTER_KEY = nouvelle clé. À lancer serveur arrêté, après sauvegarde.
func rotateMasterKey() error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	raw := os.Getenv("LEASH_NEW_MASTER_KEY")
	newKey, err := base64.StdEncoding.DecodeString(raw)
	if raw == "" || err != nil || len(newKey) != 32 {
		return errors.New("LEASH_NEW_MASTER_KEY doit contenir la nouvelle clé (32 octets en base64, ex. `openssl rand -base64 32`)")
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
	fmt.Printf("%d secret(s) rechiffré(s). Remplacez LEASH_MASTER_KEY par la nouvelle clé avant de redémarrer Leash.\n", n)
	return nil
}
