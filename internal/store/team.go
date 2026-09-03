package store

import (
	"context"
	"time"
)

// Assignment hands one waiting item (a record awaiting a duty) to one member.
type Assignment struct {
	RecordID   string    `json:"recordId"`
	Duty       string    `json:"duty"`
	UserID     string    `json:"userId"`
	AssignedBy string    `json:"assignedBy"`
	AssignedAt time.Time `json:"assignedAt"`
	Note       string    `json:"note,omitempty"`
}

// Assign gives an item to a member (replacing any earlier assignment). The
// member must belong to the organisation.
func (s *Store) Assign(ctx context.Context, orgID string, a Assignment) error {
	tag, err := s.DB.Exec(ctx, `INSERT INTO work_assignments (org_id, record_id, duty, user_id, assigned_by, note)
		SELECT $1, $2, $3, u.id, $5, $6 FROM users u WHERE u.id = $4 AND u.org_id = $1 AND u.disabled_at IS NULL
		ON CONFLICT (org_id, record_id, duty) DO UPDATE SET user_id = EXCLUDED.user_id, assigned_by = EXCLUDED.assigned_by,
			assigned_at = now(), note = EXCLUDED.note`, orgID, a.RecordID, a.Duty, a.UserID, a.AssignedBy, a.Note)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Unassign returns an item to the shared queue.
func (s *Store) Unassign(ctx context.Context, orgID, recordID, duty string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM work_assignments WHERE org_id=$1 AND record_id=$2 AND duty=$3`, orgID, recordID, duty)
	return err
}

// Assignments returns an organisation's assignments keyed by record id + "|" + duty.
func (s *Store) Assignments(ctx context.Context, orgID string) (map[string]Assignment, error) {
	rows, err := s.DB.Query(ctx, `SELECT record_id, duty, user_id, assigned_by, assigned_at, note FROM work_assignments WHERE org_id=$1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Assignment{}
	for rows.Next() {
		var a Assignment
		if err := rows.Scan(&a.RecordID, &a.Duty, &a.UserID, &a.AssignedBy, &a.AssignedAt, &a.Note); err != nil {
			return nil, err
		}
		out[a.RecordID+"|"+a.Duty] = a
	}
	return out, rows.Err()
}

// MemberActivity is what one member (or API key) of an organisation did.
type MemberActivity struct {
	Actor        string         `json:"actor"` // user or API key id
	Created      map[string]int `json:"created"`
	Corrections  int            `json:"corrections"`
	Receipts     map[string]int `json:"receipts"` // by intake decision
	OpenFindings int            `json:"openFindings"`
	Assigned     int            `json:"assigned"`
	LastAction   *time.Time     `json:"lastAction,omitempty"`
	Daily        []int          `json:"daily"` // actions per day, oldest first
}

func (m *MemberActivity) touch(t time.Time) {
	if m.LastAction == nil || t.After(*m.LastAction) {
		m.LastAction = &t
	}
}

// TeamActivity summarises each member's work over the last `days` days.
func (s *Store) TeamActivity(ctx context.Context, orgID string, days int) (map[string]*MemberActivity, error) {
	since := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -days+1)
	out := map[string]*MemberActivity{}
	get := func(actor string) *MemberActivity {
		m := out[actor]
		if m == nil {
			m = &MemberActivity{Actor: actor, Created: map[string]int{}, Receipts: map[string]int{}, Daily: make([]int, days)}
			out[actor] = m
		}
		return m
	}
	day := func(t time.Time) int { return int(t.UTC().Sub(since).Hours() / 24) }

	rows, err := s.DB.Query(ctx, `SELECT created_by, type, version > 1, date_trunc('day', created_at), count(*), max(created_at)
		FROM records WHERE org_id = $1 AND created_by IS NOT NULL AND created_at >= $2 GROUP BY 1, 2, 3, 4`, orgID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var actor, typ string
		var correction bool
		var d, last time.Time
		var n int
		if err := rows.Scan(&actor, &typ, &correction, &d, &n, &last); err != nil {
			rows.Close()
			return nil, err
		}
		m := get(actor)
		m.Created[typ] += n
		if correction {
			m.Corrections += n
		}
		if i := day(d); i >= 0 && i < days {
			m.Daily[i] += n
		}
		m.touch(last)
	}
	rows.Close()

	rows, err = s.DB.Query(ctx, `SELECT received_by, COALESCE(rcpt_decision, 'accepted'), date_trunc('day', received_at), count(*), max(received_at)
		FROM records WHERE counterparty = $1 AND latest AND received_by IS NOT NULL AND received_at >= $2 GROUP BY 1, 2, 3`, orgID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var actor, decision string
		var d, last time.Time
		var n int
		if err := rows.Scan(&actor, &decision, &d, &n, &last); err != nil {
			rows.Close()
			return nil, err
		}
		m := get(actor)
		m.Receipts[decision] += n
		if i := day(d); i >= 0 && i < days {
			m.Daily[i] += n
		}
		m.touch(last)
	}
	rows.Close()

	rows, err = s.DB.Query(ctx, `SELECT r.created_by, count(*) FROM findings f JOIN records r ON r.id = f.record_id AND r.latest
		WHERE f.org_id = $1 AND f.status = 'open' AND r.created_by IS NOT NULL GROUP BY 1`, orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var actor string
		var n int
		if err := rows.Scan(&actor, &n); err != nil {
			rows.Close()
			return nil, err
		}
		get(actor).OpenFindings = n
	}
	rows.Close()

	rows, err = s.DB.Query(ctx, `SELECT user_id, count(*) FROM work_assignments WHERE org_id = $1 GROUP BY 1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var actor string
		var n int
		if err := rows.Scan(&actor, &n); err != nil {
			return nil, err
		}
		get(actor).Assigned = n
	}
	return out, rows.Err()
}
