package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/leash-dev/leash/internal/audit"
	"github.com/leash-dev/leash/internal/catalog"
	"github.com/leash-dev/leash/internal/executions"
	"github.com/leash-dev/leash/internal/store"
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

func (in *commandInput) validate() (catalog.LintResult, error) {
	in.Slug, in.Name = strings.TrimSpace(in.Slug), strings.TrimSpace(in.Name)
	if err := checkSlug(in.Slug); err != nil {
		return catalog.LintResult{}, err
	}
	if in.Name == "" || len(in.Name) > 120 {
		return catalog.LintResult{}, bad("invalid_name", "nom requis (120 caractères max)")
	}
	if len(in.Description) > 4000 {
		return catalog.LintResult{}, bad("invalid_description", "description trop longue (4000 caractères max)")
	}
	if in.Status == "" {
		in.Status = "enabled"
	}
	if in.Status != "enabled" && in.Status != "disabled" && in.Status != "pending" {
		return catalog.LintResult{}, bad("invalid_status", "statut invalide")
	}
	if in.TimeoutS == 0 {
		in.TimeoutS = 30
	}
	if in.TimeoutS < 1 || in.TimeoutS > 600 {
		return catalog.LintResult{}, bad("invalid_timeout", "délai entre 1 et 600 secondes")
	}
	if in.MaxOutputKB == 0 {
		in.MaxOutputKB = 64
	}
	if in.MaxOutputKB < 1 || in.MaxOutputKB > 4096 {
		return catalog.LintResult{}, bad("invalid_max_output", "sortie max entre 1 et 4096 Ko")
	}
	if in.Params == nil {
		in.Params = map[string]catalog.ParamDef{}
	}
	res := catalog.Lint(in.Template, in.Params, in.Risk, catalog.ModeAdmin)
	if !res.OK() {
		return res, &apiError{Status: 400, Code: "lint_failed", Message: res.Errors[0].Message, Details: res}
	}
	return res, nil
}

func (in commandInput) toCommand() store.Command {
	return store.Command{Slug: in.Slug, Name: in.Name, Description: in.Description, Risk: in.Risk, Template: in.Template, Params: in.Params,
		TimeoutS: in.TimeoutS, MaxOutputKB: in.MaxOutputKB, Status: in.Status}
}

func (s *Server) listCommands(w http.ResponseWriter, r *http.Request) {
	cmds, err := store.ListCommands(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": cmds})
}

func (s *Server) getCommand(w http.ResponseWriter, r *http.Request) {
	c, err := store.CommandByID(r.Context(), s.st.DB, id(r))
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
	lint, err := in.validate()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		c := in.toCommand()
		c.Source, c.CreatedBy = "manual", u.ID
		c, err := store.InsertCommand(ctx, tx, c)
		if err != nil {
			return outcome{}, err
		}
		return outcome{201, map[string]any{"command": c, "lint": lint}, &audit.Event{Action: "command.create", TargetType: "command", TargetID: c.ID, TargetName: c.Slug, Diff: map[string]any{"after": c}}}, nil
	})
}

func (s *Server) updateCommand(w http.ResponseWriter, r *http.Request) {
	var in commandInput
	if !decode(w, r, &in) {
		return
	}
	lint, err := in.validate()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		c := in.toCommand()
		c.ID = id(r)
		c, old, err := store.UpdateCommand(ctx, tx, c)
		if err != nil {
			return outcome{}, err
		}
		return outcome{200, map[string]any{"command": c, "lint": lint}, &audit.Event{Action: "command.update", TargetType: "command", TargetID: c.ID, TargetName: c.Slug, Diff: map[string]any{"before": old, "after": c}}}, nil
	})
}

func (s *Server) deleteCommand(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, err := store.CommandByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		if err := store.DeleteCommand(ctx, tx, old.ID); err != nil {
			return outcome{}, err
		}
		return outcome{Event: &audit.Event{Action: "command.delete", TargetType: "command", TargetID: old.ID, TargetName: old.Slug, Diff: map[string]any{"before": old}}}, nil
	})
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
	c, err := store.CommandByID(r.Context(), s.st.DB, id(r))
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

