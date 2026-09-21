package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// Emit is one event a transition produces.
type Emit struct {
	State string // firing | reminder | escalated | resolved
	Step  *Step  // escalation step, for "escalated"
}

// Decide is the lifecycle of one (rule, key) at now: present says whether the
// condition holds. It returns the next state (nil = forget it) and the events
// to send. Pure, so the rules of the lifecycle are tested on their own.
func Decide(r Rule, s *State, present bool, now time.Time, maintenance bool) (*State, []Emit) {
	if !present {
		if s != nil && s.State == "firing" && r.NotifyResolved {
			return nil, []Emit{{State: "resolved"}}
		}
		return nil, nil
	}
	if s == nil {
		s = &State{RuleID: r.ID, State: "pending", Since: now}
	}
	next := *s
	quiet := maintenance || (s.SnoozeUntil != nil && now.Before(*s.SnoozeUntil))
	switch s.State {
	case "pending":
		if !maintenance && now.Sub(s.Since) >= time.Duration(r.ForMinutes)*time.Minute {
			next.State, next.FiredAt, next.LastNotified = "firing", &now, &now
			return &next, []Emit{{State: "firing"}}
		}
		return &next, nil
	case "firing":
		if quiet || s.AckAt != nil {
			return &next, nil // acknowledged or snoozed: no reminders, no escalation
		}
		steps := append([]Step(nil), r.Escalation...)
		sort.Slice(steps, func(i, j int) bool { return steps[i].AfterMinutes < steps[j].AfterMinutes })
		if s.EscLevel < len(steps) && s.FiredAt != nil && now.Sub(*s.FiredAt) >= time.Duration(steps[s.EscLevel].AfterMinutes)*time.Minute {
			st := steps[s.EscLevel]
			next.EscLevel++
			next.LastNotified = &now
			return &next, []Emit{{State: "escalated", Step: &st}}
		}
		if r.RepeatMinutes > 0 && s.LastNotified != nil && now.Sub(*s.LastNotified) >= time.Duration(r.RepeatMinutes)*time.Minute {
			next.LastNotified = &now
			return &next, []Emit{{State: "reminder"}}
		}
	}
	return &next, nil
}

// Engine evaluates every enabled rule on a schedule.
type Engine struct {
	DB  *DB
	N   *Notifier
	Src Sources
	Now func() time.Time
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now().UTC()
}

// Tick evaluates every enabled rule once.
func (e *Engine) Tick(ctx context.Context) error {
	rules, err := e.DB.Rules(ctx, "")
	if err != nil {
		return err
	}
	ms, err := e.DB.Maintenances(ctx, "")
	if err != nil {
		return err
	}
	suspended := map[string]bool{}
	rows, err := e.DB.P.Query(ctx, `SELECT id FROM orgs WHERE status='suspended'`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			suspended[id] = true
		}
	}
	rows.Close()
	now := e.now()
	for _, r := range rules {
		if suspended[r.OrgID] {
			continue
		}
		if err := e.evaluate(ctx, r, now, InMaintenance(ms, r, now)); err != nil {
			slog.Warn("alerts: rule failed", "rule", r.ID, "kind", r.Kind, "err", err)
		}
	}
	return nil
}

func (e *Engine) evaluate(ctx context.Context, r Rule, now time.Time, maintenance bool) error {
	ectx, cancel := context.WithTimeout(ctx, 20*time.Second)
	hits, err := e.Src.Evaluate(ectx, r, now)
	cancel()
	if err != nil {
		return err
	}
	states, err := e.DB.States(ctx, r.ID)
	if err != nil {
		return err
	}
	// Events are recorded first, the new state stored, and only then delivered:
	// whoever a notification wakes up must find the alert already in its state.
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.Key] = true
		next, emits := Decide(r, states[h.Key], true, now, maintenance)
		next.Key, next.Value = h.Key, h.Value
		if b, err := json.Marshal(withSubject(h)); err == nil {
			next.Detail = b
		}
		var deliver []func()
		for _, em := range emits {
			id, send := e.emit(ctx, r, h, em, next)
			if em.State == "firing" {
				next.EventID = &id
			}
			deliver = append(deliver, send)
		}
		if err := e.DB.PutState(ctx, next); err != nil {
			return err
		}
		for _, send := range deliver {
			send()
		}
	}
	for key, s := range states {
		if seen[key] {
			continue
		}
		_, emits := Decide(r, s, false, now, maintenance)
		var deliver []func()
		for _, em := range emits {
			_, send := e.emit(ctx, r, Hit{Key: key, Value: s.Value, Title: "Resolved", Link: ""}, em, s)
			deliver = append(deliver, send)
		}
		if err := e.DB.ClearState(ctx, r.ID, key); err != nil {
			return err
		}
		for _, send := range deliver {
			send()
		}
	}
	return nil
}

