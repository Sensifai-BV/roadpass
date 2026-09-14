package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sensifai/roadpass/internal/domain"
)

// WorkPlan is work a company expects to do. It is not evidence: plans are never
// signed or sealed, and they can be edited until they are done.
type WorkPlan struct {
	ID           string            `json:"id"`
	OrgID        string            `json:"orgId"`
	RecordType   domain.RecordType `json:"recordType"`
	Title        string            `json:"title"`
	SiteID       string            `json:"siteId,omitempty"`
	Counterparty string            `json:"counterpartyOrgId,omitempty"`
	ToSiteID     string            `json:"toSiteId,omitempty"` // shipments: the receiving site
	OwnerOrgID   string            `json:"ownerOrgId,omitempty"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       *time.Time        `json:"endsAt,omitempty"`
	MassKg       float64           `json:"massKg"`
	Note         string            `json:"note"`
	Status       string            `json:"status"` // planned, done, cancelled
	RecordID     string            `json:"recordId,omitempty"`
	DoneAt       *time.Time        `json:"doneAt,omitempty"`
	DoneBy       string            `json:"doneBy,omitempty"`
	CreatedBy    string            `json:"createdBy"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedBy    string            `json:"updatedBy"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	Overdue      bool              `json:"overdue"` // still planned after its time
}

// WorkPlanStatuses a plan can be in.
var WorkPlanStatuses = []string{"planned", "done", "cancelled"}

const planCols = `id, org_id, record_type, title, COALESCE(site_id,''), COALESCE(counterparty,''), COALESCE(owner_org_id,''),
	starts_at, ends_at, mass_kg, note, status, COALESCE(record_id,''), done_at, COALESCE(done_by,''),
	created_by, created_at, updated_by, updated_at, COALESCE(to_site_id,'')`

func scanPlan(row pgx.Row) (WorkPlan, error) {
	var p WorkPlan
	var rt string
	err := row.Scan(&p.ID, &p.OrgID, &rt, &p.Title, &p.SiteID, &p.Counterparty, &p.OwnerOrgID,
		&p.StartsAt, &p.EndsAt, &p.MassKg, &p.Note, &p.Status, &p.RecordID, &p.DoneAt, &p.DoneBy,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedBy, &p.UpdatedAt, &p.ToSiteID)
	p.RecordType = domain.RecordType(rt)
	p.Overdue = p.Status == "planned" && p.due().Before(time.Now())
	return p, err
}

// due is when the planned work should have finished.
func (p WorkPlan) due() time.Time {
	if p.EndsAt != nil {
		return *p.EndsAt
	}
	return p.StartsAt
}

// planVisible restricts plans to the companies involved: the planner, the
// receiver and the road owner. Oversight viewers see every plan.
func planVisible(v Viewer, n int) (string, []any) {
	if v.All() {
		return "TRUE", nil
	}
	return fmt.Sprintf("(org_id = $%d OR counterparty = $%d OR owner_org_id = $%d)", n, n, n), []any{v.OrgID}
}

// WorkPlanFilter narrows a plan listing.
type WorkPlanFilter struct {
	From, To time.Time // plans whose time span overlaps [From, To)
	Status   string
	Limit    int
}

// WorkPlans lists the plans a viewer may see, in time order.
func (s *Store) WorkPlans(ctx context.Context, v Viewer, f WorkPlanFilter) ([]WorkPlan, error) {
	vis, args := planVisible(v, 1)
	where := []string{vis}
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if !f.From.IsZero() {
		add("COALESCE(ends_at, starts_at) >= ?", f.From)
	}
	if !f.To.IsZero() {
		add("starts_at < ?", f.To)
	}
	if f.Status != "" {
		add("status = ?", f.Status)
	}
	limit := f.Limit
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	args = append(args, limit)
	rows, err := s.DB.Query(ctx, fmt.Sprintf(`SELECT %s FROM work_plans WHERE %s ORDER BY starts_at, id LIMIT $%d`,
		planCols, strings.Join(where, " AND "), len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkPlan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// WorkPlan returns one plan the viewer may see.
func (s *Store) WorkPlan(ctx context.Context, v Viewer, id string) (WorkPlan, error) {
	vis, args := planVisible(v, 2)
	p, err := scanPlan(s.DB.QueryRow(ctx, `SELECT `+planCols+` FROM work_plans WHERE id = $1 AND `+vis, append([]any{id}, args...)...))
	return p, notFound(err)
}

// CreateWorkPlan stores a new plan and returns it with its id.
func (s *Store) CreateWorkPlan(ctx context.Context, p WorkPlan) (WorkPlan, error) {
	id, err := NextID(ctx, s.DB, "PLN", p.StartsAt.Year())
	if err != nil {
		return WorkPlan{}, err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO work_plans (id, org_id, record_type, title, site_id, counterparty, owner_org_id,
			starts_at, ends_at, mass_kg, note, created_by, updated_by, to_site_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12,$13)`,
		id, p.OrgID, string(p.RecordType), p.Title, nullIf(p.SiteID), nullIf(p.Counterparty), nullIf(p.OwnerOrgID),
		p.StartsAt, p.EndsAt, p.MassKg, p.Note, p.CreatedBy, nullIf(p.ToSiteID))
	if err != nil {
		if isForeignKey(err) {
			return WorkPlan{}, fmt.Errorf("%w: unknown site or company", ErrConflict)
		}
		return WorkPlan{}, err
	}
	return s.WorkPlan(ctx, SystemViewer, id)
}

