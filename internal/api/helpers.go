package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/leassh-dev/leassh/internal/store"
)

type apiError struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *apiError) Error() string { return e.Message }

func bad(code, msg string) *apiError { return &apiError{Status: 400, Code: code, Message: msg} }

func managedErr(msg string) *apiError { return &apiError{Status: 409, Code: "managed", Message: msg} }

func (s *Server) fail(w http.ResponseWriter, err error) {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		body := map[string]any{"code": ae.Code, "message": ae.Message}
		if ae.Details != nil {
			body["details"] = ae.Details
		}
		writeJSON(w, ae.Status, map[string]any{"error": body})
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "not_found", "ressource introuvable")
	case errors.Is(err, store.ErrConflict):
		writeErr(w, 409, "conflict", "un objet avec ce slug ou ce nom existe déjà")
	case errors.Is(err, store.ErrInUse):
		writeErr(w, 409, "in_use", "l'objet est encore référencé par un autre élément")
	default:
		s.log.Error("erreur interne", "err", err)
		writeErr(w, 500, "internal", "erreur interne")
	}
}

func currentUser(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}

func id(r *http.Request) string { return chi.URLParam(r, "id") }
