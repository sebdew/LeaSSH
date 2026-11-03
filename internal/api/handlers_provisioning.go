package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/leash-dev/leash/internal/audit"
	"github.com/leash-dev/leash/internal/provisioning"
	"github.com/leash-dev/leash/internal/sshexec"
	"github.com/leash-dev/leash/internal/store"
)

type provisionerInput struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Enabled     *bool          `json:"enabled"`
	IntervalMin int            `json:"interval_min"`
	IdentityID  string         `json:"identity_id"`
	DefaultUser string         `json:"default_user"`
	Config      map[string]any `json:"config"`
	Secret      string         `json:"secret"` // vide à la modification = jeton conservé
	RuleIDs     []string       `json:"rule_ids"`
}

func (s *Server) provisionerTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"items": provisioning.Types()})
}

func (s *Server) listProvisioners(w http.ResponseWriter, r *http.Request) {
	ps, err := store.ListProvisioners(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": ps})
}

// build valide l'entrée et produit le provisioner à enregistrer (config réduite aux champs connus du type).
func (s *Server) buildProvisioner(ctx context.Context, q store.Q, in provisionerInput, old *store.Provisioner) (store.Provisioner, error) {
	p := store.Provisioner{Name: strings.TrimSpace(in.Name), Type: in.Type, Enabled: in.Enabled == nil || *in.Enabled, IntervalMin: in.IntervalMin,
		IdentityID: in.IdentityID, DefaultUser: strings.TrimSpace(in.DefaultUser), RuleIDs: in.RuleIDs}
	if old != nil {
		p.ID, p.Type, p.SecretEnc = old.ID, old.Type, old.SecretEnc
	}
	t, err := provisioning.Lookup(p.Type)
	if err != nil {
		return p, bad("invalid_type", err.Error())
	}
	if p.Name == "" {
		return p, bad("invalid_name", "nom requis")
	}
	if p.IntervalMin < 0 || p.IntervalMin > 1440 {
		return p, bad("invalid_interval", "intervalle: 0 (manuel) à 1440 minutes")
	}
	if _, err := store.IdentityByID(ctx, q, p.IdentityID); err != nil {
		return p, bad("invalid_identity", "identité SSH inconnue")
	}
	p.Config = map[string]any{}
	for _, f := range t.Fields {
		if f.Kind != provisioning.FieldPassword {
			if v, ok := in.Config[f.Key]; ok {
				p.Config[f.Key] = v
			}
		}
	}
	if old != nil { // l'empreinte épinglée ne survit pas à un changement de point d'entrée
		same := true
		for _, k := range []string{"ssh_host", "ssh_port"} {
			if provisioning.Str(old.Config, k) != provisioning.Str(p.Config, k) || provisioning.Int(old.Config, k, 0) != provisioning.Int(p.Config, k, 0) {
				same = false
			}
		}
		if fp := provisioning.Str(old.Config, "host_key_fingerprint"); fp != "" && same {
			p.Config["host_key_fingerprint"] = fp
		}
	}
	secret := strings.TrimSpace(in.Secret)
	if secret != "" {
		if p.SecretEnc, err = s.box.Seal([]byte(secret)); err != nil {
			return p, err
		}
	} else if old != nil && len(old.SecretEnc) > 0 {
		b, err := s.box.Open(old.SecretEnc)
		if err != nil {
			return p, err
		}
		secret = string(b)
	}
	if _, err := t.New(p.Config, secret, provisioning.Env{User: "x"}); err != nil { // validation de la configuration
		return p, bad("invalid_config", err.Error())
	}
	return p, nil
}

func (s *Server) createProvisioner(w http.ResponseWriter, r *http.Request) {
	var in provisionerInput
	if !decode(w, r, &in) {
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		p, err := s.buildProvisioner(ctx, tx, in, nil)
		if err != nil {
			return outcome{}, err
		}
		if p, err = store.InsertProvisioner(ctx, tx, p); err != nil {
			return outcome{}, err
		}
		return outcome{201, p, &audit.Event{Action: "provisioner.create", TargetType: "provisioner", TargetID: p.ID, TargetName: p.Name, Diff: map[string]any{"after": p}}}, nil
	})
}

func (s *Server) updateProvisioner(w http.ResponseWriter, r *http.Request) {
	var in provisionerInput
	if !decode(w, r, &in) {
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, err := store.ProvisionerByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		p, err := s.buildProvisioner(ctx, tx, in, &old)
		if err != nil {
			return outcome{}, err
		}
		if err := store.UpdateProvisioner(ctx, tx, p); err != nil {
			return outcome{}, err
		}
		p.HasSecret = len(p.SecretEnc) > 0
		return outcome{200, p, &audit.Event{Action: "provisioner.update", TargetType: "provisioner", TargetID: p.ID, TargetName: p.Name, Diff: map[string]any{"before": old, "after": p, "secret_changed": strings.TrimSpace(in.Secret) != ""}}}, nil
	})
}

