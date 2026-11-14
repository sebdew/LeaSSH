package api

import (
	"errors"
	"net/http"

	"github.com/sebdew/LeaSSH/internal/accounts"
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	items, err := s.accounts.ListUsers(r.Context())
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

func (in userInput) toService() accounts.UserInput {
	return accounts.UserInput{Username: in.Username, DisplayName: in.DisplayName, Password: in.Password, Role: in.Role, Disabled: in.Disabled}
}

// userErr traduit les erreurs métier du service en erreurs d'API.
func userErr(err error) error {
	switch {
	case errors.Is(err, accounts.ErrInvalidUsername):
		return bad("invalid_username", "nom d'utilisateur requis (64 caractères max)")
	case errors.Is(err, accounts.ErrInvalidRole):
		return bad("invalid_role", err.Error())
	case errors.Is(err, accounts.ErrWeakPassword):
		return bad("weak_password", err.Error())
	case errors.Is(err, accounts.ErrInvalidTheme):
		return bad("invalid_theme", err.Error())
	case errors.Is(err, accounts.ErrSelfDisable):
		return bad("self_disable", err.Error())
	case errors.Is(err, accounts.ErrSelfDelete):
		return bad("self_delete", err.Error())
	case errors.Is(err, accounts.ErrLastAdmin):
		return bad("last_admin", err.Error())
	case errors.Is(err, accounts.ErrManagedAccount):
		return managedErr(err.Error())
	}
	return err
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in userInput
	if !decode(w, r, &in) {
		return
	}
	u, err := s.accounts.CreateUser(r.Context(), currentUser(r), s.clientIP(r), in.toService())
	if err != nil {
		s.fail(w, userErr(err))
		return
	}
	writeJSON(w, 201, u)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var in userInput
	if !decode(w, r, &in) {
		return
	}
	u, err := s.accounts.UpdateUser(r.Context(), currentUser(r), s.clientIP(r), id(r), in.toService())
	if err != nil {
		s.fail(w, userErr(err))
		return
	}
	writeJSON(w, 200, u)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	if err := s.accounts.DeleteUser(r.Context(), currentUser(r), s.clientIP(r), id(r)); err != nil {
		s.fail(w, userErr(err))
		return
	}
	w.WriteHeader(204)
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
	keep := ""
	if c, err := r.Cookie(cookieName); err == nil {
		keep = accounts.HashSession(c.Value)
	}
	err := s.accounts.ChangePassword(r.Context(), currentUser(r), s.clientIP(r), in.Current, in.New, keep)
	switch {
	case errors.Is(err, accounts.ErrSSOAccount):
		writeErr(w, 400, "sso_account", err.Error())
	case errors.Is(err, accounts.ErrWrongPassword):
		writeErr(w, 403, "invalid_credentials", err.Error())
	case err != nil:
		s.fail(w, userErr(err))
	default:
		w.WriteHeader(204)
	}
}

// setTheme: préférence de thème mémorisée côté serveur, par utilisateur.
func (s *Server) setTheme(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Theme string `json:"theme"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.accounts.SetTheme(r.Context(), currentUser(r).ID, in.Theme); err != nil {
		s.fail(w, userErr(err))
		return
	}
	w.WriteHeader(204)
}
