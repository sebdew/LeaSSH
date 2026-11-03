package proposals

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/leash-dev/leash/internal/catalog"
	"github.com/leash-dev/leash/internal/store"
)

func setup(t *testing.T) (*Service, *store.Store, store.MCPKey, store.User) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var k store.MCPKey
	var u store.User
	err = st.Tx(ctx, func(tx *sql.Tx) (err error) {
		k, err = store.InsertKey(ctx, tx, store.MCPKey{Name: "N1", Prefix: "lsh_aaaa", Hash: "h", CanPropose: true})
		if err != nil {
			return err
		}
		u, err = store.InsertUser(ctx, tx, store.User{Username: "adm", Role: "admin", PasswordHash: "x"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return New(st), st, k, u
}

var good = Payload{Slug: "mysql.replica_status", Name: "Réplication", Description: "Affiche l'état de la réplication MySQL.", Template: `mysql --batch -e "SHOW REPLICA STATUS"`, Risk: catalog.RiskRead}

// Critère 15.4
func TestMetacharProposalIsInvalidAndCannotBeApproved(t *testing.T) {
	s, st, k, u := setup(t)
	ctx := context.Background()
	bad := good
	bad.Template = "mysql -e x; rm -rf /"
	p, res, err := s.Create(ctx, k, bad, "j")
	if err != nil || p.Status != "invalid" || res.OK() {
		t.Fatalf("invalide attendu: %+v %v", p, err)
	}
	if _, err := s.Approve(ctx, p.ID, u, nil, nil, ""); err == nil || !strings.Contains(err.Error(), "linter") {
		t.Fatalf("approbation d'une proposition invalide: %v", err)
	}
	// correction par l'admin puis approbation
	fixed := good
	c, err := s.Approve(ctx, p.ID, u, &fixed, nil, "corrigé")
	if err != nil || c.Source != "proposal" || c.Status != "enabled" || c.Version != 1 {
		t.Fatalf("approbation corrigée: %+v %v", c, err)
	}
	got, _ := store.ProposalByID(ctx, st.DB, p.ID)
	if got.Status != "approved" || got.CommandID != c.ID || got.ReviewedBy != u.ID {
		t.Fatalf("proposition: %+v", got)
	}
	if _, err := s.Approve(ctx, p.ID, u, nil, nil, ""); err != ErrNotPending {
		t.Fatalf("double approbation: %v", err)
	}
}

func TestLimitsAndPermissions(t *testing.T) {
	s, _, k, _ := setup(t)
	ctx := context.Background()
	nope := k
	nope.CanPropose = false
	if _, _, err := s.Create(ctx, nope, good, "j"); err != ErrCannotPropose {
		t.Fatalf("can_propose: %v", err)
	}
	for i := 0; i < MaxOpenPerKey; i++ {
		p := good
		p.Slug = "mysql.cmd" + string(rune('a'+i))
		if _, _, err := s.Create(ctx, k, p, "j"); err != nil {
			t.Fatalf("proposition %d: %v", i, err)
		}
	}
	if _, _, err := s.Create(ctx, k, good, "j"); err != ErrTooMany {
		t.Fatalf("limite: %v", err)
	}
}

func TestSlugTakenAndReject(t *testing.T) {
	s, st, k, u := setup(t)
	ctx := context.Background()
	st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := store.InsertCommand(ctx, tx, store.Command{Slug: good.Slug, Name: "x", Risk: catalog.RiskRead, Template: "uptime", Status: "enabled", Source: "manual"})
		return err
	})
	p, _, _ := s.Create(ctx, k, good, "j")
	if p.Status != "invalid" {
		t.Fatalf("slug existant: %+v", p)
	}
	if err := s.Reject(ctx, p.ID, u, "doublon"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ProposalByID(ctx, st.DB, p.ID); got.Status != "rejected" {
		t.Fatalf("rejet: %+v", got)
	}
}
