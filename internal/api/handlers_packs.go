package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/leash-dev/leash/internal/audit"
	"github.com/leash-dev/leash/internal/packs"
	"github.com/leash-dev/leash/internal/store"
	"github.com/leash-dev/leash/internal/webhook"
)

type bundledInfo struct {
	packs.Meta
	CommandCount     int            `json:"command_count"`
	InstalledVersion string         `json:"installed_version"`
	Risks            map[string]int `json:"risks"`       // commandes par niveau de risque
	CommandIDs       []string       `json:"command_ids"` // pour la recherche
}

func (s *Server) listPacks(w http.ResponseWriter, r *http.Request) {
	installed, err := store.ListPacks(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	bundled, err := packs.Bundled()
	if err != nil {
		s.fail(w, err)
		return
	}
	ver := map[string]string{}
	for _, p := range installed {
		ver[p.ID] = p.Version
	}
	var list []bundledInfo
	for id, f := range bundled {
		bi := bundledInfo{Meta: f.Pack, CommandCount: len(f.Commands), InstalledVersion: ver[id], Risks: map[string]int{"read": 0, "write": 0, "destructive": 0}, CommandIDs: []string{}}
		for _, c := range f.Commands {
			bi.Risks[string(c.Risk)]++
			bi.CommandIDs = append(bi.CommandIDs, c.ID)
		}
		list = append(list, bi)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	writeJSON(w, 200, map[string]any{"installed": installed, "bundled": list})
}

type packSource struct {
	Bundled string `json:"bundled"` // id d'un pack fourni
	YAML    string `json:"yaml"`    // ou contenu d'un fichier
}

func (in packSource) load() (*packs.File, bool, error) {
	if in.Bundled != "" {
		all, err := packs.Bundled()
		if err != nil {
			return nil, false, err
		}
		f, ok := all[in.Bundled]
		if !ok {
			return nil, false, store.ErrNotFound
		}
		return f, true, nil
	}
	f, err := packs.Parse([]byte(in.YAML))
	if err != nil {
		return nil, false, bad("invalid_pack", err.Error())
	}
	return f, false, nil
}

func (s *Server) previewPack(w http.ResponseWriter, r *http.Request) {
	var in packSource
	if !decode(w, r, &in) {
		return
	}
	f, trusted, err := in.load()
	if err != nil {
		s.fail(w, err)
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
	packSource
	CreateGroup   bool `json:"create_group"`
	AllowWarnings bool `json:"allow_warnings"`
}

// importPack: un pack fourni avec le binaire est de confiance (avertissements acceptés); un fichier importé doit les faire accepter.
func (s *Server) importPack(w http.ResponseWriter, r *http.Request) {
	var in importInput
	if !decode(w, r, &in) {
		return
	}
	f, trusted, err := in.load()
	if err != nil {
		s.fail(w, err)
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
	var ids []string
	if g := q.Get("group"); g != "" {
		grp, err := store.GroupByID(r.Context(), s.st.DB, store.CommandGroups, g)
		if err != nil {
			s.fail(w, err)
			return
		}
		ids = grp.MemberIDs
		if len(ids) == 0 {
			s.fail(w, bad("empty_group", "le groupe ne contient aucune commande"))
			return
		}
	} else if v := q.Get("ids"); v != "" {
		ids = strings.Split(v, ",")
	}
	meta := packs.Meta{ID: q.Get("pack_id"), Name: q.Get("name"), Version: q.Get("version")}
	if meta.ID == "" {
		meta.ID = "export"
	}
	if meta.Name == "" {
		meta.Name = "Export Leash"
	}
	if meta.Version == "" {
		meta.Version = "1.0.0"
	}
	data, err := s.packs.Export(r.Context(), ids, meta)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+meta.ID+`.yaml"`)
	_, _ = w.Write(data)
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
	URL     string `json:"url"`
	Secret  string `json:"secret"`
	Enabled bool   `json:"enabled"`
}

func (s *Server) putWebhook(w http.ResponseWriter, r *http.Request) {
	var in webhookInput
	if !decode(w, r, &in) {
		return
	}
	in.URL = strings.TrimSpace(in.URL)
	if in.URL != "" {
		if err := webhook.ValidateURL(in.URL); err != nil {
			s.fail(w, bad("invalid_url", err.Error()))
			return
		}
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, secret, err := s.hook.ConfigQ(ctx, tx)
		if err != nil {
			return outcome{}, err
		}
		if in.Enabled {
			if in.URL == "" {
				return outcome{}, bad("url_required", "une URL est requise pour activer le webhook")
			}
			if in.Secret == "" && secret == "" {
				return outcome{}, bad("secret_required", "un secret de signature est requis pour activer le webhook")
			}
		}
		if err := s.hook.Save(ctx, tx, in.URL, in.Secret, in.Enabled); err != nil {
			return outcome{}, err
		}
		cfg := webhook.Config{URL: in.URL, Enabled: in.Enabled, HasSecret: secret != "" || in.Secret != ""}
		return outcome{200, map[string]any{"config": cfg}, &audit.Event{Action: "webhook.update", TargetType: "webhook", TargetName: in.URL,
			Diff: map[string]any{"before": map[string]any{"url": old.URL, "enabled": old.Enabled},
				"after": map[string]any{"url": in.URL, "enabled": in.Enabled, "signing_key_rotated": in.Secret != ""}}}}, nil
	})
}

// testWebhook: envoi d'essai immédiat (une seule tentative) avec le résultat détaillé.
func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	cfg, secret, err := s.hook.Config(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	if cfg.URL == "" || secret == "" {
		s.fail(w, bad("not_configured", "renseignez une URL et un secret avant de tester"))
		return
	}
	res := s.hook.Send(r.Context(), cfg.URL, secret, "webhook.test", map[string]any{"message": "Test d'envoi depuis Leash"}, s.hook.Link("/reglages"), 1)
	_ = store.InsertDelivery(r.Context(), s.st.DB, store.WebhookDelivery{Event: "webhook.test", OK: res.OK, Attempts: res.Attempts, HTTPStatus: res.Status, Error: res.Error, URL: cfg.URL})
	writeJSON(w, 200, res)
}

func (s *Server) webhookDeliveries(w http.ResponseWriter, r *http.Request) {
	items, err := store.ListDeliveries(r.Context(), s.st.DB, 50)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
