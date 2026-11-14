package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/sebdew/LeaSSH/internal/settings"
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

func (s *Server) getGeneral(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.generalOf(r, s.set.Get()))
}

func (s *Server) putGeneral(w http.ResponseWriter, r *http.Request) {
	var in generalInput
	if !decode(w, r, &in) {
		return
	}
	nv, err := s.set.Update(r.Context(), currentUser(r), s.clientIP(r), settings.UpdateInput{
		Values: settings.Values{BaseURL: in.BaseURL, TrustedProxies: in.TrustedProxies, SessionTTLHours: in.SessionTTLHours, ApprovalTTLMin: in.ApprovalTTLMin,
			LogRetentionDays: in.LogRetentionDays, LogLevel: in.LogLevel},
		MetricsToken: in.MetricsToken, ClearMetricsToken: in.ClearMetricsToken})
	var inv *settings.InvalidError
	switch {
	case errors.As(err, &inv):
		s.fail(w, bad("invalid_settings", inv.Msg))
	case err != nil:
		s.fail(w, err)
	default:
		writeJSON(w, 200, s.generalOf(r, nv))
	}
}
