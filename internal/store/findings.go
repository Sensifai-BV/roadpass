package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Finding is one data-check result on one record.
type Finding struct {
	ID          int64          `json:"id"`
	RecordID    string         `json:"recordId"`
	RecordType  string         `json:"recordType,omitempty"`
	Code        string         `json:"code"`
	Severity    string         `json:"severity"`
	Message     string         `json:"message"`
	Fix         string         `json:"fix"`
	Detail      map[string]any `json:"detail,omitempty"`
	Status      string         `json:"status"`
	OrgID       string         `json:"orgId"`
	CreatedAt   time.Time      `json:"createdAt"`
	ClosedAt    *time.Time     `json:"closedAt,omitempty"`
	ClosedBy    string         `json:"closedBy,omitempty"`
	CloseReason string         `json:"closeReason,omitempty"`
}

// PutFindings inserts findings, ignoring a code already open on a record.
func PutFindings(ctx context.Context, tx pgx.Tx, fs []Finding) error {
	for _, f := range fs {
		var detail []byte
		if f.Detail != nil {
			detail, _ = json.Marshal(f.Detail)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO findings(record_id,code,severity,message,fix,detail,org_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (record_id, code) DO NOTHING`,
			f.RecordID, f.Code, f.Severity, f.Message, f.Fix, detail, f.OrgID); err != nil {
			return err
		}
	}
	return nil
}

// RaiseFinding opens a finding outside ingest (an intake review) and marks
// the record blocked or flagged.
func (s *Store) RaiseFinding(ctx context.Context, f Finding) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := PutFindings(ctx, tx, []Finding{f}); err != nil {
		return err
	}
	st := "flagged"
	if f.Severity == "blocker" {
		st = "blocked"
	}
	if _, err := tx.Exec(ctx, `UPDATE records SET status=$2 WHERE id=$1 AND latest AND status <> 'blocked'`, f.RecordID, st); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO reproject(id) VALUES ($1) ON CONFLICT (id) DO UPDATE SET queued_at = now()`, f.RecordID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CloseCorrected resolves a record's open findings that its new version no
// longer raises (codes still raised stay open).
func CloseCorrected(ctx context.Context, tx pgx.Tx, recordID string, version int, still []string, actor string) error {
	if still == nil {
		still = []string{}
	}
	_, err := tx.Exec(ctx, `UPDATE findings SET status='resolved', closed_at=now(), closed_by=$3, close_reason=$4
		WHERE record_id=$1 AND status='open' AND NOT (code = ANY($2))`,
		recordID, still, actor, fmt.Sprintf("Fixed in version %d", version))
	return err
}

// CopyFindings bulk-loads findings (seeding).
func (s *Store) CopyFindings(ctx context.Context, fs []Finding) error {
	rows := make([][]any, len(fs))
	for i, f := range fs {
		var detail []byte
		if f.Detail != nil {
			detail, _ = json.Marshal(f.Detail)
		}
		status := f.Status
		if status == "" {
			status = "open"
		}
		rows[i] = []any{f.RecordID, f.Code, f.Severity, f.Message, f.Fix, detail, status, f.OrgID, f.CreatedAt, f.ClosedAt,
			nullIf(f.ClosedBy), nullIf(f.CloseReason)}
	}
	_, err := s.DB.CopyFrom(ctx, pgx.Identifier{"findings"},
		[]string{"record_id", "code", "severity", "message", "fix", "detail", "status", "org_id", "created_at", "closed_at", "closed_by", "close_reason"},
		pgx.CopyFromRows(rows))
	return err
}

// FindingFilter narrows the findings list.
type FindingFilter struct {
	Status   string
	Code     string
	RecordID string
	Limit    int
	Offset   int
}

// partyOf is true when organisation $2 takes part in a finding's record: it
// issued it, received it, owns the road section, or owns the section a
// delivery was laid on. Road owners see (and may accept) findings on their roads.
const partyOf = `(f.org_id = $2 OR EXISTS (SELECT 1 FROM records x WHERE x.id = f.record_id AND x.latest AND (x.counterparty = $2
	OR (x.type = 'road_section' AND x.body->>'owner_org_id' = $2)
	OR (x.type = 'delivery' AND EXISTS (SELECT 1 FROM records y WHERE y.id = x.body->>'section_id' AND y.latest AND y.body->>'owner_org_id' = $2)))))`

// FindingParty reports whether an organisation takes part in a finding's record.
func (s *Store) FindingParty(ctx context.Context, orgID string, id int64) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM findings f WHERE f.id = $1 AND `+partyOf+`)`, id, orgID).Scan(&ok)
	return ok, err
}

// Findings lists findings visible to the viewer, newest first.
func (s *Store) Findings(ctx context.Context, v Viewer, f FindingFilter) ([]Finding, int, error) {
	args := []any{v.All(), v.OrgID, f.Status, f.Code, f.RecordID}
	where := `($1 OR ` + partyOf + `) AND ($3 = '' OR f.status = $3) AND ($4 = '' OR f.code = $4) AND ($5 = '' OR f.record_id = $5)`
	var total int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM findings f WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.Query(ctx, `SELECT f.id, f.record_id, COALESCE(r.type,''), f.code, f.severity, f.message, f.fix, f.detail, f.status,
			f.org_id, f.created_at, f.closed_at, COALESCE(f.closed_by,''), COALESCE(f.close_reason,'')
		FROM findings f LEFT JOIN records r ON r.id = f.record_id AND r.latest
		WHERE `+where+` ORDER BY f.created_at DESC, f.id DESC LIMIT $6 OFFSET $7`, append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Finding
	for rows.Next() {
		var x Finding
		var detail []byte
		if err := rows.Scan(&x.ID, &x.RecordID, &x.RecordType, &x.Code, &x.Severity, &x.Message, &x.Fix, &detail, &x.Status,
			&x.OrgID, &x.CreatedAt, &x.ClosedAt, &x.ClosedBy, &x.CloseReason); err != nil {
			return nil, 0, err
		}
		if len(detail) > 0 {
			_ = json.Unmarshal(detail, &x.Detail)
		}
		out = append(out, x)
	}
	return out, total, rows.Err()
}

// FindingCounts returns open findings per code for the viewer.
func (s *Store) FindingCounts(ctx context.Context, v Viewer) (map[string]map[string]int, error) {
	rows, err := s.DB.Query(ctx, `SELECT f.code, f.status, count(*) FROM findings f WHERE ($1 OR `+partyOf+`) GROUP BY f.code, f.status`, v.All(), v.OrgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]int{}
	for rows.Next() {
		var code, status string
		var n int
		if err := rows.Scan(&code, &status, &n); err != nil {
			return nil, err
		}
		if out[code] == nil {
			out[code] = map[string]int{}
		}
		out[code][status] = n
	}
	return out, rows.Err()
}

// CloseFinding resolves or accepts an open finding the viewer owns, and updates
// the record status when no blocker remains.
func (s *Store) CloseFinding(ctx context.Context, v Viewer, id int64, status, actor, reason string) (Finding, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Finding{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	var f Finding
	err = tx.QueryRow(ctx, `UPDATE findings SET status=$2, closed_at=now(), closed_by=$3, close_reason=$4
		WHERE id=$1 AND status='open' AND ($5 OR org_id=$6) RETURNING id, record_id, code, severity, status, org_id`,
		id, status, actor, reason, v.All(), v.OrgID).Scan(&f.ID, &f.RecordID, &f.Code, &f.Severity, &f.Status, &f.OrgID)
	if err != nil {
		return f, notFound(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE records SET status = CASE
			WHEN EXISTS (SELECT 1 FROM findings WHERE record_id=$1 AND status='open' AND severity='blocker') THEN 'blocked'
			WHEN EXISTS (SELECT 1 FROM findings WHERE record_id=$1 AND status='open') THEN 'flagged'
			WHEN EXISTS (SELECT 1 FROM findings WHERE record_id=$1 AND status='accepted') THEN 'accepted'
			ELSE 'ok' END
		WHERE id=$1 AND latest`, f.RecordID); err != nil {
		return f, err
	}
	// The dashboards leave blocked records out, so a status change is re-projected.
	if _, err := tx.Exec(ctx, `INSERT INTO reproject(id) VALUES ($1) ON CONFLICT (id) DO UPDATE SET queued_at = now()`, f.RecordID); err != nil {
		return f, err
	}
	return f, tx.Commit(ctx)
}

// FindingByID returns one finding visible to the viewer.
func (s *Store) FindingByID(ctx context.Context, v Viewer, id int64) (Finding, error) {
	var f Finding
	err := s.DB.QueryRow(ctx, `SELECT id, record_id, code, severity, status, org_id FROM findings WHERE id=$1 AND ($2 OR org_id=$3)`,
		id, v.All(), v.OrgID).Scan(&f.ID, &f.RecordID, &f.Code, &f.Severity, &f.Status, &f.OrgID)
	return f, notFound(err)
}
