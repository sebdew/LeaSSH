// Package executions: cycle de vie d'une demande — autorisation → paramètres → rendu → hash →
// décision de validation → exécution → journal. Invariant 1: aucun chemin SSH ne contourne Run().
package executions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/leassh-dev/leassh/internal/access"
	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/catalog"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/metrics"
	"github.com/leassh-dev/leassh/internal/sshexec"
	"github.com/leassh-dev/leassh/internal/store"
)

// Statuts d'exécution (SPEC §4.9).
const (
	StDenied    = "denied"
	StPending   = "pending_approval"
	StRejected  = "rejected"
	StExpired   = "expired"
	StRunning   = "running"
	StSucceeded = "succeeded"
	StFailed    = "failed"
	StTimeout   = "timeout"
	StError     = "error"
)

// GenericDenial est le seul message renvoyé aux agents pour un refus d'accès (invariant 8).
const GenericDenial = "serveur ou commande indisponible pour cette clé"

var (
	ErrNotPending  = errors.New("la demande n'est plus en attente")
	ErrConfirm     = errors.New("confirmation requise: saisir le slug du serveur")
	ErrNeedComment = errors.New("un commentaire est obligatoire pour refuser")
	ErrForbidden   = errors.New("droits insuffisants")
)

// Actor: l'auteur de la demande. Soit une clé MCP, soit un admin testant une commande depuis l'éditeur.
type Actor struct {
	Key       *store.MCPKey
	User      *store.User
	IP        string
	SessionID string
	UserAgent string
}

type Request struct {
	Actor   Actor
	Server  string
	Command string
	Params  map[string]any
	Reason  string
}

type Outcome struct {
	Exec          store.Execution
	DenialMessage string // message destiné à l'agent si status=denied
}

// Notifier reçoit les événements webhook (execution.pending_approval, execution.finished, execution.denied, host_key.mismatch).
type Notifier func(event string, e store.Execution)

type Service struct {
	st     *store.Store
	acc    *access.Service
	box    *crypto.Box
	pool   *sshexec.Pool
	log    *slog.Logger
	ttl    time.Duration
	Broker *Broker
	Notify Notifier
	// Observe reçoit en plus les transitions sans webhook (execution.rejected, execution.expired): export syslog.
	Observe Notifier
	// Metrics est facultatif (nil = désactivé).
	Metrics *metrics.Registry
	bg      context.Context
	wg      sync.WaitGroup
}

// New: bg est le contexte de vie du service (les exécutions survivent à la requête qui les a lancées).
func New(bg context.Context, st *store.Store, acc *access.Service, box *crypto.Box, pool *sshexec.Pool, log *slog.Logger, approvalTTL time.Duration) *Service {
	return &Service{st: st, acc: acc, box: box, pool: pool, log: log, ttl: approvalTTL, Broker: NewBroker(), bg: bg, Notify: func(string, store.Execution) {}, Observe: func(string, store.Execution) {}}
}

// Wait attend la fin des exécutions en cours (arrêt propre, tests).
func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) notify(event string, e store.Execution) {
	s.Broker.Publish(e.ID)
	if event != "" {
		s.Notify(event, e)
		s.Observe(event, e)
	}
}

// notifyState: transition de statut sans événement webhook (publie pour le direct et l'observateur seulement).
func (s *Service) notifyState(event string, e store.Execution) {
	s.Broker.Publish(e.ID)
	s.Observe(event, e)
}

func baseRecord(req Request) store.Execution {
	e := store.Execution{ClientIP: req.Actor.IP, MCPSessionID: req.Actor.SessionID, UserAgent: req.Actor.UserAgent,
		ServerSlug: req.Server, CommandSlug: req.Command, Reason: req.Reason, Status: StDenied}
	if b, err := json.Marshal(req.Params); err == nil && req.Params != nil {
		e.Params = string(b)
	}
	if k := req.Actor.Key; k != nil {
		e.KeyID, e.KeyName = k.ID, k.Name
	}
	if u := req.Actor.User; u != nil {
		e.ActorUserID = u.ID
		e.DecidedBy, e.DecidedByName = u.ID, u.Username
	}
	return e
}

