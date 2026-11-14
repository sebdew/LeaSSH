package api

import (
	"errors"
	"net/http"

	"github.com/sebdew/LeaSSH/internal/syslog"
)

// ---- export syslog (SPEC §11bis) ----

func (s *Server) syslogBody(cfg syslog.Config) map[string]any {
	return map[string]any{"config": cfg, "status": s.sysl.Status(),
		"options": map[string]any{"facilities": syslog.Facilities(), "severities": syslog.Severities(), "types": syslog.Types}}
}

func (s *Server) getSyslog(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.sysl.Current(r.Context())
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

func (s *Server) putSyslog(w http.ResponseWriter, r *http.Request) {
	var in syslog.Input
	if !decode(w, r, &in) {
		return
	}
	after, err := s.sysl.Update(r.Context(), currentUser(r), s.clientIP(r), in)
	if err != nil {
		s.fail(w, syslogErr(err))
		return
	}
	writeJSON(w, 200, s.syslogBody(after))
}

// testSyslog: message de test immédiat (hors file), avec le résultat détaillé. Chaque test est audité.
func (s *Server) testSyslog(w http.ResponseWriter, r *http.Request) {
	res, err := s.sysl.TestAudited(r.Context(), currentUser(r), s.clientIP(r))
	var ve *syslog.ValidationError
	if errors.As(err, &ve) {
		s.fail(w, bad("not_configured", ve.Msg))
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, res)
}
