package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sensifai/roadpass/internal/domain"
)

// SetupNeeded reports a fresh installation: no active admin in a platform
// (operator) organisation yet.
func (s *Store) SetupNeeded(ctx context.Context) (bool, error) {
	var exists bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN orgs o ON o.id = u.org_id
		WHERE o.kind = 'operator' AND u.role = 'admin' AND u.disabled_at IS NULL)`).Scan(&exists)
	return !exists, err
}

// Plan is a price plan.
type Plan struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	PriceCents      int64  `json:"priceCents"`
	Currency        string `json:"currency"`
	Seats           int    `json:"seats"`           // -1 unlimited
	RecordsPerMonth int    `json:"recordsPerMonth"` // -1 unlimited
	Position        int    `json:"position"`
	Active          bool   `json:"active"`
}

// Plans lists the price plans.
func (s *Store) Plans(ctx context.Context) ([]Plan, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, name, description, price_cents, currency, seats, records_per_month, position, active FROM plans ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Plan, error) {
		var p Plan
		err := r.Scan(&p.ID, &p.Name, &p.Description, &p.PriceCents, &p.Currency, &p.Seats, &p.RecordsPerMonth, &p.Position, &p.Active)
		return p, err
	})
}

// UpdatePlan changes a plan's terms (existing invoices keep theirs).
func (s *Store) UpdatePlan(ctx context.Context, p Plan) error {
	tag, err := s.DB.Exec(ctx, `UPDATE plans SET name=$2, description=$3, price_cents=$4, currency=$5, seats=$6, records_per_month=$7, active=$8 WHERE id=$1`,
		p.ID, p.Name, p.Description, p.PriceCents, p.Currency, p.Seats, p.RecordsPerMonth, p.Active)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Subscription is an organisation's plan.
type Subscription struct {
	OrgID       string     `json:"orgId"`
	PlanID      string     `json:"planId"`
	Status      string     `json:"status"` // trial | active | past_due | cancelled
	TrialEndsAt *time.Time `json:"trialEndsAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// SetSubscription creates or changes an organisation's subscription.
func (s *Store) SetSubscription(ctx context.Context, sub Subscription, by string) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO subscriptions (org_id, plan_id, status, trial_ends_at, updated_by) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (org_id) DO UPDATE SET plan_id=EXCLUDED.plan_id, status=EXCLUDED.status, trial_ends_at=EXCLUDED.trial_ends_at,
			updated_at=now(), updated_by=EXCLUDED.updated_by`, sub.OrgID, sub.PlanID, sub.Status, sub.TrialEndsAt, by)
	if isForeignKey(err) {
		return fmt.Errorf("%w: unknown plan or organisation", ErrNotFound)
	}
	return err
}

// Usage is what an organisation used in one month.
type Usage struct {
	Records int `json:"records"` // operational records created (simulated data is not billed)
	Seats   int `json:"seats"`   // active members
}

// UsageByOrg meters every organisation for the month starting at period.
func (s *Store) UsageByOrg(ctx context.Context, period time.Time) (map[string]Usage, error) {
	out := map[string]Usage{}
	rows, err := s.DB.Query(ctx, `SELECT o.id,
			(SELECT count(*) FROM records r WHERE r.org_id = o.id AND r.provenance <> 'Simulated' AND r.version = 1
				AND r.created_at >= $1 AND r.created_at < $1::timestamptz + interval '1 month'),
			(SELECT count(*) FROM users u WHERE u.org_id = o.id AND u.disabled_at IS NULL)
		FROM orgs o`, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var u Usage
		if err := rows.Scan(&id, &u.Records, &u.Seats); err != nil {
			return nil, err
		}
		out[id] = u
	}
	return out, rows.Err()
}

// OrgSummary is one row of the platform's organisation list.
type OrgSummary struct {
	domain.Org
	Members      int           `json:"members"`
	Subscription *Subscription `json:"subscription,omitempty"`
	Usage        Usage         `json:"usage"`
	Records      int64         `json:"records"` // all records, all time
}

// PlatformOrgs lists every organisation with its plan and this month's usage.
func (s *Store) PlatformOrgs(ctx context.Context) ([]OrgSummary, error) {
	orgs, err := s.Orgs(ctx)
	if err != nil {
		return nil, err
	}
	usage, err := s.UsageByOrg(ctx, MonthStart(time.Now()))
	if err != nil {
		return nil, err
	}
	subs := map[string]*Subscription{}
	rows, err := s.DB.Query(ctx, `SELECT org_id, plan_id, status, trial_ends_at, updated_at FROM subscriptions`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x Subscription
		if err := rows.Scan(&x.OrgID, &x.PlanID, &x.Status, &x.TrialEndsAt, &x.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		subs[x.OrgID] = &x
	}
	rows.Close()
	totals := map[string]int64{}
	rows, err = s.DB.Query(ctx, `SELECT org_id, count(*) FROM records WHERE latest GROUP BY org_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		totals[id] = n
	}
	rows.Close()
	out := make([]OrgSummary, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, OrgSummary{Org: o, Members: usage[o.ID].Seats, Subscription: subs[o.ID], Usage: usage[o.ID], Records: totals[o.ID]})
	}
	return out, nil
}

