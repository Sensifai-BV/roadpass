package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------- limits

// Limit is a bound on a measured value (AquaSense-style limit reporting).
type Limit struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"orgId"`
	Name      string    `json:"name"`
	Source    string    `json:"source"`
	Measure   string    `json:"measure"`
	Types     []string  `json:"types"`
	SiteID    string    `json:"siteId,omitempty"`
	Basis     string    `json:"basis"`
	Min       *float64  `json:"min,omitempty"`
	Max       *float64  `json:"max,omitempty"`
	Note      string    `json:"note"`
	Enabled   bool      `json:"enabled"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const limitCols = `id, org_id, name, source, measure, types, coalesce(site_id, ''), basis, min_value, max_value, note, enabled, created_by, created_at, updated_at`

func scanLimit(row pgx.Row) (Limit, error) {
	var l Limit
	err := row.Scan(&l.ID, &l.OrgID, &l.Name, &l.Source, &l.Measure, &l.Types, &l.SiteID, &l.Basis, &l.Min, &l.Max, &l.Note, &l.Enabled, &l.CreatedBy, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

// Limits lists an organisation's limits.
func (s *Store) Limits(ctx context.Context, orgID string) ([]Limit, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+limitCols+` FROM compliance_limits WHERE org_id=$1 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Limit{}
	for rows.Next() {
		l, err := scanLimit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CountLimits is how many limits an organisation has (a cap guards the report).
func (s *Store) CountLimits(ctx context.Context, orgID string) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, `SELECT count(*) FROM compliance_limits WHERE org_id=$1`, orgID).Scan(&n)
	return n, err
}

// SaveLimit inserts or updates a limit of the organisation.
func (s *Store) SaveLimit(ctx context.Context, l Limit) (Limit, error) {
	return scanLimit(s.DB.QueryRow(ctx, `INSERT INTO compliance_limits (id, org_id, name, source, measure, types, site_id, basis, min_value, max_value, note, enabled, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,nullif($7,''),$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO UPDATE SET name=excluded.name, source=excluded.source, measure=excluded.measure, types=excluded.types, site_id=excluded.site_id,
			basis=excluded.basis, min_value=excluded.min_value, max_value=excluded.max_value, note=excluded.note, enabled=excluded.enabled, updated_at=now()
			WHERE compliance_limits.org_id = excluded.org_id
		RETURNING `+limitCols, l.ID, l.OrgID, l.Name, l.Source, l.Measure, l.Types, l.SiteID, l.Basis, l.Min, l.Max, l.Note, l.Enabled, l.CreatedBy))
}

// DeleteLimit removes a limit of the organisation.
func (s *Store) DeleteLimit(ctx context.Context, orgID, id string) error {
	t, err := s.DB.Exec(ctx, `DELETE FROM compliance_limits WHERE org_id=$1 AND id=$2`, orgID, id)
	if err == nil && t.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ---------------------------------------------------------------- calibrations

// Calibration is a note that an instrument was calibrated, checked or replaced.
type Calibration struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"orgId"`
	SiteID    string    `json:"siteId,omitempty"`
	Parameter string    `json:"parameter"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	By        string    `json:"by"`
	Note      string    `json:"note"`
	CreatedBy string    `json:"createdBy"`
}

// Calibrations lists an organisation's calibration notes in [from, to).
func (s *Store) Calibrations(ctx context.Context, orgID string, from, to time.Time) ([]Calibration, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, org_id, coalesce(site_id,''), parameter, at, kind, by_who, note, created_by
		FROM compliance_calibrations WHERE org_id=$1 AND at >= $2 AND at < $3 ORDER BY at DESC LIMIT 500`, orgID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Calibration{}
	for rows.Next() {
		var c Calibration
		if err := rows.Scan(&c.ID, &c.OrgID, &c.SiteID, &c.Parameter, &c.At, &c.Kind, &c.By, &c.Note, &c.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddCalibration records a calibration note.
func (s *Store) AddCalibration(ctx context.Context, c Calibration) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO compliance_calibrations (id, org_id, site_id, parameter, at, kind, by_who, note, created_by)
		VALUES ($1,$2,nullif($3,''),$4,$5,$6,$7,$8,$9)`, c.ID, c.OrgID, c.SiteID, c.Parameter, c.At, c.Kind, c.By, c.Note, c.CreatedBy)
	return err
}

// DeleteCalibration removes a calibration note entered by mistake.
func (s *Store) DeleteCalibration(ctx context.Context, orgID, id string) error {
	t, err := s.DB.Exec(ctx, `DELETE FROM compliance_calibrations WHERE org_id=$1 AND id=$2`, orgID, id)
	if err == nil && t.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ---------------------------------------------------------------- schedule

// Schedule e-mails the monthly compliance report.
type Schedule struct {
	ID          string     `json:"id"`
	OrgID       string     `json:"orgId"`
	Recipients  []string   `json:"recipients"`
	Day         int        `json:"day"`
	Hour        int        `json:"hour"`
	Timezone    string     `json:"timezone"`
	Enabled     bool       `json:"enabled"`
	LastPeriod  *time.Time `json:"lastPeriod,omitempty"`
	LastRunAt   *time.Time `json:"lastRunAt,omitempty"`
	LastStatus  string     `json:"lastStatus,omitempty"`
	LastMessage string     `json:"lastMessage,omitempty"`
	CreatedBy   string     `json:"createdBy"`
}

const scheduleCols = `id, org_id, recipients, day, hour, timezone, enabled, last_period, last_run_at, coalesce(last_status,''), coalesce(last_message,''), created_by`

func scanSchedule(row pgx.Row) (Schedule, error) {
	var x Schedule
	err := row.Scan(&x.ID, &x.OrgID, &x.Recipients, &x.Day, &x.Hour, &x.Timezone, &x.Enabled, &x.LastPeriod, &x.LastRunAt, &x.LastStatus, &x.LastMessage, &x.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// ReportSchedule returns the organisation's schedule (ErrNotFound: none).
func (s *Store) ReportSchedule(ctx context.Context, orgID string) (Schedule, error) {
	return scanSchedule(s.DB.QueryRow(ctx, `SELECT `+scheduleCols+` FROM compliance_schedules WHERE org_id=$1`, orgID))
}

// SaveReportSchedule creates or replaces the organisation's schedule.
func (s *Store) SaveReportSchedule(ctx context.Context, x Schedule) (Schedule, error) {
	return scanSchedule(s.DB.QueryRow(ctx, `INSERT INTO compliance_schedules (id, org_id, recipients, day, hour, timezone, enabled, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (org_id) DO UPDATE SET recipients=excluded.recipients, day=excluded.day, hour=excluded.hour, timezone=excluded.timezone, enabled=excluded.enabled
		RETURNING `+scheduleCols, x.ID, x.OrgID, x.Recipients, x.Day, x.Hour, x.Timezone, x.Enabled, x.CreatedBy))
}

// DeleteReportSchedule stops the e-mails.
func (s *Store) DeleteReportSchedule(ctx context.Context, orgID string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM compliance_schedules WHERE org_id=$1`, orgID)
	return err
}

// ReportSchedules lists every enabled schedule (the worker decides which are due).
func (s *Store) ReportSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+scheduleCols+` FROM compliance_schedules WHERE enabled`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		x, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// MarkReportRun records a scheduled run for a period (the first day of the month).
func (s *Store) MarkReportRun(ctx context.Context, orgID string, period time.Time, status, message string) error {
	_, err := s.DB.Exec(ctx, `UPDATE compliance_schedules SET last_run_at=now(), last_status=$3, last_message=$4,
		last_period = CASE WHEN $3 = 'sent' THEN $2::date ELSE last_period END WHERE org_id=$1`, orgID, period, status, message)
	return err
}

// ---------------------------------------------------------------- attestations

// Attestation is a member's signed statement about a requirement (or a month's
// report). Attestations are append-only; the newest one counts.
type Attestation struct {
	ID            string    `json:"id"`
	OrgID         string    `json:"orgId"`
	RequirementID string    `json:"requirementId"`
	Period        string    `json:"period,omitempty"`
	Status        string    `json:"status"`
	Note          string    `json:"note"`
	EvidenceURL   string    `json:"evidenceUrl,omitempty"`
	By            string    `json:"by"`
	At            time.Time `json:"at"`
	KeyID         string    `json:"keyId,omitempty"`
	Signature     []byte    `json:"signature,omitempty"`
	Message       string    `json:"message"`
	ByName        string    `json:"byName,omitempty"`
	PublicKey     []byte    `json:"publicKey,omitempty"`
}

const attCols = `a.id, a.org_id, a.requirement_id, a.period, a.status, a.note, a.evidence_url, a.attested_by, a.attested_at, coalesce(a.key_id,''), a.signature, a.message,
	coalesce(u.name, ''), k.public_key`
const attFrom = ` FROM compliance_attestations a LEFT JOIN users u ON u.id = a.attested_by LEFT JOIN signing_keys k ON k.id = a.key_id`

func scanAttestations(rows pgx.Rows) ([]Attestation, error) {
	defer rows.Close()
	out := []Attestation{}
	for rows.Next() {
		var a Attestation
		if err := rows.Scan(&a.ID, &a.OrgID, &a.RequirementID, &a.Period, &a.Status, &a.Note, &a.EvidenceURL, &a.By, &a.At, &a.KeyID, &a.Signature, &a.Message, &a.ByName, &a.PublicKey); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AddAttestation appends a signed statement.
func (s *Store) AddAttestation(ctx context.Context, a Attestation) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO compliance_attestations (id, org_id, requirement_id, period, status, note, evidence_url, attested_by, attested_at, key_id, signature, message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,nullif($10,''),$11,$12)`,
		a.ID, a.OrgID, a.RequirementID, a.Period, a.Status, a.Note, a.EvidenceURL, a.By, a.At, a.KeyID, a.Signature, a.Message)
	return err
}

// LatestAttestations returns the newest statement per requirement (and period).
func (s *Store) LatestAttestations(ctx context.Context, orgID string) ([]Attestation, error) {
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT ON (a.requirement_id, a.period) `+attCols+attFrom+`
		WHERE a.org_id=$1 ORDER BY a.requirement_id, a.period, a.attested_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	return scanAttestations(rows)
}

// AttestationHistory lists every statement on a requirement, newest first.
func (s *Store) AttestationHistory(ctx context.Context, orgID, requirementID string) ([]Attestation, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+attCols+attFrom+` WHERE a.org_id=$1 AND a.requirement_id=$2 ORDER BY a.attested_at DESC LIMIT 100`, orgID, requirementID)
	if err != nil {
		return nil, err
	}
	return scanAttestations(rows)
}

// ---------------------------------------------------------------- evidence

// Evidence is what the data show about an organisation over the last year; the
// EU register derives its automatic statuses from it.
type Evidence struct {
	Records, Signed, PersonallySigned, Sealed, Operational int
	Passports, PassportsWithInputs                         int
	// Linkable passports can name inputs (everything but the chain origin); Linked is how many do,
	// a road section counting when a delivery was laid on it. Recycled* is the waste-origin subset.
	Linkable, Linked, Recycled, RecycledLinked int
	Lots, LotsTested, LotsReleased             int
	LabTests, LabPassed                        int
	ShipmentsIn, Receipted, Reviewed, Rejected int
	Deliveries                                 int
	OpenBlockers, OpenWarnings, StaleBlockers  int
	Members, Admins, MembersWithKey            int
	OrgVerified, OrgKeyOnLedger                bool
	Limits, Calibrations                       int
	AuditEntries                               int
	DisabledNotErased                          int
}

// ComplianceEvidence gathers Evidence for an organisation since `since`.
func (s *Store) ComplianceEvidence(ctx context.Context, orgID string, since time.Time) (Evidence, error) {
	var e Evidence
	err := s.DB.QueryRow(ctx, `
		WITH mine AS (SELECT * FROM records WHERE latest AND org_id=$1 AND occurred_at >= $2),
		lots AS (SELECT id FROM mine WHERE type IN ('reject_lot','recycled_lot','additive_batch','asphalt_lot')),
		tested AS (SELECT DISTINCT body->>'subject_id' AS id FROM mine WHERE type='lab_test'),
		released AS (SELECT DISTINCT body->>'subject_id' AS id FROM mine WHERE type='release')
		SELECT
			count(*), count(*) FILTER (WHERE signature IS NOT NULL), count(*) FILTER (WHERE actor_signature IS NOT NULL),
			count(*) FILTER (WHERE seal_id IS NOT NULL AND EXISTS (SELECT 1 FROM seals se WHERE se.id = mine.seal_id AND se.status='sealed')),
			count(*) FILTER (WHERE provenance='Operational'),
			count(*) FILTER (WHERE type IN ('reject_lot','recycled_lot','additive_batch','asphalt_lot','road_section')),
			count(*) FILTER (WHERE type IN ('recycled_lot','additive_batch','asphalt_lot','road_section') AND jsonb_typeof(inputs)='array' AND jsonb_array_length(inputs) > 0),
			(SELECT count(*) FROM lots), (SELECT count(*) FROM lots WHERE id IN (SELECT id FROM tested)), (SELECT count(*) FROM lots WHERE id IN (SELECT id FROM released)),
			count(*) FILTER (WHERE type='lab_test'), count(*) FILTER (WHERE type='lab_test' AND body->>'result'='pass'),
			count(*) FILTER (WHERE type='delivery'),
			count(*) FILTER (WHERE type IN ('recycled_lot','additive_batch','asphalt_lot','road_section')),
			count(*) FILTER (WHERE (type IN ('recycled_lot','additive_batch','asphalt_lot') AND jsonb_typeof(inputs)='array' AND jsonb_array_length(inputs) > 0)
				OR (type='road_section' AND ((jsonb_typeof(inputs)='array' AND jsonb_array_length(inputs) > 0)
					OR EXISTS (SELECT 1 FROM mine d WHERE d.type='delivery' AND d.body->>'section_id'=mine.id)))),
			count(*) FILTER (WHERE type IN ('recycled_lot','additive_batch')),
			count(*) FILTER (WHERE type IN ('recycled_lot','additive_batch') AND jsonb_typeof(inputs)='array' AND jsonb_array_length(inputs) > 0)
		FROM mine`, orgID, since).Scan(&e.Records, &e.Signed, &e.PersonallySigned, &e.Sealed, &e.Operational,
		&e.Passports, &e.PassportsWithInputs, &e.Lots, &e.LotsTested, &e.LotsReleased, &e.LabTests, &e.LabPassed, &e.Deliveries, &e.Linkable, &e.Linked, &e.Recycled, &e.RecycledLinked)
	if err != nil {
		return e, err
	}
	err = s.DB.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE received_at IS NOT NULL), count(*) FILTER (WHERE rcpt_decision IS NOT NULL),
			count(*) FILTER (WHERE rcpt_decision='rejected')
		FROM records WHERE latest AND type='shipment' AND counterparty=$1 AND occurred_at >= $2`, orgID, since).Scan(&e.ShipmentsIn, &e.Receipted, &e.Reviewed, &e.Rejected)
	if err != nil {
		return e, err
	}
	err = s.DB.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM findings WHERE org_id=$1 AND status='open' AND severity='blocker'),
			(SELECT count(*) FROM findings WHERE org_id=$1 AND status='open' AND severity='warning'),
			(SELECT count(*) FROM findings WHERE org_id=$1 AND status='open' AND severity='blocker' AND created_at < now() - interval '30 days'),
			(SELECT count(*) FROM users WHERE org_id=$1 AND disabled_at IS NULL),
			(SELECT count(*) FROM users WHERE org_id=$1 AND disabled_at IS NULL AND role='admin'),
			(SELECT count(DISTINCT user_id) FROM signing_keys WHERE org_id=$1 AND user_id IS NOT NULL AND status='active' AND ledger_tx_id IS NOT NULL),
			(SELECT status='verified' FROM orgs WHERE id=$1),
			EXISTS (SELECT 1 FROM signing_keys WHERE org_id=$1 AND user_id IS NULL AND status='active' AND ledger_tx_id IS NOT NULL),
			(SELECT count(*) FROM compliance_limits WHERE org_id=$1 AND enabled),
			(SELECT count(*) FROM compliance_calibrations WHERE org_id=$1 AND at >= $2),
			(SELECT count(*) FROM audit_log WHERE org_id=$1 AND at >= $2),
			(SELECT count(*) FROM users WHERE org_id=$1 AND disabled_at < now() - interval '30 days' AND email NOT LIKE 'erased+%')`, orgID, since).
		Scan(&e.OpenBlockers, &e.OpenWarnings, &e.StaleBlockers, &e.Members, &e.Admins, &e.MembersWithKey, &e.OrgVerified, &e.OrgKeyOnLedger,
			&e.Limits, &e.Calibrations, &e.AuditEntries, &e.DisabledNotErased)
	return e, err
}

// HasRecords reports whether an organisation has any record, of any provenance, in [from, to).
func (s *Store) HasRecords(ctx context.Context, orgID string, from, to time.Time) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM records WHERE latest AND org_id=$1 AND occurred_at >= $2 AND occurred_at < $3)`, orgID, from, to).Scan(&ok)
	return ok, err
}

// HasOperational reports whether an organisation has operational (not
// simulated) records in [from, to).
func (s *Store) HasOperational(ctx context.Context, orgID string, from, to time.Time) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM records WHERE latest AND org_id=$1 AND provenance='Operational' AND occurred_at >= $2 AND occurred_at < $3)`, orgID, from, to).Scan(&ok)
	return ok, err
}

// ---------------------------------------------------------------- data protection (GDPR)

// PrivacyRequest logs an access or erasure request.
type PrivacyRequest struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"orgId"`
	SubjectUser string    `json:"subjectUser"`
	Kind        string    `json:"kind"`
	RequestedBy string    `json:"requestedBy"`
	At          time.Time `json:"at"`
}

// PrivacyRequests lists an organisation's requests, newest first.
func (s *Store) PrivacyRequests(ctx context.Context, orgID string) ([]PrivacyRequest, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, org_id, subject_user, kind, requested_by, at FROM privacy_requests WHERE org_id=$1 ORDER BY at DESC LIMIT 200`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PrivacyRequest{}
	for rows.Next() {
		var p PrivacyRequest
		if err := rows.Scan(&p.ID, &p.OrgID, &p.SubjectUser, &p.Kind, &p.RequestedBy, &p.At); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// LogPrivacyRequest records an access or erasure request.
func (s *Store) LogPrivacyRequest(ctx context.Context, p PrivacyRequest, detail any) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO privacy_requests (id, org_id, subject_user, kind, requested_by, detail) VALUES ($1,$2,$3,$4,$5,$6)`,
		p.ID, p.OrgID, p.SubjectUser, p.Kind, p.RequestedBy, detail)
	return err
}

