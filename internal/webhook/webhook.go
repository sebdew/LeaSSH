// Package webhook: un webhook sortant générique, corps JSON signé HMAC-SHA256 (SPEC §11).
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/leash-dev/leash/internal/crypto"
	"github.com/leash-dev/leash/internal/store"
)

const (
	keyURL     = "webhook.url"
	keySecret  = "webhook.secret_enc"
	keyEnabled = "webhook.enabled"
	maxAttempt = 3
)

// Events émis (SPEC §11).
var Events = []string{"execution.pending_approval", "execution.finished", "execution.denied", "proposal.created", "host_key.mismatch"}

type Config struct {
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
	HasSecret bool   `json:"has_secret"`
}

// ValidateURL: http(s) uniquement, avec hôte.
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("URL invalide: http:// ou https:// attendu")
	}
	return nil
}

// Sign renvoie la valeur de l'en-tête X-Leash-Signature.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

type job struct {
	event string
	data  any
	link  string
}

type Dispatcher struct {
	st      *store.Store
	box     *crypto.Box
	baseURL string
	log     *slog.Logger
	client  *http.Client
	queue   chan job
	// Backoff entre les tentatives (modifiable pour les tests).
	Backoff []time.Duration
}

func New(st *store.Store, box *crypto.Box, baseURL string, log *slog.Logger) *Dispatcher {
	return &Dispatcher{
		st: st, box: box, baseURL: strings.TrimRight(baseURL, "/"), log: log, queue: make(chan job, 256),
		Backoff: []time.Duration{time.Second, 5 * time.Second},
		client: &http.Client{Timeout: 10 * time.Second,
			// pas de redirection suivie: une redirection ne doit pas détourner la livraison signée
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

// Link construit l'URL absolue d'un écran de l'interface (vide si LEASH_BASE_URL n'est pas définie).
func (d *Dispatcher) Link(path string) string {
	if d.baseURL == "" {
		return ""
	}
	return d.baseURL + path
}

func (d *Dispatcher) Config(ctx context.Context) (Config, string, error) {
	return d.ConfigQ(ctx, d.st.DB)
}

// ConfigQ lit la configuration via q (DB ou transaction courante: la base n'a qu'une connexion).
func (d *Dispatcher) ConfigQ(ctx context.Context, q store.Q) (Config, string, error) {
	u, err := store.GetSetting(ctx, q, keyURL)
	if err != nil {
		return Config{}, "", err
	}
	en, _ := store.GetSetting(ctx, q, keyEnabled)
	enc, _ := store.GetSetting(ctx, q, keySecret)
	secret := ""
	if enc != "" {
		blob, err := base64.StdEncoding.DecodeString(enc)
		if err == nil {
			if b, err := d.box.Open(blob); err == nil {
				secret = string(b)
			}
		}
	}
	return Config{URL: u, Enabled: en == "1", HasSecret: secret != ""}, secret, nil
}

// Save enregistre la configuration; secret vide = secret inchangé.
func (d *Dispatcher) Save(ctx context.Context, q store.Q, url, secret string, enabled bool) error {
	if err := store.SetSetting(ctx, q, keyURL, url); err != nil {
		return err
	}
	if secret != "" {
		sealed, err := d.box.Seal([]byte(secret))
		if err != nil {
			return err
		}
		if err := store.SetSetting(ctx, q, keySecret, base64.StdEncoding.EncodeToString(sealed)); err != nil {
			return err
		}
	}
	v := "0"
	if enabled {
		v = "1"
	}
	return store.SetSetting(ctx, q, keyEnabled, v)
}

// Enqueue ne bloque jamais l'appelant; en cas de file pleine l'événement est abandonné (et journalisé).
func (d *Dispatcher) Enqueue(event string, data any, link string) {
	select {
	case d.queue <- job{event, data, link}:
	default:
		d.log.Warn("file webhook pleine, événement abandonné", "event", event)
	}
}

// Run traite la file jusqu'à l'annulation du contexte.
func (d *Dispatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-d.queue:
			d.deliver(ctx, j)
		}
	}
}

type envelope struct {
	ID        string `json:"id"`
	Event     string `json:"event"`
	Timestamp string `json:"timestamp"`
	URL       string `json:"url,omitempty"`
	Data      any    `json:"data"`
}

func (d *Dispatcher) deliver(ctx context.Context, j job) {
	cfg, secret, err := d.Config(ctx)
	if err != nil || !cfg.Enabled || cfg.URL == "" || secret == "" {
		return
	}
	res := d.Send(ctx, cfg.URL, secret, j.event, j.data, j.link, maxAttempt)
	if err := store.InsertDelivery(context.WithoutCancel(ctx), d.st.DB, store.WebhookDelivery{Event: j.event, OK: res.OK, Attempts: res.Attempts, HTTPStatus: res.Status, Error: res.Error, URL: cfg.URL}); err != nil {
		d.log.Error("journal des livraisons webhook", "err", err)
	}
}

type Result struct {
	OK       bool   `json:"ok"`
	Attempts int    `json:"attempts"`
	Status   int    `json:"http_status"`
	Error    string `json:"error,omitempty"`
}

// Send livre un événement (jusqu'à `attempts` essais avec backoff). Utilisé aussi pour le test d'envoi.
func (d *Dispatcher) Send(ctx context.Context, target, secret, event string, data any, link string, attempts int) Result {
	body, err := json.Marshal(envelope{ID: store.NewID(), Event: event, Timestamp: store.Now(), URL: link, Data: data})
	if err != nil {
		return Result{Error: "sérialisation: " + err.Error()}
	}
	var res Result
	for i := 0; i < attempts; i++ {
		if i > 0 {
			wait := d.Backoff[min(i-1, len(d.Backoff)-1)]
			select {
			case <-ctx.Done():
				return res
			case <-time.After(wait):
			}
		}
		res.Attempts = i + 1
		res.Status, res.Error = d.once(ctx, target, secret, event, body)
		if res.Error == "" {
			res.OK = true
			return res
		}
	}
	return res
}

func (d *Dispatcher) once(ctx context.Context, target, secret, event string, body []byte) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "leash-webhook")
	req.Header.Set("X-Leash-Event", event)
	req.Header.Set("X-Leash-Signature", Sign(secret, body))
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, truncate(err.Error())
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Sprintf("réponse HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, ""
}

