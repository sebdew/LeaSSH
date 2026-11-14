package settings

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/sebdew/LeaSSH/internal/store"
)

func TestParseProxies(t *testing.T) {
	got, err := ParseProxies("10.0.0.1, 192.168.0.0/16\n::1\t2001:db8::/32")
	if err != nil || len(got) != 4 {
		t.Fatalf("liste valide: %v %v", got, err)
	}
	if got[0].String() != "10.0.0.1/32" || got[2].String() != "::1/128" {
		t.Fatalf("une adresse seule doit devenir un préfixe complet: %v", got)
	}
	if got, err := ParseProxies("  "); err != nil || len(got) != 0 {
		t.Fatalf("liste vide: %v %v", got, err)
	}
	for _, bad := range []string{"10.0.0.1, nope", "10.0.0.0/33", "1.2.3"} {
		if _, err := ParseProxies(bad); err == nil {
			t.Errorf("%q aurait dû être refusé", bad)
		}
	}
}

func TestTrusted(t *testing.T) {
	s := &Service{}
	s.apply(Values{TrustedProxies: "10.0.0.1, 192.168.0.0/16", LogLevel: "info"})
	for addr, want := range map[string]bool{"10.0.0.1": true, "10.0.0.2": false, "192.168.4.5": true, "8.8.8.8": false} {
		if got := s.Trusted(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Trusted(%s) = %v, attendu %v", addr, got, want)
		}
	}
	s.apply(Values{})
	if s.Trusted(netip.MustParseAddr("10.0.0.1")) {
		t.Error("sans proxy déclaré, personne n'est de confiance")
	}
}

func TestCheckMetricsToken(t *testing.T) {
	s := &Service{}
	s.apply(Values{})
	if s.CheckMetricsToken("") || s.CheckMetricsToken("x") {
		t.Fatal("sans jeton défini, /metrics doit rester fermé, même avec un jeton vide")
	}
	s.apply(Values{MetricsTokenHash: HashToken("secret")})
	if !s.CheckMetricsToken("secret") {
		t.Fatal("le bon jeton doit être accepté")
	}
	for _, bad := range []string{"", "Secret", "secret ", "autre"} {
		if s.CheckMetricsToken(bad) {
			t.Errorf("jeton %q accepté à tort", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Values{BaseURL: " https://leassh.example.com/ ", TrustedProxies: "10.0.0.1,  10.0.0.2", SessionTTLHours: 8, ApprovalTTLMin: 60, LogLevel: "info"}
	if err := ok.Validate(); err != nil || ok.BaseURL != "https://leassh.example.com" || ok.TrustedProxies != "10.0.0.1, 10.0.0.2" {
		t.Fatalf("normalisation: %+v %v", ok, err)
	}
	for name, mod := range map[string]func(*Values){
		"url avec chemin": func(v *Values) { v.BaseURL = "https://x/y" },
		"schéma":          func(v *Values) { v.BaseURL = "javascript:alert(1)" },
		"proxy":           func(v *Values) { v.TrustedProxies = "pas-une-ip" },
		"session":         func(v *Values) { v.SessionTTLHours = 0 },
		"validation":      func(v *Values) { v.ApprovalTTLMin = 0 },
		"rétention":       func(v *Values) { v.LogRetentionDays = -1 },
		"niveau":          func(v *Values) { v.LogLevel = "trace" },
	} {
		v := Values{SessionTTLHours: 8, ApprovalTTLMin: 60, LogLevel: "info"}
		mod(&v)
		if v.Validate() == nil {
			t.Errorf("%s: valeur refusée attendue", name)
		}
	}
}

func TestUpdateAppliesAuditsAndHashesToken(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var u store.User
	if err := st.Tx(ctx, func(tx *sql.Tx) (err error) {
		u, err = store.InsertUser(ctx, tx, store.User{Username: "adm", Role: "admin", PasswordHash: "x"})
		return
	}); err != nil {
		t.Fatal(err)
	}
	svc, err := New(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := Values{BaseURL: "https://leassh.example.com", SessionTTLHours: 4, ApprovalTTLMin: 30, LogRetentionDays: 90, LogLevel: "debug"}

	var inv *InvalidError
	bad := base
	bad.SessionTTLHours = 0
	if _, err := svc.Update(ctx, u, "192.0.2.1", UpdateInput{Values: bad}); !errors.As(err, &inv) {
		t.Fatalf("valeur invalide: %v", err)
	}
	if _, err := svc.Update(ctx, u, "", UpdateInput{Values: base, MetricsToken: strings.Repeat("x", 257)}); !errors.As(err, &inv) {
		t.Fatalf("jeton trop long: %v", err)
	}
	if svc.Get().SessionTTLHours == 4 {
		t.Fatal("un refus ne doit rien appliquer")
	}

	nv, err := svc.Update(ctx, u, "192.0.2.1", UpdateInput{Values: base, MetricsToken: "jeton-metrics"})
	if err != nil || nv.MetricsTokenHash == "" || nv.MetricsTokenHash == "jeton-metrics" {
		t.Fatalf("jeton haché: %+v %v", nv, err)
	}
	if svc.Get().SessionTTLHours != 4 || !svc.CheckMetricsToken("jeton-metrics") {
		t.Fatal("les réglages doivent être appliqués à chaud")
	}
	// jeton conservé si non fourni, supprimé sur demande
	if nv, _ = svc.Update(ctx, u, "", UpdateInput{Values: base}); nv.MetricsTokenHash == "" {
		t.Fatal("le jeton doit être conservé")
	}
	if nv, _ = svc.Update(ctx, u, "", UpdateInput{Values: base, ClearMetricsToken: true}); nv.MetricsTokenHash != "" || svc.CheckMetricsToken("jeton-metrics") {
		t.Fatal("le jeton doit être supprimé")
	}
	var diffs string
	rows, _ := st.DB.Query(`SELECT diff FROM audit_events WHERE action = 'settings.update'`)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var d string
		_ = rows.Scan(&d)
		diffs += d
		n++
	}
	if n != 3 || strings.Contains(diffs, "jeton-metrics") {
		t.Fatalf("audit: %d événements, jeton exposé=%v", n, strings.Contains(diffs, "jeton-metrics"))
	}
}