// PersonalData is everything ROADPASS holds about one person (GDPR art. 15/20).
type PersonalData struct {
	Account       User             `json:"account"`
	Preferences   map[string]any   `json:"notificationPreferences,omitempty"`
	Devices       []map[string]any `json:"pushDevices"`
	Notifications []map[string]any `json:"notifications"`
	Keys          []map[string]any `json:"signingKeys"`
	Activity      []map[string]any `json:"activity"`
	Signed        map[string]int   `json:"recordsSigned"`
}

// ExportPersonalData collects a person's data in the organisation.
func (s *Store) ExportPersonalData(ctx context.Context, orgID, userID string) (PersonalData, error) {
	var d PersonalData
	u, err := s.UserByID(ctx, userID)
	if err != nil {
		return d, err
	}
	if u.OrgID != orgID {
		return d, ErrNotFound
	}
	d.Account = u
	collect := func(q string, args ...any) ([]map[string]any, error) {
		rows, err := s.DB.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowToMap)
	}
	if d.Devices, err = collect(`SELECT id, user_agent, created_at, last_ok_at FROM push_subscriptions WHERE user_id=$1`, userID); err != nil {
		return d, err
	}
	if d.Notifications, err = collect(`SELECT kind, title, body, created_at, read_at FROM notifications WHERE user_id=$1 ORDER BY created_at DESC LIMIT 1000`, userID); err != nil {
		return d, err
	}
	if d.Keys, err = collect(`SELECT id, encode(public_key, 'base64') AS public_key, custody, status, created_at, revoked_at FROM signing_keys WHERE user_id=$1`, userID); err != nil {
		return d, err
	}
	if d.Activity, err = collect(`SELECT at, action, subject FROM audit_log WHERE actor=$1 ORDER BY at DESC LIMIT 5000`, userID); err != nil {
		return d, err
	}
	prefs, err := collect(`SELECT browser, email, min_severity, quiet_from, quiet_to, muted FROM notification_prefs WHERE user_id=$1`, userID)
	if err != nil {
		return d, err
	}
	if len(prefs) > 0 {
		d.Preferences = prefs[0]
	}
	d.Signed = map[string]int{}
	rows, err := s.DB.Query(ctx, `SELECT type, count(*) FROM records WHERE latest AND (created_by=$1 OR received_by=$1) GROUP BY type`, userID)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		var n int
		if err := rows.Scan(&t, &n); err != nil {
			return d, err
		}
		d.Signed[t] = n
	}
	return d, rows.Err()
}