func truncate(s string) string {
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

// ---- charges utiles ----

// ExecutionData: résumé d'une exécution, sans sortie (stdout/stderr) ni secret.
func ExecutionData(e store.Execution) map[string]any {
	agent := e.KeyName
	if agent == "" {
		agent = e.DecidedByName
	}
	rendered := ""
	if e.Rendered != nil {
		rendered = *e.Rendered
	}
	return map[string]any{"id": e.ID, "status": e.Status, "agent": agent, "server": e.ServerSlug, "command": e.CommandSlug, "risk": e.Risk,
		"reason": e.Reason, "rendered": rendered, "denial_reason": e.DenialReason, "exit_code": e.ExitCode, "duration_ms": e.DurationMS,
		"requested_at": e.RequestedAt, "finished_at": e.FinishedAt, "decided_by": e.DecidedByName, "rule": e.RuleName}
}

func ProposalData(p store.Proposal) map[string]any {
	var payload struct{ Slug, Name, Risk string }
	_ = json.Unmarshal([]byte(p.Payload), &payload)
	return map[string]any{"id": p.ID, "agent": p.KeyName, "slug": payload.Slug, "name": payload.Name, "risk": payload.Risk, "status": p.Status, "justification": p.Justification, "created_at": p.CreatedAt}
}

// OnExecution / OnProposal sont branchés sur executions.Service.Notify et proposals.Service.Notify.
func (d *Dispatcher) OnExecution(event string, e store.Execution) {
	switch event {
	case "host_key.mismatch":
		d.Enqueue(event, map[string]any{"server": e.ServerSlug}, d.Link("/serveurs"))
	case "execution.pending_approval":
		d.Enqueue(event, ExecutionData(e), d.Link("/validations"))
	default:
		d.Enqueue(event, ExecutionData(e), d.Link("/journal?exec="+e.ID))
	}
}

func (d *Dispatcher) OnProposal(event string, p store.Proposal) {
	d.Enqueue(event, ProposalData(p), d.Link("/propositions/"+p.ID))
}
