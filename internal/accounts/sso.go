package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/oidc"
	"github.com/sebdew/LeaSSH/internal/store"
)

var (
	ErrNoLocalAdmin       = errors.New("il faut au moins un administrateur local actif (compte de secours) pour restreindre le mot de passe local")
	ErrNoSSOAdmin         = errors.New("connectez-vous d'abord une fois en tant qu'administrateur via le SSO avant de désactiver le mot de passe local")
	ErrSSONotProvisioned  = errors.New("compte SSO non provisionné")
	ErrSSOAccountDisabled = errors.New("compte SSO désactivé")
)

// SSOUpdate: modification de la configuration SSO. Le secret client arrive déjà chiffré (SecretEnc); ReplaceSecret
// distingue « remplacer ou effacer » (true) de « conserver l'existant » (false).
type SSOUpdate struct {
	Config        store.OIDCConfig // ClientSecretEnc est ignoré: voir SecretEnc
	ReplaceSecret bool
	SecretEnc     []byte
	Actor         store.User
	IP            string
}

// UpdateSSOConfig enregistre la configuration SSO avec ses garde-fous (compte de secours local, administrateur SSO déjà
// connecté avant de couper le mot de passe local) et l'événement d'audit, dans une même transaction.
func (s *Service) UpdateSSOConfig(ctx context.Context, in SSOUpdate) (store.OIDCConfig, error) {
	var saved store.OIDCConfig
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		old, err := store.GetOIDCConfig(ctx, tx)
		if err != nil {
			return err
		}
		c := in.Config
		c.ClientSecretEnc = old.ClientSecretEnc
		if in.ReplaceSecret {
			c.ClientSecretEnc = in.SecretEnc
		}
		if c.LocalPasswordAdminsOnly {
			if n, err := store.CountLocalAdmins(ctx, tx); err != nil {
				return err
			} else if n == 0 {
				return ErrNoLocalAdmin
			}
		}
		if c.LocalLoginDisabled {
			if n, err := store.CountOIDCAdmins(ctx, tx); err != nil {
				return err
			} else if n == 0 {
				return ErrNoSSOAdmin
			}
		}
		if err := store.SaveOIDCConfig(ctx, tx, c); err != nil {
			return err
		}
		saved = c
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: in.Actor.ID, ActorName: in.Actor.Username, IP: in.IP, Action: "oidc.update",
			TargetType: "oidc", TargetName: c.Issuer,
			Diff: map[string]any{"before": ssoAuditView(old), "after": ssoAuditView(c), "credential_changed": in.ReplaceSecret}})
	})
	if err != nil {
		return store.OIDCConfig{}, fmt.Errorf("configuration SSO: %w", err)
	}
	return saved, nil
}

// ssoAuditView: ce qui est écrit dans l'audit (jamais le secret).
func ssoAuditView(c store.OIDCConfig) map[string]any {
	var rm oidc.RoleMap
	_ = json.Unmarshal([]byte(c.RoleMap), &rm)
	return map[string]any{"enabled": c.Enabled, "name": c.Name, "issuer": c.Issuer, "client_id": c.ClientID, "has_client_secret": len(c.ClientSecretEnc) > 0,
		"scopes": c.Scopes, "role_claim": c.RoleClaim, "role_map": rm, "default_role": c.DefaultRole, "jit": c.JIT,
		"local_password_admins_only": c.LocalPasswordAdminsOnly, "local_login_disabled": c.LocalLoginDisabled, "end_session": c.EndSession, "auto_redirect": c.AutoRedirect}
}

// SSOSignIn: identité validée par le fournisseur (jeton vérifié) et rôle déjà déduit du mapping.
type SSOSignIn struct {
	Issuer, Sub, Username, DisplayName, Role string
	JIT                                      bool // création automatique du compte autorisée
	IP                                       string
}

