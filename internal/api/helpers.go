package api

import (
	"context"
	"database/sql"
	"errors"
	"github.com/leassh-dev/leassh/internal/catalog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/leassh-dev/leassh/internal/audit"
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

func checkSlug(slug string) error {
	if err := catalog.CheckSlug(slug); err != nil {
		return bad("invalid_slug", err.Error())
	}
	return nil
}

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

type outcome struct {
	Status int
	Body   any
	Event  *audit.Event
}

func currentUser(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}

// mutate exécute une mutation de configuration; l'événement d'audit est écrit dans la même transaction.
// fn ne doit utiliser que tx (la base n'a qu'une connexion).
func (s *Server) mutate(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error)) {
	u := currentUser(r)
	var out outcome
	err := s.st.Tx(r.Context(), func(tx *sql.Tx) error {
		var err error
		if out, err = fn(r.Context(), tx, u); err != nil {
			return err
		}
		if out.Event != nil {
			out.Event.ActorType, out.Event.ActorID, out.Event.ActorName, out.Event.IP = "user", u.ID, u.Username, s.clientIP(r)
			return audit.Record(r.Context(), tx, *out.Event)
		}
		return nil
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	if out.Body == nil {
		w.WriteHeader(204)
		return
	}
	writeJSON(w, out.Status, out.Body)
}

func id(r *http.Request) string { return chi.URLParam(r, "id") }
