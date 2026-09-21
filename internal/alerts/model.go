// Package alerts watches the chain and tells people. Rules (a metric crossing a
// threshold, a check finding, a late or rejected shipment, missing telemetry, a
// seal backlog, a plan limit) are evaluated by the worker; a firing rule creates
// an event that is delivered in-app, by browser push, and through the
// organisation's channels (e-mail, webhook, Slack, Teams, SMS, Telegram).
//
// The lifecycle follows AquaSense's: per (rule, key) the state goes ok → pending
// → firing → ok. A rule holds for ForMinutes before firing, reminds every
// RepeatMinutes while firing, escalates along its steps, and sends an all-clear
// when NotifyResolved. Acknowledging or snoozing stops reminders and escalation;
// maintenance windows suppress new firings.
package alerts

import (
	"encoding/json"
	"time"

	"github.com/sensifai/roadpass/internal/analytics"
)

// Kinds of rule.
const (
	KindMetric         = "metric"          // an explorer query compared with a threshold
	KindFinding        = "finding"         // a new data-check finding on the organisation's records
	KindReceiptOverdue = "receipt_overdue" // a shipment to us not received within N hours
	KindRejected       = "rejected"        // one of our shipments rejected at intake
	KindTelemetryGap   = "telemetry_gap"   // a plant sent no readings for N minutes
	KindSealBacklog    = "seal_backlog"    // records waiting for a ledger seal longer than N minutes
	KindPlanLimit      = "plan_limit"      // usage at or above N % of the plan
)

// Kinds lists every rule kind.
var Kinds = []string{KindMetric, KindFinding, KindReceiptOverdue, KindRejected, KindTelemetryGap, KindSealBacklog, KindPlanLimit}

// Severities, lowest first.
var Severities = []string{"info", "warning", "critical"}

// SeverityRank orders severities.
func SeverityRank(s string) int {
	for i, x := range Severities {
		if x == s {
			return i
		}
	}
	return 0
}

// Params configures a rule; which fields matter depends on its kind.
type Params struct {
	// metric
	Query      *analytics.Query `json:"query,omitempty"`
	WindowMins int              `json:"windowMinutes,omitempty"` // evaluated over the last N minutes
	Op         string           `json:"op,omitempty"`            // > >= < <=
	Value      float64          `json:"value,omitempty"`
	// finding
	MinSeverity string `json:"minSeverity,omitempty"` // warning | blocker
	// receipt_overdue
	Hours int `json:"hours,omitempty"`
	// telemetry_gap, seal_backlog
	Minutes int `json:"minutes,omitempty"`
	// plan_limit
	Percent int `json:"percent,omitempty"`
}

// Step escalates a firing alert nobody acknowledged.
type Step struct {
	AfterMinutes int      `json:"afterMinutes"`
	ChannelIDs   []string `json:"channelIds"`
	Audience     []string `json:"audience"`
}