// SignInSSO retrouve le compte lié à (issuer, sub), le crée si le JIT l'autorise, synchronise nom et rôle (sans retirer le
// dernier administrateur) et journalise, le tout dans une transaction. Les refus sont ErrSSONotProvisioned et ErrSSOAccountDisabled.
func (s *Service) SignInSSO(ctx context.Context, in SSOSignIn) (store.User, error) {
	var user store.User
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		actor := audit.Event{ActorType: "system", ActorName: "sso", IP: in.IP}
		u, err := store.UserByOIDC(ctx, tx, in.Issuer, in.Sub)
		switch {
		case errors.Is(err, store.ErrNotFound):
			if !in.JIT {
				return ErrSSONotProvisioned
			}
			name, err := freeUsername(ctx, tx, in.Username, in.Issuer, in.Sub)
			if err != nil {
				return err
			}
			if u, err = store.InsertOIDCUser(ctx, tx, store.User{Username: name, DisplayName: in.DisplayName, Role: in.Role, OIDCIssuer: in.Issuer, OIDCSub: in.Sub}); err != nil {
				return err
			}
			ev := actor
			ev.Action, ev.TargetType, ev.TargetID, ev.TargetName = "user.create", "user", u.ID, u.Username
			ev.Diff = map[string]any{"after": map[string]any{"username": u.Username, "role": u.Role, "auth_provider": "oidc", "issuer": in.Issuer}, "jit": true}
			if err := audit.Record(ctx, tx, ev); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if u.Disabled {
				return ErrSSOAccountDisabled
			}
			if u.Role != in.Role || u.DisplayName != in.DisplayName {
				newRole := in.Role
				diff := map[string]any{"before": map[string]any{"role": u.Role, "display_name": u.DisplayName}}
				if u.Role == "admin" && in.Role != "admin" {
					if n, err := store.CountEnabledAdmins(ctx, tx); err != nil {
						return err
					} else if n <= 1 {
						newRole = "admin" // garde-fou « dernier administrateur »
						diff["role_sync_skipped"] = "last_admin"
					}
				}
				if err := store.SyncOIDCUser(ctx, tx, u.ID, in.DisplayName, newRole); err != nil {
					return err
				}
				if newRole != u.Role {
					if err := store.DeleteSessionsOf(ctx, tx, u.ID, ""); err != nil { // le nouveau rôle s'applique tout de suite
						return err
					}
				}
				diff["after"] = map[string]any{"role": newRole, "display_name": in.DisplayName}
				ev := actor
				ev.Action, ev.TargetType, ev.TargetID, ev.TargetName, ev.Diff = "user.update", "user", u.ID, u.Username, diff
				if err := audit.Record(ctx, tx, ev); err != nil {
					return err
				}
				u.Role, u.DisplayName = newRole, in.DisplayName
			}
		}
		user = u
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: u.ID, ActorName: u.Username, Action: "user.sso_login",
			TargetType: "user", TargetID: u.ID, TargetName: u.Username, Diff: map[string]any{"issuer": in.Issuer, "role": u.Role}, IP: in.IP})
	})
	if err != nil {
		if errors.Is(err, ErrSSONotProvisioned) || errors.Is(err, ErrSSOAccountDisabled) {
			return store.User{}, err
		}
		return store.User{}, fmt.Errorf("connexion SSO: %w", err)
	}
	_ = s.st.TouchLogin(ctx, user.ID)
	return user, nil
}

// CleanName retire les caractères de contrôle et borne la longueur d'un nom d'utilisateur venu d'un fournisseur externe.
func CleanName(v string) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if len(v) > MaxUsername {
		v = strings.ToValidUTF8(v[:MaxUsername], "")
	}
	return v
}

// freeUsername: nom d'utilisateur libre. Un compte existant du même nom n'est JAMAIS lié: on suffixe.
func freeUsername(ctx context.Context, tx *sql.Tx, want, issuer, sub string) (string, error) {
	host := issuer
	if u, err := url.Parse(issuer); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	short, _ := oidc.RandomString(4)
	for _, cand := range []string{want, want + "@" + host, want + "-" + short} {
		cand = CleanName(cand)
		if cand == "" {
			continue
		}
		taken, err := store.UsernameExists(ctx, tx, cand)
		if err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
	}
	if len(sub) > 8 {
		sub = strings.ToValidUTF8(sub[:8], "")
	}
	return "sso-" + short + "-" + sub, nil
}

// SSOConfig lit la configuration SSO enregistrée.
func (s *Service) SSOConfig(ctx context.Context) (store.OIDCConfig, error) {
	return store.GetOIDCConfig(ctx, s.st.DB)
}