// UserRow is a user with the organisation they belong to.
type UserRow struct {
	User
	OrgName string         `json:"orgName"`
	OrgKind domain.OrgKind `json:"orgKind"`
	OrgDemo bool           `json:"orgDemo,omitempty"`
}

// PlatformUsers searches users across organisations (q matches name or e-mail).
func (s *Store) PlatformUsers(ctx context.Context, q, orgID string, includeDisabled bool, limit int) ([]UserRow, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+prefixed("u", userCols)+`, o.name, o.kind, o.demo FROM users u JOIN orgs o ON o.id = u.org_id
		WHERE ($1 = '' OR u.name ILIKE '%' || $1 || '%' OR u.email ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR u.org_id = $2) AND ($3 OR u.disabled_at IS NULL)
		ORDER BY o.name, u.name LIMIT $4`, q, orgID, includeDisabled, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var x UserRow
		var kind string
		if err := rows.Scan(&x.ID, &x.OrgID, &x.Email, &x.Name, &x.Role, &x.Duties, &x.PasswordHash, &x.Demo, &x.DisabledAt,
			&x.LastLoginAt, &x.CreatedAt, &x.Timezone, &x.Steps, &x.OrgName, &kind, &x.OrgDemo); err != nil {
			return nil, err
		}
		x.OrgKind = domain.OrgKind(kind)
		out = append(out, x)
	}
	return out, rows.Err()
}

