// Package rotation: changement de la clé maître (`leassh rotate-master-key`).
package rotation

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/auditreport"
	"github.com/sebdew/LeaSSH/internal/crypto"
	"github.com/sebdew/LeaSSH/internal/store"
)

func reseal(oldBox, newBox *crypto.Box, blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return blob, nil
	}
	plain, err := oldBox.Open(blob)
	if err != nil {
		return nil, err
	}
	return newBox.Seal(plain)
}

// Rotate rechiffre tous les secrets (identités SSH, secret du webhook, clé TLS du syslog, secret client SSO, clé d'API de la synthèse d'audit, jetons des connecteurs) dans une seule transaction:
// si un seul secret ne se déchiffre pas avec l'ancienne clé, rien n'est modifié.
func Rotate(ctx context.Context, st *store.Store, oldBox, newBox *crypto.Box) (int, error) {
	n := 0
	err := st.Tx(ctx, func(tx *sql.Tx) error {
		idents, err := store.ListIdentities(ctx, tx)
		if err != nil {
			return err
		}
		for _, i := range idents {
			pk, err := reseal(oldBox, newBox, i.PrivateKeyEnc)
			if err != nil {
				return fmt.Errorf("identité %q (clé privée): %w", i.Name, err)
			}
			pw, err := reseal(oldBox, newBox, i.PasswordEnc)
			if err != nil {
				return fmt.Errorf("identité %q (mot de passe): %w", i.Name, err)
			}
			if err := store.UpdateIdentitySecrets(ctx, tx, i.ID, pk, pw); err != nil {
				return err
			}
			if len(pk)+len(pw) > 0 {
				n++
			}
		}
		for _, setting := range []struct{ key, what string }{{"webhook.secret_enc", "secret du webhook"}, {auditreport.KeyAISecret, "clé d'API de la synthèse d'audit"}} {
			enc, _ := store.GetSetting(ctx, tx, setting.key)
			if enc == "" {
				continue
			}
			blob, err := base64.StdEncoding.DecodeString(enc)
			if err != nil {
				return fmt.Errorf("%s: %w", setting.what, err)
			}
			nb, err := reseal(oldBox, newBox, blob)
			if err != nil {
				return fmt.Errorf("%s: %w", setting.what, err)
			}
			if err := store.SetSetting(ctx, tx, setting.key, base64.StdEncoding.EncodeToString(nb)); err != nil {
				return err
			}
			n++
		}
		sc, err := store.GetSyslogConfig(ctx, tx)
		if err != nil {
			return err
		}
		if len(sc.TLSClientKeyEnc) > 0 {
			if sc.TLSClientKeyEnc, err = reseal(oldBox, newBox, sc.TLSClientKeyEnc); err != nil {
				return fmt.Errorf("clé TLS cliente du syslog: %w", err)
			}
			if err := store.SaveSyslogConfig(ctx, tx, sc); err != nil {
				return err
			}
			n++
		}
		oc, err := store.GetOIDCConfig(ctx, tx)
		if err != nil {
			return err
		}
		if len(oc.ClientSecretEnc) > 0 {
			if oc.ClientSecretEnc, err = reseal(oldBox, newBox, oc.ClientSecretEnc); err != nil {
				return fmt.Errorf("secret client SSO: %w", err)
			}
			if err := store.SaveOIDCConfig(ctx, tx, oc); err != nil {
				return err
			}
			n++
		}
		provs, err := store.ListProvisioners(ctx, tx)
		if err != nil {
			return err
		}
		for _, p := range provs {
			if !p.HasSecret {
				continue
			}
			nb, err := reseal(oldBox, newBox, p.SecretEnc)
			if err != nil {
				return fmt.Errorf("connecteur %q (jeton d'API): %w", p.Name, err)
			}
			if err := store.UpdateProvisionerSecret(ctx, tx, p.ID, nb); err != nil {
				return err
			}
			n++
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "system", ActorName: "cli", Action: "master_key.rotate", TargetType: "secrets",
			Diff: map[string]any{"re_encrypted": n}})
	})
	return n, err
}
