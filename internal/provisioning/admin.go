package provisioning

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sebdew/LeaSSH/internal/audit"
	"github.com/sebdew/LeaSSH/internal/sshexec"
	"github.com/sebdew/LeaSSH/internal/store"
)

// InputError: configuration de provisioner refusée. Code est repris tel quel par l'API.
type InputError struct{ Code, Msg string }

func (e *InputError) Error() string { return e.Msg }

func invalid(code, msg string) error { return &InputError{Code: code, Msg: msg} }

// Input: création ou modification d'un provisioner. Secret vide à la modification = jeton conservé.
type Input struct {
	Name        string
	Type        string
	Enabled     *bool
	IntervalMin int
	IdentityID  string
	DefaultUser string
	Config      map[string]any
	Secret      string
	RuleIDs     []string
}

func (s *Service) List(ctx context.Context) ([]store.Provisioner, error) {
	return store.ListProvisioners(ctx, s.st.DB)
}

func (s *Service) Get(ctx context.Context, id string) (store.Provisioner, error) {
	return store.ProvisionerByID(ctx, s.st.DB, id)
}

// build valide l'entrée et produit le provisioner à enregistrer (config réduite aux champs connus du type).
func (s *Service) build(ctx context.Context, q store.Q, in Input, old *store.Provisioner) (store.Provisioner, error) {
	p := store.Provisioner{Name: strings.TrimSpace(in.Name), Type: in.Type, Enabled: in.Enabled == nil || *in.Enabled, IntervalMin: in.IntervalMin,
		IdentityID: in.IdentityID, DefaultUser: strings.TrimSpace(in.DefaultUser), RuleIDs: in.RuleIDs}
	if old != nil {
		p.ID, p.Type, p.SecretEnc = old.ID, old.Type, old.SecretEnc
	}
	t, err := Lookup(p.Type)
	if err != nil {
		return p, invalid("invalid_type", err.Error())
	}
	if p.Name == "" {
		return p, invalid("invalid_name", "nom requis")
	}
	if p.IntervalMin < 0 || p.IntervalMin > 1440 {
		return p, invalid("invalid_interval", "intervalle: 0 (manuel) à 1440 minutes")
	}
	if _, err := store.IdentityByID(ctx, q, p.IdentityID); err != nil {
		return p, invalid("invalid_identity", "identité SSH inconnue")
	}
	p.Config = map[string]any{}
	for _, f := range t.Fields {
		if f.Kind != FieldPassword {
			if v, ok := in.Config[f.Key]; ok {
				p.Config[f.Key] = v
			}
		}
	}
	if old != nil { // l'empreinte épinglée ne survit pas à un changement de point d'entrée
		same := true
		for _, k := range []string{"ssh_host", "ssh_port"} {
			if Str(old.Config, k) != Str(p.Config, k) || Int(old.Config, k, 0) != Int(p.Config, k, 0) {
				same = false
			}
		}
		if fp := Str(old.Config, "host_key_fingerprint"); fp != "" && same {
			p.Config["host_key_fingerprint"] = fp
		}
	}
	secret := strings.TrimSpace(in.Secret)
	if secret != "" {
		if p.SecretEnc, err = s.box.Seal([]byte(secret)); err != nil {
			return p, fmt.Errorf("chiffrement du jeton: %w", err)
		}
	} else if old != nil && len(old.SecretEnc) > 0 {
		b, err := s.box.Open(old.SecretEnc)
		if err != nil {
			return p, fmt.Errorf("déchiffrement du jeton: %w", err)
		}
		secret = string(b)
	}
	if _, err := t.New(p.Config, secret, Env{User: "x"}); err != nil { // validation de la configuration
		return p, invalid("invalid_config", err.Error())
	}
	return p, nil
}

// Create enregistre un provisioner; l'événement d'audit est écrit dans la même transaction (le jeton n'y figure jamais).
func (s *Service) Create(ctx context.Context, actor store.User, ip string, in Input) (store.Provisioner, error) {
	var out store.Provisioner
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		p, err := s.build(ctx, tx, in, nil)
		if err != nil {
			return audit.Event{}, err
		}
		if out, err = store.InsertProvisioner(ctx, tx, p); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "provisioner.create", TargetType: "provisioner", TargetID: out.ID, TargetName: out.Name, Diff: map[string]any{"after": out}}, nil
	})
	return out, err
}

func (s *Service) Update(ctx context.Context, actor store.User, ip, id string, in Input) (store.Provisioner, error) {
	var out store.Provisioner
	err := audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.ProvisionerByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		p, err := s.build(ctx, tx, in, &old)
		if err != nil {
			return audit.Event{}, err
		}
		if err := store.UpdateProvisioner(ctx, tx, p); err != nil {
			return audit.Event{}, err
		}
		p.HasSecret = len(p.SecretEnc) > 0
		out = p
		return audit.Event{Action: "provisioner.update", TargetType: "provisioner", TargetID: p.ID, TargetName: p.Name,
			Diff: map[string]any{"before": old, "after": p, "secret_changed": strings.TrimSpace(in.Secret) != ""}}, nil
	})
	return out, err
}

