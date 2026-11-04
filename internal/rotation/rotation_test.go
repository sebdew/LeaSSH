package rotation

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/leassh-dev/leassh/internal/auditreport"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

func TestRotate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	oldB, _ := crypto.NewBox(bytes.Repeat([]byte{1}, 32))
	newB, _ := crypto.NewBox(bytes.Repeat([]byte{2}, 32))
	pk, _ := oldB.Seal([]byte("PRIVATE-KEY"))
	pw, _ := oldB.Seal([]byte("hunter2"))
	if err := auditreport.SaveAIConfig(ctx, st.DB, oldB, auditreport.AIConfig{Enabled: true, URL: "http://llm/v1", Model: "m"}, "sk-ia"); err != nil {
		t.Fatal(err)
	}
	tok, _ := oldB.Seal([]byte("wg-token"))
	slKey, _ := oldB.Seal([]byte("TLS-PRIVATE-KEY"))
	sc, _ := store.GetSyslogConfig(ctx, st.DB)
	sc.TLSClientKeyEnc = slKey
	if err := store.SaveSyslogConfig(ctx, st.DB, sc); err != nil {
		t.Fatal(err)
	}
	ssoSecret, _ := oldB.Seal([]byte("sso-client-secret"))
	oc, _ := store.GetOIDCConfig(ctx, st.DB)
	oc.ClientSecretEnc = ssoSecret
	if err := store.SaveOIDCConfig(ctx, st.DB, oc); err != nil {
		t.Fatal(err)
	}
	var provIdent store.Identity
	st.Tx(ctx, func(tx *sql.Tx) error {
		store.InsertIdentity(ctx, tx, store.Identity{Name: "k", Username: "u", AuthType: "key", PrivateKeyEnc: pk})
		provIdent, _ = store.InsertIdentity(ctx, tx, store.Identity{Name: "wg", Username: "ia", AuthType: "key"})
		store.InsertProvisioner(ctx, tx, store.Provisioner{Name: "bastion", Type: "warpgate", Enabled: true, IdentityID: provIdent.ID, Config: map[string]any{}, SecretEnc: tok})
		store.InsertIdentity(ctx, tx, store.Identity{Name: "p", Username: "u", AuthType: "password", PasswordEnc: pw})
		return nil
	})

	// mauvaise ancienne clé: rien ne change
	wrong, _ := crypto.NewBox(bytes.Repeat([]byte{9}, 32))
	if _, err := Rotate(ctx, st, wrong, newB); err == nil {
		t.Fatal("rotation avec une mauvaise clé acceptée")
	}
	ids, _ := store.ListIdentities(ctx, st.DB)
	if got, err := oldB.Open(ids[0].PrivateKeyEnc); err != nil && ids[0].Name == "k" {
		t.Fatalf("secret modifié malgré l'échec: %v %q", err, got)
	}

	n, err := Rotate(ctx, st, oldB, newB)
	if err != nil || n != 6 { // 2 identités + clé d'API de la synthèse + clé TLS syslog + secret client SSO + jeton du connecteur
		t.Fatalf("rotation: %d %v", n, err)
	}
	if _, key, err := auditreport.LoadAIConfig(ctx, st.DB, newB); err != nil || key != "sk-ia" {
		t.Fatalf("clé d'API de la synthèse après rotation: %q %v", key, err)
	}
	if _, _, err := auditreport.LoadAIConfig(ctx, st.DB, oldB); err == nil {
		t.Fatal("clé d'API de la synthèse encore lisible avec l'ancienne clé")
	}
	if sc, _ := store.GetSyslogConfig(ctx, st.DB); func() bool {
		b, err := newB.Open(sc.TLSClientKeyEnc)
		return err != nil || string(b) != "TLS-PRIVATE-KEY"
	}() {
		t.Fatal("clé TLS du syslog non rechiffrée")
	}
	if oc, _ := store.GetOIDCConfig(ctx, st.DB); func() bool {
		b, err := newB.Open(oc.ClientSecretEnc)
		return err != nil || string(b) != "sso-client-secret"
	}() {
		t.Fatal("secret client SSO non rechiffré")
	}
	provs, _ := store.ListProvisioners(ctx, st.DB)
	if got, err := newB.Open(provs[0].SecretEnc); err != nil || string(got) != "wg-token" {
		t.Fatalf("jeton du connecteur après rotation: %q %v", got, err)
	}
	ids, _ = store.ListIdentities(ctx, st.DB)
	for _, i := range ids {
		if i.Name == "wg" {
			continue
		}
		blob, want := i.PrivateKeyEnc, "PRIVATE-KEY"
		if i.AuthType == "password" {
			blob, want = i.PasswordEnc, "hunter2"
		}
		if got, err := newB.Open(blob); err != nil || string(got) != want {
			t.Fatalf("%s avec la nouvelle clé: %q %v", i.Name, got, err)
		}
		if _, err := oldB.Open(blob); err == nil {
			t.Fatalf("%s lisible avec l'ancienne clé", i.Name)
		}
	}
	var act string
	st.DB.QueryRow(`SELECT action FROM audit_events ORDER BY seq DESC LIMIT 1`).Scan(&act)
	if act != "master_key.rotate" {
		t.Fatalf("audit: %s", act)
	}
}
