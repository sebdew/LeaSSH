package provisioning

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leassh-dev/leassh/internal/store"
)

type bastionConn struct{ fakeConn }

func (bastionConn) Endpoint() (string, int) { return "bastion.example.com", 2222 }

func adminEnv(t *testing.T) (*env, store.User) {
	t.Helper()
	e := newEnv(t)
	Register(Type{ID: "fakebastion", Label: "Fake bastion",
		Fields: []Field{{Key: "url", Kind: FieldText}, {Key: "token", Kind: FieldPassword}},
		New: func(cfg map[string]any, secret string, _ Env) (Connector, error) {
			if Str(cfg, "url") == "" {
				return nil, errors.New("url requise")
			}
			return bastionConn{fakeConn{&e.ms}}, nil
		}})
	return e, store.User{ID: "U1", Username: "adm", Role: "admin"}
}

func ident(t *testing.T, e *env) string {
	t.Helper()
	return e.p.IdentityID
}

func TestServiceValidatesInput(t *testing.T) {
	e, u := adminEnv(t)
	id := ident(t, e)
	for name, c := range map[string]struct {
		in   Input
		code string
	}{
		"type":     {Input{Name: "x", Type: "inconnu", IdentityID: id}, "invalid_type"},
		"nom":      {Input{Name: "  ", Type: "fakebastion", IdentityID: id}, "invalid_name"},
		"période":  {Input{Name: "x", Type: "fakebastion", IdentityID: id, IntervalMin: 2000}, "invalid_interval"},
		"identité": {Input{Name: "x", Type: "fakebastion", IdentityID: "absente"}, "invalid_identity"},
		"config":   {Input{Name: "x", Type: "fakebastion", IdentityID: id}, "invalid_config"},
	} {
		_, err := e.svc.Create(e.ctx, u, "", c.in)
		var ie *InputError
		if !errors.As(err, &ie) || ie.Code != c.code {
			t.Errorf("%s: code %s attendu, obtenu %v", name, c.code, err)
		}
	}
	var n int
	_ = e.st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action LIKE 'provisioner.%'`).Scan(&n)
	if n != 0 {
		t.Fatalf("un refus ne doit rien auditer (%d)", n)
	}
}

func TestServiceLifecycleNeverAuditsSecret(t *testing.T) {
	e, u := adminEnv(t)
	id := ident(t, e)
	p, err := e.svc.Create(e.ctx, u, "192.0.2.1", Input{Name: " pont ", Type: "fakebastion", IdentityID: id, Secret: "jeton-super-secret",
		Config: map[string]any{"url": "https://wg", "token": "ne-doit-pas-etre-garde", "inconnu": 1}})
	if err != nil || p.Name != "pont" || !p.Enabled {
		t.Fatalf("création: %+v %v", p, err)
	}
	if _, kept := p.Config["token"]; kept || p.Config["inconnu"] != nil {
		t.Fatalf("seuls les champs connus non secrets sont conservés: %+v", p.Config)
	}
	up, err := e.svc.Update(e.ctx, u, "192.0.2.1", p.ID, Input{Name: "pont2", Type: "autre-ignoré", IdentityID: id, Config: map[string]any{"url": "https://wg2"}})
	if err != nil || up.Type != "fakebastion" || !up.HasSecret {
		t.Fatalf("modification (type et jeton conservés): %+v %v", up, err)
	}
	list, err := e.svc.List(e.ctx)
	if err != nil || len(list) != 2 { // le provisioner du jeu d'essai + celui-ci
		t.Fatalf("liste: %d %v", len(list), err)
	}
	if err := e.svc.Delete(e.ctx, u, "192.0.2.1", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(e.ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("après suppression: %v", err)
	}
	rows, _ := e.st.DB.Query(`SELECT action, diff FROM audit_events WHERE action LIKE 'provisioner.%'`)
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var a, d string
		_ = rows.Scan(&a, &d)
		seen[a] = true
		if strings.Contains(d, "jeton-super-secret") || strings.Contains(d, "ne-doit-pas-etre-garde") {
			t.Fatalf("secret dans l'audit de %s: %s", a, d)
		}
	}
	for _, a := range []string{"provisioner.create", "provisioner.update", "provisioner.delete"} {
		if !seen[a] {
			t.Errorf("événement %s manquant", a)
		}
	}
}

func TestTestSourceAndPinHostKey(t *testing.T) {
	e, u := adminEnv(t)
	id := ident(t, e)
	e.ms = []Machine{m("1", "a", "G"), m("2", "b", "G"), m("3", "c", "")}
	p, err := e.svc.Create(e.ctx, u, "", Input{Name: "pont", Type: "fakebastion", IdentityID: id, Config: map[string]any{"url": "https://wg"}})
	if err != nil {
		t.Fatal(err)
	}
	e.svc.ScanHostKey = func(context.Context, string, int) (string, string, error) {
		return "SHA256:presented", "ssh-ed25519", nil
	}

	res, err := e.svc.TestSource(e.ctx, p.ID)
	if err != nil || res.Machines != 3 || res.Groups != 1 || res.SSH == nil || res.SSH.Fingerprint != "SHA256:presented" || res.SSH.PinnedFingerprint != "" || res.SSH.Host != "bastion.example.com" {
		t.Fatalf("test de la source: %+v %v", res, err)
	}
	var ie *InputError
	if _, err := e.svc.PinHostKey(e.ctx, u, "", p.ID, "SHA256:autre"); !errors.As(err, &ie) || ie.Code != "fingerprint_changed" {
		t.Fatalf("empreinte divergente: %v", err)
	}
	if _, err := e.svc.PinHostKey(e.ctx, u, "", p.ID, ""); !errors.As(err, &ie) || ie.Code != "fingerprint_changed" {
		t.Fatalf("empreinte vide: %v", err)
	}
	pinned, err := e.svc.PinHostKey(e.ctx, u, "192.0.2.1", p.ID, "SHA256:presented")
	if err != nil || Str(pinned.Config, "host_key_fingerprint") != "SHA256:presented" {
		t.Fatalf("épinglage: %+v %v", pinned, err)
	}
	if res, _ = e.svc.TestSource(e.ctx, p.ID); res.SSH.PinnedFingerprint != "SHA256:presented" {
		t.Fatalf("l'empreinte épinglée doit être rapportée: %+v", res.SSH)
	}
	e.svc.ScanHostKey = func(context.Context, string, int) (string, string, error) {
		return "", "", errors.New("délai dépassé")
	}
	if _, err := e.svc.PinHostKey(e.ctx, u, "", p.ID, "SHA256:presented"); !errors.As(err, &ie) || ie.Code != "unreachable" {
		t.Fatalf("hôte injoignable: %v", err)
	}
	if res, err = e.svc.TestSource(e.ctx, p.ID); err != nil || res.SSH.Error == "" {
		t.Fatalf("injoignable rapporté dans le résultat: %+v %v", res, err)
	}
	// un connecteur sans point d'entrée unique refuse l'épinglage
	if _, err := e.svc.PinHostKey(e.ctx, u, "", e.p.ID, "x"); !errors.As(err, &ie) || ie.Code != "no_bastion" {
		t.Fatalf("pas un bastion: %v", err)
	}
}
