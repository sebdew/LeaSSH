package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/leash-dev/leash/internal/store"
)

// configRoutes: lecture pour tous les rôles, mutations réservées à l'admin (SPEC §9, tableau des droits).
func (s *Server) configRoutes(r chi.Router) {
	admin := Role("admin")
	approver := Role("approver")

	r.Route("/commands", func(r chi.Router) {
		r.Get("/", s.listCommands)
		r.Get("/{id}", s.getCommand)
		r.With(admin).Post("/", s.createCommand)
		r.With(admin).Post("/lint", s.lintCommand)
		r.With(admin).Post("/preview", s.previewCommand)
		r.With(admin).Put("/{id}", s.updateCommand)
		r.With(admin).Delete("/{id}", s.deleteCommand)
		r.With(admin).Post("/{id}/test", s.testCommand)
	})
	for prefix, kind := range map[string]string{"/server-groups": "server", "/command-groups": "command"} {
		list, create, update, del, members := s.groupHandlers(groupKind(kind))
		r.Route(prefix, func(r chi.Router) {
			r.Get("/", list)
			r.With(admin).Post("/", create)
			r.With(admin).Put("/{id}", update)
			r.With(admin).Delete("/{id}", del)
			r.With(admin).Put("/{id}/members", members)
		})
	}
	r.Route("/identities", func(r chi.Router) {
		r.Get("/", s.listIdentities)
		r.With(admin).Post("/", s.createIdentity)
		r.With(admin).Delete("/{id}", s.deleteIdentity)
	})
	r.Route("/servers", func(r chi.Router) {
		r.Get("/", s.listServers)
		r.Get("/{id}", s.getServer)
		r.With(admin).Post("/", s.createServer)
		r.With(admin).Put("/{id}", s.updateServer)
		r.With(admin).Delete("/{id}", s.deleteServer)
		r.With(admin).Post("/{id}/test-connection", s.testConnection)
		r.With(admin).Post("/{id}/host-key", s.pinHostKey)
	})
	r.Route("/provisioners", func(r chi.Router) {
		r.Get("/", s.listProvisioners)
		r.Get("/types", s.provisionerTypes)
		r.With(admin).Post("/", s.createProvisioner)
		r.With(admin).Put("/{id}", s.updateProvisioner)
		r.With(admin).Delete("/{id}", s.deleteProvisioner)
		r.With(admin).Post("/{id}/test", s.testProvisioner)
		r.With(admin).Post("/{id}/host-key", s.pinProvisionerKey)
		r.With(admin).Post("/{id}/preview", s.previewProvisioner)
		r.With(admin).Post("/{id}/sync", s.syncProvisioner)
	})
	r.Route("/audit-reports", func(r chi.Router) {
		r.Use(admin)
		r.Get("/", s.listAuditReports)
		r.Post("/", s.createAuditReport)
		r.Post("/verify", s.verifyAuditReport)
		r.Get("/{id}", s.getAuditReport)
		r.Get("/{id}/html", s.htmlAuditReport)
		r.Get("/{id}/json", s.jsonAuditReport)
		r.Post("/{id}/summary", s.summarizeAuditReport)
	})
	r.Route("/settings/audit-ai", func(r chi.Router) {
		r.Use(admin)
		r.Get("/", s.getAuditAI)
		r.Put("/", s.putAuditAI)
	})
	r.Route("/keys", func(r chi.Router) {
		r.Get("/", s.listKeys)
		r.With(admin).Post("/", s.createKey)
		r.With(admin).Put("/{id}", s.updateKey)
		r.With(admin).Post("/{id}/revoke", s.revokeKey)
		r.With(admin).Delete("/{id}", s.deleteKey)
	})
	r.Route("/rules", func(r chi.Router) {
		r.Get("/", s.listRules)
		r.With(admin).Post("/", s.createRule)
		r.With(admin).Put("/{id}", s.updateRule)
		r.With(admin).Delete("/{id}", s.deleteRule)
	})
	r.Get("/dashboard", s.dashboard)
	r.Get("/dashboard/insights", s.insights)
	r.Get("/simulator", s.simulate)
	r.Route("/executions", func(r chi.Router) {
		r.Get("/", s.listExecutions)
		r.Get("/stream", s.streamExecutions)
		r.Get("/export", s.exportExecutions)
		r.Get("/{id}", s.getExecution)
		r.With(approver).Post("/{id}/decision", s.decideExecution)
	})
	r.Route("/audit", func(r chi.Router) {
		r.Get("/", s.listAudit)
		r.Get("/export", s.exportAudit)
	})
	r.With(admin).Post("/integrity/verify", s.verifyIntegrity)
	r.Route("/users", func(r chi.Router) {
		r.Use(admin)
		r.Get("/", s.listUsers)
		r.Post("/", s.createUser)
		r.Put("/{id}", s.updateUser)
		r.Delete("/{id}", s.deleteUser)
	})
	r.Post("/auth/password", s.changePassword)
	r.Put("/auth/theme", s.setTheme)
	r.Route("/packs", func(r chi.Router) {
		r.Get("/", s.listPacks)
		r.Get("/export", s.exportPack)
		r.With(admin).Post("/preview", s.previewPack)
		r.With(admin).Post("/import", s.importPack)
	})
	r.Route("/settings/webhook", func(r chi.Router) {
		r.Use(admin)
		r.Get("/", s.getWebhook)
		r.Put("/", s.putWebhook)
		r.Post("/test", s.testWebhook)
		r.Get("/deliveries", s.webhookDeliveries)
	})
	r.Route("/settings/syslog", func(r chi.Router) {
		r.Use(admin)
		r.Get("/", s.getSyslog)
		r.Put("/", s.putSyslog)
		r.Post("/test", s.testSyslog)
	})
	r.Route("/settings/oidc", func(r chi.Router) {
		r.Use(admin)
		r.Get("/", s.getOIDC)
		r.Put("/", s.putOIDC)
		r.Post("/test", s.testOIDC)
	})
	r.Route("/proposals", func(r chi.Router) {
		r.With(admin).Post("/lint", s.lintProposal)
		r.Get("/", s.listProposals)
		r.Get("/{id}", s.getProposal)
		r.With(admin).Post("/{id}/approve", s.approveProposal)
		r.With(admin).Post("/{id}/reject", s.rejectProposal)
	})
}

func groupKind(k string) store.GroupKind { return store.GroupKind(k) }
