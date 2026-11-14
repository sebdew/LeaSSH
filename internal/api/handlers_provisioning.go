package api

import (
	"errors"
	"net/http"

	"github.com/sebdew/LeaSSH/internal/provisioning"
	"github.com/sebdew/LeaSSH/internal/store"
)

type provisionerInput struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Enabled     *bool          `json:"enabled"`
	IntervalMin int            `json:"interval_min"`
	IdentityID  string         `json:"identity_id"`
	DefaultUser string         `json:"default_user"`
	Config      map[string]any `json:"config"`
	Secret      string         `json:"secret"` // vide à la modification = jeton conservé
	RuleIDs     []string       `json:"rule_ids"`
}

func (in provisionerInput) toInput() provisioning.Input {
	return provisioning.Input{Name: in.Name, Type: in.Type, Enabled: in.Enabled, IntervalMin: in.IntervalMin, IdentityID: in.IdentityID,
		DefaultUser: in.DefaultUser, Config: in.Config, Secret: in.Secret, RuleIDs: in.RuleIDs}
}

// provisionInputErr: les refus de saisie gardent leur code d'API.
func provisionInputErr(err error) error {
	var ie *provisioning.InputError
	if errors.As(err, &ie) {
		return bad(ie.Code, ie.Msg)
	}
	return err
}

func (s *Server) provisionerTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"items": provisioning.Types()})
}

func (s *Server) listProvisioners(w http.ResponseWriter, r *http.Request) {
	ps, err := s.prov.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": ps})
}

func (s *Server) createProvisioner(w http.ResponseWriter, r *http.Request) {
	var in provisionerInput
	if !decode(w, r, &in) {
		return
	}
	p, err := s.prov.Create(r.Context(), currentUser(r), s.clientIP(r), in.toInput())
	if err != nil {
		s.fail(w, provisionInputErr(err))
		return
	}
	writeJSON(w, 201, p)
}

func (s *Server) updateProvisioner(w http.ResponseWriter, r *http.Request) {
	var in provisionerInput
	if !decode(w, r, &in) {
		return
	}
	p, err := s.prov.Update(r.Context(), currentUser(r), s.clientIP(r), id(r), in.toInput())
	if err != nil {
		s.fail(w, provisionInputErr(err))
		return
	}
	writeJSON(w, 200, p)
}

// deleteProvisioner rend les serveurs et groupes à la gestion manuelle; rien n'est supprimé.
func (s *Server) deleteProvisioner(w http.ResponseWriter, r *http.Request) {
	if err := s.prov.Delete(r.Context(), currentUser(r), s.clientIP(r), id(r)); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func provisionerError(err error, report provisioning.Report) error {
	switch {
	case errors.Is(err, provisioning.ErrBlocked):
		return &apiError{Status: 409, Code: "sync_blocked", Message: err.Error(), Details: report}
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrInUse):
		return err
	default:
		return &apiError{Status: 502, Code: "source_error", Message: err.Error()}
	}
}

// testProvisioner lit la source (sans rien écrire) et, pour un bastion, relève la clé d'hôte présentée.
func (s *Server) testProvisioner(w http.ResponseWriter, r *http.Request) {
	res, err := s.prov.TestSource(r.Context(), id(r))
	if err != nil {
		s.fail(w, provisionerError(err, provisioning.Report{}))
		return
	}
	writeJSON(w, 200, res)
}

// pinProvisionerKey épingle la clé d'hôte du bastion après confirmation de l'empreinte par l'admin.
func (s *Server) pinProvisionerKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Fingerprint string `json:"fingerprint"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := s.prov.PinHostKey(r.Context(), currentUser(r), s.clientIP(r), id(r), in.Fingerprint)
	var ie *provisioning.InputError
	switch {
	case errors.As(err, &ie):
		s.fail(w, bad(ie.Code, ie.Msg))
	case err != nil:
		s.fail(w, provisionerError(err, provisioning.Report{}))
	default:
		writeJSON(w, 200, p)
	}
}

func (s *Server) previewProvisioner(w http.ResponseWriter, r *http.Request) {
	rep, err := s.prov.Preview(r.Context(), id(r))
	if err != nil && !errors.Is(err, provisioning.ErrBlocked) {
		s.fail(w, provisionerError(err, rep))
		return
	}
	writeJSON(w, 200, rep) // un aperçu bloqué se montre quand même (rep.blocked renseigné)
}

func (s *Server) syncProvisioner(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Force bool `json:"force"`
	}
	if r.ContentLength > 0 && !decode(w, r, &in) {
		return
	}
	u := currentUser(r)
	rep, err := s.prov.Sync(r.Context(), id(r), in.Force, provisioning.Actor{Type: "user", ID: u.ID, Name: u.Username, IP: s.clientIP(r)})
	if err != nil {
		s.fail(w, provisionerError(err, rep))
		return
	}
	writeJSON(w, 200, rep)
}