func (s *Service) insert(ctx context.Context, e store.Execution) (store.Execution, error) {
	var out store.Execution
	err := s.st.Tx(ctx, func(tx *sqlTx) (err error) {
		out, err = store.InsertExecution(ctx, tx, e)
		return
	})
	return out, err
}

func (s *Service) deny(ctx context.Context, e store.Execution, reason, msg string) (Outcome, error) {
	e.Status, e.DenialReason = StDenied, reason
	e.FinishedAt = store.Now()
	out, err := s.insert(ctx, e)
	if err != nil {
		return Outcome{}, fmt.Errorf("journalisation du refus: %w", err)
	}
	s.Metrics.Execution(StDenied)
	s.notify("execution.denied", out)
	return Outcome{Exec: out, DenialMessage: msg}, nil
}

// Run traite une demande d'exécution. Toute demande, refusée ou non, produit une ligne de journal.
func (s *Service) Run(ctx context.Context, req Request) (Outcome, error) {
	e := baseRecord(req)
	req.Reason = strings.TrimSpace(req.Reason)
	e.Reason = req.Reason
	if n := len([]rune(req.Reason)); n < 1 || n > 500 {
		return s.deny(ctx, e, "reason_invalid", "`reason` est obligatoire (1 à 500 caractères)")
	}
	if req.Actor.Key == nil && (req.Actor.User == nil || req.Actor.User.Role != "admin") {
		return s.deny(ctx, e, "actor_forbidden", GenericDenial)
	}
	snap, err := s.acc.Load(ctx)
	if err != nil {
		return Outcome{}, fmt.Errorf("chargement de la configuration: %w", err)
	}
	srv, okS := snap.ServerBySlug(req.Server)
	cmd, okC := snap.CommandBySlug(req.Command)
	if okS {
		e.ServerID = srv.ID
	}
	if okC {
		e.CommandID, e.CommandVersion, e.CommandHash, e.Risk = cmd.ID, cmd.Version, cmd.Hash, string(cmd.Risk)
	}
	if !okS || !okC {
		return s.deny(ctx, e, "target_not_found", GenericDenial)
	}

	// 1. autorisation
	requiresApproval := false
	if req.Actor.Key != nil {
		d := snap.Evaluate(time.Now(), *req.Actor.Key, srv, cmd)
		if !d.Allowed {
			return s.deny(ctx, e, d.Reason, GenericDenial)
		}
		e.RuleID, e.RuleName, requiresApproval = d.Rule.ID, d.Rule.Name, d.RequiresApproval
	} else { // test depuis l'éditeur par un admin: pas de règle, mais serveur actif et clé d'hôte épinglée
		if !srv.Enabled {
			return s.deny(ctx, e, "server_disabled", "serveur désactivé")
		}
		if srv.HostKeyStatus != "pinned" {
			return s.deny(ctx, e, "host_key_not_pinned", "clé d'hôte non épinglée pour ce serveur")
		}
		e.RuleName = "test administrateur"
	}

	// 2-3. paramètres typés puis rendu (validation + échappement POSIX)
	rendered, err := catalog.Render(cmd.Template, cmd.Params, req.Params)
	if err != nil {
		var pe *catalog.ParamError
		if errors.As(err, &pe) {
			return s.deny(ctx, e, pe.Denial(), "Refusée : "+pe.Error())
		}
		return s.deny(ctx, e, "render_failed", GenericDenial)
	}
	// 4. vérification du hash courant (détecte une altération en base)
	if catalog.Hash(cmd.Template, cmd.Params, cmd.Risk) != cmd.Hash {
		return s.deny(ctx, e, "hash_mismatch", GenericDenial)
	}
	e.Rendered = &rendered

	// 5. décision de validation
	if requiresApproval {
		e.Status = StPending
		out, err := s.insert(ctx, e)
		if err != nil {
			return Outcome{}, fmt.Errorf("journalisation: %w", err)
		}
		s.Metrics.Execution(StPending)
		s.notify("execution.pending_approval", out)
		return Outcome{Exec: out}, nil
	}

	// 6. exécution
	e.Status, e.StartedAt = StRunning, store.Now()
	out, err := s.insert(ctx, e)
	if err != nil {
		return Outcome{}, fmt.Errorf("journalisation: %w", err)
	}
	s.Broker.Publish(out.ID)
	done := s.launch(out, srv, cmd)
	select {
	case res := <-done:
		return Outcome{Exec: res}, nil
	case <-ctx.Done(): // le client est parti: la commande continue, l'agent pourra suivre avec get_execution
		return Outcome{Exec: out}, nil
	}
}

