package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is the alerts' persistence.
type DB struct{ P *pgxpool.Pool }

// ErrNotFound is a missing rule, channel, alert or notification.
var ErrNotFound = errors.New("not found")

// ErrConflict is a rule saved by someone else since it was read.
var ErrConflict = errors.New("conflict")

func nf(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// ---------------------------------------------------------------- rules

const ruleCols = `id, org_id, name, kind, params, severity, audience, channel_ids, for_minutes, repeat_minutes, notify_resolved, escalation, enabled, version, created_by, created_at, updated_at`

func scanRule(r pgx.Row) (Rule, error) {
	var x Rule
	var params, esc []byte
	err := r.Scan(&x.ID, &x.OrgID, &x.Name, &x.Kind, &params, &x.Severity, &x.Audience, &x.ChannelIDs, &x.ForMinutes, &x.RepeatMinutes,
		&x.NotifyResolved, &esc, &x.Enabled, &x.Version, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt)
	if err != nil {
		return x, nf(err)
	}
	_ = json.Unmarshal(params, &x.Params)
	_ = json.Unmarshal(esc, &x.Escalation)
	if x.Escalation == nil {
		x.Escalation = []Step{}
	}
	return x, nil
}

func collectRules(rows pgx.Rows, err error) ([]Rule, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		x, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Rules lists an organisation's rules ("" = every organisation's enabled rules).
func (d *DB) Rules(ctx context.Context, orgID string) ([]Rule, error) {
	if orgID == "" {
		return collectRules(d.P.Query(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE enabled ORDER BY org_id, created_at`))
	}
	return collectRules(d.P.Query(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE org_id=$1 ORDER BY created_at`, orgID))
}

// Rule returns one rule of an organisation.
func (d *DB) Rule(ctx context.Context, orgID, id string) (Rule, error) {
	return scanRule(d.P.QueryRow(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE org_id=$1 AND id=$2`, orgID, id))
}

// SaveRule creates a rule (Version 0) or replaces one at its version. Changing
// a rule clears its state: the conditions are evaluated afresh.
func (d *DB) SaveRule(ctx context.Context, x Rule) (Rule, error) {
	params, _ := json.Marshal(x.Params)
	esc, _ := json.Marshal(x.Escalation)
	if x.Audience == nil {
		x.Audience = []string{}
	}
	if x.ChannelIDs == nil {
		x.ChannelIDs = []string{}
	}
	if x.Version == 0 {
		return scanRule(d.P.QueryRow(ctx, `INSERT INTO alert_rules (id, org_id, name, kind, params, severity, audience, channel_ids, for_minutes,
				repeat_minutes, notify_resolved, escalation, enabled, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING `+ruleCols,
			x.ID, x.OrgID, x.Name, x.Kind, params, x.Severity, x.Audience, x.ChannelIDs, x.ForMinutes, x.RepeatMinutes, x.NotifyResolved, esc, x.Enabled, x.CreatedBy))
	}
	tx, err := d.P.Begin(ctx)
	if err != nil {
		return x, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	out, err := scanRule(tx.QueryRow(ctx, `UPDATE alert_rules SET name=$3, kind=$4, params=$5, severity=$6, audience=$7, channel_ids=$8, for_minutes=$9,
			repeat_minutes=$10, notify_resolved=$11, escalation=$12, enabled=$13, version=version+1, updated_at=now()
		WHERE org_id=$1 AND id=$2 AND version=$14 RETURNING `+ruleCols,
		x.OrgID, x.ID, x.Name, x.Kind, params, x.Severity, x.Audience, x.ChannelIDs, x.ForMinutes, x.RepeatMinutes, x.NotifyResolved, esc, x.Enabled, x.Version))
	if errors.Is(err, ErrNotFound) {
		if _, e := d.Rule(ctx, x.OrgID, x.ID); e == nil {
			return out, ErrConflict
		}
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM alert_state WHERE rule_id=$1`, x.ID); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// DeleteRule removes a rule and its state (its history stays).
func (d *DB) DeleteRule(ctx context.Context, orgID, id string) error {
	tag, err := d.P.Exec(ctx, `DELETE FROM alert_rules WHERE org_id=$1 AND id=$2`, orgID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ---------------------------------------------------------------- state

// States returns every pending or firing (key) of a rule.
func (d *DB) States(ctx context.Context, ruleID string) (map[string]*State, error) {
	rows, err := d.P.Query(ctx, `SELECT rule_id, key, state, since, fired_at, last_notified, COALESCE(value,0), detail, event_id,
		COALESCE(ack_by,''), ack_at, snooze_until, COALESCE(assigned_to,''), esc_level FROM alert_state WHERE rule_id=$1`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*State{}
	for rows.Next() {
		var s State
		if err := rows.Scan(&s.RuleID, &s.Key, &s.State, &s.Since, &s.FiredAt, &s.LastNotified, &s.Value, &s.Detail, &s.EventID,
			&s.AckBy, &s.AckAt, &s.SnoozeUntil, &s.AssignedTo, &s.EscLevel); err != nil {
			return nil, err
		}
		out[s.Key] = &s
	}
	return out, rows.Err()
}

// PutState writes a (rule, key) state.
func (d *DB) PutState(ctx context.Context, s *State) error {
	_, err := d.P.Exec(ctx, `INSERT INTO alert_state (rule_id, key, state, since, fired_at, last_notified, value, detail, event_id, ack_by, ack_at,
			snooze_until, assigned_to, esc_level)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),$11,$12,NULLIF($13,''),$14)
		ON CONFLICT (rule_id, key) DO UPDATE SET state=EXCLUDED.state, since=EXCLUDED.since, fired_at=EXCLUDED.fired_at,
			last_notified=EXCLUDED.last_notified, value=EXCLUDED.value, detail=EXCLUDED.detail, event_id=EXCLUDED.event_id,
			ack_by=EXCLUDED.ack_by, ack_at=EXCLUDED.ack_at, snooze_until=EXCLUDED.snooze_until, assigned_to=EXCLUDED.assigned_to,
			esc_level=EXCLUDED.esc_level`,
		s.RuleID, s.Key, s.State, s.Since, s.FiredAt, s.LastNotified, s.Value, s.Detail, s.EventID, s.AckBy, s.AckAt, s.SnoozeUntil, s.AssignedTo, s.EscLevel)
	return err
}

// ClearState forgets a (rule, key): the condition cleared.
func (d *DB) ClearState(ctx context.Context, ruleID, key string) error {
	_, err := d.P.Exec(ctx, `DELETE FROM alert_state WHERE rule_id=$1 AND key=$2`, ruleID, key)
	return err
}

// Active lists an organisation's firing alerts with their rule.
type Active struct {
	State
	RuleName string `json:"ruleName"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Link     string `json:"link,omitempty"`
}

// ActiveAlerts returns what is firing now, most severe and newest first.
func (d *DB) ActiveAlerts(ctx context.Context, orgID string) ([]Active, error) {
	rows, err := d.P.Query(ctx, `SELECT s.rule_id, s.key, s.state, s.since, s.fired_at, s.last_notified, COALESCE(s.value,0), s.detail, s.event_id,
			COALESCE(s.ack_by,''), s.ack_at, s.snooze_until, COALESCE(s.assigned_to,''), s.esc_level,
			r.name, r.kind, r.severity, COALESCE(e.title, r.name), COALESCE(e.body,''), COALESCE(e.link,'')
		FROM alert_state s JOIN alert_rules r ON r.id = s.rule_id LEFT JOIN alert_events e ON e.id = s.event_id
		WHERE r.org_id=$1 AND s.state='firing'
		ORDER BY CASE r.severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END, s.fired_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Active{}
	for rows.Next() {
		var a Active
		if err := rows.Scan(&a.RuleID, &a.Key, &a.State.State, &a.Since, &a.FiredAt, &a.LastNotified, &a.Value, &a.Detail, &a.EventID,
			&a.AckBy, &a.AckAt, &a.SnoozeUntil, &a.AssignedTo, &a.EscLevel, &a.RuleName, &a.Kind, &a.Severity, &a.Title, &a.Body, &a.Link); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Ack, Snooze and Assign act on a firing alert of the organisation.
func (d *DB) Ack(ctx context.Context, orgID, ruleID, key, by string) error {
	return d.touch(ctx, orgID, ruleID, key, `ack_by=$4, ack_at=now()`, by)
}

// Snooze silences reminders and escalation until t.
func (d *DB) Snooze(ctx context.Context, orgID, ruleID, key string, until time.Time) error {
	return d.touch(ctx, orgID, ruleID, key, `snooze_until=$4`, until)
}

// Assign names who handles an alert ("" to clear).
func (d *DB) Assign(ctx context.Context, orgID, ruleID, key, userID string) error {
	return d.touch(ctx, orgID, ruleID, key, `assigned_to=NULLIF($4,'')`, userID)
}

func (d *DB) touch(ctx context.Context, orgID, ruleID, key, set string, arg any) error {
	tag, err := d.P.Exec(ctx, `UPDATE alert_state s SET `+set+` FROM alert_rules r
		WHERE r.id = s.rule_id AND r.org_id=$1 AND s.rule_id=$2 AND s.key=$3 AND s.state='firing'`, orgID, ruleID, key, arg)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err == nil {
		_, err = d.P.Exec(ctx, `UPDATE alert_events e SET ack_by = s.ack_by, ack_at = s.ack_at, assigned_to = s.assigned_to
			FROM alert_state s WHERE s.rule_id=$1 AND s.key=$2 AND e.id = s.event_id`, ruleID, key)
	}
	return err
}

// ---------------------------------------------------------------- events

// AddEvent records an event and returns its id.
func (d *DB) AddEvent(ctx context.Context, e *Event) error {
	del, _ := json.Marshal(e.Deliveries)
	return d.P.QueryRow(ctx, `INSERT INTO alert_events (org_id, rule_id, key, state, severity, title, body, link, value, deliveries)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10) RETURNING id, at`,
		e.OrgID, e.RuleID, e.Key, e.State, e.Severity, e.Title, e.Body, e.Link, e.Value, del).Scan(&e.ID, &e.At)
}

// SetDeliveries stores what happened when an event was sent.
func (d *DB) SetDeliveries(ctx context.Context, id int64, ds []Delivery) error {
	b, _ := json.Marshal(ds)
	_, err := d.P.Exec(ctx, `UPDATE alert_events SET deliveries=$2 WHERE id=$1`, id, b)
	return err
}

// History lists an organisation's events, newest first.
func (d *DB) History(ctx context.Context, orgID string, ruleID string, limit int) ([]Event, error) {
	rows, err := d.P.Query(ctx, `SELECT id, org_id, rule_id, key, state, severity, title, body, COALESCE(link,''), value, at, deliveries,
		COALESCE(ack_by,''), ack_at, COALESCE(assigned_to,'') FROM alert_events
		WHERE org_id=$1 AND ($2 = '' OR rule_id=$2) ORDER BY at DESC LIMIT $3`, orgID, ruleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var del []byte
		if err := rows.Scan(&e.ID, &e.OrgID, &e.RuleID, &e.Key, &e.State, &e.Severity, &e.Title, &e.Body, &e.Link, &e.Value, &e.At, &del,
			&e.AckBy, &e.AckAt, &e.AssignedTo); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(del, &e.Deliveries)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Stats summarises an organisation's alerting over a window.
type Stats struct {
	Fired       int            `json:"fired"`
	Open        int            `json:"open"`
	AckedPct    float64        `json:"ackedPct"`
	MedianAckS  float64        `json:"medianAckSeconds"`
	PerDay      map[string]int `json:"perDay"`
	NoisiestIDs []struct {
		RuleID string `json:"ruleId"`
		N      int    `json:"n"`
	} `json:"noisiest"`
	Flood bool `json:"flood"` // more than 10 firings in the last 10 minutes
}

// Statistics over the last `days` days.
func (d *DB) Statistics(ctx context.Context, orgID string, days int) (Stats, error) {
	var st Stats
	st.PerDay = map[string]int{}
	st.NoisiestIDs = []struct {
		RuleID string `json:"ruleId"`
		N      int    `json:"n"`
	}{}
	since := time.Now().AddDate(0, 0, -days)
	err := d.P.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state='firing'),
			COALESCE(100.0 * count(*) FILTER (WHERE state='firing' AND ack_at IS NOT NULL) / NULLIF(count(*) FILTER (WHERE state='firing'),0), 0),
			COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM ack_at - at)) FILTER (WHERE state='firing' AND ack_at IS NOT NULL), 0),
			count(*) FILTER (WHERE state='firing' AND at > now() - interval '10 minutes') > 10
		FROM alert_events WHERE org_id=$1 AND at >= $2`, orgID, since).Scan(&st.Fired, &st.AckedPct, &st.MedianAckS, &st.Flood)
	if err != nil {
		return st, err
	}
	if err := d.P.QueryRow(ctx, `SELECT count(*) FROM alert_state s JOIN alert_rules r ON r.id=s.rule_id WHERE r.org_id=$1 AND s.state='firing'`, orgID).Scan(&st.Open); err != nil {
		return st, err
	}
	rows, err := d.P.Query(ctx, `SELECT to_char(date_trunc('day', at), 'YYYY-MM-DD'), count(*) FROM alert_events
		WHERE org_id=$1 AND state='firing' AND at >= $2 GROUP BY 1`, orgID, since)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var day string
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			rows.Close()
			return st, err
		}
		st.PerDay[day] = n
	}
	rows.Close()
	rows, err = d.P.Query(ctx, `SELECT rule_id, count(*) FROM alert_events WHERE org_id=$1 AND state='firing' AND at >= $2
		GROUP BY 1 ORDER BY 2 DESC LIMIT 5`, orgID, since)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var x struct {
			RuleID string `json:"ruleId"`
			N      int    `json:"n"`
		}
		if err := rows.Scan(&x.RuleID, &x.N); err != nil {
			return st, err
		}
		st.NoisiestIDs = append(st.NoisiestIDs, x)
	}
	return st, rows.Err()
}

// ---------------------------------------------------------------- maintenance

// Maintenance is a window in which an organisation's rules do not fire.
type Maintenance struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"orgId"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	RuleIDs   []string  `json:"ruleIds"`
	Note      string    `json:"note"`
	CreatedBy string    `json:"createdBy"`
}

// Maintenances lists an organisation's windows that have not ended.
func (d *DB) Maintenances(ctx context.Context, orgID string) ([]Maintenance, error) {
	rows, err := d.P.Query(ctx, `SELECT id, org_id, starts_at, ends_at, rule_ids, note, created_by FROM alert_maintenance
		WHERE ($1 = '' OR org_id=$1) AND ends_at > now() ORDER BY starts_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Maintenance{}
	for rows.Next() {
		var m Maintenance
		if err := rows.Scan(&m.ID, &m.OrgID, &m.StartsAt, &m.EndsAt, &m.RuleIDs, &m.Note, &m.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMaintenance stores a window.
func (d *DB) AddMaintenance(ctx context.Context, m Maintenance) error {
	if m.RuleIDs == nil {
		m.RuleIDs = []string{}
	}
	_, err := d.P.Exec(ctx, `INSERT INTO alert_maintenance (id, org_id, starts_at, ends_at, rule_ids, note, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		m.ID, m.OrgID, m.StartsAt, m.EndsAt, m.RuleIDs, m.Note, m.CreatedBy)
	return err
}

// DeleteMaintenance ends a window early.
func (d *DB) DeleteMaintenance(ctx context.Context, orgID, id string) error {
	_, err := d.P.Exec(ctx, `DELETE FROM alert_maintenance WHERE org_id=$1 AND id=$2`, orgID, id)
	return err
}

// InMaintenance reports whether rule r is suppressed at t.
func InMaintenance(ms []Maintenance, r Rule, t time.Time) bool {
	for _, m := range ms {
		if m.OrgID != r.OrgID || t.Before(m.StartsAt) || !t.Before(m.EndsAt) {
			continue
		}
		if len(m.RuleIDs) == 0 {
			return true
		}
		for _, id := range m.RuleIDs {
			if id == r.ID {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------- cursors

// Cursor returns where an event-driven rule kind left off (now, the first time).
func (d *DB) Cursor(ctx context.Context, name string) (time.Time, error) {
	var at time.Time
	err := d.P.QueryRow(ctx, `INSERT INTO alert_cursors (name, at) VALUES ($1, now()) ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name RETURNING at`, name).Scan(&at)
	return at, err
}

// SetCursor moves a cursor forward.
func (d *DB) SetCursor(ctx context.Context, name string, at time.Time) error {
	_, err := d.P.Exec(ctx, `UPDATE alert_cursors SET at=$2 WHERE name=$1 AND at < $2`, name, at)
	return err
}