// UpdateWorkPlan changes the details of an open plan of the organisation.
func (s *Store) UpdateWorkPlan(ctx context.Context, orgID string, p WorkPlan) (WorkPlan, error) {
	tag, err := s.DB.Exec(ctx, `UPDATE work_plans SET record_type=$3, title=$4, site_id=$5, counterparty=$6, owner_org_id=$7,
			starts_at=$8, ends_at=$9, mass_kg=$10, note=$11, updated_by=$12, updated_at=now(), to_site_id=$13
		WHERE id=$1 AND org_id=$2 AND status='planned'`,
		p.ID, orgID, string(p.RecordType), p.Title, nullIf(p.SiteID), nullIf(p.Counterparty), nullIf(p.OwnerOrgID),
		p.StartsAt, p.EndsAt, p.MassKg, p.Note, p.UpdatedBy, nullIf(p.ToSiteID))
	if err != nil {
		if isForeignKey(err) {
			return WorkPlan{}, fmt.Errorf("%w: unknown site or company", ErrConflict)
		}
		return WorkPlan{}, err
	}
	if tag.RowsAffected() == 0 {
		return WorkPlan{}, fmt.Errorf("%w: only open plans of your company can be edited", ErrConflict)
	}
	return s.WorkPlan(ctx, SystemViewer, p.ID)
}

// SetWorkPlanStatus marks a plan done (optionally linked to the record that did the
// work), cancelled, or open again.
func (s *Store) SetWorkPlanStatus(ctx context.Context, orgID, id, status, recordID, actor string) (WorkPlan, error) {
	var err error
	switch status {
	case "done":
		_, err = s.DB.Exec(ctx, `UPDATE work_plans SET status='done', record_id=$3, done_at=now(), done_by=$4, updated_by=$4, updated_at=now()
			WHERE id=$1 AND org_id=$2`, id, orgID, nullIf(recordID), actor)
	default:
		_, err = s.DB.Exec(ctx, `UPDATE work_plans SET status=$3, record_id=NULL, done_at=NULL, done_by=NULL, updated_by=$4, updated_at=now()
			WHERE id=$1 AND org_id=$2`, id, orgID, status, actor)
	}
	if isUnique(err) {
		return WorkPlan{}, fmt.Errorf("%w: that record is already linked to another plan", ErrConflict)
	}
	if err != nil {
		return WorkPlan{}, err
	}
	return s.WorkPlan(ctx, Viewer{OrgID: orgID}, id)
}

