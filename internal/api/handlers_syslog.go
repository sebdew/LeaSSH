package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/leash-dev/leash/internal/audit"
	"github.com/leash-dev/leash/internal/syslog"
)

// ---- export syslog (SPEC §11bis) ----

func (s *Server) syslogBody(cfg syslog.Config) map[string]any {
	return map[string]any{"config": cfg, "status": s.sysl.Status(),
		"options": map[string]any{"facilities": syslog.Facilities(), "severities": syslog.Severities(), "types": syslog.Types}}
}

func (s *Server) getSyslog(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.sysl.Config(r.Context(), s.st.DB)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, s.syslogBody(cfg))
}

func syslogErr(err error) error {
	var ve *syslog.ValidationError
	if errors.As(err, &ve) {
		return bad("invalid_syslog", ve.Msg)
	}
	return err
}

// diffView: ce qui est écrit dans l'audit (jamais la clé privée; les PEM publics sont réduits à un indicateur).
func diffView(c syslog.Config) map[string]any {
	return map[string]any{"enabled": c.Enabled, "protocol": c.Protocol, "host": c.Host, "port": c.Port, "message_format": c.MessageFormat, "framing": c.Framing,
		"facility": c.Facility, "app_name": c.AppName, "hostname": c.Hostname, "min_severity": c.MinSeverity, "types": c.Types,
		"tls_ca": c.TLSCAPEM != "", "tls_server_name": c.TLSServerName, "tls_skip_verify": c.TLSSkipVerify,
		"tls_client_cert": c.TLSClientCertPEM != "", "tls_client_auth": c.HasClientKey}
}

func (s *Server) putSyslog(w http.ResponseWriter, r *http.Request) {
	var in syslog.Input
	if !decode(w, r, &in) {
		return
	}
	u := currentUser(r)
	var after syslog.Config
	// transaction propre à ce handler: la configuration n'est rechargée qu'après le commit
	err := s.st.Tx(r.Context(), func(tx *sql.Tx) error {
		before, a, err := s.sysl.Save(r.Context(), tx, in)
		if err != nil {
			return syslogErr(err)
		}
		after = a
		return audit.Record(r.Context(), tx, audit.Event{ActorType: "user", ActorID: u.ID, ActorName: u.Username, IP: s.clientIP(r), Action: "syslog.update",
			TargetType: "syslog", TargetName: a.Host,
			Diff: map[string]any{"before": diffView(before), "after": diffView(a), "tls_client_key_changed": in.TLSClientKeyPEM != "" || in.ClearClientKey}})
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.sysl.Reload(r.Context()); err != nil {
		s.log.Error("rechargement syslog", "err", err)
	}
	writeJSON(w, 200, s.syslogBody(after))
}

// testSyslog: message de test immédiat (hors file), avec le résultat détaillé. Chaque test est audité.
func (s *Server) testSyslog(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	err := s.sysl.SendTest(r.Context())
	if err != nil {
		var ve *syslog.ValidationError
		if errors.As(err, &ve) {
			s.fail(w, bad("not_configured", ve.Msg))
			return
		}
	}
	res := map[string]any{"ok": err == nil}
	if err != nil {
		res["error"] = err.Error()
	}
	_ = audit.RecordStandalone(r.Context(), s.st, audit.Event{ActorType: "user", ActorID: u.ID, ActorName: u.Username, IP: s.clientIP(r), Action: "syslog.test",
		TargetType: "syslog", Diff: res})
	writeJSON(w, 200, res)
}
