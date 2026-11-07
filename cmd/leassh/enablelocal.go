package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/store"
)

// enableLocalLogin: secours hors interface quand le SSO est en panne et que le mot de passe local a été désactivé.
func enableLocalLogin() error {
	dir := os.Getenv("LEASSH_DATA_DIR")
	if dir == "" {
		dir = "/data"
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dir)
	if err != nil {
		return err
	}
	defer st.Close()
	err = st.Tx(ctx, func(tx *sql.Tx) error {
		c, err := store.GetOIDCConfig(ctx, tx)
		if err != nil {
			return err
		}
		c.LocalLoginDisabled = false
		if err := store.SaveOIDCConfig(ctx, tx, c); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "system", ActorName: "cli", Action: "oidc.local_login_enabled_cli", TargetType: "oidc", TargetName: c.Issuer})
	})
	if err != nil {
		return err
	}
	fmt.Println("connexion par mot de passe local réactivée")
	return nil
}
