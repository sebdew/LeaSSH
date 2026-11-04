package authz

import (
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/catalog"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func base() (Key, Server, Command, Rule) {
	k := Key{ID: "k1"}
	s := Server{ID: "s1", Enabled: true, HostKeyStatus: "pinned"}
	c := Command{ID: "c1", Status: "enabled", Risk: catalog.RiskRead}
	r := Rule{ID: "r1", Name: "r", Enabled: true, Profile: RO, WriteRequiresApproval: true,
		KeyIDs: map[string]bool{"k1": true}, ServerIDs: map[string]bool{"s1": true}, CommandIDs: map[string]bool{"c1": true}}
	return k, s, c, r
}

// Table de vérité profils × risques × write_requires_approval (SPEC §5, critères 15.1 et 15.2).
func TestTruthTable(t *testing.T) {
	cases := []struct {
		profile  Profile
		risk     catalog.Risk
		wra      bool
		allowed  bool
		approval bool
	}{
		{RO, catalog.RiskRead, true, true, false},
		{RO, catalog.RiskRead, false, true, false},
		{RO, catalog.RiskWrite, true, false, false},
		{RO, catalog.RiskWrite, false, false, false},
		{RO, catalog.RiskDestructive, true, false, false},
		{RO, catalog.RiskDestructive, false, false, false},
		{RW, catalog.RiskRead, true, true, false},
		{RW, catalog.RiskRead, false, true, false},
		{RW, catalog.RiskWrite, true, true, true},
		{RW, catalog.RiskWrite, false, true, false},
		{RW, catalog.RiskDestructive, true, true, true},
		{RW, catalog.RiskDestructive, false, true, true}, // toujours validé
	}
	for _, c := range cases {
		k, s, cmd, r := base()
		r.Profile, r.WriteRequiresApproval, cmd.Risk = c.profile, c.wra, c.risk
		d := Evaluate(now, k, s, cmd, []Rule{r})
		if d.Allowed != c.allowed || (c.allowed && d.RequiresApproval != c.approval) {
			t.Errorf("%s/%s/wra=%v: obtenu %+v", c.profile, c.risk, c.wra, d)
		}
		if !c.allowed && d.Reason != DenyProfile {
			t.Errorf("%s/%s: raison %q", c.profile, c.risk, d.Reason)
		}
	}
}

func TestPreconditions(t *testing.T) {
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	cases := []struct {
		name   string
		mutate func(*Key, *Server, *Command, *Rule)
		reason string
	}{
		{"clé désactivée", func(k *Key, _ *Server, _ *Command, _ *Rule) { k.Disabled = true }, DenyKeyInactive},
		{"clé expirée", func(k *Key, _ *Server, _ *Command, _ *Rule) { k.ExpiresAt = &past }, DenyKeyExpired},
		{"clé non expirée", func(k *Key, _ *Server, _ *Command, _ *Rule) { k.ExpiresAt = &future }, ""},
		{"serveur désactivé", func(_ *Key, s *Server, _ *Command, _ *Rule) { s.Enabled = false }, DenyServerDisabled},
		{"clé d'hôte divergente", func(_ *Key, s *Server, _ *Command, _ *Rule) { s.HostKeyStatus = "mismatch" }, DenyHostKey},
		{"clé d'hôte inconnue", func(_ *Key, s *Server, _ *Command, _ *Rule) { s.HostKeyStatus = "unknown" }, DenyHostKey},
		{"commande en attente", func(_ *Key, _ *Server, c *Command, _ *Rule) { c.Status = "pending" }, DenyCommandStatus},
		{"commande désactivée", func(_ *Key, _ *Server, c *Command, _ *Rule) { c.Status = "disabled" }, DenyCommandStatus},
		{"règle désactivée", func(_ *Key, _ *Server, _ *Command, r *Rule) { r.Enabled = false }, DenyNoRule},
		{"clé hors règle", func(_ *Key, _ *Server, _ *Command, r *Rule) { r.KeyIDs = map[string]bool{} }, DenyNoRule},
		{"serveur hors groupe", func(_ *Key, _ *Server, _ *Command, r *Rule) { r.ServerIDs = map[string]bool{} }, DenyNoRule},
		{"commande hors groupe", func(_ *Key, _ *Server, _ *Command, r *Rule) { r.CommandIDs = map[string]bool{} }, DenyNoRule},
	}
	for _, c := range cases {
		k, s, cmd, r := base()
		c.mutate(&k, &s, &cmd, &r)
		d := Evaluate(now, k, s, cmd, []Rule{r})
		if c.reason == "" {
			if !d.Allowed {
				t.Errorf("%s: autorisation attendue, obtenu %+v", c.name, d)
			}
		} else if d.Allowed || d.Reason != c.reason {
			t.Errorf("%s: raison %q attendue, obtenu %+v", c.name, c.reason, d)
		}
	}
	if d := Evaluate(now, Key{ID: "k1"}, Server{ID: "s1", Enabled: true, HostKeyStatus: "pinned"}, Command{ID: "c1", Status: "enabled", Risk: catalog.RiskRead}, nil); d.Allowed {
		t.Error("aucune règle: refus attendu")
	}
	// expiration exacte = expiré
	exp := now
	k, s, c, r := base()
	k.ExpiresAt = &exp
	if Evaluate(now, k, s, c, []Rule{r}).Allowed {
		t.Error("expiration à l'instant exact doit refuser")
	}
}

func TestLeastRestrictiveRule(t *testing.T) {
	k, s, c, strict := base()
	c.Risk = catalog.RiskWrite
	strict.ID, strict.Profile, strict.WriteRequiresApproval = "r1", RW, true
	loose := strict
	loose.ID, loose.WriteRequiresApproval = "r2", false
	ro := strict
	ro.ID, ro.Profile = "r0", RO // ne permet pas write: ignorée
	for _, rules := range [][]Rule{{strict, loose, ro}, {ro, loose, strict}, {loose, strict}} {
		d := Evaluate(now, k, s, c, rules)
		if !d.Allowed || d.RequiresApproval || d.Rule.ID != "r2" {
			t.Fatalf("règle la moins contraignante attendue (r2 sans validation): %+v", d)
		}
	}
	// destructive: toutes les règles exigent la validation; choix déterministe par id
	c.Risk = catalog.RiskDestructive
	d := Evaluate(now, k, s, c, []Rule{loose, strict})
	if !d.Allowed || !d.RequiresApproval || d.Rule.ID != "r1" {
		t.Fatalf("destructive: %+v", d)
	}
}
