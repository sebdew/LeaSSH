// Package infra: administration des identités SSH, des serveurs et de l'épinglage des clés d'hôte (ARC-03).
// Les secrets sont scellés avant écriture et n'apparaissent ni dans l'audit ni dans les réponses; chaque mutation
// écrit son événement d'audit dans la même transaction.
package infra

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/catalog"
	"github.com/sebdew/LeaSSH/internal/sshexec"
	"github.com/sebdew/LeaSSH/internal/store"
)

var (
	ErrIdentityIncomplete = errors.New("nom et utilisateur SSH requis")
	ErrPasswordRequired   = errors.New("mot de passe requis")
	ErrInvalidMode        = errors.New("mode inconnu (generate, import, password)")

	ErrInvalidName        = errors.New("nom requis")
	ErrInvalidHost        = errors.New("hôte invalide")
	ErrInvalidPort        = errors.New("port invalide")
	ErrIdentityRequired   = errors.New("identité SSH requise")
	ErrUnknownIdentity    = errors.New("identité SSH inconnue")
	ErrInvalidSSHUser     = errors.New("utilisateur SSH invalide (128 caractères max, sans caractère de contrôle)")
	ErrManagedLocked      = errors.New("ce serveur est synchronisé: nom, description et hôte viennent de la source")
	ErrManagedUndeletable = errors.New("ce serveur est synchronisé: désactivez-le, ou retirez-le de la source")
	ErrFingerprintChanged = errors.New("l'empreinte présentée par l'hôte ne correspond pas à celle confirmée; relancez le test de connexion")
)

// KeyError: la clé privée importée est illisible.
type KeyError struct{ Msg string }

func (e *KeyError) Error() string { return e.Msg }

// UnreachableError: l'hôte n'a pas répondu au scan de sa clé.
type UnreachableError struct{ Err error }

func (e *UnreachableError) Error() string { return "hôte injoignable: " + e.Err.Error() }
func (e *UnreachableError) Unwrap() error { return e.Err }

// Sealer chiffre un secret avant son stockage (crypto.Box).
type Sealer interface {
	Seal(plain []byte) ([]byte, error)
}

// Pool: connexions SSH réutilisées; invalidées quand la configuration d'un serveur change.
type Pool interface{ Invalidate(serverID string) }

// Targeter construit la cible de connexion d'un serveur (identité déchiffrée, empreinte épinglée).
type Targeter interface {
	TargetFor(ctx context.Context, srv store.Server) (sshexec.Target, error)
}

type Service struct {
	st   *store.Store
	box  Sealer
	pool Pool
	tgt  Targeter
}

func New(st *store.Store, box Sealer, pool Pool, tgt Targeter) *Service {
	return &Service{st: st, box: box, pool: pool, tgt: tgt}
}

// ---- identités ----

// IdentityInput: Mode vaut generate (défaut), import ou password.
type IdentityInput struct {
	Name, Username, Mode, PrivateKey, Password string
}

func (s *Service) ListIdentities(ctx context.Context) ([]store.Identity, error) {
	return store.ListIdentities(ctx, s.st.DB)
}

