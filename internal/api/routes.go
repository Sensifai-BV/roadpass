package api

import "net/http"

func (s *Server) routes(mux *http.ServeMux) {
	viewer := guard{scope: "records:read"}
	editor := guard{role: "editor", scope: "records:write"}
	admin := guard{role: "admin"}

	// health
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)

	// session and self-service
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("POST /api/v1/auth/password/forgot", s.forgotPassword)
	mux.HandleFunc("POST /api/v1/auth/password/reset", s.resetPassword)
	mux.HandleFunc("GET /api/v1/auth/me", s.with(guard{}, s.me))
	mux.HandleFunc("PATCH /api/v1/auth/me", s.with(guard{}, s.updateMe))
	mux.HandleFunc("POST /api/v1/auth/password", s.with(guard{}, s.changePassword))
	mux.HandleFunc("GET /api/v1/auth/me/keys", s.with(guard{}, s.myKeys))
	mux.HandleFunc("POST /api/v1/auth/me/keys/rotate", s.with(guard{}, s.rotateMyKey))
	mux.HandleFunc("GET /api/v1/auth/demo-users", s.demoUsers)
	mux.HandleFunc("POST /api/v1/auth/demo-login", s.demoLogin)
	// first-run setup (no accounts yet): creates the platform manager
	mux.HandleFunc("GET /api/v1/setup", s.setupStatus)
	mux.HandleFunc("POST /api/v1/setup", s.setup)
	mux.HandleFunc("GET /api/v1/auth/invitations/{token}", s.invitationInfo)
	mux.HandleFunc("POST /api/v1/auth/invitations/accept", s.acceptInvitation)

	// public contact form
	mux.HandleFunc("GET /api/v1/contact/config", s.contactConfig)
	mux.HandleFunc("POST /api/v1/contact", s.contactSend)

	// reference data
	mux.HandleFunc("GET /api/v1/meta", s.meta)
	mux.HandleFunc("GET /api/v1/orgs", s.with(viewer, s.listOrgs))
	mux.HandleFunc("GET /api/v1/orgs/{id}", s.with(viewer, s.getOrg))
	mux.HandleFunc("GET /api/v1/sites", s.with(viewer, s.listSites))
	mux.HandleFunc("GET /api/v1/templates/{file}", s.template)

	// own organisation
	mux.HandleFunc("GET /api/v1/org", s.with(guard{}, s.ownOrg))
	mux.HandleFunc("PUT /api/v1/org", s.with(admin, s.updateOrg))
	mux.HandleFunc("PUT /api/v1/org/logo", s.with(admin, s.setOrgLogo))
	mux.HandleFunc("DELETE /api/v1/org/logo", s.with(admin, s.deleteOrgLogo))
	mux.HandleFunc("GET /api/v1/orgs/{id}/logo", s.orgLogo)
	mux.HandleFunc("POST /api/v1/org/sites", s.with(editor, s.createSite))
	mux.HandleFunc("PUT /api/v1/org/sites/{id}", s.with(editor, s.updateSite))
	mux.HandleFunc("POST /api/v1/org/sites/{id}/archive", s.with(editor, s.archiveSite))
	mux.HandleFunc("GET /api/v1/org/setup", s.with(guard{}, s.orgSetup))
	mux.HandleFunc("PUT /api/v1/org/setup", s.with(admin, s.dismissSetup))
	mux.HandleFunc("PUT /api/v1/org/activities", s.with(admin, s.setActivities))
	mux.HandleFunc("GET /api/v1/org/members", s.with(guard{}, s.members))
	mux.HandleFunc("PUT /api/v1/org/members/{id}", s.with(admin, s.updateMember))
	mux.HandleFunc("DELETE /api/v1/org/members/{id}", s.with(admin, s.removeMember))
	mux.HandleFunc("GET /api/v1/org/invitations", s.with(admin, s.listInvitations))
	mux.HandleFunc("POST /api/v1/org/invitations", s.with(admin, s.invite))
	mux.HandleFunc("DELETE /api/v1/org/invitations/{id}", s.with(admin, s.revokeInvitation))
	mux.HandleFunc("GET /api/v1/org/api-keys", s.with(admin, s.listKeys))
	mux.HandleFunc("POST /api/v1/org/api-keys", s.with(admin, s.createKey))
	mux.HandleFunc("DELETE /api/v1/org/api-keys/{id}", s.with(admin, s.revokeKey))
	mux.HandleFunc("GET /api/v1/org/signing-keys", s.with(guard{}, s.signingKeys))

	// records
	mux.HandleFunc("GET /api/v1/records", s.with(viewer, s.listRecords))
	mux.HandleFunc("POST /api/v1/records", s.with(editor, s.submitRecords))
	mux.HandleFunc("GET /api/v1/records/{id}", s.with(viewer, s.getRecord))
	mux.HandleFunc("GET /api/v1/records/{id}/versions", s.with(viewer, s.recordVersions))
	mux.HandleFunc("GET /api/v1/records/{id}/proof", s.with(viewer, s.recordProof))
	mux.HandleFunc("POST /api/v1/shipments/{id}/receipt", s.with(editor, s.receipt))
	mux.HandleFunc("POST /api/v1/records/{id}/void", s.with(editor, s.voidRecord))
	mux.HandleFunc("GET /api/v1/activity", s.with(viewer, s.activity))
	mux.HandleFunc("GET /api/v1/work", s.with(viewer, s.work))
	mux.HandleFunc("GET /api/v1/chain/status", s.with(viewer, s.chainStatus))
	mux.HandleFunc("GET /api/v1/road-sections/open", s.with(viewer, s.openSections))

	// passports
	mux.HandleFunc("GET /api/v1/passports/{id}", s.with(guard{optional: true, scope: "records:read"}, s.getPassport))
	mux.HandleFunc("GET /api/v1/passports/{id}/share", s.with(viewer, s.getShare))
	mux.HandleFunc("POST /api/v1/passports/{id}/share", s.with(guard{role: "editor"}, s.createShare))
	mux.HandleFunc("POST /api/v1/passports/{id}/share/rotate", s.with(guard{role: "editor"}, s.rotateShare))
	mux.HandleFunc("DELETE /api/v1/passports/{id}/share", s.with(guard{role: "editor"}, s.stopShare))
	// The public id is a random share token, never the passport id.
	mux.HandleFunc("GET /api/v1/public/passports/{id}", s.publicPassport)

	// CSV
	mux.HandleFunc("POST /api/v1/imports", s.with(editor, s.importCSV))
	mux.HandleFunc("GET /api/v1/imports", s.with(viewer, s.listImports))
	mux.HandleFunc("GET /api/v1/imports/{id}", s.with(viewer, s.getImport))

	// plant control logs
	mux.HandleFunc("POST /api/v1/telemetry", s.with(editor, s.pushTelemetry))

	// data checks
	mux.HandleFunc("GET /api/v1/findings", s.with(viewer, s.listFindings))
	mux.HandleFunc("GET /api/v1/findings/summary", s.with(viewer, s.findingSummary))
	mux.HandleFunc("POST /api/v1/findings/{id}/resolve", s.with(guard{role: "editor"}, s.resolveFinding))

	// notifications (everyone, their own) and alerts (members see; admins configure)
	mux.HandleFunc("GET /api/v1/notifications", s.with(guard{}, s.notifications))
	mux.HandleFunc("POST /api/v1/notifications/read", s.with(guard{}, s.readNotifications))
	mux.HandleFunc("GET /api/v1/notifications/prefs", s.with(guard{}, s.notificationPrefs))
	mux.HandleFunc("PUT /api/v1/notifications/prefs", s.with(guard{}, s.setNotificationPrefs))
	mux.HandleFunc("GET /api/v1/notifications/push/key", s.with(guard{}, s.pushKey))
	mux.HandleFunc("GET /api/v1/notifications/push", s.with(guard{}, s.pushDevices))
	mux.HandleFunc("POST /api/v1/notifications/push", s.with(guard{}, s.pushSubscribe))
	mux.HandleFunc("POST /api/v1/notifications/push/remove", s.with(guard{}, s.pushRemove))
	mux.HandleFunc("POST /api/v1/notifications/test", s.with(guard{}, s.testNotification))
	mux.HandleFunc("GET /api/v1/alerts/active", s.with(guard{}, s.activeAlerts))
	mux.HandleFunc("POST /api/v1/alerts/action", s.with(guard{role: "editor"}, s.alertAction))
	mux.HandleFunc("GET /api/v1/alerts/history", s.with(guard{}, s.alertHistory))
	mux.HandleFunc("GET /api/v1/alerts/stats", s.with(guard{}, s.alertStats))
	mux.HandleFunc("GET /api/v1/alerts/rules", s.with(guard{}, s.alertRules))
	mux.HandleFunc("POST /api/v1/alerts/rules", s.with(admin, s.saveRule))
	mux.HandleFunc("POST /api/v1/alerts/rules/preview", s.with(admin, s.previewRule))
	mux.HandleFunc("PUT /api/v1/alerts/rules/{id}", s.with(admin, s.saveRule))
	mux.HandleFunc("DELETE /api/v1/alerts/rules/{id}", s.with(admin, s.deleteRule))
	mux.HandleFunc("GET /api/v1/alerts/channels", s.with(admin, s.alertChannels))
	mux.HandleFunc("POST /api/v1/alerts/channels", s.with(admin, s.addChannel))
	mux.HandleFunc("DELETE /api/v1/alerts/channels/{id}", s.with(admin, s.deleteChannel))
	mux.HandleFunc("POST /api/v1/alerts/channels/{id}/verify", s.with(admin, s.sendVerification))
	mux.HandleFunc("POST /api/v1/alerts/channels/{id}/verify/confirm", s.with(admin, s.confirmVerification))
	mux.HandleFunc("POST /api/v1/alerts/channels/{id}/test", s.with(admin, s.testChannel))
	mux.HandleFunc("GET /api/v1/alerts/maintenance", s.with(guard{}, s.maintenance))
	mux.HandleFunc("POST /api/v1/alerts/maintenance", s.with(admin, s.addMaintenance))
	mux.HandleFunc("DELETE /api/v1/alerts/maintenance/{id}", s.with(admin, s.deleteMaintenance))

	// dashboards (designed by admins for their own team) and the widget query engine
	// downloads: any page as PDF, Word, Excel or CSV
	// compliance
	mux.HandleFunc("GET /api/v1/compliance/register", s.with(viewer, s.complianceRegister))
	mux.HandleFunc("GET /api/v1/compliance/requirements/{id}/history", s.with(viewer, s.attestationHistory))
	mux.HandleFunc("POST /api/v1/compliance/attestations", s.with(guard{role: "editor"}, s.attest))
	mux.HandleFunc("GET /api/v1/compliance/limits", s.with(viewer, s.complianceLimits))
	mux.HandleFunc("POST /api/v1/compliance/limits", s.with(guard{role: "editor"}, s.saveLimit))
	mux.HandleFunc("PUT /api/v1/compliance/limits/{id}", s.with(guard{role: "editor"}, s.saveLimit))
	mux.HandleFunc("DELETE /api/v1/compliance/limits/{id}", s.with(guard{role: "editor"}, s.deleteLimit))
	mux.HandleFunc("GET /api/v1/compliance/status", s.with(viewer, s.limitsStatus))
	mux.HandleFunc("POST /api/v1/compliance/calibrations", s.with(guard{role: "editor"}, s.addCalibration))
	mux.HandleFunc("DELETE /api/v1/compliance/calibrations/{id}", s.with(guard{role: "editor"}, s.deleteCalibration))
	mux.HandleFunc("GET /api/v1/compliance/report", s.with(viewer, s.reportState))
	mux.HandleFunc("POST /api/v1/compliance/report/sign", s.with(guard{role: "editor"}, s.signReport))
	mux.HandleFunc("GET /api/v1/compliance/schedule", s.with(admin, s.getSchedule))
	mux.HandleFunc("PUT /api/v1/compliance/schedule", s.with(admin, s.saveSchedule))
	mux.HandleFunc("DELETE /api/v1/compliance/schedule", s.with(admin, s.deleteSchedule))
	mux.HandleFunc("GET /api/v1/compliance/privacy", s.with(admin, s.privacyOverview))
	mux.HandleFunc("GET /api/v1/compliance/privacy/{id}/export", s.with(admin, s.exportMember))
	mux.HandleFunc("POST /api/v1/compliance/privacy/{id}/erase", s.with(admin, s.eraseMember))
	mux.HandleFunc("GET /api/v1/auth/me/export", s.with(guard{}, s.exportMyData))
	mux.HandleFunc("GET /api/v1/reports/{kind}", s.with(viewer, s.downloadReport))

	mux.HandleFunc("GET /api/v1/me/layout", s.with(guard{}, s.myLayout))
	mux.HandleFunc("PUT /api/v1/me/layout", s.with(guard{}, s.saveMyLayout))
	mux.HandleFunc("DELETE /api/v1/me/layout", s.with(guard{}, s.resetMyLayout))
	mux.HandleFunc("POST /api/v1/me/layout/publish", s.with(admin, s.publishLayout))
	mux.HandleFunc("GET /api/v1/dashboards", s.with(guard{}, s.dashboards))
	mux.HandleFunc("GET /api/v1/dashboards/{id}", s.with(guard{}, s.dashboard))
	mux.HandleFunc("POST /api/v1/dashboards", s.with(admin, s.createDashboard))
	mux.HandleFunc("PUT /api/v1/dashboards/{id}", s.with(admin, s.updateDashboard))
	mux.HandleFunc("DELETE /api/v1/dashboards/{id}", s.with(admin, s.deleteDashboard))
	mux.HandleFunc("GET /api/v1/explore/catalog", s.with(viewer, s.exploreCatalog))
	mux.HandleFunc("POST /api/v1/explore/query", s.with(viewer, s.exploreQuery))

	// analytics
	mux.HandleFunc("GET /api/v1/analytics/overview", s.with(viewer, s.anOverview))
	mux.HandleFunc("GET /api/v1/analytics/material", s.with(viewer, s.anMaterial))
	mux.HandleFunc("GET /api/v1/analytics/impact", s.with(viewer, s.anImpact))
	mux.HandleFunc("GET /api/v1/analytics/flows", s.with(viewer, s.anFlows))
	mux.HandleFunc("GET /api/v1/analytics/funnel", s.with(viewer, s.anFunnel))
	mux.HandleFunc("GET /api/v1/analytics/map", s.with(viewer, s.anMap))
	mux.HandleFunc("GET /api/v1/analytics/breakdown", s.with(viewer, s.anBreakdown))
	mux.HandleFunc("GET /api/v1/analytics/lab", s.with(viewer, s.anLab))
	mux.HandleFunc("GET /api/v1/analytics/dosing", s.with(viewer, s.anDosing))
	mux.HandleFunc("GET /api/v1/analytics/lots/{id}/cycles", s.with(viewer, s.anLotCycles))
	mux.HandleFunc("GET /api/v1/analytics/lines", s.with(viewer, s.anLines))
	mux.HandleFunc("GET /api/v1/analytics/extruder", s.with(viewer, s.anExtruder))

	// public explorer (organisations opt in: org setting "public ledger")
	mux.HandleFunc("GET /api/v1/public/explorer/summary", s.explorerSummary)
	mux.HandleFunc("GET /api/v1/public/explorer/orgs", s.explorerOrgs)
	mux.HandleFunc("GET /api/v1/public/explorer/orgs/{id}", s.explorerOrg)
	mux.HandleFunc("GET /api/v1/public/explorer/blocks", s.explorerBlocks)
	mux.HandleFunc("GET /api/v1/public/explorer/blocks/{n}", s.explorerBlock)
	mux.HandleFunc("GET /api/v1/public/explorer/seals/{id}", s.explorerSeal)
	mux.HandleFunc("GET /api/v1/public/explorer/search", s.explorerSearch)
	mux.HandleFunc("PUT /api/v1/org/public-ledger", s.with(admin, s.setPublicLedger))

	// ledger explorer
	mux.HandleFunc("GET /api/v1/ledger/summary", s.with(viewer, s.ledgerSummary))
	mux.HandleFunc("GET /api/v1/ledger/seals", s.with(viewer, s.ledgerSeals))
	mux.HandleFunc("GET /api/v1/ledger/seals/{id}", s.with(viewer, s.ledgerSeal))
	mux.HandleFunc("GET /api/v1/ledger/orgs", s.with(viewer, s.ledgerOrgs))
	mux.HandleFunc("GET /api/v1/ledger/orgs/{id}/seals", s.with(viewer, s.ledgerOrgSeals))
	mux.HandleFunc("GET /api/v1/ledger/blocks", s.with(guard{}, s.ledgerBlocks))
