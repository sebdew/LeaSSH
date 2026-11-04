// Package testkit assemble une instance complète (base, SSH factice, services) pour les tests d'intégration.
// Réservé aux tests: ne jamais importer depuis le code de production.
package testkit

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/access"
	"github.com/leassh-dev/leassh/internal/catalog"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/executions"
	"github.com/leassh-dev/leassh/internal/proposals"
	"github.com/leassh-dev/leassh/internal/sshexec"
	"github.com/leassh-dev/leassh/internal/sshtest"
	"github.com/leassh-dev/leassh/internal/store"
)

type Kit struct {
	Ctx    context.Context
	St     *store.Store
	Box    *crypto.Box
	SSH    *sshtest.Server
	Pool   *sshexec.Pool
	Acc    *access.Service
	Exec   *executions.Service
	Props  *proposals.Service
	Server store.Server
	Key    store.MCPKey
	Token  string
	Admin  store.User
	Rule   store.AccessRule
	Cmds   map[string]store.Command
}

func i64(n int64) *int64 { return &n }

// New crée: 1 serveur épinglé (web-01), 5 commandes (t.read, t.write, t.destroy, t.lines, t.fail), 1 clé, 1 règle.
func New(t testing.TB, profile string, writeRequiresApproval bool, approvalTTL time.Duration) *Kit {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := crypto.NewBox(bytes.Repeat([]byte{1}, 32))
	fs := sshtest.New(t)
	pool := sshexec.NewPool(time.Minute, 2)
	t.Cleanup(pool.Close)
	acc := access.New(st)
	k := &Kit{Ctx: ctx, St: st, Box: box, SSH: fs, Pool: pool, Acc: acc, Cmds: map[string]store.Command{}}
	k.Exec = executions.New(ctx, st, acc, box, pool, slog.New(slog.NewTextHandler(io.Discard, nil)), approvalTTL)
	t.Cleanup(k.Exec.Wait)
	k.Props = proposals.New(st)
	k.Token, _ = crypto.NewToken()
	enc, _ := box.Seal(fs.ClientPEM)
	err = st.Tx(ctx, func(tx *sql.Tx) error {
		id, err := store.InsertIdentity(ctx, tx, store.Identity{Name: "i", Username: "u", AuthType: "key", PrivateKeyEnc: enc})
		if err != nil {
			return err
		}
		k.Server, err = store.InsertServer(ctx, tx, store.Server{Slug: "web-01", Name: "Web 01", Description: "Frontal nginx", Host: "127.0.0.1", Port: fs.Port,
			IdentityID: id.ID, HostKeyFingerprint: fs.Fingerprint, HostKeyStatus: "pinned", Enabled: true, Tags: []string{"web"}})
		if err != nil {
			return err
		}
		var ids []string
		for _, c := range []store.Command{
			{Slug: "t.read", Risk: catalog.RiskRead, Template: "echo hello", Description: "Dit bonjour"},
			{Slug: "t.write", Risk: catalog.RiskWrite, Template: "echo wrote"},
			{Slug: "t.destroy", Risk: catalog.RiskDestructive, Template: "echo destroyed"},
			{Slug: "t.lines", Risk: catalog.RiskRead, Template: "echo {{lines}}", Params: map[string]catalog.ParamDef{"lines": {Type: catalog.TypeInt, Min: i64(1), Max: i64(100)}}},
			{Slug: "t.fail", Risk: catalog.RiskRead, Template: "exit 4"},
		} {
			c.Name, c.Status, c.Source = c.Slug, "enabled", "manual"
			if c, err = store.InsertCommand(ctx, tx, c); err != nil {
				return err
			}
			k.Cmds[c.Slug] = c
			ids = append(ids, c.ID)
		}
		sg, _ := store.InsertGroup(ctx, tx, store.ServerGroups, store.Group{Slug: "sg", Name: "sg", MemberIDs: []string{k.Server.ID}})
		cg, _ := store.InsertGroup(ctx, tx, store.CommandGroups, store.Group{Slug: "cg", Name: "cg", MemberIDs: ids})
		k.Key, err = store.InsertKey(ctx, tx, store.MCPKey{Name: "Agent N1", Prefix: crypto.Prefix(k.Token), Hash: crypto.HashToken(k.Token), CanPropose: true})
		if err != nil {
			return err
		}
		k.Rule, err = store.InsertRule(ctx, tx, store.AccessRule{Name: "rule", ServerGroupIDs: []string{sg.ID}, CommandGroupIDs: []string{cg.ID}, Profile: profile,
			WriteRequiresApproval: writeRequiresApproval, Enabled: true, KeyIDs: []string{k.Key.ID}})
		if err != nil {
			return err
		}
		k.Admin, err = store.InsertUser(ctx, tx, store.User{Username: "sd", DisplayName: "SD", Role: "admin", PasswordHash: "x"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return k
}
