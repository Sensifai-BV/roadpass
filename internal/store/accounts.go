package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// User is a person who signs in.
type User struct {
	ID           string     `json:"id"`
	OrgID        string     `json:"orgId"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	Role         string     `json:"role"`
	Duties       []string   `json:"duties"`
	PasswordHash string     `json:"-"`
	Demo         bool       `json:"demo"`
	DisabledAt   *time.Time `json:"disabledAt,omitempty"`
	LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	Timezone     string     `json:"timezone"`
	Steps        []string   `json:"steps"` // sections of the chain (org kinds); empty = all of the company's
}

const userCols = `id, org_id, email, name, role, duties, password_hash, demo, disabled_at, last_login_at, created_at, timezone, steps`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.Name, &u.Role, &u.Duties, &u.PasswordHash, &u.Demo, &u.DisabledAt, &u.LastLoginAt, &u.CreatedAt, &u.Timezone, &u.Steps)
	return u, notFound(err)
}

// InsertUser creates a user.
func (s *Store) InsertUser(ctx context.Context, u User) error {
	if u.Duties == nil {
		u.Duties = []string{}
	}
	if u.Steps == nil {
		u.Steps = []string{}
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO users(id,org_id,email,name,role,duties,password_hash,demo,steps) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		u.ID, u.OrgID, u.Email, u.Name, u.Role, u.Duties, u.PasswordHash, u.Demo, u.Steps)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// UpdateProfile changes a user's own name and time zone.
func (s *Store) UpdateProfile(ctx context.Context, id, name, timezone string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE users SET name=$2, timezone=$3 WHERE id=$1`, id, name, timezone)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetPassword replaces a user's password hash.
func (s *Store) SetPassword(ctx context.Context, id, hash string) error {
	_, err := s.DB.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, id, hash)
	return err
}

// UserByEmail looks a user up for sign-in.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.DB.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE lower(email)=lower($1)`, email))
}

// UserByID returns one user.
func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(s.DB.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id=$1`, id))
}

// Users lists the users of an organisation ("" for all, demo picker).
func (s *Store) Users(ctx context.Context, orgID string, demoOnly bool) ([]User, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+userCols+` FROM users WHERE ($1='' OR org_id=$1) AND (NOT $2 OR demo) AND disabled_at IS NULL ORDER BY org_id, name`, orgID, demoOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// TouchLogin records a successful sign-in.
func (s *Store) TouchLogin(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, id)
	return err
}

// APIKey is an organisation API key (the secret is never stored).
type APIKey struct {
	ID         string     `json:"id"`
	OrgID      string     `json:"orgId"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Hash       []byte     `json:"-"`
	Scopes     []string   `json:"scopes"`
	Duties     []string   `json:"duties"`
	CreatedBy  string     `json:"createdBy,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

const keyCols = `id, org_id, name, prefix, hash, scopes, duties, COALESCE(created_by,''), created_at, last_used_at, revoked_at`

func scanKey(row pgx.Row) (APIKey, error) {
	var k APIKey
	err := row.Scan(&k.ID, &k.OrgID, &k.Name, &k.Prefix, &k.Hash, &k.Scopes, &k.Duties, &k.CreatedBy, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	return k, notFound(err)
}

// InsertAPIKey stores a new key.
func (s *Store) InsertAPIKey(ctx context.Context, k APIKey) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO api_keys(id,org_id,name,prefix,hash,scopes,duties,created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		k.ID, k.OrgID, k.Name, k.Prefix, k.Hash, k.Scopes, k.Duties, nullIf(k.CreatedBy))
	return err
}

// APIKeyByHash finds an active key.
func (s *Store) APIKeyByHash(ctx context.Context, hash []byte) (APIKey, error) {
	return scanKey(s.DB.QueryRow(ctx, `SELECT `+keyCols+` FROM api_keys WHERE hash=$1 AND revoked_at IS NULL`, hash))
}

// APIKeys lists an organisation's keys.
func (s *Store) APIKeys(ctx context.Context, orgID string) ([]APIKey, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+keyCols+` FROM api_keys WHERE org_id=$1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey revokes a key of the organisation.
func (s *Store) RevokeAPIKey(ctx context.Context, orgID, id string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND org_id=$2 AND revoked_at IS NULL`, id, orgID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// TouchAPIKey records use, at most once a minute per key.
func (s *Store) TouchAPIKey(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, id)
	return err
}

// Audit appends to the audit log.
func (s *Store) Audit(ctx context.Context, orgID, actor, action, subject string, detail any) error {
	var d []byte
	if detail != nil {
		d, _ = json.Marshal(detail)
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO audit_log(org_id,actor,action,subject,detail) VALUES ($1,$2,$3,$4,$5)`,
		nullIf(orgID), actor, action, nullIf(subject), d)
	return err
}

