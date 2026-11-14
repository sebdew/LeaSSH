package api

import (
	"errors"
	"net/http"

	"github.com/sebdew/LeaSSH/internal/packs"
	"github.com/sebdew/LeaSSH/internal/webhook"
)

func (s *Server) listPacks(w http.ResponseWriter, r *http.Request) {
	installed, bundled, err := s.packs.Overview(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"installed": installed, "bundled": bundled})
}

// packErr traduit les erreurs de lecture d'un pack en erreurs d'API.
func packErr(err error) error {
	var ip *packs.InvalidPackError
	if errors.As(err, &ip) {
		return bad("invalid_pack", ip.Msg)
	}
	return err
}

func (s *Server) previewPack(w http.ResponseWriter, r *http.Request) {
	var in packs.Source
	if !decode(w, r, &in) {
		return
	}
	f, trusted, err := in.Load()
	if err != nil {
		s.fail(w, packErr(err))
		return
	}
	p, err := s.packs.Preview(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"preview": p, "trusted": trusted})
}

type importInput struct {
	packs.Source
	CreateGroup   bool `json:"create_group"`
	AllowWarnings bool `json:"allow_warnings"`
}

// importPack: un pack fourni avec le binaire est de confiance (avertissements acceptés); un fichier importé doit les faire accepter.
func (s *Server) importPack(w http.ResponseWriter, r *http.Request) {
	var in importInput
	if !decode(w, r, &in) {
		return
	}
	f, trusted, err := in.Load()
	if err != nil {
		s.fail(w, packErr(err))
		return
	}
	res, err := s.packs.Import(r.Context(), currentUser(r), f, s.clientIP(r), packs.Options{CreateGroup: in.CreateGroup, AllowWarnings: in.AllowWarnings || trusted})
	switch {
	case errors.Is(err, packs.ErrBlocked):
		s.fail(w, &apiError{Status: 400, Code: "import_blocked", Message: err.Error()})
	case err != nil:
		s.fail(w, err)
	default:
		writeJSON(w, 200, res)
	}
}

// exportPack: ?group=ID | ?ids=a,b | (rien = tout le catalogue); métadonnées via pack_id, name, version.
func (s *Server) exportPack(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data, meta, err := s.packs.ExportFile(r.Context(), packs.ExportRequest{GroupID: q.Get("group"), IDs: packs.ParseIDs(q.Get("ids")),
		Meta: packs.Meta{ID: q.Get("pack_id"), Name: q.Get("name"), Version: q.Get("version")}})
	if errors.Is(err, packs.ErrEmptyGroup) {
		s.fail(w, bad("empty_group", err.Error()))
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+meta.ID+`.yaml"`)
	_, _ = w.Write(data) //nolint:gosec // YAML servi en téléchargement (application/yaml), jamais interprété comme HTML
}

// ---- webhook ----

func (s *Server) getWebhook(w http.ResponseWriter, r *http.Request) {
	cfg, _, err := s.hook.Config(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"config": cfg, "events": webhook.Events})
}

type webhookInput struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
	// ClearSecret supprime le secret de signature (ignoré si un nouveau secret est fourni).
	ClearSecret bool `json:"clear_secret"`
	Enabled     bool `json:"enabled"`
}

func (s *Server) putWebhook(w http.ResponseWriter, r *http.Request) {
	var in webhookInput
	if !decode(w, r, &in) {
		return
	}
	cfg, err := s.hook.Update(r.Context(), currentUser(r), s.clientIP(r), webhook.UpdateInput{URL: in.URL, Secret: in.Secret, ClearSecret: in.ClearSecret, Enabled: in.Enabled})
	var iu *webhook.InvalidURLError
	switch {
	case errors.As(err, &iu):
		s.fail(w, bad("invalid_url", iu.Msg))
	case errors.Is(err, webhook.ErrURLRequired):
		s.fail(w, bad("url_required", err.Error()))
	case err != nil:
		s.fail(w, err)
	default:
		writeJSON(w, 200, map[string]any{"config": cfg})
	}
}

// testWebhook: envoi d'essai immédiat (une seule tentative) avec le résultat détaillé.
func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	res, err := s.hook.Test(r.Context())
	switch {
	case errors.Is(err, webhook.ErrNotConfigured):
		s.fail(w, bad("not_configured", err.Error()))
	case err != nil:
		s.fail(w, err)
	default:
		writeJSON(w, 200, res)
	}
}

func (s *Server) webhookDeliveries(w http.ResponseWriter, r *http.Request) {
	items, err := s.hook.Deliveries(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
