package auditreport

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

// Réglages de la synthèse IA (désactivée par défaut). Compatible avec toute API « chat completions » de type OpenAI
// (OpenAI, LiteLLM, Ollama, vLLM…). La clé d'API est chiffrée avec la clé maître, jamais renvoyée.
const (
	keyAIEnabled = "audit_ai.enabled"
	keyAIURL     = "audit_ai.url"
	keyAIModel   = "audit_ai.model"
	KeyAISecret  = "audit_ai.key_enc" //nolint:gosec // nom de clé de configuration, pas un secret // aussi réécrit par la rotation de la clé maître
)

type AIConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Model   string `json:"model"`
	HasKey  bool   `json:"has_key"`
}

type summarizeOpts struct{ transport http.RoundTripper }

// SummarizeOption ajuste l'appel au modèle.
type SummarizeOption func(*summarizeOpts)

// WithTransport remplace le transport HTTP (mesure des durées d'appel, tests).
func WithTransport(rt http.RoundTripper) SummarizeOption {
	return func(o *summarizeOpts) { o.transport = rt }
}

// AIDefaults: valeurs issues de LLM_BASE_URL, LLM_API_KEY et LLM_MODEL, utilisées tant que l'interface n'a rien enregistré.
// Elles ne modifient jamais l'activation: la synthèse reste désactivée tant qu'un administrateur ne l'active pas.
type AIDefaults struct{ URL, Model, APIKey string }

// Apply complète c et la clé avec les valeurs par défaut pour ce qui est vide.
func (d AIDefaults) Apply(c AIConfig, key string) (AIConfig, string) {
	if c.URL == "" {
		c.URL = d.URL
	}
	if c.Model == "" {
		c.Model = d.Model
	}
	if key == "" && d.APIKey != "" {
		key, c.HasKey = d.APIKey, true
	}
	return c, key
}

func LoadAIConfig(ctx context.Context, q store.Q, box *crypto.Box) (AIConfig, string, error) {
	var c AIConfig
	en, err := store.GetSetting(ctx, q, keyAIEnabled)
	if err != nil {
		return c, "", err
	}
	c.Enabled = en == "1"
	if c.URL, err = store.GetSetting(ctx, q, keyAIURL); err != nil {
		return c, "", err
	}
	if c.Model, err = store.GetSetting(ctx, q, keyAIModel); err != nil {
		return c, "", err
	}
	enc, err := store.GetSetting(ctx, q, KeyAISecret)
	if err != nil {
		return c, "", err
	}
	var secret string
	if enc != "" {
		blob, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return c, "", fmt.Errorf("clé d'API: %w", err)
		}
		plain, err := box.Open(blob)
		if err != nil {
			return c, "", fmt.Errorf("clé d'API: %w", err)
		}
		secret, c.HasKey = string(plain), true
	}
	return c, secret, nil
}

// SaveAIConfig enregistre les réglages; apiKey vide = clé inchangée.
func SaveAIConfig(ctx context.Context, q store.Q, box *crypto.Box, c AIConfig, apiKey string) error {
	en := "0"
	if c.Enabled {
		en = "1"
	}
	for k, v := range map[string]string{keyAIEnabled: en, keyAIURL: c.URL, keyAIModel: c.Model} {
		if err := store.SetSetting(ctx, q, k, v); err != nil {
			return err
		}
	}
	if apiKey != "" {
		sealed, err := box.Seal([]byte(apiKey))
		if err != nil {
			return err
		}
		return store.SetSetting(ctx, q, KeyAISecret, base64.StdEncoding.EncodeToString(sealed))
	}
	return nil
}

// ValidateAIURL: http(s) uniquement. Une URL interne (LiteLLM, Ollama) est légitime; seul un administrateur la configure.
func ValidateAIURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("URL invalide: http:// ou https:// attendu (ex. https://api.exemple.com/v1)")
	}
	return nil
}

// AIInput est TOUT ce qui peut quitter l'instance: des comptes et des intitulés de constats fixes.
// Jamais de nom de serveur, de clé, d'utilisateur, de commande, de paramètre, de motif, de sortie ni d'adresse.
// Les types ne portent que des entiers, des booléens et des chaînes issues de constantes du programme.
type AIInput struct {
	PeriodDays     int         `json:"period_days"`
	Servers        AICounts    `json:"servers"`
	Keys           AICounts    `json:"mcp_keys"`
	Rules          AICounts    `json:"access_rules"`
	Users          AICounts    `json:"admin_accounts"`
	Commands       AICounts    `json:"commands"`
	Activity       AICounts    `json:"activity"`
	Approvals      AICounts    `json:"human_approvals"`
	ChainsIntact   bool        `json:"integrity_chains_intact"`
	RetentionDays  int         `json:"log_retention_days"`
	HTTPSAdminUI   bool        `json:"admin_ui_https"`
	WebhookEnabled bool        `json:"external_alerts_enabled"`
	Findings       []AIFinding `json:"findings"`
}

type AICounts map[string]int

type AIFinding struct {
	Severity string `json:"severity"`
	Title    string `json:"title"` // intitulé fixe du programme
	Count    int    `json:"count"`
}