// launch exécute en arrière-plan (contexte du service) et renvoie la ligne finale sur le canal.
func (s *Service) launch(e store.Execution, srv store.Server, cmd store.Command) <-chan store.Execution {
	done := make(chan store.Execution, 1)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		done <- s.execute(s.bg, e, srv, cmd)
	}()
	return done
}

func (s *Service) execute(ctx context.Context, e store.Execution, srv store.Server, cmd store.Command) store.Execution {
	start := time.Now()
	finish := func(status string, code int, res sshexec.Result, errMsg string) store.Execution {
		e.Status = status
		e.FinishedAt = store.Now()
		ms := time.Since(start).Milliseconds()
		e.DurationMS = &ms
		if status != StError {
			e.ExitCode = &code
		}
		e.Stdout, e.Stderr, e.Truncated = res.Stdout, res.Stderr, res.Truncated
		if errMsg != "" {
			e.Stderr = strings.TrimSpace(e.Stderr + "\nleassh: " + errMsg)
		}
		if err := store.SaveExecutionState(context.WithoutCancel(ctx), s.st.DB, e); err != nil {
			s.log.Error("sauvegarde de l'exécution", "id", e.ID, "err", err)
		}
		s.Metrics.Execution(status)
		s.Metrics.Duration(float64(ms) / 1000)
		s.notify("execution.finished", e)
		return e
	}
	target, err := s.target(ctx, srv)
	if err != nil {
		e.DenialReason = "identity_error"
		s.Metrics.SSHError("identity")
		return finish(StError, 0, sshexec.Result{}, "identité SSH inutilisable")
	}
	res, err := s.pool.Exec(ctx, srv.ID, target, *e.Rendered, time.Duration(cmd.TimeoutS)*time.Second, cmd.MaxOutputKB*1024)
	s.recordSSHHealth(ctx, srv.ID, err)
	var mm *sshexec.HostKeyMismatchError
	switch {
	case errors.As(err, &mm):
		s.onHostKeyMismatch(ctx, srv, mm)
		s.Metrics.SSHError("host_key_mismatch")
		e.DenialReason = "host_key_mismatch"
		return finish(StError, 0, sshexec.Result{}, "clé d'hôte divergente: serveur bloqué")
	case errors.As(err, new(*sshexec.CooldownError)):
		e.DenialReason = "ssh_cooldown"
		s.Metrics.SSHError("cooldown")
		return finish(StError, 0, sshexec.Result{}, err.Error())
	case err != nil:
		e.DenialReason = "ssh_error"
		s.Metrics.SSHError(sshErrorKind(err))
		return finish(StError, 0, res, "erreur SSH: "+err.Error())
	case res.TimedOut:
		return finish(StTimeout, res.ExitCode, res, fmt.Sprintf("délai de %d s dépassé", cmd.TimeoutS))
	case res.ExitCode != 0:
		return finish(StFailed, res.ExitCode, res, "")
	}
	return finish(StSucceeded, 0, res, "")
}

// recordSSHHealth: un résultat de commande (même en échec) prouve que SSH fonctionne; seule une erreur de connexion
// ou d'authentification marque le serveur en anomalie. La pause (cooldown) n'est pas un nouvel échec.
func (s *Service) recordSSHHealth(ctx context.Context, serverID string, err error) {
	var mm *sshexec.HostKeyMismatchError
	var cd *sshexec.CooldownError
	switch {
	case err == nil:
		_ = store.MarkSSH(context.WithoutCancel(ctx), s.st.DB, serverID, true)
	case errors.As(err, &cd) || errors.As(err, &mm):
	default:
		_ = store.MarkSSH(context.WithoutCancel(ctx), s.st.DB, serverID, false)
	}
}