// CreateIdentity renvoie l'identité et, pour une clé, la ligne à ajouter dans authorized_keys de la cible.
func (s *Service) CreateIdentity(ctx context.Context, actor store.User, ip string, in IdentityInput) (store.Identity, string, error) {
	in.Name, in.Username = strings.TrimSpace(in.Name), strings.TrimSpace(in.Username)
	if in.Name == "" || in.Username == "" {
		return store.Identity{}, "", ErrIdentityIncomplete
	}
	ident := store.Identity{Name: in.Name, Username: in.Username}
	var secret []byte
	switch in.Mode {
	case "", "generate":
		pemKey, pub, err := sshexec.GenerateKey("leassh:" + in.Name)
		if err != nil {
			return store.Identity{}, "", fmt.Errorf("génération de la clé: %w", err)
		}
		ident.AuthType, ident.PublicKey, secret = "key", pub, pemKey
	case "import":
		pub, err := sshexec.PublicKeyOf([]byte(in.PrivateKey))
		if err != nil {
			return store.Identity{}, "", &KeyError{Msg: err.Error()}
		}
		ident.AuthType, ident.PublicKey, secret = "key", pub, []byte(in.PrivateKey)
	case "password":
		if in.Password == "" {
			return store.Identity{}, "", ErrPasswordRequired
		}
		ident.AuthType = "password"
	default:
		return store.Identity{}, "", ErrInvalidMode
	}
	var err error
	if secret != nil {
		if ident.PrivateKeyEnc, err = s.box.Seal(secret); err != nil {
			return store.Identity{}, "", fmt.Errorf("chiffrement de la clé: %w", err)
		}
	}
	if in.Mode == "password" {
		if ident.PasswordEnc, err = s.box.Seal([]byte(in.Password)); err != nil {
			return store.Identity{}, "", fmt.Errorf("chiffrement du mot de passe: %w", err)
		}
	}
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		var err error
		if ident, err = store.InsertIdentity(ctx, tx, ident); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "identity.create", TargetType: "identity", TargetID: ident.ID, TargetName: ident.Name,
			Diff: map[string]any{"after": map[string]any{"name": ident.Name, "username": ident.Username, "auth_type": ident.AuthType, "public_key": ident.PublicKey}}}, nil
	})
	if err != nil {
		return store.Identity{}, "", err
	}
	line := ""
	if ident.AuthType == "key" {
		line = "echo '" + ident.PublicKey + "' >> ~/.ssh/authorized_keys"
	}
	return ident, line, nil
}

func (s *Service) DeleteIdentity(ctx context.Context, actor store.User, ip, id string) error {
	return audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.IdentityByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		if err := store.DeleteIdentity(ctx, tx, old.ID); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "identity.delete", TargetType: "identity", TargetID: old.ID, TargetName: old.Name}, nil
	})
}

// ---- serveurs ----