// Delete rend les serveurs et groupes à la gestion manuelle; rien n'est supprimé.
func (s *Service) Delete(ctx context.Context, actor store.User, ip, id string) error {
	return audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		old, err := store.ProvisionerByID(ctx, tx, id)
		if err != nil {
			return audit.Event{}, err
		}
		if err := store.DeleteProvisioner(ctx, tx, old.ID); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "provisioner.delete", TargetType: "provisioner", TargetID: old.ID, TargetName: old.Name, Diff: map[string]any{"before": old}}, nil
	})
}

// SSHProbe: clé d'hôte présentée par le point d'entrée SSH d'un bastion.
type SSHProbe struct {
	Host              string `json:"host"`
	Port              int    `json:"port"`
	PinnedFingerprint string `json:"pinned_fingerprint"`
	Fingerprint       string `json:"fingerprint,omitempty"`
	KeyType           string `json:"key_type,omitempty"`
	Error             string `json:"error,omitempty"`
}

// SourceTest: résultat d'un test de lecture de la source.
type SourceTest struct {
	Machines int       `json:"machines"`
	Groups   int       `json:"groups"`
	SSH      *SSHProbe `json:"ssh,omitempty"`
}

// TestSource lit la source (sans rien écrire) et, pour un bastion, relève la clé d'hôte présentée.
func (s *Service) TestSource(ctx context.Context, id string) (SourceTest, error) {
	p, err := store.ProvisionerByID(ctx, s.st.DB, id)
	if err != nil {
		return SourceTest{}, err
	}
	conn, err := s.Connector(ctx, p)
	if err != nil {
		return SourceTest{}, err
	}
	machines, err := conn.Discover(ctx)
	if err != nil {
		return SourceTest{}, err
	}
	groups := map[string]bool{}
	for _, m := range machines {
		if m.GroupID != "" {
			groups[m.GroupID] = true
		}
	}
	out := SourceTest{Machines: len(machines), Groups: len(groups)}
	if b, ok := conn.(Bastion); ok {
		host, port := b.Endpoint()
		probe := &SSHProbe{Host: host, Port: port, PinnedFingerprint: Str(p.Config, "host_key_fingerprint")}
		if fp, kt, err := s.ScanHostKey(ctx, host, port); err != nil {
			probe.Error = "bastion SSH injoignable: " + err.Error()
		} else {
			probe.Fingerprint, probe.KeyType = fp, kt
		}
		out.SSH = probe
	}
	return out, nil
}

// PinHostKey épingle la clé d'hôte du bastion après confirmation de l'empreinte par l'admin: l'hôte est
// rescanné et l'empreinte doit être celle que l'admin a vue.
func (s *Service) PinHostKey(ctx context.Context, actor store.User, ip, id, fingerprint string) (store.Provisioner, error) {
	p, err := store.ProvisionerByID(ctx, s.st.DB, id)
	if err != nil {
		return store.Provisioner{}, err
	}
	conn, err := s.Connector(ctx, p)
	if err != nil {
		return store.Provisioner{}, err
	}
	b, ok := conn.(Bastion)
	if !ok {
		return store.Provisioner{}, invalid("no_bastion", "ce connecteur n'a pas de point d'entrée SSH unique")
	}
	host, port := b.Endpoint()
	fp, _, err := s.ScanHostKey(ctx, host, port)
	if err != nil {
		return store.Provisioner{}, invalid("unreachable", "hôte injoignable: "+err.Error())
	}
	if fingerprint == "" || fp != fingerprint {
		return store.Provisioner{}, invalid("fingerprint_changed", "l'empreinte présentée par l'hôte ne correspond pas à celle confirmée; relancez le test")
	}
	var out store.Provisioner
	err = audit.Mutate(ctx, s.st, actor, ip, func(tx *sql.Tx) (audit.Event, error) {
		cur, err := store.ProvisionerByID(ctx, tx, p.ID)
		if err != nil {
			return audit.Event{}, err
		}
		before := Str(cur.Config, "host_key_fingerprint")
		cur.Config["host_key_fingerprint"] = fp
		if err := store.UpdateProvisioner(ctx, tx, cur); err != nil {
			return audit.Event{}, err
		}
		out = cur
		return audit.Event{Action: "provisioner.trust_host_key", TargetType: "provisioner", TargetID: cur.ID, TargetName: cur.Name,
			Diff: map[string]any{"before": map[string]any{"fingerprint": before}, "after": map[string]any{"fingerprint": fp}}}, nil
	})
	return out, err
}

// defaultScan relève la clé d'hôte présentée par host:port (remplaçable dans les tests).
func defaultScan(ctx context.Context, host string, port int) (string, string, error) {
	return sshexec.ScanHostKey(ctx, host, port)
}
