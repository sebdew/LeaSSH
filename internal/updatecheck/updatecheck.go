// Package updatecheck signale qu'une version plus récente de Leassh est publiée (ticket #86).
//
// Le serveur interroge l'API publique des releases GitHub, jamais le navigateur. La requête ne transporte aucune donnée
// d'instance. Elle est désactivable (LEASSH_UPDATE_CHECK=false) pour les environnements isolés.
package updatecheck

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const (
	// DefaultURL: dernière release publiée (hors pré-versions) du dépôt.
	DefaultURL = "https://api.github.com/repos/sebdew/LeaSSH/releases/latest"
	// releasePrefix: seuls les liens du dépôt sont repris dans l'interface.
	releasePrefix = "https://github.com/sebdew/LeaSSH/"
	interval      = 24 * time.Hour
	maxBody       = 1 << 20
)

// Status est l'état exposé à l'interface.
type Status struct {
	Enabled   bool
	Available bool
	Latest    string
	URL       string
}

type Checker struct {
	URL     string // surchargeable pour les tests
	enabled bool
	current string
	client  *http.Client
	log     *slog.Logger

	mu     sync.RWMutex
	latest string
	link   string
}

// New construit le vérificateur. rt (facultatif) permet d'instrumenter les appels sortants.
func New(enabled bool, current string, rt http.RoundTripper) *Checker {
	return &Checker{URL: DefaultURL, enabled: enabled, current: current, client: &http.Client{Timeout: 10 * time.Second, Transport: rt}, log: slog.Default()}
}

// Run interroge GitHub au démarrage puis une fois par jour, sans jamais bloquer le démarrage ni les requêtes de l'interface.
func (c *Checker) Run(ctx context.Context) {
	if !c.enabled {
		return
	}
	for {
		c.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// Refresh effectue une vérification. Toute erreur est ignorée (réseau coupé, limite d'API): l'interface n'affiche alors rien.
func (c *Checker) Refresh(ctx context.Context) {
	if !c.enabled {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "leassh-update-check")
	resp, err := c.client.Do(req) // #nosec G107 -- URL fixe, jamais issue d'une entrée utilisateur
	if err != nil {
		c.log.Debug("vérification de version impossible", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var rel struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&rel) != nil {
		return
	}
	if !semver.IsValid(rel.Tag) || semver.Prerelease(rel.Tag) != "" || !strings.HasPrefix(rel.URL, releasePrefix) {
		return
	}
	c.mu.Lock()
	c.latest, c.link = rel.Tag, rel.URL
	c.mu.Unlock()
}

// Status compare la version courante à la dernière release connue. Un build intermédiaire (v0.1.0-51-gabc) vaut sa version
// de base; « dev » ou une version illisible ne déclenche jamais d'alerte.
func (c *Checker) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st := Status{Enabled: c.enabled}
	if !c.enabled || c.latest == "" {
		return st
	}
	st.Latest, st.URL = c.latest, c.link
	cur := c.current
	if !semver.IsValid(cur) {
		return st
	}
	base := strings.TrimSuffix(cur, semver.Prerelease(cur)) // v0.1.0-51-gabc -> v0.1.0
	st.Available = semver.Compare(base, c.latest) < 0
	return st
}