// Le nom d'utilisateur SSH n'est jamais interprété par un shell (il part dans le protocole SSH): les espaces sont permis, car Warpgate
// attend « utilisateur:nom exact de la cible » (ex. « ia:reservations-dev - LXC ct145 »). Caractères de contrôle interdits.
var sshUserRe = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,128}$`)

// ServerInput: Port vide = 22, Enabled absent = actif. SSHUserFollowSync (serveur synchronisé) rend à la synchro la main sur l'utilisateur SSH.
type ServerInput struct {
	Slug, Name, Host  string
	Port              int
	IdentityID        string
	Description       string
	Tags              []string
	Enabled           *bool
	SSHUser           string
	SSHUserFollowSync bool
}

func (in *ServerInput) validate() error {
	in.Slug, in.Name, in.Host = strings.TrimSpace(in.Slug), strings.TrimSpace(in.Name), strings.TrimSpace(in.Host)
	if err := catalog.CheckSlug(in.Slug); err != nil {
		return err
	}
	switch {
	case in.Name == "":
		return ErrInvalidName
	case in.Host == "" || strings.ContainsAny(in.Host, " /\t\n"):
		return ErrInvalidHost
	}
	if in.Port == 0 {
		in.Port = 22
	}
	if in.Port < 1 || in.Port > 65535 {
		return ErrInvalidPort
	}
	if in.IdentityID == "" {
		return ErrIdentityRequired
	}
	in.SSHUser = strings.TrimSpace(in.SSHUser)
	if in.SSHUser != "" && !sshUserRe.MatchString(in.SSHUser) {
		return ErrInvalidSSHUser
	}
	return nil
}

func (in ServerInput) server() store.Server {
	return store.Server{Slug: in.Slug, Name: in.Name, Host: in.Host, Port: in.Port, IdentityID: in.IdentityID, Description: in.Description, Tags: in.Tags,
		Enabled: in.Enabled == nil || *in.Enabled, SSHUser: in.SSHUser}
}

func (s *Service) ListServers(ctx context.Context) ([]store.Server, error) {
	return store.ListServers(ctx, s.st.DB)
}

func (s *Service) GetServer(ctx context.Context, id string) (store.Server, error) {
	return store.ServerByID(ctx, s.st.DB, id)
}

// identityExists vérifie l'identité dans la transaction de la mutation.
func identityExists(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := store.IdentityByID(ctx, tx, id); err != nil {
		return ErrUnknownIdentity
	}
	return nil
}

func (s *Service) CreateServer(ctx context.Context, actor store.User, ip string, in ServerInput) (store.Server, error) {
	if err := in.validate(); err != nil {
		return store.Server{}, err
	}
	var sv store.Server
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		if err := identityExists(ctx, tx, in.IdentityID); err != nil {
			return audit.Event{}, err
		}
		var err error
		if sv, err = store.InsertServer(ctx, tx, in.server()); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "server.create", TargetType: "server", TargetID: sv.ID, TargetName: sv.Slug, Diff: map[string]any{"after": sv}}, nil
	})
	return sv, err
}

func (s *Service) UpdateServer(ctx context.Context, actor store.User, ip, id string, in ServerInput) (store.Server, error) {
	if err := in.validate(); err != nil {
		return store.Server{}, err
	}
	var out store.Server
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		if err := identityExists(ctx, tx, in.IdentityID); err != nil {
			return audit.Event{}, err
		}
		cur, err := store.ServerByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		sv := in.server()
		sv.ID = id
		if cur.ManagedBy != "" && (sv.Slug != cur.Slug || sv.Name != cur.Name || sv.Host != cur.Host || sv.Port != cur.Port || sv.Description != cur.Description) {
			return audit.Event{}, ErrManagedLocked
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
			return audit.Event{}, err
		}
		out = sv
		s.pool.Invalidate(sv.ID)
		return audit.Event{Action: "server.update", TargetType: "server", TargetID: sv.ID, TargetName: sv.Slug, Diff: map[string]any{"before": old, "after": sv}}, nil
	})
	return out, err
}

func (s *Service) DeleteServer(ctx context.Context, actor store.User, ip, id string) error {
	return audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.ServerByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		if old.ManagedBy != "" {
			return audit.Event{}, ErrManagedUndeletable
		}
		if err := store.DeleteServer(ctx, tx, old.ID); err != nil {
			return audit.Event{}, err
		}
		s.pool.Invalidate(old.ID)
		return audit.Event{Action: "server.delete", TargetType: "server", TargetID: old.ID, TargetName: old.Slug, Diff: map[string]any{"before": old}}, nil
	})
}

// ---- clé d'hôte ----

// ConnectionTest: résultat du test de connexion. Les champs optionnels sont absents quand l'hôte est injoignable
// ou que l'étape n'a pas eu lieu.
type ConnectionTest struct {
	Reachable         bool    `json:"reachable"`
	Error             string  `json:"error,omitempty"`
	Fingerprint       *string `json:"fingerprint,omitempty"`
	KeyType           *string `json:"key_type,omitempty"`
	HostKeyStatus     *string `json:"host_key_status,omitempty"`
	PinnedFingerprint *string `json:"pinned_fingerprint,omitempty"`
	Matches           *bool   `json:"matches,omitempty"`
	AuthOK            *bool   `json:"auth_ok,omitempty"`
}

func ptr[T any](v T) *T { return &v }

// TestConnection récupère l'empreinte présentée; si la clé est épinglée, vérifie la correspondance puis l'authentification.
// Une divergence sur une clé épinglée bloque le serveur (statut mismatch) et est auditée.
func (s *Service) TestConnection(ctx context.Context, id, ip string) (ConnectionTest, error) {
	sv, err := store.ServerByID(ctx, s.st.DB, id)
	if err != nil {
		return ConnectionTest{}, err
	}
	fp, kt, err := sshexec.ScanHostKey(ctx, sv.Host, sv.Port)
	if err != nil {
		return ConnectionTest{Error: err.Error()}, nil
	}
	res := ConnectionTest{Reachable: true, Fingerprint: &fp, KeyType: &kt, HostKeyStatus: ptr(sv.HostKeyStatus), PinnedFingerprint: ptr(sv.HostKeyFingerprint)}
	if sv.HostKeyStatus != "pinned" && sv.HostKeyStatus != "mismatch" {
		return res, nil
	}
	if fp != sv.HostKeyFingerprint {
		res.Matches = ptr(false)
		if sv.HostKeyStatus == "pinned" {
			s.markMismatch(ctx, sv, fp, ip)
			res.HostKeyStatus = ptr("mismatch")
		}
		return res, nil
	}
	res.Matches = ptr(true)
	if sv.HostKeyStatus == "pinned" {
		t, terr := s.tgt.TargetFor(ctx, sv)
		if terr != nil {
			res.AuthOK, res.Error = ptr(false), "identité SSH inutilisable"
		} else if c, derr := sshexec.Dial(ctx, t); derr != nil {
			res.AuthOK, res.Error = ptr(false), derr.Error()
			_ = store.MarkSSH(ctx, s.st.DB, sv.ID, false)
		} else {
			c.Close()
			res.AuthOK = ptr(true)
			_ = store.MarkSSH(ctx, s.st.DB, sv.ID, true)
			s.pool.Invalidate(sv.ID) // un test réussi lève une éventuelle pause après échec d'authentification
		}
	}
	return res, nil
}

func (s *Service) markMismatch(ctx context.Context, sv store.Server, got, ip string) {
	ctx = context.WithoutCancel(ctx)
	_ = store.SetHostKeyStatus(ctx, s.st.DB, sv.ID, "mismatch")
	s.pool.Invalidate(sv.ID)
	_ = audit.RecordStandalone(ctx, s.st, audit.Event{ActorType: "system", ActorName: "ssh", Action: "host_key.mismatch", TargetType: "server",
		TargetID: sv.ID, TargetName: sv.Slug, Diff: map[string]any{"expected": sv.HostKeyFingerprint, "received": got}, IP: ip})
}

// PinHostKey épingle l'empreinte confirmée par l'utilisateur. Un nouveau scan vérifie qu'elle est toujours celle présentée par l'hôte.
func (s *Service) PinHostKey(ctx context.Context, actor store.User, ip, id, fingerprint string) (store.Server, error) {
	sv, err := store.ServerByID(ctx, s.st.DB, id)
	if err != nil {
		return store.Server{}, err
	}
	fp, _, err := sshexec.ScanHostKey(ctx, sv.Host, sv.Port)
	if err != nil {
		return store.Server{}, &UnreachableError{Err: err}
	}
	if fingerprint == "" || fp != fingerprint {
		return store.Server{}, ErrFingerprintChanged
	}
	out := sv
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		if err := store.SetHostKey(ctx, tx, sv.ID, fp, "pinned"); err != nil {
			return audit.Event{}, err
		}
		s.pool.Invalidate(sv.ID)
		action := "server.trust_host_key"
		if sv.HostKeyStatus != "unknown" {
			action = "server.accept_host_key"
		}
		out.HostKeyFingerprint, out.HostKeyStatus = fp, "pinned"
		return audit.Event{Action: action, TargetType: "server", TargetID: sv.ID, TargetName: sv.Slug,
			Diff: map[string]any{"before": map[string]any{"fingerprint": sv.HostKeyFingerprint, "status": sv.HostKeyStatus}, "after": map[string]any{"fingerprint": fp, "status": "pinned"}}}, nil
	})
	return out, err
}