// WorkPlanCandidates are records of the plan's company and type, near its time and
// not linked to another plan: the likely real record for it.
func (s *Store) WorkPlanCandidates(ctx context.Context, p WorkPlan, window time.Duration) ([]domain.Record, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+recCols+` FROM records WHERE latest AND org_id=$1 AND type=$2
			AND occurred_at BETWEEN $3 AND $4
			AND NOT EXISTS (SELECT 1 FROM work_plans x WHERE x.record_id = records.id AND x.id <> $5)
		ORDER BY abs(extract(epoch FROM occurred_at - $6)) LIMIT 20`,
		p.OrgID, string(p.RecordType), p.StartsAt.Add(-window), p.due().Add(window), p.ID, p.StartsAt)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// CalendarEvent is one record placed on the calendar: when it happened, or when
// a shipment was received.
type CalendarEvent struct {
	Kind         string            `json:"kind"` // the record type, or "receipt"
	RecordID     string            `json:"recordId"`
	RecordType   domain.RecordType `json:"recordType"`
	Ref          string            `json:"ref"`
	At           time.Time         `json:"at"`
	OrgID        string            `json:"orgId"`
	Counterparty string            `json:"counterpartyOrgId,omitempty"`
	OwnerOrgID   string            `json:"ownerOrgId,omitempty"`
	SiteID       string            `json:"siteId,omitempty"`
	MassKg       float64           `json:"massKg"`
	Status       domain.Status     `json:"status"`
	Decision     string            `json:"decision,omitempty"` // receipts: the intake decision
	Root         string            `json:"root,omitempty"`     // the passport the record belongs to
}

// calendarRoot is the passport a record opens: its own for lots and sections,
// the section a delivery was laid on, the lot a shipment carried, the subject of a
// test or release.
const calendarRoot = `CASE WHEN type IN ('reject_lot','recycled_lot','additive_batch','asphalt_lot','road_section') THEN id
	WHEN type = 'delivery' THEN body->>'section_id'
	WHEN type = 'shipment' THEN inputs->0->>'id'
	ELSE body->>'subject_id' END`

// CalendarEvents returns the records the viewer may see that happened in
// [from, to), plus shipment receipts in that span, in time order.
func (s *Store) CalendarEvents(ctx context.Context, v Viewer, from, to time.Time, limit int) ([]CalendarEvent, bool, error) {
	vis := "TRUE"
	args := []any{from, to, limit + 1}
	if !v.All() {
		vis = "(org_id = $4 OR counterparty = $4 OR (type = 'road_section' AND body->>'owner_org_id' = $4))"
		args = append(args, v.OrgID)
	}
	rows, err := s.DB.Query(ctx, `
		SELECT kind, id, type, ref, at, org_id, COALESCE(counterparty,''), COALESCE(body->>'owner_org_id',''), COALESCE(site_id,''),
		       mass, status, COALESCE(decision,''), COALESCE(root,'')
		FROM (
			SELECT type AS kind, id, type, ref, occurred_at AS at, org_id, counterparty, body, site_id, mass_kg AS mass, status,
			       NULL::text AS decision, `+calendarRoot+` AS root
			FROM records WHERE latest AND occurred_at >= $1 AND occurred_at < $2 AND `+vis+`
			UNION ALL
			SELECT 'receipt', id, type, ref, received_at, org_id, counterparty, body, site_id, COALESCE(received_mass_kg, mass_kg), status,
			       rcpt_decision, `+calendarRoot+`
			FROM records WHERE latest AND type = 'shipment' AND received_at >= $1 AND received_at < $2 AND `+vis+`
		) e ORDER BY at, id LIMIT $3`, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []CalendarEvent{}
	for rows.Next() {
		var e CalendarEvent
		var rt, st string
		if err := rows.Scan(&e.Kind, &e.RecordID, &rt, &e.Ref, &e.At, &e.OrgID, &e.Counterparty, &e.OwnerOrgID, &e.SiteID,
			&e.MassKg, &st, &e.Decision, &e.Root); err != nil {
			return nil, false, err
		}
		e.RecordType, e.Status = domain.RecordType(rt), domain.Status(st)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, nil
}
