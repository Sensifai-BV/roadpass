package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/store"
)

// ---------------------------------------------------------------- notifications (everyone)

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, unread, err := s.Notify.DB.Notifications(r.Context(), p.User.ID, r.URL.Query().Get("unread") == "1", limit)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
}

func (s *Server) readNotifications(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"` // 0 = all
	}
	if !decode(w, r, &in, 256) {
		return
	}
	if err := s.Notify.DB.MarkRead(r.Context(), principal(r.Context()).User.ID, in.ID); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) notificationPrefs(w http.ResponseWriter, r *http.Request) {
	p, err := s.Notify.DB.Prefs(r.Context(), principal(r.Context()).User.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prefs": p, "emailAvailable": s.Notify.Senders.Mail(r.Context()).Configured()})
}

var notificationKinds = []string{"alert", "assignment", "org", "key", "billing", "system"}

func (s *Server) setNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	var in alerts.Prefs
	if !decode(w, r, &in, 2048) {
		return
	}
	ok := slices.Contains([]string{"off", "instant", "daily"}, in.Email) && slices.Contains(alerts.Severities, in.MinSeverity) &&
		(in.QuietFrom == nil) == (in.QuietTo == nil) && (in.QuietFrom == nil || (*in.QuietFrom >= 0 && *in.QuietFrom < 1440 && *in.QuietTo >= 0 && *in.QuietTo < 1440))
	for _, k := range in.Muted {
		ok = ok && slices.Contains(notificationKinds, k)
	}
	if !ok {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Check your notification settings", "")
		return
	}
	if err := s.Notify.DB.SetPrefs(r.Context(), principal(r.Context()).User.ID, in); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pushKey(w http.ResponseWriter, r *http.Request) {
	pub, _, err := s.Notify.Push.Keys(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]string{"publicKey": pub})
}

func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	// The browser's PushSubscription.toJSON(), as it comes.
	var in struct {
		Endpoint       string   `json:"endpoint"`
		ExpirationTime *float64 `json:"expirationTime"`
		Keys           struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if !decode(w, r, &in, 4096) {
		return
	}
	if !strings.HasPrefix(in.Endpoint, "https://") || len(in.Endpoint) > 1024 || in.Keys.P256dh == "" || in.Keys.Auth == "" {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Not a push subscription", "")
		return
	}
	p := principal(r.Context())
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	if err := s.Notify.DB.AddSubscription(r.Context(), alerts.Subscription{ID: auth.ID("psh_"), UserID: p.User.ID, Endpoint: in.Endpoint,
		P256dh: in.Keys.P256dh, Auth: in.Keys.Auth, UserAgent: ua}); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pushDevices(w http.ResponseWriter, r *http.Request) {
	subs, err := s.Notify.DB.Subscriptions(r.Context(), principal(r.Context()).User.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, subs)
}

func (s *Server) pushRemove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string `json:"id"`
		Endpoint string `json:"endpoint"`
	}
	if !decode(w, r, &in, 2048) {
		return
	}
	if err := s.Notify.DB.RemoveSubscription(r.Context(), principal(r.Context()).User.ID, in.ID, in.Endpoint); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testNotification sends the caller a notification through every channel their
// preferences allow, so they can see it works.
func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	rs, err := s.Notify.Recipients(r.Context(), p.Org.ID, []string{"user:" + p.User.ID})
	if err != nil {
		fail(w, r, err)
		return
	}
	in, pushed := s.Notify.NotifyPeople(r.Context(), p.Org.ID, rs, alerts.Notification{Kind: "system", Severity: "info",
		Title: "Test notification", Body: "Notifications reach you here. Change what you receive under Settings → Notifications.", Link: "/settings?tab=notifications"})
	writeJSON(w, http.StatusOK, map[string]int{"inApp": in, "pushed": pushed})
}

// ---------------------------------------------------------------- alerts: active, history

