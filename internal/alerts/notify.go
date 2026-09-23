package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/secrets"
)

// Notifier tells people: in-app (live), by browser push and e-mail per their
// preferences, and through an organisation's channels for alert events.
type Notifier struct {
	DB        *DB
	Secrets   *secrets.Store
	Senders   *Senders
	Push      *Push // nil: browser push off
	Cache     *cache.Cache
	PublicURL string
	// Backoff overrides the waits before delivery attempts 1-3 (tests); empty: 0, 1 s, 4 s.
	Backoff []time.Duration
}

// Recipient is a member who may be notified.
type Recipient struct {
	ID, OrgID, Email, Name, Role, Timezone string
	Duties                                 []string
}

// Matches reports whether the member is in an audience. An empty audience
// means the organisation's admins.
func (r Recipient) Matches(audience []string) bool {
	if len(audience) == 0 {
		return r.Role == "admin"
	}
	for _, a := range audience {
		switch {
		case a == "all", a == "role:"+r.Role, a == "user:"+r.ID:
			return true
		case strings.HasPrefix(a, "duty:") && (r.Role == "admin" || slices.Contains(r.Duties, a[5:])):
			return true
		}
	}
	return false
}

// Recipients returns the active members of an organisation in an audience.
func (n *Notifier) Recipients(ctx context.Context, orgID string, audience []string) ([]Recipient, error) {
	rows, err := n.DB.P.Query(ctx, `SELECT id, org_id, email, name, role, duties, timezone FROM users WHERE org_id=$1 AND disabled_at IS NULL`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recipient
	for rows.Next() {
		var r Recipient
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Email, &r.Name, &r.Role, &r.Duties, &r.Timezone); err != nil {
			return nil, err
		}
		if r.Matches(audience) {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

func (n *Notifier) link(path string) string {
	if path == "" || strings.HasPrefix(path, "http") {
		return path
	}
	return strings.TrimRight(n.PublicURL, "/") + path
}

// Notify sends an in-app notification to everyone in the audience, and pushes
// or e-mails those whose preferences allow it. Returns how many were reached
// in-app and by push.
func (n *Notifier) Notify(ctx context.Context, orgID string, audience []string, x Notification) (inApp, pushed int) {
	rs, err := n.Recipients(ctx, orgID, audience)
	if err != nil {
		slog.Error("notify: recipients", "err", err)
		return 0, 0
	}
	return n.NotifyPeople(ctx, orgID, rs, x)
}

// NotifyPeople is Notify for an explicit list of members.
func (n *Notifier) NotifyPeople(ctx context.Context, orgID string, rs []Recipient, x Notification) (inApp, pushed int) {
	var ids []string
	now := time.Now()
	for _, r := range rs {
		nn := x
		if err := n.DB.AddNotification(ctx, orgID, r.ID, &nn); err != nil {
			slog.Error("notify: in-app", "user", r.ID, "err", err)
			continue
		}
		inApp++
		ids = append(ids, r.ID)
		p, err := n.DB.Prefs(ctx, r.ID)
		if err != nil || SeverityRank(x.Severity) < SeverityRank(p.MinSeverity) || slices.Contains(p.Muted, x.Kind) {
			continue
		}
		loc, err := time.LoadLocation(r.Timezone)
		if err != nil || r.Timezone == "" {
			loc = time.UTC
		}
		// Quiet hours hold back push and e-mail, except for critical alerts.
		if p.Quiet(now, loc) && x.Severity != "critical" {
			continue
		}
		if p.Browser && n.Push != nil {
			k, err := n.Push.Send(ctx, r.ID, PushPayload{Title: x.Title, Body: x.Body, Link: x.Link, Severity: x.Severity, Tag: fmt.Sprint(x.Kind, x.EventID)})
			if err != nil {
				slog.Warn("notify: push", "user", r.ID, "err", err)
			}
			if k > 0 {
				pushed++
			}
		}
		if p.Email == "instant" && n.Senders != nil && n.Senders.Mail(ctx).Configured() && n.Cache.AllowN(ctx, "mail:"+r.ID, 60, time.Hour) {
			m := Message{Subject: x.Title, Text: x.Body, Severity: x.Severity, State: x.Kind, Link: n.link(x.Link)}
			if err := SendMail(ctx, n.Senders.Mail(ctx), r.Email, m); err != nil {
				slog.Warn("notify: e-mail", "user", r.ID, "err", err)
			}
		}
	}
	if len(ids) > 0 && n.Cache != nil {
		n.Cache.Announce(context.WithoutCancel(ctx), cache.Event{Kind: "notification", Users: ids, Count: 1})
	}
	return inApp, pushed
}

// Per-organisation sending limits, per channel type and window.
var channelLimits = map[string]struct {
	n      int
	window time.Duration
}{
	"email": {200, 24 * time.Hour}, "sms": {20, 24 * time.Hour}, "telegram": {60, time.Hour},
	"slack": {60, time.Hour}, "teams": {60, time.Hour}, "webhook": {120, time.Hour},
}

// SendChannel delivers m through one channel, within its limits.
func (n *Notifier) SendChannel(ctx context.Context, c Channel, m Message) error {
	if NeedsVerification(c.Type) && !c.Verified && m.State != "verify" {
		return fmt.Errorf("%s channel %q is not verified", c.Type, c.Name)
	}
	if l, ok := channelLimits[c.Type]; ok && n.Cache != nil && !n.Cache.AllowN(ctx, "ch:"+c.OrgID+":"+c.Type, l.n, l.window) {
		return fmt.Errorf("sending limit reached (%d per %s)", l.n, l.window)
	}
	var secret map[string]string
	if c.SecretRef != "" {
		s, err := n.Secrets.Get(ctx, c.SecretRef)
		if err != nil {
			return fmt.Errorf("channel secret: %w", err)
		}
		secret = s
	}
	// A real alert is retried when the far end fails in a way that may pass (5xx, 429, timeout);
	// the test and verification messages are sent once so the person sees the truth at once.
	attempts, backoff := 1, []time.Duration{0}
	if m.State != "test" && m.State != "verify" && (c.Type == "webhook" || c.Type == "slack" || c.Type == "teams" || c.Type == "telegram" || c.Type == "sms") {
		attempts, backoff = 3, n.retryBackoff()
	}
	err := retry(ctx, backoff[:attempts], func(cctx context.Context) error { return n.Senders.Send(cctx, c, secret, m) },
		func(i int, err error) {
			slog.Warn("alerts: channel send failed, will retry", "channel", c.ID, "attempt", i+1, "err", err)
		})
	n.DB.ChannelResult(context.WithoutCancel(ctx), c.ID, err)
	return err
}

// retry runs send up to len(waits) times, waiting waits[i] before attempt i, and
// stops at the first success or at an error that cannot pass on a retry. Each
// attempt gets its own 10 s timeout.
func retry(ctx context.Context, waits []time.Duration, send func(context.Context) error, onRetry func(i int, err error)) error {
	var err error
	for i, w := range waits {
		if i > 0 {
			select {
			case <-time.After(w):
			case <-ctx.Done():
				return err
			}
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = send(cctx)
		cancel()
		if err == nil || !Transient(err) {
			return err
		}
		if i < len(waits)-1 && onRetry != nil {
			onRetry(i, err)
		}
	}
	return err
}

// retryBackoff is the wait before attempts 1, 2 and 3 (tests shorten it).
func (n *Notifier) retryBackoff() []time.Duration {
	if len(n.Backoff) == 3 {
		return n.Backoff
	}
	return []time.Duration{0, time.Second, 4 * time.Second}
}

// DeliverEvent sends an alert event to people (in-app, push, e-mail) and to
// channels, and records each delivery on the event.
func (n *Notifier) DeliverEvent(ctx context.Context, r Rule, ev *Event, audience, channelIDs []string) {
	var ds []Delivery
	in, pushed := n.Notify(ctx, r.OrgID, audience, Notification{Kind: "alert", Severity: ev.Severity, Title: ev.Title, Body: ev.Body,
		Link: ev.Link, EventID: &ev.ID})
	ds = append(ds, Delivery{Channel: "in-app", Type: "in-app", OK: true, To: in})
	if pushed > 0 {
		ds = append(ds, Delivery{Channel: "browser", Type: "push", OK: true, To: pushed})
	}
	data := map[string]any{"rule": r.Name, "ruleId": r.ID, "key": ev.Key, "eventId": ev.ID, "orgId": r.OrgID}
	if ev.Value != nil && r.Kind != KindFinding && r.Kind != KindRejected {
		data["value"] = *ev.Value // a threshold reading; findings and rejections have none
	}
	for k, v := range ev.Detail { // what the rule saw: record, site, shipment…
		if _, taken := data[k]; !taken {
			data[k] = v
		}
	}
	m := Message{ID: fmt.Sprintf("evt_%d", ev.ID), Subject: ev.Title, Text: ev.Body, Severity: ev.Severity, State: ev.State, Link: n.link(ev.Link), Data: data}
	for _, id := range channelIDs {
		c, err := n.DB.Channel(ctx, r.OrgID, id)
		if err != nil {
			ds = append(ds, Delivery{Channel: id, OK: false, Error: "channel not found"})
			continue
		}
		err = n.SendChannel(ctx, c, m)
		d := Delivery{Channel: c.Name, Type: c.Type, OK: err == nil}
		if err != nil {
			d.Error = err.Error()
		}
		ds = append(ds, d)
	}
	ev.Deliveries = ds
	if err := n.DB.SetDeliveries(ctx, ev.ID, ds); err != nil {
		slog.Error("alerts: record deliveries", "event", ev.ID, "err", err)
	}
}
