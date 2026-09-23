package alerts

import (
	"context"
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------- in-app

// AddNotification stores one person's in-app notification.
func (d *DB) AddNotification(ctx context.Context, orgID, userID string, n *Notification) error {
	return d.P.QueryRow(ctx, `INSERT INTO notifications (org_id, user_id, kind, severity, title, body, link, event_id)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8) RETURNING id, created_at`,
		orgID, userID, n.Kind, n.Severity, n.Title, n.Body, n.Link, n.EventID).Scan(&n.ID, &n.CreatedAt)
}

// Notifications lists a person's notifications, newest first.
func (d *DB) Notifications(ctx context.Context, userID string, unreadOnly bool, limit int) ([]Notification, int, error) {
	var unread int
	if err := d.P.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL`, userID).Scan(&unread); err != nil {
		return nil, 0, err
	}
	rows, err := d.P.Query(ctx, `SELECT id, kind, severity, title, body, COALESCE(link,''), event_id, created_at, read_at FROM notifications
		WHERE user_id=$1 AND (NOT $2 OR read_at IS NULL) ORDER BY created_at DESC LIMIT $3`, userID, unreadOnly, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Kind, &n.Severity, &n.Title, &n.Body, &n.Link, &n.EventID, &n.CreatedAt, &n.ReadAt); err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	return out, unread, rows.Err()
}

// MarkRead marks one notification (id > 0) or all of a person's as read.
func (d *DB) MarkRead(ctx context.Context, userID string, id int64) error {
	_, err := d.P.Exec(ctx, `UPDATE notifications SET read_at=now() WHERE user_id=$1 AND read_at IS NULL AND ($2 = 0 OR id=$2)`, userID, id)
	return err
}

// PruneNotifications drops read notifications older than 90 days and every one
// older than a year.
func (d *DB) PruneNotifications(ctx context.Context) error {
	_, err := d.P.Exec(ctx, `DELETE FROM notifications WHERE (read_at IS NOT NULL AND created_at < now() - interval '90 days') OR created_at < now() - interval '1 year'`)
	if err == nil {
		_, err = d.P.Exec(ctx, `DELETE FROM alert_events WHERE at < now() - interval '1 year'`)
	}
	return err
}

// ---------------------------------------------------------------- preferences

// Prefs returns a person's choices (defaults when they have none).
func (d *DB) Prefs(ctx context.Context, userID string) (Prefs, error) {
	p := DefaultPrefs()
	err := d.P.QueryRow(ctx, `SELECT browser, email, min_severity, quiet_from, quiet_to, muted FROM notification_prefs WHERE user_id=$1`, userID).
		Scan(&p.Browser, &p.Email, &p.MinSeverity, &p.QuietFrom, &p.QuietTo, &p.Muted)
	if nf(err) == ErrNotFound {
		return DefaultPrefs(), nil
	}
	return p, err
}

// SetPrefs stores a person's choices.
func (d *DB) SetPrefs(ctx context.Context, userID string, p Prefs) error {
	if p.Muted == nil {
		p.Muted = []string{}
	}
	_, err := d.P.Exec(ctx, `INSERT INTO notification_prefs (user_id, browser, email, min_severity, quiet_from, quiet_to, muted)
		VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (user_id) DO UPDATE SET browser=EXCLUDED.browser, email=EXCLUDED.email,
			min_severity=EXCLUDED.min_severity, quiet_from=EXCLUDED.quiet_from, quiet_to=EXCLUDED.quiet_to, muted=EXCLUDED.muted`,
		userID, p.Browser, p.Email, p.MinSeverity, p.QuietFrom, p.QuietTo, p.Muted)
	return err
}

// ---------------------------------------------------------------- browser push

// Subscription is one browser registered for Web Push.
type Subscription struct {
	ID        string     `json:"id"`
	UserID    string     `json:"-"`
	Endpoint  string     `json:"endpoint"`
	P256dh    string     `json:"-"`
	Auth      string     `json:"-"`
	UserAgent string     `json:"userAgent"`
	CreatedAt time.Time  `json:"createdAt"`
	LastOKAt  *time.Time `json:"lastOkAt,omitempty"`
}

// AddSubscription registers (or re-registers) a browser for a person.
func (d *DB) AddSubscription(ctx context.Context, s Subscription) error {
	_, err := d.P.Exec(ctx, `INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (endpoint) DO UPDATE SET user_id=EXCLUDED.user_id, p256dh=EXCLUDED.p256dh, auth=EXCLUDED.auth,
			user_agent=EXCLUDED.user_agent, failures=0`, s.ID, s.UserID, s.Endpoint, s.P256dh, s.Auth, s.UserAgent)
	return err
}

// Subscriptions lists a person's browsers.
func (d *DB) Subscriptions(ctx context.Context, userID string) ([]Subscription, error) {
	rows, err := d.P.Query(ctx, `SELECT id, user_id, endpoint, p256dh, auth, user_agent, created_at, last_ok_at FROM push_subscriptions WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth, &s.UserAgent, &s.CreatedAt, &s.LastOKAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RemoveSubscription forgets a browser (by id for its owner, or by endpoint).
func (d *DB) RemoveSubscription(ctx context.Context, userID, id, endpoint string) error {
	_, err := d.P.Exec(ctx, `DELETE FROM push_subscriptions WHERE user_id=$1 AND (id=$2 OR endpoint=$3)`, userID, id, endpoint)
	return err
}

// PushResult records a delivery outcome; a subscription that failed for good
// (gone, or five failures in a row) is removed.
func (d *DB) PushResult(ctx context.Context, id string, ok, gone bool) {
	switch {
	case gone:
		_, _ = d.P.Exec(ctx, `DELETE FROM push_subscriptions WHERE id=$1`, id)
	case ok:
		_, _ = d.P.Exec(ctx, `UPDATE push_subscriptions SET last_ok_at=now(), failures=0 WHERE id=$1`, id)
	default:
		_, _ = d.P.Exec(ctx, `UPDATE push_subscriptions SET failures=failures+1 WHERE id=$1`, id)
		_, _ = d.P.Exec(ctx, `DELETE FROM push_subscriptions WHERE id=$1 AND failures >= 5`, id)
	}
}

// ---------------------------------------------------------------- channels

const chanCols = `id, org_id, type, name, config, COALESCE(secret_ref,''), verified, COALESCE(last_status,''), COALESCE(last_error,''), last_sent_at, created_by, created_at`

func scanChannel(r interface{ Scan(...any) error }) (Channel, error) {
	var c Channel
	var cfg []byte
	err := r.Scan(&c.ID, &c.OrgID, &c.Type, &c.Name, &cfg, &c.SecretRef, &c.Verified, &c.LastStatus, &c.LastError, &c.LastSentAt, &c.CreatedBy, &c.CreatedAt)
	if err != nil {
		return c, nf(err)
	}
	_ = json.Unmarshal(cfg, &c.Config)
	if c.Config == nil {
		c.Config = map[string]string{}
	}
	return c, nil
}

// Channels lists an organisation's channels.
func (d *DB) Channels(ctx context.Context, orgID string) ([]Channel, error) {
	rows, err := d.P.Query(ctx, `SELECT `+chanCols+` FROM alert_channels WHERE org_id=$1 ORDER BY created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Channel returns one of an organisation's channels.
func (d *DB) Channel(ctx context.Context, orgID, id string) (Channel, error) {
	return scanChannel(d.P.QueryRow(ctx, `SELECT `+chanCols+` FROM alert_channels WHERE org_id=$1 AND id=$2`, orgID, id))
}

// AddChannel stores a channel (its secret is stored separately).
func (d *DB) AddChannel(ctx context.Context, c Channel) error {
	cfg, _ := json.Marshal(c.Config)
	_, err := d.P.Exec(ctx, `INSERT INTO alert_channels (id, org_id, type, name, config, secret_ref, verified, created_by) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8)`,
		c.ID, c.OrgID, c.Type, c.Name, cfg, c.SecretRef, c.Verified, c.CreatedBy)
	return err
}

// DeleteChannel removes a channel and drops it from every rule.
func (d *DB) DeleteChannel(ctx context.Context, orgID, id string) error {
	tag, err := d.P.Exec(ctx, `DELETE FROM alert_channels WHERE org_id=$1 AND id=$2`, orgID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err == nil {
		_, err = d.P.Exec(ctx, `UPDATE alert_rules SET channel_ids = array_remove(channel_ids, $2) WHERE org_id=$1`, orgID, id)
	}
	return err
}

// SetVerification stores a code's hash and expiry (a new code resets attempts).
func (d *DB) SetVerification(ctx context.Context, id string, hash []byte, expires time.Time) error {
	_, err := d.P.Exec(ctx, `UPDATE alert_channels SET verify_hash=$2, verify_expires=$3, verify_attempts=0 WHERE id=$1`, id, hash, expires)
	return err
}

// Verification returns the pending code's hash, expiry and attempts so far.
func (d *DB) Verification(ctx context.Context, orgID, id string) ([]byte, *time.Time, int, error) {
	var h []byte
	var exp *time.Time
	var n int
	err := d.P.QueryRow(ctx, `UPDATE alert_channels SET verify_attempts = verify_attempts + 1 WHERE org_id=$1 AND id=$2
		RETURNING verify_hash, verify_expires, verify_attempts`, orgID, id).Scan(&h, &exp, &n)
	return h, exp, n, nf(err)
}

// MarkVerified marks a channel as proven.
func (d *DB) MarkVerified(ctx context.Context, id string) error {
	_, err := d.P.Exec(ctx, `UPDATE alert_channels SET verified=true, verify_hash=NULL, verify_expires=NULL WHERE id=$1`, id)
	return err
}

// ChannelResult records the last delivery outcome.
func (d *DB) ChannelResult(ctx context.Context, id string, err error) {
	if err != nil {
		_, _ = d.P.Exec(ctx, `UPDATE alert_channels SET last_status='failed', last_error=$2, last_sent_at=now() WHERE id=$1`, id, err.Error())
		return
	}
	_, _ = d.P.Exec(ctx, `UPDATE alert_channels SET last_status='ok', last_error=NULL, last_sent_at=now() WHERE id=$1`, id)
}
