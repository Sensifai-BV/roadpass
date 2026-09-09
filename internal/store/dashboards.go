package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Dashboard is a layout of widgets an organisation's admins design for their
// team. Widgets is opaque here; the API validates its shape.
type Dashboard struct {
	ID          string          `json:"id"`
	OrgID       string          `json:"orgId"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Audience    []string        `json:"audience"`
	Position    int             `json:"position"`
	Widgets     json.RawMessage `json:"widgets"`
	Version     int             `json:"version"`
	CreatedBy   string          `json:"createdBy"`
	UpdatedBy   string          `json:"updatedBy"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// Sees reports whether a member with role and duties is in the audience.
func (d Dashboard) Sees(role string, duties []string) bool { return d.Match(role, "", duties) > 0 }

// Match ranks how specifically a template targets a member: 3 for their named
// job role, 2 for their access level or one of their duties, 1 for everyone,
// 0 when it does not apply. The most specific template wins.
func (d Dashboard) Match(role, jobRole string, duties []string) int {
	best := 0
	for _, a := range d.Audience {
		n := 0
		switch {
		case jobRole != "" && a == "jobrole:"+jobRole:
			n = 3
		case a == "role:"+role, len(a) > 5 && a[:5] == "duty:" && slices.Contains(duties, a[5:]):
			n = 2
		case a == "all":
			n = 1
		}
		best = max(best, n)
	}
	return best
}

const dashCols = `id, org_id, name, description, audience, position, widgets, version, created_by, updated_by, created_at, updated_at`

func scanDash(row pgx.Row) (Dashboard, error) {
	var d Dashboard
	var w []byte
	err := row.Scan(&d.ID, &d.OrgID, &d.Name, &d.Description, &d.Audience, &d.Position, &w, &d.Version, &d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	d.Widgets = w
	return d, err
}

// Dashboards lists an organisation's dashboards in display order.
func (s *Store) Dashboards(ctx context.Context, orgID string) ([]Dashboard, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+dashCols+` FROM dashboards WHERE org_id = $1 ORDER BY position, created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Dashboard{}
	for rows.Next() {
		d, err := scanDash(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Dashboard returns one of an organisation's dashboards.
func (s *Store) Dashboard(ctx context.Context, orgID, id string) (Dashboard, error) {
	return scanDash(s.DB.QueryRow(ctx, `SELECT `+dashCols+` FROM dashboards WHERE org_id = $1 AND id = $2`, orgID, id))
}

// CreateDashboard stores a new dashboard at the end of the list.
func (s *Store) CreateDashboard(ctx context.Context, d Dashboard) (Dashboard, error) {
	if d.Audience == nil {
		d.Audience = []string{}
	}
	return scanDash(s.DB.QueryRow(ctx, `INSERT INTO dashboards (id, org_id, name, description, audience, position, widgets, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, (SELECT coalesce(max(position) + 1, 0) FROM dashboards WHERE org_id = $2), $6, $7, $7)
		RETURNING `+dashCols, d.ID, d.OrgID, d.Name, d.Description, d.Audience, []byte(d.Widgets), d.CreatedBy))
}

// UpdateDashboard replaces a dashboard if it is still at version; otherwise
// ErrConflict (someone else saved first).
func (s *Store) UpdateDashboard(ctx context.Context, d Dashboard, version int) (Dashboard, error) {
	if d.Audience == nil {
		d.Audience = []string{}
	}
	out, err := scanDash(s.DB.QueryRow(ctx, `UPDATE dashboards SET name = $3, description = $4, audience = $5, position = $6, widgets = $7,
			version = version + 1, updated_by = $8, updated_at = now()
		WHERE org_id = $1 AND id = $2 AND version = $9 RETURNING `+dashCols,
		d.OrgID, d.ID, d.Name, d.Description, d.Audience, d.Position, []byte(d.Widgets), d.UpdatedBy, version))
	if errors.Is(err, ErrNotFound) {
		if _, e := s.Dashboard(ctx, d.OrgID, d.ID); e == nil {
			return out, ErrConflict
		}
	}
	return out, err
}

// DeleteDashboard removes a dashboard.
func (s *Store) DeleteDashboard(ctx context.Context, orgID, id string) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM dashboards WHERE org_id = $1 AND id = $2`, orgID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
