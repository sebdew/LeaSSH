package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

var roles = map[string]bool{"admin": true, "approver": true, "viewer": true}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	items, err := store.ListUsers(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

type userInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Role        string `json:"role"`
	Disabled    bool   `json:"disabled"`
}

func checkPassword(p string) error {
	if len(p) < minPassword {
		return bad("weak_password", "le mot de passe doit faire au moins 10 caractères")
	}
	return nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in userInput
	if !decode(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	switch {
	case in.Username == "" || len(in.Username) > 64:
		s.fail(w, bad("invalid_username", "nom d'utilisateur requis (64 caractères max)"))
		return
	case !roles[in.Role]:
		s.fail(w, bad("invalid_role", "rôle: admin, approver ou viewer"))
		return
	}
	if err := checkPassword(in.Password); err != nil {
		s.fail(w, err)
		return
	}
	hash, err := crypto.HashPassword(in.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		nu, err := store.InsertUser(ctx, tx, store.User{Username: in.Username, DisplayName: in.DisplayName, Role: in.Role, PasswordHash: hash})
		if err != nil {
			return outcome{}, err
		}
		return outcome{201, nu, &audit.Event{Action: "user.create", TargetType: "user", TargetID: nu.ID, TargetName: nu.Username,
			Diff: map[string]any{"after": map[string]any{"username": nu.Username, "role": nu.Role}}}}, nil
	})
}

// updateUser: profil, rôle, désactivation, réinitialisation du mot de passe. Garde-fou: toujours au moins un admin actif.
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var in userInput
	if !decode(w, r, &in) {
		return
	}
	if !roles[in.Role] {
		s.fail(w, bad("invalid_role", "rôle: admin, approver ou viewer"))
		return
	}
	hash := ""
	if in.Password != "" {
		if err := checkPassword(in.Password); err != nil {
			s.fail(w, err)
			return
		}
		var err error
		if hash, err = crypto.HashPassword(in.Password); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, me store.User) (outcome, error) {
		old, err := store.UserByIDQ(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		if old.AuthProvider != "local" && (hash != "" || in.Role != old.Role) {
			return outcome{}, managedErr("compte SSO: le rôle vient du mapping des revendications et il n'y a pas de mot de passe local")
		}
		nu := old
		nu.DisplayName, nu.Role, nu.Disabled = strings.TrimSpace(in.DisplayName), in.Role, in.Disabled
		if nu.DisplayName == "" {
			nu.DisplayName = old.Username
		}
		if old.ID == me.ID && nu.Disabled {
			return outcome{}, bad("self_disable", "vous ne pouvez pas désactiver votre propre compte")
		}
		if old.Role == "admin" && !old.Disabled && (nu.Role != "admin" || nu.Disabled) {
			if n, err := store.CountEnabledAdmins(ctx, tx); err != nil {
				return outcome{}, err
			} else if n <= 1 {
				return outcome{}, bad("last_admin", "il doit rester au moins un administrateur actif")
			}
		}
		if err := store.UpdateUser(ctx, tx, nu, hash); err != nil {
			return outcome{}, err
		}
		if nu.Disabled || nu.Role != old.Role || hash != "" {
			if err := store.DeleteSessionsOf(ctx, tx, old.ID, ""); err != nil { // effet immédiat
				return outcome{}, err
			}
		}
		diff := map[string]any{"before": map[string]any{"display_name": old.DisplayName, "role": old.Role, "disabled": old.Disabled},
			"after": map[string]any{"display_name": nu.DisplayName, "role": nu.Role, "disabled": nu.Disabled, "password_reset": hash != ""}}
		return outcome{200, nu, &audit.Event{Action: "user.update", TargetType: "user", TargetID: nu.ID, TargetName: nu.Username, Diff: diff}}, nil
	})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, me store.User) (outcome, error) {
		old, err := store.UserByIDQ(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		if old.ID == me.ID {
			return outcome{}, bad("self_delete", "vous ne pouvez pas supprimer votre propre compte")
		}
		if old.Role == "admin" && !old.Disabled {
			if n, err := store.CountEnabledAdmins(ctx, tx); err != nil {
				return outcome{}, err
			} else if n <= 1 {
				return outcome{}, bad("last_admin", "il doit rester au moins un administrateur actif")
			}
		}
		if err := store.DeleteUser(ctx, tx, old.ID); err != nil {
			return outcome{}, err
		}
		return outcome{Event: &audit.Event{Action: "user.delete", TargetType: "user", TargetID: old.ID, TargetName: old.Username}}, nil
	})
}

// changePassword: l'utilisateur change son propre mot de passe; ses autres sessions sont fermées.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &in) {
		return
	}
	me := currentUser(r)
	if me.AuthProvider != "local" {
		writeErr(w, 400, "sso_account", "ce compte est géré par le SSO: pas de mot de passe local")
		return
	}
	if !crypto.CheckPassword(in.Current, me.PasswordHash) {
		writeErr(w, 403, "invalid_credentials", "mot de passe actuel incorrect")
		return
	}
	if err := checkPassword(in.New); err != nil {
		s.fail(w, err)
		return
	}
	hash, err := crypto.HashPassword(in.New)
	if err != nil {
		s.fail(w, err)
		return
	}
	keep := ""
	if c, err := r.Cookie(cookieName); err == nil {
		keep = hashSession(c.Value)
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		if err := store.SetPassword(ctx, tx, u.ID, hash); err != nil {
			return outcome{}, err
		}
		if err := store.DeleteSessionsOf(ctx, tx, u.ID, keep); err != nil {
			return outcome{}, err
		}
		return outcome{Event: &audit.Event{Action: "user.password_change", TargetType: "user", TargetID: u.ID, TargetName: u.Username}}, nil
	})
}

// setTheme: préférence de thème mémorisée côté serveur, par utilisateur (docs/UX.md §4.2).
func (s *Server) setTheme(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Theme string `json:"theme"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Theme != "system" && in.Theme != "light" && in.Theme != "dark" {
		s.fail(w, bad("invalid_theme", "thème: system, light ou dark"))
		return
	}
	if err := store.SetTheme(r.Context(), s.st.DB, currentUser(r).ID, in.Theme); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