// SetUserDisabled disables or re-enables any user (platform manager).
func (s *Store) SetUserDisabled(ctx context.Context, id string, disabled bool) error {
	tag, err := s.DB.Exec(ctx, `UPDATE users SET disabled_at = CASE WHEN $2 THEN coalesce(disabled_at, now()) ELSE NULL END WHERE id=$1`, id, disabled)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Invoice is one organisation's bill for one month.
type Invoice struct {
	ID          string          `json:"id"`
	Number      string          `json:"number,omitempty"`
	OrgID       string          `json:"orgId"`
	Period      time.Time       `json:"period"`
	PlanID      string          `json:"planId"`
	Currency    string          `json:"currency"`
	AmountCents int64           `json:"amountCents"`
	Lines       json.RawMessage `json:"lines"`
	Status      string          `json:"status"` // draft | issued | paid | void
	IssuedAt    *time.Time      `json:"issuedAt,omitempty"`
	PaidAt      *time.Time      `json:"paidAt,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
}

const invoiceCols = `id, COALESCE(number,''), org_id, period, plan_id, currency, amount_cents, lines, status, issued_at, paid_at, created_at`

func scanInvoice(r pgx.Row) (Invoice, error) {
	var x Invoice
	var lines []byte
	err := r.Scan(&x.ID, &x.Number, &x.OrgID, &x.Period, &x.PlanID, &x.Currency, &x.AmountCents, &lines, &x.Status, &x.IssuedAt, &x.PaidAt, &x.CreatedAt)
	x.Lines = lines
	return x, notFound(err)
}

// Invoices lists invoices, optionally for one organisation and/or one month.
func (s *Store) Invoices(ctx context.Context, orgID string, period *time.Time) ([]Invoice, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+invoiceCols+` FROM invoices WHERE ($1 = '' OR org_id = $1) AND ($2::date IS NULL OR period = $2)
		ORDER BY period DESC, org_id`, orgID, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		x, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// InvoiceLine is one priced line.
type InvoiceLine struct {
	Text        string `json:"text"`
	Quantity    int    `json:"quantity"`
	UnitCents   int64  `json:"unitCents"`
	AmountCents int64  `json:"amountCents"`
}

// GenerateInvoices drafts the month's invoice for every subscribed organisation
// that does not have one yet. Trials and cancelled subscriptions are not billed.
// Returns the number drafted.
func (s *Store) GenerateInvoices(ctx context.Context, period time.Time, newID func() string) (int, error) {
	period = MonthStart(period)
	usage, err := s.UsageByOrg(ctx, period)
	if err != nil {
		return 0, err
	}
	rows, err := s.DB.Query(ctx, `SELECT s.org_id, p.id, p.name, p.price_cents, p.currency, p.records_per_month, p.seats
		FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		WHERE s.status IN ('active', 'past_due') AND NOT EXISTS (SELECT 1 FROM invoices i WHERE i.org_id = s.org_id AND i.period = $1)`, period)
	if err != nil {
		return 0, err
	}
	type due struct {
		org, plan, name, currency string
		price                     int64
		records, seats            int
	}
	var dues []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.org, &d.plan, &d.name, &d.price, &d.currency, &d.records, &d.seats); err != nil {
			rows.Close()
			return 0, err
		}
		dues = append(dues, d)
	}
	rows.Close()
	n := 0
	for _, d := range dues {
		u := usage[d.org]
		lines := []InvoiceLine{{Text: fmt.Sprintf("%s plan, %s", d.name, period.Format("January 2006")), Quantity: 1, UnitCents: d.price, AmountCents: d.price}}
		lines = append(lines, InvoiceLine{Text: fmt.Sprintf("Usage: %d operational records, %d members (included up to %s and %s)",
			u.Records, u.Seats, limitText(d.records), limitText(d.seats)), Quantity: 1})
		var total int64
		for _, l := range lines {
			total += l.AmountCents
		}
		b, _ := json.Marshal(lines)
		if _, err := s.DB.Exec(ctx, `INSERT INTO invoices (id, org_id, period, plan_id, currency, amount_cents, lines, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'draft') ON CONFLICT (org_id, period) DO NOTHING`, newID(), d.org, period, d.plan, d.currency, total, b); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func limitText(n int) string {
	if n < 0 {
		return "unlimited"
	}
	return fmt.Sprint(n)
}

// SetInvoiceStatus moves an invoice draft → issued → paid, or voids it. Issuing
// assigns the next invoice number.
func (s *Store) SetInvoiceStatus(ctx context.Context, id, status string) (Invoice, error) {
	cur, err := scanInvoice(s.DB.QueryRow(ctx, `SELECT `+invoiceCols+` FROM invoices WHERE id=$1`, id))
	if err != nil {
		return cur, err
	}
	allowed := map[string][]string{"draft": {"issued", "void"}, "issued": {"paid", "void"}}
	ok := false
	for _, to := range allowed[cur.Status] {
		ok = ok || to == status
	}
	if !ok {
		return cur, fmt.Errorf("%w: an invoice that is %s cannot become %s", ErrConflict, cur.Status, status)
	}
	q := `UPDATE invoices SET status=$2 WHERE id=$1 RETURNING ` + invoiceCols
	switch status {
	case "issued":
		q = `UPDATE invoices SET status=$2, issued_at=now(),
			number = 'RP-' || to_char(now(), 'YYYY') || '-' || lpad(nextval('invoice_number_seq')::text, 4, '0') WHERE id=$1 RETURNING ` + invoiceCols
	case "paid":
		q = `UPDATE invoices SET status=$2, paid_at=now() WHERE id=$1 RETURNING ` + invoiceCols
	}
	return scanInvoice(s.DB.QueryRow(ctx, q, id, status))
}

// MonthStart is the first instant of t's month in UTC.
func MonthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// prefixed qualifies a column list with a table alias.
func prefixed(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

// SeatLimit is the member allowance of an organisation's current plan
// (limit -1 = unlimited; ok false without a plan).
func (s *Store) SeatLimit(ctx context.Context, orgID string) (limit int, plan string, ok bool, err error) {
	err = s.DB.QueryRow(ctx, `SELECT p.seats, p.name FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		WHERE s.org_id = $1 AND s.status <> 'cancelled'`, orgID).Scan(&limit, &plan)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, nil
	}
	return limit, plan, err == nil, err
}