// AuditEntry is one audit-log row.
type AuditEntry struct {
	ID      int64          `json:"id"`
	At      time.Time      `json:"at"`
	OrgID   string         `json:"orgId,omitempty"`
	Actor   string         `json:"actor"`
	Action  string         `json:"action"`
	Subject string         `json:"subject,omitempty"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// AuditLog lists the newest entries visible to the viewer.
func (s *Store) AuditLog(ctx context.Context, v Viewer, limit int) ([]AuditEntry, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, at, COALESCE(org_id,''), actor, action, COALESCE(subject,''), detail FROM audit_log
		WHERE $1 OR org_id=$2 ORDER BY id DESC LIMIT $3`, v.All(), v.OrgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var d []byte
		if err := rows.Scan(&e.ID, &e.At, &e.OrgID, &e.Actor, &e.Action, &e.Subject, &d); err != nil {
			return nil, err
		}
		if len(d) > 0 {
			_ = json.Unmarshal(d, &e.Detail)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Ingest tracks one API, NGSI-LD or CSV submission.
type Ingest struct {
	ID             string           `json:"id"`
	OrgID          string           `json:"orgId"`
	Channel        string           `json:"channel"`
	RecordType     string           `json:"recordType,omitempty"`
	Status         string           `json:"status"`
	Total          int              `json:"total"`
	Accepted       int              `json:"accepted"`
	Rejected       int              `json:"rejected"`
	Errors         []map[string]any `json:"errors,omitempty"`
	IdempotencyKey string           `json:"-"`
	CreatedBy      string           `json:"createdBy,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	FinishedAt     *time.Time       `json:"finishedAt,omitempty"`
}

// CreateIngest stores a new ingest; an existing idempotency key returns the
// original ingest and ErrConflict.
func (s *Store) CreateIngest(ctx context.Context, in Ingest) (Ingest, error) {
	_, err := s.DB.Exec(ctx, `INSERT INTO ingests(id,org_id,channel,record_type,status,total,idempotency_key,created_by)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,NULLIF($7,''),NULLIF($8,''))`,
		in.ID, in.OrgID, in.Channel, in.RecordType, in.Status, in.Total, in.IdempotencyKey, in.CreatedBy)
	if isUnique(err) && in.IdempotencyKey != "" {
		var id string
		if e := s.DB.QueryRow(ctx, `SELECT id FROM ingests WHERE org_id=$1 AND idempotency_key=$2`, in.OrgID, in.IdempotencyKey).Scan(&id); e != nil {
			return in, e
		}
		prev, e := s.GetIngest(ctx, in.OrgID, id)
		if e != nil {
			return in, e
		}
		return prev, ErrConflict
	}
	return in, err
}

// FinishIngest stores the outcome.
func (s *Store) FinishIngest(ctx context.Context, in Ingest) error {
	errs, _ := json.Marshal(in.Errors)
	if len(in.Errors) == 0 {
		errs = nil
	}
	_, err := s.DB.Exec(ctx, `UPDATE ingests SET status=$2, total=$3, accepted=$4, rejected=$5, errors=$6, finished_at=now() WHERE id=$1`,
		in.ID, in.Status, in.Total, in.Accepted, in.Rejected, errs)
	return err
}

// FailStaleIngests marks batches that have been "running" for longer than
// maxAge as failed (their request ended without recording an outcome).
func (s *Store) FailStaleIngests(ctx context.Context, maxAge time.Duration) error {
	_, err := s.DB.Exec(ctx, `UPDATE ingests SET status='failed', finished_at=now(),
		errors='[{"row":0,"ref":"","errors":["The request ended before the batch finished; nothing was stored. Submit it again."]}]'::jsonb
		WHERE status='running' AND created_at < now() - make_interval(secs => $1)`, maxAge.Seconds())
	return err
}

// GetIngest returns one ingest of the organisation.
func (s *Store) GetIngest(ctx context.Context, orgID, id string) (Ingest, error) {
	out, err := s.ingests(ctx, `WHERE id=$1 AND org_id=$2`, id, orgID)
	if err != nil {
		return Ingest{}, err
	}
	if len(out) == 0 {
		return Ingest{}, ErrNotFound
	}
	return out[0], nil
}

// Ingests lists an organisation's newest ingests.
func (s *Store) Ingests(ctx context.Context, orgID string, limit int) ([]Ingest, error) {
	return s.ingests(ctx, `WHERE org_id=$1 ORDER BY created_at DESC LIMIT $2`, orgID, limit)
}

func (s *Store) ingests(ctx context.Context, tail string, args ...any) ([]Ingest, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, org_id, channel, COALESCE(record_type,''), status, total, accepted, rejected, errors,
		COALESCE(created_by,''), created_at, finished_at FROM ingests `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ingest
	for rows.Next() {
		var x Ingest
		var errs []byte
		if err := rows.Scan(&x.ID, &x.OrgID, &x.Channel, &x.RecordType, &x.Status, &x.Total, &x.Accepted, &x.Rejected, &errs,
			&x.CreatedBy, &x.CreatedAt, &x.FinishedAt); err != nil {
			return nil, err
		}
		if len(errs) > 0 {
			_ = json.Unmarshal(errs, &x.Errors)
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Cursor positions of background loops.
func (s *Store) CursorPos(ctx context.Context, name string) (int64, error) {
	var p int64
	err := s.DB.QueryRow(ctx, `SELECT position FROM cursors WHERE name=$1`, name).Scan(&p)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return p, err
}

// SetCursor moves a loop's cursor forward.
func (s *Store) SetCursor(ctx context.Context, name string, pos int64) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO cursors(name, position, updated_at) VALUES ($1,$2,now())
		ON CONFLICT (name) DO UPDATE SET position=GREATEST(cursors.position, EXCLUDED.position), updated_at=now()`, name, pos)
	return err
}

// Invitation is a pending team invitation.
type Invitation struct {
	ID         string     `json:"id"`
	OrgID      string     `json:"orgId"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	Duties     []string   `json:"duties"`
	Steps      []string   `json:"steps"`
	InvitedBy  string     `json:"invitedBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	AcceptedAt *time.Time `json:"acceptedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

const invCols = `id, org_id, email, role, duties, invited_by, created_at, expires_at, accepted_at, revoked_at, steps`

func scanInv(row pgx.Row) (Invitation, error) {
	var i Invitation
	err := row.Scan(&i.ID, &i.OrgID, &i.Email, &i.Role, &i.Duties, &i.InvitedBy, &i.CreatedAt, &i.ExpiresAt, &i.AcceptedAt, &i.RevokedAt, &i.Steps)
	return i, notFound(err)
}

// CreateInvitation stores an invitation (only the token's hash).
func (s *Store) CreateInvitation(ctx context.Context, i Invitation, tokenHash []byte) error {
	if i.Duties == nil {
		i.Duties = []string{}
	}
	if i.Steps == nil {
		i.Steps = []string{}
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO invitations(id,org_id,email,role,duties,token_hash,invited_by,expires_at,steps) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		i.ID, i.OrgID, i.Email, i.Role, i.Duties, tokenHash, i.InvitedBy, i.ExpiresAt, i.Steps)
	return err
}

// InvitationByToken finds an open invitation.
func (s *Store) InvitationByToken(ctx context.Context, tokenHash []byte) (Invitation, error) {
	return scanInv(s.DB.QueryRow(ctx, `SELECT `+invCols+` FROM invitations
		WHERE token_hash=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()`, tokenHash))
}

// Invitations lists an organisation's open invitations.
func (s *Store) Invitations(ctx context.Context, orgID string) ([]Invitation, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+invCols+` FROM invitations WHERE org_id=$1 AND accepted_at IS NULL AND revoked_at IS NULL
		ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		i, err := scanInv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// AcceptInvitation creates the user and closes the invitation atomically.
func (s *Store) AcceptInvitation(ctx context.Context, inv Invitation, u User) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	tag, err := tx.Exec(ctx, `UPDATE invitations SET accepted_at=now() WHERE id=$1 AND accepted_at IS NULL AND revoked_at IS NULL`, inv.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO users(id,org_id,email,name,role,duties,password_hash,steps) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		u.ID, inv.OrgID, u.Email, u.Name, inv.Role, inv.Duties, u.PasswordHash, inv.Steps); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	return tx.Commit(ctx)
}

// RevokeInvitation cancels an open invitation.
func (s *Store) RevokeInvitation(ctx context.Context, orgID, id string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE invitations SET revoked_at=now() WHERE id=$1 AND org_id=$2 AND accepted_at IS NULL AND revoked_at IS NULL`, id, orgID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetUserSteps changes the sections of the chain a member works in.
func (s *Store) SetUserSteps(ctx context.Context, orgID, userID string, steps []string) error {
	if steps == nil {
		steps = []string{}
	}
	tag, err := s.DB.Exec(ctx, `UPDATE users SET steps=$3 WHERE id=$1 AND org_id=$2`, userID, orgID, steps)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetUserDuties changes a member's duties.
func (s *Store) SetUserDuties(ctx context.Context, orgID, userID string, duties []string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE users SET duties=$3 WHERE id=$1 AND org_id=$2`, userID, orgID, duties)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetUserRole changes a member's role; the last admin cannot be demoted.
func (s *Store) SetUserRole(ctx context.Context, orgID, userID, role string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx, `UPDATE users SET role=$3 WHERE id=$1 AND org_id=$2`, userID, orgID, role); err != nil {
		return err
	}
	var admins int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE org_id=$1 AND role='admin' AND disabled_at IS NULL`, orgID).Scan(&admins); err != nil {
		return err
	}
	if admins == 0 {
		return fmt.Errorf("%w: an organisation needs at least one admin", ErrConflict)
	}
	return tx.Commit(ctx)
}

// DisableUser removes a member's access.
func (s *Store) DisableUser(ctx context.Context, orgID, userID string) error {
	_, err := s.DB.Exec(ctx, `UPDATE users SET disabled_at=now() WHERE id=$1 AND org_id=$2`, userID, orgID)
	return err
}
