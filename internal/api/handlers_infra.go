package api

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strings"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/sshexec"
	"github.com/leassh-dev/leassh/internal/store"
)

type identityInput struct {
	Name       string `json:"name"`
	Username   string `json:"username"`
	Mode       string `json:"mode"` // generate | import | password
	PrivateKey string `json:"private_key"`
	Password   string `json:"password"`
}

func (s *Server) listIdentities(w http.ResponseWriter, r *http.Request) {
	items, err := store.ListIdentities(r.Context(), s.st.DB)
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
	in.Name, in.Username = strings.TrimSpace(in.Name), strings.TrimSpace(in.Username)
	if in.Name == "" || in.Username == "" {
		s.fail(w, bad("invalid_identity", "nom et utilisateur SSH requis"))
		return
	}
	ident := store.Identity{Name: in.Name, Username: in.Username}
	var secret []byte
	switch in.Mode {
	case "", "generate":
		pemKey, pub, err := sshexec.GenerateKey("leassh:" + in.Name)
		if err != nil {
			s.fail(w, err)
			return
		}
		ident.AuthType, ident.PublicKey, secret = "key", pub, pemKey
	case "import":
		pub, err := sshexec.PublicKeyOf([]byte(in.PrivateKey))
		if err != nil {
			s.fail(w, bad("invalid_key", err.Error()))
			return
		}
		ident.AuthType, ident.PublicKey, secret = "key", pub, []byte(in.PrivateKey)
	case "password":
		if in.Password == "" {
			s.fail(w, bad("invalid_identity", "mot de passe requis"))
			return
		}
		ident.AuthType = "password"
	default:
		s.fail(w, bad("invalid_mode", "mode inconnu (generate, import, password)"))
		return
	}
	var err error
	if secret != nil {
		if ident.PrivateKeyEnc, err = s.box.Seal(secret); err != nil {
			s.fail(w, err)
			return
		}
	}
	if in.Mode == "password" {
		if ident.PasswordEnc, err = s.box.Seal([]byte(in.Password)); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		ident, err := store.InsertIdentity(ctx, tx, ident)
		if err != nil {
			return outcome{}, err
		}
		body := map[string]any{"identity": ident}
		if ident.AuthType == "key" {
			body["authorized_keys_line"] = "echo '" + ident.PublicKey + "' >> ~/.ssh/authorized_keys"
		}
		return outcome{201, body, &audit.Event{Action: "identity.create", TargetType: "identity", TargetID: ident.ID, TargetName: ident.Name,
			Diff: map[string]any{"after": map[string]any{"name": ident.Name, "username": ident.Username, "auth_type": ident.AuthType, "public_key": ident.PublicKey}}}}, nil
	})
}

func (s *Server) deleteIdentity(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, err := store.IdentityByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		if err := store.DeleteIdentity(ctx, tx, old.ID); err != nil {
			return outcome{}, err
		}
		return outcome{Event: &audit.Event{Action: "identity.delete", TargetType: "identity", TargetID: old.ID, TargetName: old.Name}}, nil
	})
}

// ---- serveurs ----

