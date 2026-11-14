// Package warpgate: connecteur de provisioning pour le bastion Warpgate (API utilisateur, lecture seule).
package warpgate

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sebdew/LeaSSH/internal/provisioning"
)

const maxBody = 4 << 20

type target struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Group       *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"group"`
}

type connector struct {
	base    *url.URL
	token   string
	sshHost string
	sshPort int
	user    string
	fprint  string
	client  *http.Client
}

// Type est à inscrire avec provisioning.Register.
func Type() provisioning.Type {
	return provisioning.Type{
		ID:    "warpgate",
		Label: "Warpgate",
		Fields: []provisioning.Field{
			{Key: "url", Kind: provisioning.FieldText, Label: "URL de Warpgate", Help: "Interface web, ex. https://warpgate.exemple.fr:8888", Required: true},
			{Key: "token", Kind: provisioning.FieldPassword, Label: "Jeton d'API de l'utilisateur", Help: "Jeton personnel du compte Warpgate utilisé par Leassh (il n'a pas besoin d'être administrateur).", Required: true},
			{Key: "ssh_host", Kind: provisioning.FieldText, Label: "Hôte SSH du bastion", Required: true},
			{Key: "ssh_port", Kind: provisioning.FieldNumber, Label: "Port SSH du bastion", Default: 2222},
			{Key: "tls_insecure", Kind: provisioning.FieldBool, Label: "Ne pas vérifier le certificat HTTPS", Help: "À éviter: n'utilisez cette option que pour un certificat auto-signé sur un réseau de confiance."},
		},
		New: func(cfg map[string]any, secret string, env provisioning.Env) (provisioning.Connector, error) {
			return New(cfg, secret, env)
		},
	}
}

func New(cfg map[string]any, secret string, env provisioning.Env) (provisioning.Connector, error) {
	raw := strings.TrimRight(strings.TrimSpace(provisioning.Str(cfg, "url")), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("URL de Warpgate invalide (http:// ou https://)")
	}
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("jeton d'API requis")
	}
	host := strings.TrimSpace(provisioning.Str(cfg, "ssh_host"))
	if host == "" {
		return nil, errors.New("hôte SSH du bastion requis")
	}
	port := provisioning.Int(cfg, "ssh_port", 2222)
	if port < 1 || port > 65535 {
		return nil, errors.New("port SSH invalide")
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: provisioning.Bool(cfg, "tls_insecure")}} //nolint:gosec // choix explicite de l'admin
	return &connector{
		base: u, token: strings.TrimSpace(secret), sshHost: host, sshPort: port, user: env.User,
		fprint: provisioning.Str(cfg, "host_key_fingerprint"),
		client: &http.Client{Timeout: 20 * time.Second, Transport: tr,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *connector) Endpoint() (string, int) { return c.sshHost, c.sshPort }

func (c *connector) Discover(ctx context.Context) ([]provisioning.Machine, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+"/@warpgate/api/targets", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Warpgate-Token", c.token)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Warpgate injoignable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("lecture de la réponse: %w", err)
	}
	if len(body) > maxBody {
		return nil, errors.New("réponse de Warpgate trop volumineuse")
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("Warpgate refuse le jeton (HTTP %d): vérifiez qu'il est valide et non expiré", resp.StatusCode)
	case resp.StatusCode >= 300:
		return nil, fmt.Errorf("Warpgate a répondu HTTP %d", resp.StatusCode)
	}
	var ts []target
	if err := json.Unmarshal(body, &ts); err != nil {
		return nil, errors.New("réponse de Warpgate illisible (une page de connexion au lieu de JSON ? vérifiez l'URL et le jeton)")
	}
	out := make([]provisioning.Machine, 0, len(ts))
	for _, t := range ts {
		if t.Kind != "Ssh" || t.ID == "" || strings.TrimSpace(t.Name) == "" { // seules les cibles SSH concernent Leassh
			continue
		}
		m := provisioning.Machine{
			ExternalID: t.ID, Name: t.Name, Description: t.Description,
			Host: c.sshHost, Port: c.sshPort, SSHUser: c.user + ":" + t.Name, HostKeyFingerp: c.fprint,
		}
		if t.Group != nil && t.Group.ID != "" {
			m.GroupID, m.GroupName = t.Group.ID, t.Group.Name
		}
		out = append(out, m)
	}
	return out, nil
}