func (s *Server) activeAlerts(w http.ResponseWriter, r *http.Request) {
	a, err := s.Notify.DB.ActiveAlerts(r.Context(), principal(r.Context()).Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) alertAction(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		RuleID  string `json:"ruleId"`
		Key     string `json:"key"`
		Action  string `json:"action"` // ack | snooze | assign
		Minutes int    `json:"minutes"`
		UserID  string `json:"userId"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	ctx := r.Context()
	var err error
	switch in.Action {
	case "ack":
		err = s.Notify.DB.Ack(ctx, p.Org.ID, in.RuleID, in.Key, p.Actor())
	case "snooze":
		if in.Minutes < 15 || in.Minutes > 7*24*60 {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Snooze between 15 minutes and 7 days", "")
			return
		}
		err = s.Notify.DB.Snooze(ctx, p.Org.ID, in.RuleID, in.Key, time.Now().Add(time.Duration(in.Minutes)*time.Minute))
	case "assign":
		if in.UserID != "" {
			if u, e := s.St.UserByID(ctx, in.UserID); e != nil || u.OrgID != p.Org.ID {
				writeProblem(w, r, http.StatusUnprocessableEntity, "Pick a member of your organisation", "")
				return
			}
		}
		err = s.Notify.DB.Assign(ctx, p.Org.ID, in.RuleID, in.Key, in.UserID)
		if err == nil && in.UserID != "" && in.UserID != p.User.ID {
			rs, _ := s.Notify.Recipients(ctx, p.Org.ID, []string{"user:" + in.UserID})
			s.Notify.NotifyPeople(ctx, p.Org.ID, rs, alerts.Notification{Kind: "assignment", Severity: "warning",
				Title: "An alert was assigned to you", Body: "Open Alerts to see what needs attention.", Link: "/alerts"})
		}
	default:
		writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown action", "Use ack, snooze or assign.")
		return
	}
	if errors.Is(err, alerts.ErrNotFound) {
		writeProblem(w, r, http.StatusNotFound, "That alert is no longer firing", "")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(ctx, p.Org.ID, p.Actor(), "alert."+in.Action, in.RuleID, map[string]any{"key": in.Key})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) alertHistory(w http.ResponseWriter, r *http.Request) {
	ev, err := s.Notify.DB.History(r.Context(), principal(r.Context()).Org.ID, r.URL.Query().Get("rule"), 300)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (s *Server) alertStats(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 365 {
		days = 30
	}
	st, err := s.Notify.DB.Statistics(r.Context(), principal(r.Context()).Org.ID, days)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ---------------------------------------------------------------- rules (admins)

func (s *Server) alertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Notify.DB.Rules(r.Context(), principal(r.Context()).Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules, "kinds": alerts.Kinds})
}

// checkRule validates a rule against the catalogue and the caller's organisation.
func (s *Server) checkRule(r *http.Request, x *alerts.Rule) []fieldErr {
	p := principal(r.Context())
	var errs []fieldErr
	add := func(f, format string, a ...any) { errs = append(errs, fieldErr{f, fmt.Sprintf(format, a...)}) }
	x.Name = strings.TrimSpace(x.Name)
	if l := len([]rune(x.Name)); l < 1 || l > 80 {
		add("name", "Name the rule (up to 80 characters).")
	}
	if !slices.Contains(alerts.Kinds, x.Kind) {
		add("kind", "Pick what the rule watches.")
	}
	if x.Kind == alerts.KindSealBacklog && !p.Operator() {
		add("kind", "Only the platform operator watches the seal backlog.")
	}
	if !slices.Contains(alerts.Severities, x.Severity) {
		add("severity", "Use info, warning or critical.")
	}
	if x.ForMinutes < 0 || x.ForMinutes > 1440 {
		add("forMinutes", "Hold between 0 and 1440 minutes.")
	}
	if x.RepeatMinutes != 0 && (x.RepeatMinutes < 15 || x.RepeatMinutes > 10080) {
		add("repeatMinutes", "Remind every 15 minutes to 7 days, or never.")
	}
	members, _ := s.St.Users(r.Context(), p.Org.ID, false)
	okAudience := func(field string, aud []string) {
		for _, a := range aud {
			role, isRole := strings.CutPrefix(a, "role:")
			duty, isDuty := strings.CutPrefix(a, "duty:")
			uid, isUser := strings.CutPrefix(a, "user:")
			ok := a == "all" || (isRole && roleRank[role] > 0) || (isDuty && slices.Contains(domain.AllDuties(), duty)) ||
				(isUser && slices.ContainsFunc(members, func(u store.User) bool { return u.ID == uid }))
			if !ok {
				add(field, "Unknown recipient %q.", a)
			}
		}
	}
	okAudience("audience", x.Audience)
	channels, _ := s.Notify.DB.Channels(r.Context(), p.Org.ID)
	okChannels := func(field string, ids []string) {
		for _, id := range ids {
			if !slices.ContainsFunc(channels, func(c alerts.Channel) bool { return c.ID == id }) {
				add(field, "Unknown channel %q.", id)
			}
		}
	}
	okChannels("channelIds", x.ChannelIDs)
	if len(x.Escalation) > 4 {
		add("escalation", "At most four escalation steps.")
	}
	for i, st := range x.Escalation {
		if st.AfterMinutes < 1 || st.AfterMinutes > 10080 {
			add(fmt.Sprintf("escalation[%d]", i), "Escalate after 1 minute to 7 days.")
		}
		okChannels(fmt.Sprintf("escalation[%d]", i), st.ChannelIDs)
		okAudience(fmt.Sprintf("escalation[%d]", i), st.Audience)
	}
	pr := x.Params
	switch x.Kind {
	case alerts.KindMetric:
		if pr.Query == nil {
			add("params.query", "Choose what to measure.")
		} else if err := pr.Query.Validate(); err != nil {
			add("params.query", "%s", strings.TrimPrefix(err.Error(), "query: "))
		}
		if !slices.Contains([]string{">", ">=", "<", "<="}, pr.Op) {
			add("params.op", "Pick a comparison.")
		}
		if pr.WindowMins < 5 || pr.WindowMins > 10080 {
			add("params.windowMinutes", "Look back 5 minutes to 7 days.")
		}
	case alerts.KindFinding:
		if !slices.Contains([]string{"warning", "blocker"}, pr.MinSeverity) {
			add("params.minSeverity", "Use warning or blocker.")
		}
	case alerts.KindReceiptOverdue, alerts.KindRejected:
		if pr.Hours < 1 || pr.Hours > 720 {
			add("params.hours", "Between 1 hour and 30 days.")
		}
	case alerts.KindTelemetryGap, alerts.KindSealBacklog:
		if pr.Minutes < 5 || pr.Minutes > 10080 {
			add("params.minutes", "Between 5 minutes and 7 days.")
		}
	case alerts.KindPlanLimit:
		if pr.Percent < 50 || pr.Percent > 100 {
			add("params.percent", "Between 50 and 100 %%.")
		}
	}
	return errs
}

func (s *Server) saveRule(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in alerts.Rule
	if !decode(w, r, &in, 32<<10) {
		return
	}
	in.OrgID = p.Org.ID
	if id := r.PathValue("id"); id != "" {
		in.ID = id
		if in.Version == 0 {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Send the version you edited", "")
			return
		}
	} else {
		in.ID, in.Version, in.CreatedBy = auth.ID("rul_"), 0, p.Actor()
	}
	if errs := s.checkRule(r, &in); errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Rule not saved", "Fix the highlighted fields.", errs)
		return
	}
	out, err := s.Notify.DB.SaveRule(r.Context(), in)
	switch {
	case errors.Is(err, alerts.ErrConflict):
		writeProblem(w, r, http.StatusConflict, "Changed by someone else", "Reload the rule to see their changes.")
		return
	case errors.Is(err, alerts.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "Not found", "")
		return
	case err != nil:
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "alert.rule.save", out.ID, map[string]any{"name": out.Name, "kind": out.Kind})
	status := http.StatusOK
	if r.PathValue("id") == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, out)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if err := s.Notify.DB.DeleteRule(r.Context(), p.Org.ID, r.PathValue("id")); err != nil {
		if errors.Is(err, alerts.ErrNotFound) {
			writeProblem(w, r, http.StatusNotFound, "Not found", "")
			return
		}
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "alert.rule.delete", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

// previewRule evaluates a rule now without notifying anyone.
func (s *Server) previewRule(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in alerts.Rule
	if !decode(w, r, &in, 32<<10) {
		return
	}
	in.OrgID = p.Org.ID
	if in.UpdatedAt.IsZero() {
		in.UpdatedAt = time.Now().AddDate(0, 0, -30) // a preview looks back further than a new rule would
	}
	if errs := s.checkRule(r, &in); errs != nil {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Rule not valid", "Fix the highlighted fields.", errs)
		return
	}
	hits, err := alerts.Sources{St: s.St, CH: s.CH}.Evaluate(r.Context(), in, time.Now().UTC())
	if err != nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Cannot evaluate", err.Error())
		return
	}
	type view struct {
		Key   string  `json:"key"`
		Value float64 `json:"value"`
		Title string  `json:"title"`
	}
	out := []view{}
	for i, h := range hits {
		if i == 20 {
			break
		}
		out = append(out, view{h.Key, h.Value, h.Title})
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": out, "total": len(hits)})
}

// ---------------------------------------------------------------- channels (admins)

func (s *Server) alertChannels(w http.ResponseWriter, r *http.Request) {
	cs, err := s.Notify.DB.Channels(r.Context(), principal(r.Context()).Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": cs, "smtp": s.Notify.Senders.Mail(r.Context()).Configured(), "sms": s.Notify.Senders.Twilio.Configured()})
}

var (
	reEmail = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	rePhone = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
	reChat  = regexp.MustCompile(`^-?\d{1,20}$|^@[A-Za-z0-9_]{4,32}$`)
)

func (s *Server) addChannel(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Type   string            `json:"type"`
		Name   string            `json:"name"`
		Config map[string]string `json:"config"`
		Secret map[string]string `json:"secret"`
	}
	if !decode(w, r, &in, 8192) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Config == nil {
		in.Config = map[string]string{}
	}
	if in.Secret == nil {
		in.Secret = map[string]string{}
	}
	bad := func(msg string) { writeProblem(w, r, http.StatusUnprocessableEntity, "Check the channel", msg) }
	if !slices.Contains(alerts.ChannelTypes, in.Type) {
		bad("Pick a channel type.")
		return
	}
	if in.Name == "" || len([]rune(in.Name)) > 80 {
		bad("Name the channel.")
		return
	}
	allow := s.Notify.Senders.AllowPrivate
	cfg := map[string]string{}
	secret := map[string]string{}
	var reveal map[string]string // shown once: the webhook signing secret
	switch in.Type {
	case "email":
		if !reEmail.MatchString(in.Config["to"]) {
			bad("Enter the e-mail address.")
			return
		}
		cfg["to"] = strings.ToLower(in.Config["to"])
		if h := in.Config["smtpHost"]; h != "" { // the organisation's own mail server
			for _, k := range []string{"smtpHost", "smtpPort", "smtpUser", "from", "smtpTls"} {
				cfg[k] = in.Config[k]
			}
			if !reEmail.MatchString(addressPart(cfg["from"])) {
				bad("Enter the sender address of your mail server.")
				return
			}
			secret["smtpPassword"] = in.Secret["smtpPassword"]
		} else if !s.Notify.Senders.Mail(r.Context()).Configured() {
			bad("This platform has no mail server. Enter your own SMTP server for this channel.")
			return
		}
	case "webhook":
		if err := alerts.CheckURL(in.Config["url"], allow); err != nil {
			bad("Webhook URL: " + err.Error())
			return
		}
		cfg["url"] = in.Config["url"]
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		secret["signingSecret"] = "whsec_" + base64.RawURLEncoding.EncodeToString(b)
		reveal = map[string]string{"signingSecret": secret["signingSecret"]}
	case "slack", "teams":
		if err := alerts.CheckURL(in.Secret["url"], allow); err != nil {
			bad("Incoming webhook URL: " + err.Error())
			return
		}
		secret["url"] = in.Secret["url"]
		cfg["host"] = hostOf(in.Secret["url"])
	case "sms":
		if !rePhone.MatchString(in.Config["to"]) {
			bad("Enter the phone number in international form, e.g. +31612345678.")
			return
		}
		cfg["to"] = in.Config["to"]
		if in.Secret["accountSid"] != "" {
			if !rePhone.MatchString(in.Config["from"]) {
				bad("Enter your Twilio sender number.")
				return
			}
			cfg["from"] = in.Config["from"]
			secret["accountSid"], secret["authToken"] = in.Secret["accountSid"], in.Secret["authToken"]
		} else if !s.Notify.Senders.Twilio.Configured() {
			bad("This platform has no SMS provider. Enter your own Twilio account for this channel.")
			return
		}
	case "telegram":
		if !reChat.MatchString(in.Config["chatId"]) || in.Secret["botToken"] == "" {
			bad("Enter the chat id and the bot token.")
			return
		}
		cfg["chatId"] = in.Config["chatId"]
		secret["botToken"] = in.Secret["botToken"]
	}
	c := alerts.Channel{ID: auth.ID("chn_"), OrgID: p.Org.ID, Type: in.Type, Name: in.Name, Config: cfg, CreatedBy: p.Actor(),
		Verified: !alerts.NeedsVerification(in.Type)}
	if len(secret) > 0 {
		ref, err := s.Notify.Secrets.Put(r.Context(), "channels/"+c.ID, secret)
		if err != nil {
			fail(w, r, err)
			return
		}
		c.SecretRef = ref
	}
	if err := s.Notify.DB.AddChannel(r.Context(), c); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "alert.channel.add", c.ID, map[string]string{"type": c.Type, "name": c.Name})
	writeJSON(w, http.StatusCreated, map[string]any{"channel": c, "secret": reveal})
}

func addressPart(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}

func hostOf(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	return u
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	c, err := s.Notify.DB.Channel(r.Context(), p.Org.ID, r.PathValue("id"))
	if err != nil {
		writeProblem(w, r, http.StatusNotFound, "Not found", "")
		return
	}
	if err := s.Notify.DB.DeleteChannel(r.Context(), p.Org.ID, c.ID); err != nil {
		fail(w, r, err)
		return
	}
	if c.SecretRef != "" {
		_ = s.Notify.Secrets.Delete(r.Context(), c.SecretRef)
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "alert.channel.delete", c.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

func codeHash(channelID, code string) []byte {
	h := sha256.Sum256([]byte(channelID + "|" + code))
	return h[:]
}

// sendVerification sends a six-digit code through an e-mail or SMS channel.
func (s *Server) sendVerification(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	c, err := s.Notify.DB.Channel(r.Context(), p.Org.ID, r.PathValue("id"))
	if err != nil || !alerts.NeedsVerification(c.Type) || c.Verified {
		writeProblem(w, r, http.StatusConflict, "Nothing to verify", "")
		return
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	code := fmt.Sprintf("%06d", binary.BigEndian.Uint32(b[:])%1_000_000)
	if err := s.Notify.DB.SetVerification(r.Context(), c.ID, codeHash(c.ID, code), time.Now().Add(15*time.Minute)); err != nil {
		fail(w, r, err)
		return
	}
	err = s.Notify.SendChannel(r.Context(), c, alerts.Message{Subject: "Your ROADPASS verification code: " + code, State: "verify", Severity: "info",
		Text: "Enter this code in ROADPASS to confirm this channel. It expires in 15 minutes. If you did not ask for it, ignore this message."})
	if err != nil {
		writeProblem(w, r, http.StatusBadGateway, "Could not send the code", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) confirmVerification(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in, 256) {
		return
	}
	h, exp, attempts, err := s.Notify.DB.Verification(r.Context(), p.Org.ID, r.PathValue("id"))
	switch {
	case err != nil:
		writeProblem(w, r, http.StatusNotFound, "Not found", "")
	case h == nil || exp == nil || time.Now().After(*exp):
		writeProblem(w, r, http.StatusGone, "The code expired", "Send a new code.")
	case attempts > 5:
		writeProblem(w, r, http.StatusTooManyRequests, "Too many tries", "Send a new code.")
	case subtle.ConstantTimeCompare(h, codeHash(r.PathValue("id"), strings.TrimSpace(in.Code))) != 1:
		writeProblem(w, r, http.StatusUnprocessableEntity, "That code is not right", "")
	default:
		if err := s.Notify.DB.MarkVerified(r.Context(), r.PathValue("id")); err != nil {
			fail(w, r, err)
			return
		}
		_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "alert.channel.verify", r.PathValue("id"), nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) testChannel(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	c, err := s.Notify.DB.Channel(r.Context(), p.Org.ID, r.PathValue("id"))
	if err != nil {
		writeProblem(w, r, http.StatusNotFound, "Not found", "")
		return
	}
	err = s.Notify.SendChannel(r.Context(), c, alerts.Message{Subject: "ROADPASS test message", State: "test", Severity: "info",
		Text: "This channel works: alerts from " + p.Org.Name + " will arrive here.", Link: s.Cfg.PublicURL + "/alerts"})
	if err != nil {
		writeProblem(w, r, http.StatusBadGateway, "The test message failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- maintenance (admins)

func (s *Server) maintenance(w http.ResponseWriter, r *http.Request) {
	ms, err := s.Notify.DB.Maintenances(r.Context(), principal(r.Context()).Org.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

func (s *Server) addMaintenance(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in alerts.Maintenance
	if !decode(w, r, &in, 4096) {
		return
	}
	if !in.EndsAt.After(in.StartsAt) || in.EndsAt.Sub(in.StartsAt) > 30*24*time.Hour || in.EndsAt.Before(time.Now()) || len([]rune(in.Note)) > 280 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Check the window", "It must end after it starts, in the future, and last at most 30 days.")
		return
	}
	in.ID, in.OrgID, in.CreatedBy = auth.ID("mnt_"), p.Org.ID, p.Actor()
	if err := s.Notify.DB.AddMaintenance(r.Context(), in); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "alert.maintenance", in.ID, map[string]any{"from": in.StartsAt, "to": in.EndsAt})
	writeJSON(w, http.StatusCreated, in)
}

func (s *Server) deleteMaintenance(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if err := s.Notify.DB.DeleteMaintenance(r.Context(), p.Org.ID, r.PathValue("id")); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