// BuildAIInput projette l'instantané sur la liste blanche ci-dessus.
func BuildAIInput(s Snapshot) AIInput {
	in := AIInput{
		PeriodDays:     s.Period.Days,
		Servers:        AICounts{"total": s.Scope.Servers.Total, "pinned_host_key": s.Scope.Servers.Pinned, "unverified_host_key": s.Scope.Servers.Unknown, "host_key_mismatch": s.Scope.Servers.Mismatch},
		Keys:           AICounts{"total": s.Scope.Keys.Total, "active": s.Scope.Keys.Active, "active_without_expiry": s.Scope.Keys.NoExpiry, "active_unused_90d": s.Scope.Keys.Unused},
		Rules:          AICounts{"total": s.Scope.Rules.Total, "read_only": s.Scope.Rules.ReadOnly, "read_write": s.Scope.Rules.ReadWrite, "read_write_without_approval": s.Scope.Rules.RWNoApproval},
		Users:          AICounts{"enabled_admins": s.Scope.Users.Admins, "disabled": s.Scope.Users.Disabled},
		Commands:       AICounts{"total": s.Scope.Commands.Total, "read": s.Scope.Commands.ByRisk["read"], "write": s.Scope.Commands.ByRisk["write"], "destructive": s.Scope.Commands.ByRisk["destructive"], "from_agent_proposals": s.Scope.Commands.BySource["proposal"]},
		Activity:       AICounts{"requests": s.Activity.Requests, "succeeded": s.Activity.Succeeded, "failed": s.Activity.Failed, "denied": s.Activity.Denied, "destructive_requests": s.Activity.DestructiveTot, "write_without_approval": s.Activity.WriteNoApproval},
		Approvals:      AICounts{"approved": s.Activity.Approvals.Approved, "rejected": s.Activity.Approvals.Rejected, "expired": s.Activity.Approvals.Expired, "avg_delay_seconds": int(s.Activity.Approvals.AvgDelaySec)},
		ChainsIntact:   s.Integrity.Executions.Verified.OK && s.Integrity.Admin.Verified.OK,
		RetentionDays:  s.Config.RetentionDays,
		HTTPSAdminUI:   s.Config.BaseURLHTTPS,
		WebhookEnabled: s.Config.WebhookEnabled,
		Findings:       []AIFinding{},
	}
	for _, f := range s.Findings {
		in.Findings = append(in.Findings, AIFinding{Severity: string(f.Severity), Title: f.Title, Count: f.Count})
	}
	return in
}

const aiSystemPromptFR = `Tu aides à rédiger la synthèse d'un rapport d'audit de sécurité pour un outil qui donne à des agents IA un accès SSH restreint à un catalogue de commandes validées.
Règles strictes:
- Utilise UNIQUEMENT les données JSON fournies. N'invente aucun chiffre, aucun nom, aucun fait. Si une information manque, dis-le.
- Ne conclus jamais à la conformité ou à la non-conformité à ISO 27001 ou HDS: ce n'est pas ton rôle, l'auditeur décide.
- Écris en français, ton factuel et sobre, sans promesse.
- Structure en trois parties courtes séparées par une ligne vide: « Résumé » (3 à 4 phrases), « Points d'attention prioritaires » (liste à tirets, du plus grave au moins grave, en t'appuyant sur les constats fournis), « Pistes de recommandation » (liste à tirets, concrètes, une par point d'attention).
- 300 mots maximum.`

const aiSystemPromptEN = `You help write the summary of a security audit report for a tool that gives AI agents restricted SSH access to a catalog of validated commands.
Strict rules:
- Use ONLY the JSON data provided. Do not invent any figure, name or fact. If information is missing, say so.
- Never conclude that anything is compliant or non-compliant with ISO 27001 or HDS: that is not your role, the auditor decides.
- Write in English, in a factual and sober tone, with no promises. The finding titles in the data are in French: translate them.
- Structure the text in three short parts separated by a blank line: "Summary" (3 to 4 sentences), "Priority points of attention" (dash list, from most to least serious, based on the findings provided), "Recommendations" (dash list, concrete, one per point of attention).
- 300 words maximum.`

func aiPrompts(lang string) (system, userPrefix string) {
	if lang == "en" {
		return aiSystemPromptEN, "Aggregated data for the period (JSON):\n"
	}
	return aiSystemPromptFR, "Données agrégées de la période (JSON):\n"
}

const (
	aiTimeout  = 90 * time.Second
	aiMaxBytes = 1 << 20
)

// Summarize envoie l'agrégat au modèle configuré et renvoie le texte et le nom du modèle.
// lang: "fr" (défaut) ou "en", langue de la synthèse rédigée.
func Summarize(ctx context.Context, cfg AIConfig, apiKey string, in AIInput, lang string, opts ...SummarizeOption) (string, string, error) {
	var o summarizeOpts
	for _, f := range opts {
		f(&o)
	}
	if !cfg.Enabled {
		return "", "", errors.New("la synthèse IA est désactivée")
	}
	if err := ValidateAIURL(cfg.URL); err != nil || cfg.Model == "" {
		return "", "", errors.New("URL ou modèle de l'IA non configuré")
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return "", "", err
	}
	system, userPrefix := aiPrompts(lang)
	body, _ := json.Marshal(map[string]any{
		"model": cfg.Model, "temperature": 0.2, "max_tokens": 1200,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": userPrefix + string(payload)},
		},
	})
	ctx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.URL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Transport: o.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("modèle injoignable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, aiMaxBytes))
	if err != nil {
		return "", "", fmt.Errorf("lecture de la réponse: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("le modèle a répondu HTTP %d", resp.StatusCode)
	}
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", "", errors.New("réponse du modèle vide ou illisible")
	}
	model := out.Model
	if model == "" {
		model = cfg.Model
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), model, nil
}