// ErasePerson removes a former member's personal data (GDPR art. 17). The
// account must already be disabled. Records they signed stay: they are part of
// passports other companies rely on and refer to the person only by a
// pseudonymous id, which is kept so signatures still verify.
func (s *Store) ErasePerson(ctx context.Context, orgID, userID string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	t, err := tx.Exec(ctx, `UPDATE users SET name='Former member', email='erased+' || id || '@invalid', password_hash='!', timezone='', duties='{}'
		WHERE id=$1 AND org_id=$2 AND disabled_at IS NOT NULL`, userID, orgID)
	if err != nil {
		return err
	}
	if t.RowsAffected() == 0 {
		return ErrConflict
	}
	for _, q := range []string{
		`DELETE FROM notifications WHERE user_id=$1`, `DELETE FROM push_subscriptions WHERE user_id=$1`,
		`DELETE FROM notification_prefs WHERE user_id=$1`, `DELETE FROM user_layouts WHERE user_id=$1`,
		`DELETE FROM work_assignments WHERE user_id=$1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// FormerMembers lists an organisation's disabled accounts ("" = every organisation).
func (s *Store) FormerMembers(ctx context.Context, orgID string) ([]User, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+userCols+` FROM users WHERE ($1='' OR org_id=$1) AND disabled_at IS NOT NULL ORDER BY disabled_at DESC LIMIT 500`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