// emit records an event; it returns the event id and a function that delivers it.
func (e *Engine) emit(ctx context.Context, r Rule, h Hit, em Emit, s *State) (int64, func()) {
	title, body, link := h.Title, h.Body, h.Link
	switch em.State {
	case "reminder":
		title = "Still open: " + title
	case "escalated":
		title = "Escalated: " + title
		body = strings.TrimSpace(body + " Nobody has acknowledged it yet.")
	case "resolved":
		// The state kept what the rule saw while it fired: say which thing cleared.
		var was map[string]any
		_ = json.Unmarshal(s.Detail, &was)
		subject, _ := was["subject"].(string)
		title = "Resolved: " + r.Name
		if subject != "" {
			title = "Resolved: " + subject
		}
		body = "The condition has cleared."
		if s.FiredAt != nil {
			body = fmt.Sprintf("The condition has cleared after %s.", humanDur(e.now().Sub(*s.FiredAt)))
		}
		link = "/alerts?tab=history"
		h.Detail = was
	}
	sev := r.Severity
	if em.State == "resolved" {
		sev = "info"
	}
	v := h.Value
	ev := &Event{OrgID: r.OrgID, RuleID: r.ID, Key: h.Key, State: em.State, Severity: sev, Title: title, Body: body, Link: link, Value: &v, Detail: withSubject(h)}
	if err := e.DB.AddEvent(ctx, ev); err != nil {
		slog.Error("alerts: record event", "rule", r.ID, "err", err)
		return 0, func() {}
	}
	audience, channels := r.Audience, r.ChannelIDs
	if em.Step != nil {
		audience = append(append([]string{}, r.Audience...), em.Step.Audience...)
		channels = em.Step.ChannelIDs
	}
	if s != nil && s.AssignedTo != "" {
		audience = append(audience, "user:"+s.AssignedTo)
	}
	return ev.ID, func() { e.N.DeliverEvent(ctx, r, ev, audience, channels) }
}

// Digest e-mails people who chose a daily summary, once a day after 08:00 in
// their own time zone, with what they have not read.
func (e *Engine) Digest(ctx context.Context) error {
	if e.N.Senders == nil || !e.N.Senders.Mail(ctx).Configured() {
		return nil
	}
	rows, err := e.DB.P.Query(ctx, `SELECT u.id, u.email, u.name, u.timezone, p.digest_sent FROM notification_prefs p JOIN users u ON u.id = p.user_id
		WHERE p.email = 'daily' AND u.disabled_at IS NULL`)
	if err != nil {
		return err
	}
	type due struct {
		id, email, name, tz string
		sent                *time.Time
	}
	var dues []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.email, &d.name, &d.tz, &d.sent); err != nil {
			rows.Close()
			return err
		}
		dues = append(dues, d)
	}
	rows.Close()
	now := e.now()
	for _, d := range dues {
		loc, err := time.LoadLocation(d.tz)
		if err != nil || d.tz == "" {
			loc = time.UTC
		}
		local := now.In(loc)
		today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
		if local.Hour() < 8 || (d.sent != nil && !d.sent.Before(today)) {
			continue
		}
		ns, unread, err := e.DB.Notifications(ctx, d.id, true, 30)
		if err != nil {
			return err
		}
		if unread > 0 {
			var b strings.Builder
			for _, n := range ns {
				fmt.Fprintf(&b, "• %s — %s\n", n.Title, n.CreatedAt.In(loc).Format("2 Jan 15:04"))
			}
			if unread > len(ns) {
				fmt.Fprintf(&b, "…and %d more.\n", unread-len(ns))
			}
			m := Message{Subject: fmt.Sprintf("ROADPASS: %d unread notifications", unread), Text: b.String(), Severity: "info", State: "digest", Link: e.N.link("/")}
			if err := SendMail(ctx, e.N.Senders.Mail(ctx), d.email, m); err != nil {
				slog.Warn("alerts: digest", "user", d.id, "err", err)
				continue
			}
		}
		_, _ = e.DB.P.Exec(ctx, `UPDATE notification_prefs SET digest_sent=$2 WHERE user_id=$1`, d.id, today)
	}
	return nil
}

// withSubject is what a hit saw plus its headline, kept with the alert state so
// the all-clear can say what cleared and webhooks get the same context.
func withSubject(h Hit) map[string]any {
	d := make(map[string]any, len(h.Detail)+1)
	for k, v := range h.Detail {
		d[k] = v
	}
	if h.Title != "" && h.Title != "Resolved" {
		d["subject"] = h.Title
	}
	return d
}
