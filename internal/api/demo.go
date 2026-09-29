package api

import (
	"fmt"
	"net/http"
	"time"
)

// The public demo workspace (internal/demo) is open to anyone holding its shared
// password, so it limits what a visitor can do: no outbound messages, no account
// or key changes, and a daily budget for writes. The workspace is rebuilt every
// day, which also resets the budget.

// DemoSeedAddr is the RemoteAddr of the builder's in-process requests. The HTTP
// server sets RemoteAddr from the TCP peer, so a client cannot claim it.
const DemoSeedAddr = "roadpass-demo-builder"

// SessionCookie is the name of the session cookie.
const SessionCookie = cookieName

// demoDenied lists the routes (by mux pattern) that stay closed in the demo
// workspace: they send mail or webhooks, change who can sign in, or issue
// credentials.
var demoDenied = map[string]string{
	"PUT /api/v1/org":                                  "Editing the company profile is disabled in the demo workspace.",
	"PUT /api/v1/org/public-ledger":                    "The demo workspace's ledger data is always public.",
	"PUT /api/v1/org/logo":                             "Uploading a logo is disabled in the demo workspace.",
	"DELETE /api/v1/org/logo":                          "Removing the logo is disabled in the demo workspace.",
	"PUT /api/v1/org/activities":                       "Changing the company's activities is disabled in the demo workspace.",
	"PUT /api/v1/org/members/{id}":                     "Changing members is disabled in the demo workspace.",
	"DELETE /api/v1/org/members/{id}":                  "Removing members is disabled in the demo workspace.",
	"POST /api/v1/org/invitations":                     "Invitations send e-mail and are disabled in the demo workspace.",
	"DELETE /api/v1/org/invitations/{id}":              "Invitations are disabled in the demo workspace.",
	"POST /api/v1/org/api-keys":                        "API keys are disabled in the demo workspace.",
	"DELETE /api/v1/org/api-keys/{id}":                 "API keys are disabled in the demo workspace.",
	"POST /api/v1/auth/password":                       "The demo password is shared and cannot be changed.",
	"POST /api/v1/auth/me/keys/rotate":                 "Signing keys cannot be rotated in the demo workspace.",
	"POST /api/v1/alerts/channels":                     "Alert channels send e-mail, SMS or webhooks and are disabled in the demo workspace.",
	"DELETE /api/v1/alerts/channels/{id}":              "Alert channels are disabled in the demo workspace.",
	"POST /api/v1/alerts/channels/{id}/verify":         "Alert channels are disabled in the demo workspace.",
	"POST /api/v1/alerts/channels/{id}/verify/confirm": "Alert channels are disabled in the demo workspace.",
	"POST /api/v1/alerts/channels/{id}/test":           "Alert channels are disabled in the demo workspace.",
	"PUT /api/v1/compliance/schedule":                  "Scheduled e-mail reports are disabled in the demo workspace.",
	"DELETE /api/v1/compliance/schedule":               "Scheduled e-mail reports are disabled in the demo workspace.",
	"POST /api/v1/compliance/privacy/{id}/erase":       "Erasing members is disabled in the demo workspace.",
	"POST /api/v1/notifications/push":                  "Browser push is disabled in the demo workspace.",
	"POST /api/v1/notifications/push/remove":           "Browser push is disabled in the demo workspace.",
}

// demoDay names the budget period: it starts at the daily rebuild.
func (s *Server) demoDay() string {
	return time.Now().UTC().Add(-time.Duration(s.Cfg.DemoResetHour) * time.Hour).Format("2006-01-02")
}

// demoGate decides whether a write in the demo workspace may go ahead; it has
// answered the request when it returns false.
func (s *Server) demoGate(w http.ResponseWriter, r *http.Request, p *Principal) bool {
	if msg, ok := demoDenied[r.Pattern]; ok {
		writeProblem(w, r, http.StatusForbidden, "Disabled in the demo", msg)
		return false
	}
	day := s.demoDay()
	if !s.demoTake(r, "demo-writes:"+p.Org.ID+":"+day, 1, s.Cfg.DemoWritesPerDay) {
		s.demoLimit(w, r, "The demo workspace has reached today's limit of changes.")
		return false
	}
	if !s.demoTake(r, "demo-ip:"+clientIP(r)+":"+day, 1, s.Cfg.DemoWritesPerIP) {
		s.demoLimit(w, r, "Too many changes from your network today.")
		return false
	}
	return true
}

// demoTake spends n from a daily budget; a Redis failure lets the request through.
func (s *Server) demoTake(r *http.Request, key string, n, limit int) bool {
	used, err := s.Cache.R.IncrBy(r.Context(), key, int64(n)).Result()
	if err != nil {
		return true
	}
	s.Cache.R.Expire(r.Context(), key, 25*time.Hour)
	if used > int64(limit) {
		s.Cache.R.DecrBy(r.Context(), key, int64(n))
		return false
	}
	return true
}

// demoRecords charges n new records to the day's budget.
func (s *Server) demoRecords(w http.ResponseWriter, r *http.Request, p *Principal, n int) bool {
	if !p.Org.Demo || r.RemoteAddr == DemoSeedAddr {
		return true
	}
	if !s.demoTake(r, "demo-records:"+p.Org.ID+":"+s.demoDay(), n, s.Cfg.DemoRecordsPerDay) {
		s.demoLimit(w, r, fmt.Sprintf("The demo workspace accepts %d new records a day, and today's are used up.", s.Cfg.DemoRecordsPerDay))
		return false
	}
	return true
}

func (s *Server) demoLimit(w http.ResponseWriter, r *http.Request, msg string) {
	writeProblem(w, r, http.StatusTooManyRequests, "Demo limit reached",
		fmt.Sprintf("%s The demo resets every day at %02d:00 UTC. To use ROADPASS without limits, ask for your own workspace.", msg, s.Cfg.DemoResetHour))
}

// demoProtected refuses platform-manager changes to the demo workspace (suspending,
// editing, inviting, plan changes): it is managed by the server itself. It has
// answered the request when it returns true.
func (s *Server) demoProtected(w http.ResponseWriter, r *http.Request, orgID string) bool {
	o, err := s.St.Org(r.Context(), orgID)
	if err != nil || !o.Demo {
		return false
	}
	writeProblem(w, r, http.StatusForbidden, "Managed automatically",
		"The demo workspace is created and reset by the server. It cannot be changed, suspended or deleted here.")
	return true
}