func (s *Service) target(ctx context.Context, srv store.Server) (sshexec.Target, error) {
	id, err := store.IdentityByID(ctx, s.st.DB, srv.IdentityID)
	if err != nil {
		return sshexec.Target{}, err
	}
	user := id.Username
	if srv.SSHUser != "" { // ex. Warpgate: « utilisateur:cible », différent pour chaque serveur derrière le même bastion
		user = srv.SSHUser
	}
	t := sshexec.Target{Host: srv.Host, Port: srv.Port, User: user, Fingerprint: srv.HostKeyFingerprint}
	if len(id.PrivateKeyEnc) > 0 {
		if t.Auth.PrivateKeyPEM, err = s.box.Open(id.PrivateKeyEnc); err != nil {
			return t, err
		}
	}
	if len(id.PasswordEnc) > 0 {
		pw, err := s.box.Open(id.PasswordEnc)
		if err != nil {
			return t, err
		}
		t.Auth.Password = string(pw)
	}
	return t, nil
}

// TargetFor est utilisé par l'API (test de connexion) pour partager la construction de la cible SSH.
func (s *Service) TargetFor(ctx context.Context, srv store.Server) (sshexec.Target, error) {
	return s.target(ctx, srv)
}

func (s *Service) onHostKeyMismatch(ctx context.Context, srv store.Server, mm *sshexec.HostKeyMismatchError) {
	bg := context.WithoutCancel(ctx)
	s.pool.Invalidate(srv.ID)
	if err := store.SetHostKeyStatus(bg, s.st.DB, srv.ID, "mismatch"); err != nil {
		s.log.Error("marquage mismatch", "server", srv.Slug, "err", err)
	}
	_ = audit.RecordStandalone(bg, s.st, audit.Event{ActorType: "system", ActorName: "ssh", Action: "host_key.mismatch",
		TargetType: "server", TargetID: srv.ID, TargetName: srv.Slug,
		Diff: map[string]any{"expected": mm.Expected, "received": mm.Got}})
	s.Notify("host_key.mismatch", store.Execution{ServerID: srv.ID, ServerSlug: srv.Slug})
}

// Decide applique la décision d'un humain sur une demande en attente.
func (s *Service) Decide(ctx context.Context, id string, approver store.User, approve bool, comment, confirmSlug string) (store.Execution, error) {
	if approver.Role != "admin" && approver.Role != "approver" {
		return store.Execution{}, ErrForbidden
	}
	e, err := store.ExecutionByID(ctx, s.st.DB, id)
	if err != nil {
		return e, err
	}
	if e.Status != StPending {
		return e, ErrNotPending
	}
	if t, err := store.ParseTime(e.RequestedAt); err == nil && time.Since(t) > s.ttl {
		if ok, _ := store.TransitionExecution(ctx, s.st.DB, id, StPending, StExpired); ok {
			e.Status, e.FinishedAt = StExpired, store.Now()
			_ = store.SaveExecutionState(ctx, s.st.DB, e)
			s.notifyState("execution.expired", e)
		}
		return e, ErrNotPending
	}
	comment = strings.TrimSpace(comment)
	if !approve && comment == "" {
		return e, ErrNeedComment
	}
	if approve && e.Risk == string(catalog.RiskDestructive) && confirmSlug != e.ServerSlug {
		return e, ErrConfirm
	}
	to := StRejected
	if approve {
		to = StRunning
	}
	if ok, err := store.TransitionExecution(ctx, s.st.DB, id, StPending, to); err != nil {
		return e, err
	} else if !ok {
		return e, ErrNotPending
	}
	e.Status, e.DecidedBy, e.DecidedByName, e.DecidedAt, e.DecisionComment = to, approver.ID, approver.Username, store.Now(), comment
	action := "execution.reject"
	if !approve {
		e.FinishedAt = store.Now()
	} else {
		action, e.StartedAt = "execution.approve", store.Now()
	}
	_ = audit.RecordStandalone(ctx, s.st, audit.Event{ActorType: "user", ActorID: approver.ID, ActorName: approver.Username, Action: action,
		TargetType: "execution", TargetID: e.ID, TargetName: e.CommandSlug + "@" + e.ServerSlug, Diff: map[string]any{"comment": comment}})
	if !approve {
		_ = store.SaveExecutionState(ctx, s.st.DB, e)
		s.Metrics.Execution(StRejected)
		s.notifyState("execution.rejected", e)
		return e, nil
	}
	// revérification au moment d'exécuter (SPEC §8.7)
	srv, cmd, why := s.revalidate(ctx, e)
	if why != "" {
		e.Status, e.DenialReason, e.FinishedAt = StError, "revalidation_failed:"+why, store.Now()
		e.Stderr = "leassh: " + why
		_ = store.SaveExecutionState(ctx, s.st.DB, e)
		s.notify("execution.finished", e)
		return e, nil
	}
	_ = store.SaveExecutionState(ctx, s.st.DB, e)
	s.Broker.Publish(e.ID)
	s.launch(e, srv, cmd)
	return e, nil
}