// Rule is one alert rule.
type Rule struct {
	ID             string    `json:"id"`
	OrgID          string    `json:"orgId"`
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	Params         Params    `json:"params"`
	Severity       string    `json:"severity"`
	Audience       []string  `json:"audience"`
	ChannelIDs     []string  `json:"channelIds"`
	ForMinutes     int       `json:"forMinutes"`
	RepeatMinutes  int       `json:"repeatMinutes"`
	NotifyResolved bool      `json:"notifyResolved"`
	Escalation     []Step    `json:"escalation"`
	Enabled        bool      `json:"enabled"`
	Version        int       `json:"version"`
	CreatedBy      string    `json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Hit is one thing a rule currently sees as wrong.
type Hit struct {
	Key    string         // stable identity: a site, a shipment, a finding…
	Value  float64        // the observed value, when there is one
	Title  string         // short headline
	Body   string         // one or two sentences
	Link   string         // where in ROADPASS to look
	Detail map[string]any // structured context for webhooks
}

// State is a (rule, key) that is pending or firing.
type State struct {
	RuleID       string          `json:"ruleId"`
	Key          string          `json:"key"`
	State        string          `json:"state"` // pending | firing
	Since        time.Time       `json:"since"`
	FiredAt      *time.Time      `json:"firedAt,omitempty"`
	LastNotified *time.Time      `json:"lastNotified,omitempty"`
	Value        float64         `json:"value"`
	Detail       json.RawMessage `json:"detail,omitempty"`
	EventID      *int64          `json:"eventId,omitempty"`
	AckBy        string          `json:"ackBy,omitempty"`
	AckAt        *time.Time      `json:"ackAt,omitempty"`
	SnoozeUntil  *time.Time      `json:"snoozeUntil,omitempty"`
	AssignedTo   string          `json:"assignedTo,omitempty"`
	EscLevel     int             `json:"escLevel"`
}

// Delivery records one attempt to tell someone.
type Delivery struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	To      int    `json:"to,omitempty"` // recipients reached (in-app, push)
}

// Event is one notification-worthy moment of an alert.
type Event struct {
	ID         int64      `json:"id"`
	OrgID      string     `json:"orgId"`
	RuleID     string     `json:"ruleId"`
	Key        string     `json:"key"`
	State      string     `json:"state"` // firing | reminder | escalated | resolved
	Severity   string     `json:"severity"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	Link       string     `json:"link,omitempty"`
	Value      *float64   `json:"value,omitempty"`
	At         time.Time  `json:"at"`
	Deliveries []Delivery `json:"deliveries"`
	AckBy      string     `json:"ackBy,omitempty"`
	AckAt      *time.Time `json:"ackAt,omitempty"`
	AssignedTo string     `json:"assignedTo,omitempty"`
	// Detail is what the rule saw (record, site, shipment…), for webhooks; not stored with the event.
	Detail map[string]any `json:"-"`
}

// Channel is a destination outside ROADPASS.
type Channel struct {
	ID         string            `json:"id"`
	OrgID      string            `json:"orgId"`
	Type       string            `json:"type"` // email | webhook | slack | teams | sms | telegram
	Name       string            `json:"name"`
	Config     map[string]string `json:"config"`
	SecretRef  string            `json:"-"`
	Verified   bool              `json:"verified"`
	LastStatus string            `json:"lastStatus,omitempty"`
	LastError  string            `json:"lastError,omitempty"`
	LastSentAt *time.Time        `json:"lastSentAt,omitempty"`
	CreatedBy  string            `json:"createdBy"`
	CreatedAt  time.Time         `json:"createdAt"`
}

// ChannelTypes lists every channel type.
var ChannelTypes = []string{"email", "webhook", "slack", "teams", "sms", "telegram"}

// NeedsVerification: channels that reach a person must prove they are theirs.
func NeedsVerification(t string) bool { return t == "email" || t == "sms" }

// Notification is an in-app message for one person.
type Notification struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	Severity  string     `json:"severity"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      string     `json:"link,omitempty"`
	EventID   *int64     `json:"eventId,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
}

// Prefs are a person's notification choices.
type Prefs struct {
	Browser     bool     `json:"browser"`
	Email       string   `json:"email"` // off | instant | daily
	MinSeverity string   `json:"minSeverity"`
	QuietFrom   *int     `json:"quietFrom,omitempty"` // minutes after midnight, in their time zone
	QuietTo     *int     `json:"quietTo,omitempty"`
	Muted       []string `json:"muted"` // notification kinds they do not want pushed or e-mailed
}

// DefaultPrefs: in-app and browser on, e-mail off.
func DefaultPrefs() Prefs {
	return Prefs{Browser: true, Email: "off", MinSeverity: "info", Muted: []string{}}
}

// Quiet reports whether t falls inside the quiet hours in loc.
func (p Prefs) Quiet(t time.Time, loc *time.Location) bool {
	if p.QuietFrom == nil || p.QuietTo == nil || *p.QuietFrom == *p.QuietTo {
		return false
	}
	lt := t.In(loc)
	m := lt.Hour()*60 + lt.Minute()
	from, to := *p.QuietFrom, *p.QuietTo
	if from < to {
		return m >= from && m < to
	}
	return m >= from || m < to // across midnight, e.g. 22:00–07:00
}