func (s *Server) groupHandlers(kind store.GroupKind) (list, create, update, del, members http.HandlerFunc) {
	label := string(kind)
	list = func(w http.ResponseWriter, r *http.Request) {
		gs, err := store.ListGroups(r.Context(), s.st.DB, kind)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": gs})
	}
	check := func(in *groupInput) error {
		in.Slug, in.Name = strings.TrimSpace(in.Slug), strings.TrimSpace(in.Name)
		if err := checkSlug(in.Slug); err != nil {
			return err
		}
		if in.Name == "" {
			return bad("invalid_name", "nom requis")
		}
		return nil
	}
	create = func(w http.ResponseWriter, r *http.Request) {
		var in groupInput
		if !decode(w, r, &in) {
			return
		}
		if err := check(&in); err != nil {
			s.fail(w, err)
			return
		}
		s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
			g, err := store.InsertGroup(ctx, tx, kind, store.Group{Slug: in.Slug, Name: in.Name, Description: in.Description, MemberIDs: in.MemberIDs})
			if err != nil {
				return outcome{}, err
			}
			return outcome{201, g, &audit.Event{Action: label + "_group.create", TargetType: label + "_group", TargetID: g.ID, TargetName: g.Slug, Diff: map[string]any{"after": g}}}, nil
		})
	}
	update = func(w http.ResponseWriter, r *http.Request) {
		var in groupInput
		if !decode(w, r, &in) {
			return
		}
		if err := check(&in); err != nil {
			s.fail(w, err)
			return
		}
		s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
			old, err := store.GroupByID(ctx, tx, kind, id(r))
			if err != nil {
				return outcome{}, err
			}
			if old.ManagedBy != "" {
				return outcome{}, managedErr("ce groupe est synchronisé: nom et identifiant sont verrouillés (seuls ses membres se modifient, via la liste des membres)")
			}
			g := store.Group{ID: old.ID, Slug: in.Slug, Name: in.Name, Description: in.Description, MemberIDs: in.MemberIDs}
			if err := store.UpdateGroup(ctx, tx, kind, g); err != nil {
				return outcome{}, err
			}
			if g.MemberIDs == nil {
				g.MemberIDs = old.MemberIDs
			}
			return outcome{200, g, &audit.Event{Action: label + "_group.update", TargetType: label + "_group", TargetID: g.ID, TargetName: g.Slug, Diff: map[string]any{"before": old, "after": g}}}, nil
		})
	}
	del = func(w http.ResponseWriter, r *http.Request) {
		s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
			old, err := store.GroupByID(ctx, tx, kind, id(r))
			if err != nil {
				return outcome{}, err
			}
			if old.ManagedBy != "" {
				return outcome{}, managedErr("ce groupe est synchronisé: il ne peut pas être supprimé")
			}
			if err := store.DeleteGroup(ctx, tx, kind, old.ID); err != nil {
				return outcome{}, err
			}
			return outcome{Event: &audit.Event{Action: label + "_group.delete", TargetType: label + "_group", TargetID: old.ID, TargetName: old.Slug, Diff: map[string]any{"before": old}}}, nil
		})
	}
	members = func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if !decode(w, r, &in) {
			return
		}
		s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
			old, err := store.GroupByID(ctx, tx, kind, id(r))
			if err != nil {
				return outcome{}, err
			}
			if err := store.SetGroupMembers(ctx, tx, kind, old.ID, in.IDs); err != nil {
				return outcome{}, err
			}
			if updated, err := store.GroupByID(ctx, tx, kind, old.ID); err == nil {
				in.IDs = updated.MemberIDs // les membres posés par une synchro restent
			}
			return outcome{200, map[string]any{"member_ids": in.IDs}, &audit.Event{Action: label + "_group.members", TargetType: label + "_group", TargetID: old.ID, TargetName: old.Slug,
				Diff: map[string]any{"before": old.MemberIDs, "after": in.IDs}}}, nil
		})
	}
	return
}
