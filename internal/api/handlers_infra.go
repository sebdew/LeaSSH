package api

import (
	"errors"
	"net/http"

	"github.com/leassh-dev/leassh/internal/catalog"
	"github.com/leassh-dev/leassh/internal/infra"
)

type identityInput struct {
	Name       string `json:"name"`
	Username   string `json:"username"`
	Mode       string `json:"mode"` // generate | import | password
	PrivateKey string `json:"private_key"`
	Password   string `json:"password"`
}

type serverInput struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	IdentityID  string   `json:"identity_id"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Enabled     *bool    `json:"enabled"`
	SSHUser     string   `json:"ssh_user"`
	// Serveur synchronisé: rend à la synchro la main sur l'utilisateur SSH (recalculé à la prochaine synchro).
	SSHUserFollowSync bool `json:"ssh_user_follow_sync"`
}

func (in serverInput) toService() infra.ServerInput {
	return infra.ServerInput{Slug: in.Slug, Name: in.Name, Host: in.Host, Port: in.Port, IdentityID: in.IdentityID, Description: in.Description, Tags: in.Tags,
		Enabled: in.Enabled, SSHUser: in.SSHUser, SSHUserFollowSync: in.SSHUserFollowSync}
}

// infraErr traduit les erreurs de validation du service infra en erreurs d'API.
func infraErr(err error) error {
	var ke *infra.KeyError
	var ue *infra.UnreachableError
	switch {
	case errors.As(err, &ke):
		return bad("invalid_key", err.Error())
	case errors.As(err, &ue):
		return bad("unreachable", err.Error())
	case errors.Is(err, infra.ErrIdentityIncomplete), errors.Is(err, infra.ErrPasswordRequired),
		errors.Is(err, infra.ErrIdentityRequired), errors.Is(err, infra.ErrUnknownIdentity):
		return bad("invalid_identity", err.Error())
	case errors.Is(err, infra.ErrInvalidMode):
		return bad("invalid_mode", err.Error())
	case errors.Is(err, catalog.ErrInvalidSlug):
		return bad("invalid_slug", err.Error())
	case errors.Is(err, infra.ErrInvalidName):
		return bad("invalid_name", err.Error())
	case errors.Is(err, infra.ErrInvalidHost):
		return bad("invalid_host", err.Error())
	case errors.Is(err, infra.ErrInvalidPort):
		return bad("invalid_port", err.Error())
	case errors.Is(err, infra.ErrInvalidSSHUser):
		return bad("invalid_ssh_user", err.Error())
	case errors.Is(err, infra.ErrFingerprintChanged):
		return bad("fingerprint_changed", err.Error())
	case errors.Is(err, infra.ErrManagedLocked), errors.Is(err, infra.ErrManagedUndeletable):
		return managedErr(err.Error())
	}
	return err
}

func (s *Server) listIdentities(w http.ResponseWriter, r *http.Request) {
	items, err := s.infra.ListIdentities(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items}) // les secrets sont en json:"-"
}

func (s *Server) createIdentity(w http.ResponseWriter, r *http.Request) {
	var in identityInput
	if !decode(w, r, &in) {
		return
	}
	ident, line, err := s.infra.CreateIdentity(r.Context(), currentUser(r), s.clientIP(r), infra.IdentityInput(in))
	if err != nil {
		s.fail(w, infraErr(err))
		return
	}
	body := map[string]any{"identity": ident}
	if line != "" {
		body["authorized_keys_line"] = line
	}
	writeJSON(w, 201, body)
}

func (s *Server) deleteIdentity(w http.ResponseWriter, r *http.Request) {
	if err := s.infra.DeleteIdentity(r.Context(), currentUser(r), s.clientIP(r), id(r)); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) listServers(w http.ResponseWriter, r *http.Request) {
	items, err := s.infra.ListServers(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) getServer(w http.ResponseWriter, r *http.Request) {
	sv, err := s.infra.GetServer(r.Context(), id(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, sv)
}

func (s *Server) createServer(w http.ResponseWriter, r *http.Request) {
	var in serverInput
	if !decode(w, r, &in) {
		return
	}
	sv, err := s.infra.CreateServer(r.Context(), currentUser(r), s.clientIP(r), in.toService())
	if err != nil {
		s.fail(w, infraErr(err))
		return
	}
	writeJSON(w, 201, sv)
}

func (s *Server) updateServer(w http.ResponseWriter, r *http.Request) {
	var in serverInput
	if !decode(w, r, &in) {
		return
	}
	sv, err := s.infra.UpdateServer(r.Context(), currentUser(r), s.clientIP(r), id(r), in.toService())
	if err != nil {
		s.fail(w, infraErr(err))
		return
	}
	writeJSON(w, 200, sv)
}

func (s *Server) deleteServer(w http.ResponseWriter, r *http.Request) {
	if err := s.infra.DeleteServer(r.Context(), currentUser(r), s.clientIP(r), id(r)); err != nil {
		s.fail(w, infraErr(err))
		return
	}
	w.WriteHeader(204)
}

// testConnection: récupère l'empreinte présentée; si la clé est épinglée, vérifie la correspondance puis l'authentification.
func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	res, err := s.infra.TestConnection(r.Context(), id(r), s.clientIP(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, res)
}

// pinHostKey épingle l'empreinte confirmée par l'utilisateur.
func (s *Server) pinHostKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Fingerprint string `json:"fingerprint"`
	}
	if !decode(w, r, &in) {
		return
	}
	sv, err := s.infra.PinHostKey(r.Context(), currentUser(r), s.clientIP(r), id(r), in.Fingerprint)
	if err != nil {
		s.fail(w, infraErr(err))
		return
	}
	writeJSON(w, 200, sv)
}
