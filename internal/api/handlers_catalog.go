package api

import (
	"errors"
	"net/http"

	"github.com/sebdew/LeaSSH/internal/catalog"
	"github.com/sebdew/LeaSSH/internal/commands"
	"github.com/sebdew/LeaSSH/internal/executions"
	"github.com/sebdew/LeaSSH/internal/groups"
	"github.com/sebdew/LeaSSH/internal/store"
)

type commandInput struct {
	Slug        string                      `json:"slug"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Risk        catalog.Risk                `json:"risk"`
	Template    string                      `json:"template"`
	Params      map[string]catalog.ParamDef `json:"params"`
	TimeoutS    int                         `json:"timeout_s"`
	MaxOutputKB int                         `json:"max_output_kb"`
	Status      string                      `json:"status"`
}

func (in commandInput) toService() commands.Input {
	return commands.Input{Slug: in.Slug, Name: in.Name, Description: in.Description, Risk: in.Risk, Template: in.Template, Params: in.Params,
		TimeoutS: in.TimeoutS, MaxOutputKB: in.MaxOutputKB, Status: in.Status}
}

// catalogErr traduit les erreurs de validation des services commands et groups en erreurs d'API.
func catalogErr(err error) error {
	var le *commands.LintError
	switch {
	case errors.As(err, &le):
		return &apiError{Status: 400, Code: "lint_failed", Message: le.Error(), Details: le.Result}
	case errors.Is(err, catalog.ErrInvalidSlug):
		return bad("invalid_slug", err.Error())
	case errors.Is(err, commands.ErrInvalidName), errors.Is(err, groups.ErrInvalidName):
		return bad("invalid_name", err.Error())
	case errors.Is(err, commands.ErrInvalidDescription):
		return bad("invalid_description", err.Error())
	case errors.Is(err, commands.ErrInvalidStatus):
		return bad("invalid_status", err.Error())
	case errors.Is(err, commands.ErrInvalidTimeout):
		return bad("invalid_timeout", err.Error())
	case errors.Is(err, commands.ErrInvalidMaxOutput):
		return bad("invalid_max_output", err.Error())
	case errors.Is(err, groups.ErrManagedLocked), errors.Is(err, groups.ErrManagedUndeletable):
		return managedErr(err.Error())
	}
	return err
}

func (s *Server) listCommands(w http.ResponseWriter, r *http.Request) {
	cmds, err := s.commands.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": cmds})
}

func (s *Server) getCommand(w http.ResponseWriter, r *http.Request) {
	c, err := s.commands.Get(r.Context(), id(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) createCommand(w http.ResponseWriter, r *http.Request) {
	var in commandInput
	if !decode(w, r, &in) {
		return
	}
	c, lint, err := s.commands.Create(r.Context(), currentUser(r), s.clientIP(r), in.toService())
	if err != nil {
		s.fail(w, catalogErr(err))
		return
	}
	writeJSON(w, 201, map[string]any{"command": c, "lint": lint})
}

func (s *Server) updateCommand(w http.ResponseWriter, r *http.Request) {
	var in commandInput
	if !decode(w, r, &in) {
		return
	}
	c, lint, err := s.commands.Update(r.Context(), currentUser(r), s.clientIP(r), id(r), in.toService())
	if err != nil {
		s.fail(w, catalogErr(err))
		return
	}
	writeJSON(w, 200, map[string]any{"command": c, "lint": lint})
}

func (s *Server) deleteCommand(w http.ResponseWriter, r *http.Request) {
	if err := s.commands.Delete(r.Context(), currentUser(r), s.clientIP(r), id(r)); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

type lintInput struct {
	Template string                      `json:"template"`
	Params   map[string]catalog.ParamDef `json:"params"`
	Risk     catalog.Risk                `json:"risk"`
}

// lintCommand: linter en direct sous le champ template; renvoie aussi les placeholders détectés (paramètres à déclarer).
func (s *Server) lintCommand(w http.ResponseWriter, r *http.Request) {
	var in lintInput
	if !decode(w, r, &in) {
		return
	}
	writeJSON(w, 200, map[string]any{
		"lint":         catalog.Lint(in.Template, in.Params, in.Risk, catalog.ModeAdmin),
		"placeholders": catalog.PlaceholderNames(in.Template),
	})
}

type previewInput struct {
	lintInput
	Values map[string]any `json:"values"`
}

// previewCommand: commande rendue avec l'échappement réel (même fonction que l'exécution).
func (s *Server) previewCommand(w http.ResponseWriter, r *http.Request) {
	var in previewInput
	if !decode(w, r, &in) {
		return
	}
	rendered, err := catalog.Render(in.Template, in.Params, in.Values)
	if err != nil {
		var pe *catalog.ParamError
		if errors.As(err, &pe) {
			writeJSON(w, 200, map[string]any{"ok": false, "param": pe.Name, "error": pe.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "rendered": rendered})
}

type testInput struct {
	Server string         `json:"server"`
	Params map[string]any `json:"params"`
	Reason string         `json:"reason"`
}

// testCommand: exécution réelle depuis l'éditeur; passe par executions.Run avec actor = utilisateur (journalisée).
func (s *Server) testCommand(w http.ResponseWriter, r *http.Request) {
	var in testInput
	if !decode(w, r, &in) {
		return
	}
	c, err := s.commands.Get(r.Context(), id(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	if in.Reason == "" {
		in.Reason = "test depuis l'éditeur"
	}
	u := currentUser(r)
	o, err := s.exec.Run(r.Context(), executions.Request{
		Actor:  executions.Actor{User: &u, IP: s.clientIP(r), UserAgent: r.UserAgent()},
		Server: in.Server, Command: c.Slug, Params: in.Params, Reason: in.Reason,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	body := map[string]any{"execution": o.Exec}
	if o.DenialMessage != "" {
		body["message"] = o.DenialMessage
	}
	writeJSON(w, 200, body)
}

// ---- groupes ----

type groupInput struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	MemberIDs   []string `json:"member_ids"`
}

func (in groupInput) toService() groups.Input {
	return groups.Input{Slug: in.Slug, Name: in.Name, Description: in.Description, MemberIDs: in.MemberIDs}
}

func (s *Server) groupHandlers(kind store.GroupKind) (list, create, update, del, members http.HandlerFunc) {
	list = func(w http.ResponseWriter, r *http.Request) {
		gs, err := s.groups.List(r.Context(), kind)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": gs})
	}
	create = func(w http.ResponseWriter, r *http.Request) {
		var in groupInput
		if !decode(w, r, &in) {
			return
		}
		g, err := s.groups.Create(r.Context(), currentUser(r), s.clientIP(r), kind, in.toService())
		if err != nil {
			s.fail(w, catalogErr(err))
			return
		}
		writeJSON(w, 201, g)
	}
	update = func(w http.ResponseWriter, r *http.Request) {
		var in groupInput
		if !decode(w, r, &in) {
			return
		}
		g, err := s.groups.Update(r.Context(), currentUser(r), s.clientIP(r), kind, id(r), in.toService())
		if err != nil {
			s.fail(w, catalogErr(err))
			return
		}
		writeJSON(w, 200, g)
	}
	del = func(w http.ResponseWriter, r *http.Request) {
		if err := s.groups.Delete(r.Context(), currentUser(r), s.clientIP(r), kind, id(r)); err != nil {
			s.fail(w, catalogErr(err))
			return
		}
		w.WriteHeader(204)
	}
	members = func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if !decode(w, r, &in) {
			return
		}
		ids, err := s.groups.SetMembers(r.Context(), currentUser(r), s.clientIP(r), kind, id(r), in.IDs)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"member_ids": ids})
	}
	return
}
