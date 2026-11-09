package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

const minPassword = 10

// resetPassword: récupération hors interface quand l'accès administrateur est perdu.
// Le mot de passe est saisi au terminal (ou lu sur l'entrée standard), jamais passé en argument.
func resetPassword(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: leassh reset-password <identifiant>")
	}
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
	u, err := st.UserByUsername(ctx, args[0])
	if errors.Is(err, store.ErrNotFound) {
		users, _ := store.ListUsers(ctx, st.DB)
		names := make([]string, 0, len(users))
		for _, x := range users {
			names = append(names, x.Username)
		}
		return fmt.Errorf("utilisateur %q introuvable (existants: %s)", args[0], strings.Join(names, ", "))
	}
	if err != nil {
		return err
	}
	pw, err := readPassword()
	if err != nil {
		return err
	}
	if len(pw) < minPassword {
		return fmt.Errorf("mot de passe trop court (%d caractères minimum)", minPassword)
	}
	hash, err := crypto.HashPassword(pw)
	if err != nil {
		return err
	}
	err = st.Tx(ctx, func(tx *sql.Tx) error {
		if err := store.SetPassword(ctx, tx, u.ID, hash); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET disabled=0 WHERE id=?`, u.ID); err != nil {
			return err
		}
		if err := store.DeleteSessionsOf(ctx, tx, u.ID, ""); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "system", ActorName: "cli", Action: "user.password_reset_cli",
			TargetType: "user", TargetID: u.ID, TargetName: u.Username})
	})
	if err != nil {
		return err
	}
	fmt.Printf("mot de passe de %q réinitialisé; compte réactivé, sessions fermées\n", u.Username)
	return nil
}

func readPassword() (string, error) {
	fd := int(os.Stdin.Fd()) //nolint:gosec // un descripteur de fichier tient toujours dans un int
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "Nouveau mot de passe: ")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		fmt.Fprint(os.Stderr, "Confirmer: ")
		c, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if string(b) != string(c) {
			return "", errors.New("les deux saisies diffèrent")
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