// deleteProvisioner rend les serveurs et groupes à la gestion manuelle; rien n'est supprimé.
func (s *Server) deleteProvisioner(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		old, err := store.ProvisionerByID(ctx, tx, id(r))
		if err != nil {
			return outcome{}, err
		}
		if err := store.DeleteProvisioner(ctx, tx, old.ID); err != nil {
			return outcome{}, err
		}
		return outcome{Event: &audit.Event{Action: "provisioner.delete", TargetType: "provisioner", TargetID: old.ID, TargetName: old.Name, Diff: map[string]any{"before": old}}}, nil
	})
}

func provisionerError(err error, report provisioning.Report) error {
	switch {
	case errors.Is(err, provisioning.ErrBlocked):
		return &apiError{Status: 409, Code: "sync_blocked", Message: err.Error(), Details: report}
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrInUse):
		return err
	default:
		return &apiError{Status: 502, Code: "source_error", Message: err.Error()}
	}
}

func (s *Server) loadProvisioner(r *http.Request) (store.Provisioner, error) {
	return store.ProvisionerByID(r.Context(), s.st.DB, id(r))
}

// testProvisioner lit la source (sans rien écrire) et, pour un bastion, relève la clé d'hôte présentée.
func (s *Server) testProvisioner(w http.ResponseWriter, r *http.Request) {
	p, err := s.loadProvisioner(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	conn, err := s.prov.Connector(r.Context(), p)
	if err != nil {
		s.fail(w, provisionerError(err, provisioning.Report{}))
		return
	}
	machines, err := conn.Discover(r.Context())
	if err != nil {
		s.fail(w, provisionerError(err, provisioning.Report{}))
		return
	}
	groups := map[string]bool{}
	for _, m := range machines {
		if m.GroupID != "" {
			groups[m.GroupID] = true
		}
	}
	out := map[string]any{"machines": len(machines), "groups": len(groups)}
	if b, ok := conn.(provisioning.Bastion); ok {
		host, port := b.Endpoint()
		ssh := map[string]any{"host": host, "port": port, "pinned_fingerprint": provisioning.Str(p.Config, "host_key_fingerprint")}
		if fp, kt, err := sshexec.ScanHostKey(r.Context(), host, port); err != nil {
			ssh["error"] = "bastion SSH injoignable: " + err.Error()
		} else {
			ssh["fingerprint"], ssh["key_type"] = fp, kt
		}
		out["ssh"] = ssh
	}
	writeJSON(w, 200, out)
}

// pinProvisionerKey épingle la clé d'hôte du bastion après confirmation de l'empreinte par l'admin.
func (s *Server) pinProvisionerKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Fingerprint string `json:"fingerprint"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := s.loadProvisioner(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	conn, err := s.prov.Connector(r.Context(), p)
	if err != nil {
		s.fail(w, provisionerError(err, provisioning.Report{}))
		return
	}
	b, ok := conn.(provisioning.Bastion)
	if !ok {
		s.fail(w, bad("no_bastion", "ce connecteur n'a pas de point d'entrée SSH unique"))
		return
	}
	host, port := b.Endpoint()
	fp, _, err := sshexec.ScanHostKey(r.Context(), host, port)
	if err != nil {
		s.fail(w, bad("unreachable", "hôte injoignable: "+err.Error()))
		return
	}
	if in.Fingerprint == "" || fp != in.Fingerprint {
		s.fail(w, bad("fingerprint_changed", "l'empreinte présentée par l'hôte ne correspond pas à celle confirmée; relancez le test"))
		return
	}
	s.mutate(w, r, func(ctx context.Context, tx *sql.Tx, u store.User) (outcome, error) {
		cur, err := store.ProvisionerByID(ctx, tx, p.ID)
		if err != nil {
			return outcome{}, err
		}
		before := provisioning.Str(cur.Config, "host_key_fingerprint")
		cur.Config["host_key_fingerprint"] = fp
		if err := store.UpdateProvisioner(ctx, tx, cur); err != nil {
			return outcome{}, err
		}
		return outcome{200, cur, &audit.Event{Action: "provisioner.trust_host_key", TargetType: "provisioner", TargetID: cur.ID, TargetName: cur.Name,
			Diff: map[string]any{"before": map[string]any{"fingerprint": before}, "after": map[string]any{"fingerprint": fp}}}}, nil
	})
}

func (s *Server) previewProvisioner(w http.ResponseWriter, r *http.Request) {
	rep, err := s.prov.Preview(r.Context(), id(r))
	if err != nil && !errors.Is(err, provisioning.ErrBlocked) {
		s.fail(w, provisionerError(err, rep))
		return
	}
	writeJSON(w, 200, rep) // un aperçu bloqué se montre quand même (rep.blocked renseigné)
}

func (s *Server) syncProvisioner(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Force bool `json:"force"`
	}
	if r.ContentLength > 0 && !decode(w, r, &in) {
		return
	}
	u := currentUser(r)
	rep, err := s.prov.Sync(r.Context(), id(r), in.Force, provisioning.Actor{Type: "user", ID: u.ID, Name: u.Username, IP: s.clientIP(r)})
	if err != nil {
		s.fail(w, provisionerError(err, rep))
		return
	}
	writeJSON(w, 200, rep)
}
