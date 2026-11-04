package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/settings"
)

// ---- Réglages généraux (Réglages → Général): stockés en base, appliqués à chaud ----

type generalView struct {
	BaseURL          string `json:"base_url"`
	TrustedProxies   string `json:"trusted_proxies"`
	SessionTTLHours  int    `json:"session_ttl_hours"`
	ApprovalTTLMin   int    `json:"approval_ttl_minutes"`
	LogRetentionDays int    `json:"log_retention_days"`
	LogLevel         string `json:"log_level"`
	HasMetricsToken  bool   `json:"has_metrics_token"`
	DetectedURL      string `json:"detected_url"` // URL telle que vue par le serveur pour cette requête (aide à la saisie)
}

type generalInput struct {
	generalView
	MetricsToken      string `json:"metrics_token"`
	ClearMetricsToken bool   `json:"clear_metrics_token"`
}

func (s *Server) generalOf(r *http.Request, v settings.Values) generalView {
	// URL détectée sans tenir compte de l'URL fixe enregistrée.
	scheme := "http"
	if r.TLS != nil || (s.fromTrustedProxy(r) && r.Header.Get("X-Forwarded-Proto") == "https") {
		scheme = "https"
	}
	host := r.Host
	if s.fromTrustedProxy(r) {
		if h := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); validHost(h) {
			host = h
		}
	}
	return generalView{BaseURL: v.BaseURL, TrustedProxies: v.TrustedProxies, SessionTTLHours: v.SessionTTLHours, ApprovalTTLMin: v.ApprovalTTLMin,
		LogRetentionDays: v.LogRetentionDays, LogLevel: v.LogLevel, HasMetricsToken: v.MetricsTokenHash != "", DetectedURL: scheme + "://" + host}
}

func (v generalView) auditView() map[string]any {
	return map[string]any{"base_url": v.BaseURL, "trusted_proxies": v.TrustedProxies, "session_ttl_hours": v.SessionTTLHours, "approval_ttl_minutes": v.ApprovalTTLMin,
		"log_retention_days": v.LogRetentionDays, "log_level": v.LogLevel, "has_metrics_token": v.HasMetricsToken}
}

func (s *Server) getGeneral(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.generalOf(r, s.set.Get()))
}

func (s *Server) putGeneral(w http.ResponseWriter, r *http.Request) {
	var in generalInput
	if !decode(w, r, &in) {
		return
	}
	nv := settings.Values{BaseURL: in.BaseURL, TrustedProxies: in.TrustedProxies, SessionTTLHours: in.SessionTTLHours, ApprovalTTLMin: in.ApprovalTTLMin,
		LogRetentionDays: in.LogRetentionDays, LogLevel: in.LogLevel}
	if err := nv.Validate(); err != nil {
		s.fail(w, bad("invalid_settings", err.Error()))
		return
	}
	if len(in.MetricsToken) > 256 {
		s.fail(w, bad("invalid_settings", "jeton trop long"))
		return
	}
	u := currentUser(r)
	var old settings.Values
	err := s.st.Tx(r.Context(), func(tx *sql.Tx) error {
		var err error
		if old, err = settings.Load(r.Context(), tx); err != nil {
			return err
		}
		nv.MetricsTokenHash = old.MetricsTokenHash
		switch {
		case in.MetricsToken != "":
			nv.MetricsTokenHash = settings.HashToken(in.MetricsToken)
		case in.ClearMetricsToken:
			nv.MetricsTokenHash = ""
		}
		if err := settings.Save(r.Context(), tx, nv); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Event{ActorType: "user", ActorID: u.ID, ActorName: u.Username, IP: s.clientIP(r), Action: "settings.update",
			TargetType: "settings", TargetName: "general",
			Diff: map[string]any{"before": s.generalOf(r, old).auditView(), "after": s.generalOf(r, nv).auditView(), "credential_changed": in.MetricsToken != "" || in.ClearMetricsToken}})
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.set.Apply(nv)
	writeJSON(w, 200, s.generalOf(r, nv))
}