// Le nom d'utilisateur SSH n'est jamais interprété par un shell (il part dans le protocole SSH): les espaces sont permis, car Warpgate
// attend « utilisateur:nom exact de la cible » (ex. « ia:reservations-dev - LXC ct145 »). Caractères de contrôle interdits.
var sshUserRe = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,128}$`)

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

func (in *serverInput) validate() error {
	in.Slug, in.Name, in.Host = strings.TrimSpace(in.Slug), strings.TrimSpace(in.Name), strings.TrimSpace(in.Host)
	if err := checkSlug(in.Slug); err != nil {
		return err
	}
	if in.Name == "" {
		return bad("invalid_name", "nom requis")
	}
	if in.Host == "" || strings.ContainsAny(in.Host, " /\t\n") {
		return bad("invalid_host", "hôte invalide")
	}
	if in.Port == 0 {
		in.Port = 22
	}
	if in.Port < 1 || in.Port > 65535 {
		return bad("invalid_port", "port invalide")
	}
	if in.IdentityID == "" {
		return bad("invalid_identity", "identité SSH requise")
	}
	in.SSHUser = strings.TrimSpace(in.SSHUser)
	if in.SSHUser != "" && !sshUserRe.MatchString(in.SSHUser) {
		return bad("invalid_ssh_user", "utilisateur SSH invalide (128 caractères max, sans caractère de contrôle)")
	}
	return nil
}

func (in serverInput) toServer() store.Server {
	enabled := in.Enabled == nil || *in.Enabled
	return store.Server{Slug: in.Slug, Name: in.Name, Host: in.Host, Port: in.Port, IdentityID: in.IdentityID, Description: in.Description, Tags: in.Tags, Enabled: enabled, SSHUser: in.SSHUser}
}

func (s *Server) listServers(w http.ResponseWriter, r *http.Request) {
	items, err := store.ListServers(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) getServer(w http.ResponseWriter, r *http.Request) {
	sv, err := store.ServerByID(r.Context(), s.st.DB, id(r))
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
	if err := in.validate(); err != nil {
		s.fail(w, err)
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		if _, err := store.IdentityByID(ctx, tx, in.IdentityID); err != nil {
			return outcome{}, bad("invalid_identity", "identité SSH inconnue")
		}
		sv, err := store.InsertServer(ctx, tx, in.toServer())
		if err != nil {
			return outcome{}, err
		}
		return outcome{201, sv, &audit.Event{Action: "server.create", TargetType: "server", TargetID: sv.ID, TargetName: sv.Slug, Diff: map[string]any{"after": sv}}}, nil
	})
}

func (s *Server) updateServer(w http.ResponseWriter, r *http.Request) {
	var in serverInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.validate(); err != nil {
		s.fail(w, err)
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		if _, err := store.IdentityByID(ctx, tx, in.IdentityID); err != nil {
			return outcome{}, bad("invalid_identity", "identité SSH inconnue")
		}
		cur, err := store.ServerByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		sv := in.toServer()
		sv.ID = id(r)
		if cur.ManagedBy != "" && (sv.Slug != cur.Slug || sv.Name != cur.Name || sv.Host != cur.Host || sv.Port != cur.Port || sv.Description != cur.Description) {
			return outcome{}, managedErr("ce serveur est synchronisé: nom, description et hôte viennent de la source")
		}
		switch {
		case cur.ManagedBy == "":
		case in.SSHUserFollowSync:
			sv.SSHUser, sv.SSHUserCustom = cur.SSHUser, false
		default:
			sv.SSHUserCustom = cur.SSHUserCustom || sv.SSHUser != cur.SSHUser // choix d'un admin: la synchro ne l'écrase plus
		}
		sv, old, err := store.UpdateServer(ctx, tx, sv)
		if err != nil {
			return outcome{}, err
		}
		s.pool.Invalidate(sv.ID)
		return outcome{200, sv, &audit.Event{Action: "server.update", TargetType: "server", TargetID: sv.ID, TargetName: sv.Slug, Diff: map[string]any{"before": old, "after": sv}}}, nil
	})
}

func (s *Server) deleteServer(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, err := store.ServerByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		if old.ManagedBy != "" {
			return outcome{}, managedErr("ce serveur est synchronisé: désactivez-le, ou retirez-le de la source")
		}
		if err := store.DeleteServer(ctx, tx, old.ID); err != nil {
			return outcome{}, err
		}
		s.pool.Invalidate(old.ID)
		return outcome{Event: &audit.Event{Action: "server.delete", TargetType: "server", TargetID: old.ID, TargetName: old.Slug, Diff: map[string]any{"before": old}}}, nil
	})
}

// testConnection: récupère l'empreinte présentée; si la clé est épinglée, vérifie la correspondance puis l'authentification.
func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	sv, err := store.ServerByID(r.Context(), s.st.DB, id(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	fp, kt, err := sshexec.ScanHostKey(r.Context(), sv.Host, sv.Port)
	if err != nil {
		writeJSON(w, 200, map[string]any{"reachable": false, "error": err.Error()})
		return
	}
	res := map[string]any{"reachable": true, "fingerprint": fp, "key_type": kt, "host_key_status": sv.HostKeyStatus, "pinned_fingerprint": sv.HostKeyFingerprint}
	if sv.HostKeyStatus == "pinned" || sv.HostKeyStatus == "mismatch" {
		if fp != sv.HostKeyFingerprint {
			res["matches"] = false
			if sv.HostKeyStatus == "pinned" {
				s.markMismatch(r, sv, fp)
				res["host_key_status"] = "mismatch"
			}
			writeJSON(w, 200, res)
			return
		}
		res["matches"] = true
		if sv.HostKeyStatus == "pinned" {
			t, terr := s.exec.TargetFor(r.Context(), sv)
			if terr != nil {
				res["auth_ok"], res["error"] = false, "identité SSH inutilisable"
			} else if c, derr := sshexec.Dial(r.Context(), t); derr != nil {
				res["auth_ok"], res["error"] = false, derr.Error()
				_ = store.MarkSSH(r.Context(), s.st.DB, sv.ID, false)
			} else {
				c.Close()
				res["auth_ok"] = true
				_ = store.MarkSSH(r.Context(), s.st.DB, sv.ID, true)
				s.pool.Invalidate(sv.ID) // un test réussi lève une éventuelle pause après échec d'authentification
			}
		}
	}
	writeJSON(w, 200, res)
}

func (s *Server) markMismatch(r *http.Request, sv store.Server, got string) {
	ctx := context.WithoutCancel(r.Context())
	_ = store.SetHostKeyStatus(ctx, s.st.DB, sv.ID, "mismatch")
	s.pool.Invalidate(sv.ID)
	_ = audit.RecordStandalone(ctx, s.st, audit.Event{ActorType: "system", ActorName: "ssh", Action: "host_key.mismatch", TargetType: "server",
		TargetID: sv.ID, TargetName: sv.Slug, Diff: map[string]any{"expected": sv.HostKeyFingerprint, "received": got}, IP: s.clientIP(r)})
}

// pinHostKey épingle l'empreinte confirmée par l'utilisateur. Un nouveau scan vérifie qu'elle est toujours celle présentée par l'hôte.
func (s *Server) pinHostKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Fingerprint string `json:"fingerprint"`
	}
	if !decode(w, r, &in) {
		return
	}
	sv, err := store.ServerByID(r.Context(), s.st.DB, id(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	fp, _, err := sshexec.ScanHostKey(r.Context(), sv.Host, sv.Port)
	if err != nil {
		s.fail(w, bad("unreachable", "hôte injoignable: "+err.Error()))
		return
	}
	if in.Fingerprint == "" || fp != in.Fingerprint {
		s.fail(w, bad("fingerprint_changed", "l'empreinte présentée par l'hôte ne correspond pas à celle confirmée; relancez le test de connexion"))
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		if err := store.SetHostKey(ctx, tx, sv.ID, fp, "pinned"); err != nil {
			return outcome{}, err
		}
		s.pool.Invalidate(sv.ID)
		action := "server.trust_host_key"
		if sv.HostKeyStatus != "unknown" {
			action = "server.accept_host_key"
		}
		out := sv
		out.HostKeyFingerprint, out.HostKeyStatus = fp, "pinned"
		return outcome{200, out, &audit.Event{Action: action, TargetType: "server", TargetID: sv.ID, TargetName: sv.Slug,
			Diff: map[string]any{"before": map[string]any{"fingerprint": sv.HostKeyFingerprint, "status": sv.HostKeyStatus}, "after": map[string]any{"fingerprint": fp, "status": "pinned"}}}}, nil
	})
}