// revalidate: clé active, règle toujours valide, hash identique, rendu identique.
func (s *Service) revalidate(ctx context.Context, e store.Execution) (store.Server, store.Command, string) {
	key, err := store.KeyByID(ctx, s.st.DB, e.KeyID)
	if err != nil {
		return store.Server{}, store.Command{}, "clé supprimée"
	}
	snap, err := s.acc.Load(ctx)
	if err != nil {
		return store.Server{}, store.Command{}, "configuration illisible"
	}
	srv, okS := snap.ServerBySlug(e.ServerSlug)
	cmd, okC := snap.CommandBySlug(e.CommandSlug)
	if !okS || !okC || srv.ID != e.ServerID || cmd.ID != e.CommandID {
		return srv, cmd, "serveur ou commande supprimé"
	}
	if d := snap.Evaluate(time.Now(), key, srv, cmd); !d.Allowed {
		return srv, cmd, "autorisation retirée (" + d.Reason + ")"
	}
	if cmd.Hash != e.CommandHash || catalog.Hash(cmd.Template, cmd.Params, cmd.Risk) != cmd.Hash {
		return srv, cmd, "la commande a changé depuis la demande"
	}
	var params map[string]any
	_ = json.Unmarshal([]byte(e.Params), &params)
	if r, err := catalog.Render(cmd.Template, cmd.Params, params); err != nil || e.Rendered == nil || r != *e.Rendered {
		return srv, cmd, "le rendu de la commande a changé"
	}
	return srv, cmd, ""
}

// ExpireLoop marque expirées les demandes en attente plus anciennes que le TTL.
func (s *Service) ExpireLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.ExpireNow(ctx)
		}
	}
}

func (s *Service) ExpireNow(ctx context.Context) {
	ids, err := store.ExpirePending(ctx, s.st.DB, store.FormatTime(time.Now().Add(-s.ttl)))
	if err != nil {
		s.log.Error("expiration des demandes", "err", err)
		return
	}
	for _, id := range ids {
		s.Metrics.Execution(StExpired)
		s.Broker.Publish(id)
		if e, err := store.ExecutionByID(ctx, s.st.DB, id); err == nil {
			s.Observe("execution.expired", e)
		}
	}
}

// WaitChange attend (jusqu'à d) que le statut de l'exécution diffère de `from`.
func (s *Service) WaitChange(ctx context.Context, id, from string, d time.Duration) (store.Execution, error) {
	ch, cancel := s.Broker.Subscribe()
	defer cancel()
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		e, err := store.ExecutionByID(ctx, s.st.DB, id)
		if err != nil || e.Status != from || d <= 0 {
			return e, err
		}
		select {
		case <-ch:
		case <-poll.C:
		case <-deadline.C:
			return e, nil
		case <-ctx.Done():
			return e, ctx.Err()
		}
	}
}

// sshErrorKind classe une erreur SSH pour les métriques.
func sshErrorKind(err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, sshexec.ErrOpenChannel):
		return "channel"
	case strings.Contains(msg, "connexion TCP"):
		return "connect"
	case strings.Contains(msg, "unable to authenticate"):
		return "auth"
	case strings.Contains(msg, "handshake"):
		return "handshake"
	}
	return "other"
}
