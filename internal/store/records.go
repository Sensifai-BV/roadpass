package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sensifai/roadpass/internal/domain"
)

// recColsFor lists the record columns qualified by a table alias.
func recColsFor(a string) string {
	return a + `.seq, ` + a + `.id, ` + a + `.version, COALESCE(` + a + `.prev_hash,''), ` + a + `.type, ` + a + `.org_id,
	COALESCE(` + a + `.site_id,''), COALESCE(` + a + `.counterparty,''), ` + a + `.ref, ` + a + `.occurred_at, ` + a + `.mass_kg,
	` + a + `.inputs, ` + a + `.body, ` + a + `.provenance, ` + a + `.content_hash, ` + a + `.status,
	COALESCE(` + a + `.key_id,''), ` + a + `.signature, ` + a + `.signed_at, ` + a + `.received_at, COALESCE(` + a + `.received_mass_kg,0),
	COALESCE(` + a + `.rcpt_key_id,''), ` + a + `.rcpt_signature, COALESCE(` + a + `.seal_id,''), COALESCE(` + a + `.leaf_index,0),
	` + a + `.created_at, COALESCE((SELECT sx.status = 'sealed' FROM seals sx WHERE sx.id = ` + a + `.seal_id), false),
	` + a + `.note, COALESCE(` + a + `.created_by,''), COALESCE(` + a + `.received_by,''), COALESCE(` + a + `.rcpt_decision,''), ` + a + `.rcpt_note,
	COALESCE(` + a + `.actor_key_id,''), ` + a + `.actor_signature, COALESCE(` + a + `.rcpt_actor_key_id,''), ` + a + `.rcpt_actor_signature`
}

var recCols = recColsFor("records")

// recScan scans one row of recColsFor into a record: dest() for Scan, then finish().
type recScan struct {
	r                              *domain.Record
	typ, status, keyID, rcptKeyID  string
	inputs, body, sig, rcptSig     []byte
	signedAt, receivedAt           *time.Time
	receivedKg                     float64
	receivedBy, decision, rcptNote string
	actorKey, rcptActorKey         string
	actorSig, rcptActorSig         []byte
}

func newRecScan(r *domain.Record) *recScan { return &recScan{r: r} }

func (x *recScan) dest() []any {
	r := x.r
	return []any{&r.Seq, &r.ID, &r.Version, &r.PrevHash, &x.typ, &r.OrgID, &r.SiteID, &r.Counterparty,
		&r.Ref, &r.OccurredAt, &r.MassKg, &x.inputs, &x.body, &r.Provenance, &r.ContentHash, &x.status,
		&x.keyID, &x.sig, &x.signedAt, &x.receivedAt, &x.receivedKg, &x.rcptKeyID, &x.rcptSig, &r.SealID, &r.LeafIndex,
		&r.CreatedAt, &r.Sealed, &r.Note, &r.CreatedBy, &x.receivedBy, &x.decision, &x.rcptNote,
		&x.actorKey, &x.actorSig, &x.rcptActorKey, &x.rcptActorSig}
}

func (x *recScan) finish() error {
	r := x.r
	r.Type, r.Status = domain.RecordType(x.typ), domain.Status(x.status)
	r.OccurredAt = r.OccurredAt.UTC()
	if len(x.inputs) > 0 {
		if err := json.Unmarshal(x.inputs, &r.Inputs); err != nil {
			return fmt.Errorf("record %s inputs: %w", r.ID, err)
		}
	}
	if err := json.Unmarshal(x.body, &r.Body); err != nil {
		return fmt.Errorf("record %s body: %w", r.ID, err)
	}
	if x.keyID != "" && x.signedAt != nil {
		r.Signature = &domain.Signature{KeyID: x.keyID, OrgID: orgOfKey(x.keyID), Value: x.sig, SignedAt: x.signedAt.UTC()}
	}
	if x.actorKey != "" && x.signedAt != nil {
		r.ActorSignature = &domain.Signature{KeyID: x.actorKey, OrgID: r.OrgID, UserID: holderOfKey(x.actorKey), Value: x.actorSig, SignedAt: x.signedAt.UTC()}
	}
	if x.receivedAt != nil {
		r.Receipt = &domain.Receipt{ReceivedAt: x.receivedAt.UTC(), MassKg: x.receivedKg, Decision: x.decision, Note: x.rcptNote, ReceivedBy: x.receivedBy,
			Signature: domain.Signature{KeyID: x.rcptKeyID, OrgID: orgOfKey(x.rcptKeyID), Value: x.rcptSig, SignedAt: x.receivedAt.UTC()}}
		if x.rcptActorKey != "" {
			r.Receipt.ActorSignature = &domain.Signature{KeyID: x.rcptActorKey, OrgID: r.Counterparty, UserID: holderOfKey(x.rcptActorKey),
				Value: x.rcptActorSig, SignedAt: x.receivedAt.UTC()}
		}
	}
	return nil
}

func scanRecord(row pgx.Row) (domain.Record, error) {
	var r domain.Record
	x := newRecScan(&r)
	if err := row.Scan(x.dest()...); err != nil {
		return r, err
	}
	return r, x.finish()
}

// holderOfKey is the member a personal key id ("usr_…:kN") belongs to.
func holderOfKey(keyID string) string {
	if strings.HasPrefix(keyID, "usr_") {
		return orgOfKey(keyID)
	}
	return ""
}

func orgOfKey(keyID string) string {
	if i := strings.LastIndexByte(keyID, ':'); i > 0 {
		return keyID[:i]
	}
	return keyID
}

func collect(rows pgx.Rows) ([]domain.Record, error) {
	defer rows.Close()
	var out []domain.Record
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// visible returns a WHERE fragment restricting records to the viewer, with its args
// numbered from n.
func visible(v Viewer, n int) (string, []any) {
	if v.All() {
		return "TRUE", nil
	}
	return fmt.Sprintf("(org_id = $%d OR counterparty = $%d)", n, n), []any{v.OrgID}
}

func sigParts(s *domain.Signature) (keyID any, val []byte, at any) {
	if s == nil {
		return nil, nil, nil
	}
	return s.KeyID, s.Value, s.SignedAt
}

func rcptParts(rc *domain.Receipt) (at any, kg any, keyID any, val []byte) {
	if rc == nil {
		return nil, nil, nil, nil
	}
	return rc.ReceivedAt, rc.MassKg, nullIf(rc.Signature.KeyID), rc.Signature.Value
}

// createdAt is when a bulk-loaded record was submitted (its own CreatedAt, e.g.
// simulated history), or now.
func createdAt(r domain.Record) time.Time {
	if r.CreatedAt.IsZero() {
		return time.Now().UTC()
	}
	return r.CreatedAt
}

// rcptStr returns a receipt's decision, note or receiver for COPY (NULL when unset).
func rcptStr(rc *domain.Receipt, f string) any {
	if rc == nil {
		if f == "note" {
			return ""
		}
		return nil
	}
	switch f {
	case "decision":
		return nullIf(rc.Decision)
	case "note":
		return rc.Note
	}
	return nullIf(rc.ReceivedBy)
}

func nullIf(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// InsertRecord writes one record version and its links inside tx. A version > 1
// clears the previous latest flag; the caller has checked prev_hash.
func InsertRecord(ctx context.Context, tx pgx.Tx, r *domain.Record, ingestID string) error {
	if r.Version > 1 {
		tag, err := tx.Exec(ctx, `UPDATE records SET latest=false WHERE id=$1 AND version=$2 AND latest`, r.ID, r.Version-1)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: %s v%d is not the latest version", ErrConflict, r.ID, r.Version-1)
		}
	}
	inputs, _ := json.Marshal(r.Inputs)
	if len(r.Inputs) == 0 {
		inputs = nil
	}
	body, err := json.Marshal(r.Body)
	if err != nil {
		return err
	}
	k, sv, sa := sigParts(r.Signature)
	ak, as, _ := sigParts(r.ActorSignature)
	dup, err := r.DupHash()
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `INSERT INTO records(id,version,prev_hash,type,org_id,site_id,counterparty,ref,occurred_at,mass_kg,
			inputs,body,provenance,content_hash,dup_hash,status,key_id,signature,signed_at,ingest_id,note,created_by,actor_key_id,actor_signature)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24) RETURNING seq, created_at`,
		r.ID, r.Version, nullIf(r.PrevHash), string(r.Type), r.OrgID, nullIf(r.SiteID), nullIf(r.Counterparty), r.Ref,
		r.OccurredAt, r.MassKg, inputs, body, r.Provenance, r.ContentHash, dup, string(r.Status),
		k, sv, sa, nullIf(ingestID), r.Note, nullIf(r.CreatedBy), ak, as).Scan(&r.Seq, &r.CreatedAt)
	if isUnique(err) {
		return fmt.Errorf("%w: %s %q already exists", ErrConflict, domain.Types[r.Type].Name, r.Ref)
	}
	if err != nil {
		return err
	}
	if r.Version > 1 {
		// The lineage follows the latest version: drop the edges the previous one
		// drew (a void's edges come back with zero mass). Laid edges are the only
		// ones a record draws as parent; everything else names it as child.
		if _, err := tx.Exec(ctx, `DELETE FROM links WHERE (child_id=$1 AND kind <> 'laid') OR (parent_id=$1 AND kind = 'laid')`, r.ID); err != nil {
			return err
		}
	}
	for _, l := range r.Links() {
		if _, err := tx.Exec(ctx, `INSERT INTO links(parent_id,child_id,kind,mass_kg) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			l.Parent, l.Child, l.Kind, l.MassKg); err != nil {
			return err
		}
	}
	return nil
}

// CopyRecords bulk-loads records and their links (seeding). It bypasses nothing but
// row-at-a-time round trips: hashes and signatures are computed by the caller exactly
// as on ingest. Rows are written with latest=false; FinalizeLatest marks the newest
// version of every record once the load is complete, so versions may arrive in
// different batches without tripping the one-latest-per-id index.
func (s *Store) CopyRecords(ctx context.Context, recs []domain.Record, ingestID string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	rows := make([][]any, len(recs))
	var links [][]any
	for i := range recs {
		r := &recs[i]
		var inputs []byte
		if len(r.Inputs) > 0 {
			inputs, _ = json.Marshal(r.Inputs)
		}
		body, err := json.Marshal(r.Body)
		if err != nil {
			return err
		}
		k, sv, sa := sigParts(r.Signature)
		ra, rk, rkey, rsig := rcptParts(r.Receipt)
		dup, err := r.DupHash()
		if err != nil {
			return err
		}
		rows[i] = []any{r.ID, r.Version, false, nullIf(r.PrevHash), string(r.Type), r.OrgID, nullIf(r.SiteID), nullIf(r.Counterparty),
			r.Ref, r.OccurredAt, r.MassKg, inputs, body, r.Provenance, r.ContentHash, dup, string(r.Status),
			k, sv, sa, ra, rk, rkey, rsig, ingestID, r.Note, nullIf(r.CreatedBy), rcptStr(r.Receipt, "decision"), rcptStr(r.Receipt, "note"), rcptStr(r.Receipt, "by"),
			createdAt(*r)}
		if r.Version == 1 {
			for _, l := range r.Links() {
				links = append(links, []any{l.Parent, l.Child, l.Kind, l.MassKg})
			}
		}
	}
	cols := []string{"id", "version", "latest", "prev_hash", "type", "org_id", "site_id", "counterparty", "ref", "occurred_at", "mass_kg",
		"inputs", "body", "provenance", "content_hash", "dup_hash", "status", "key_id", "signature", "signed_at",
		"received_at", "received_mass_kg", "rcpt_key_id", "rcpt_signature", "ingest_id", "note", "created_by", "rcpt_decision", "rcpt_note", "received_by", "created_at"}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"records"}, cols, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("copy records: %w", err)
	}
	if len(links) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"links"}, []string{"parent_id", "child_id", "kind", "mass_kg"}, pgx.CopyFromRows(links)); err != nil {
			return fmt.Errorf("copy links: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// Record returns the latest version of a record the viewer may see in full.
func (s *Store) Record(ctx context.Context, v Viewer, id string) (domain.Record, error) {
	w, args := visible(v, 2)
	r, err := scanRecord(s.DB.QueryRow(ctx, `SELECT `+recCols+` FROM records WHERE id=$1 AND latest AND `+w, append([]any{id}, args...)...))
	return r, notFound(err)
}

// RecordAny returns the latest version regardless of viewer (passport assembly,
// which applies its own redaction).
func (s *Store) RecordAny(ctx context.Context, id string) (domain.Record, error) {
	r, err := scanRecord(s.DB.QueryRow(ctx, `SELECT `+recCols+` FROM records WHERE id=$1 AND latest`, id))
	return r, notFound(err)
}

// RecordsByID returns the latest versions of the given ids (no visibility filter;
// callers redact).
func (s *Store) RecordsByID(ctx context.Context, ids []string) ([]domain.Record, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT `+recCols+` FROM records WHERE latest AND id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// Versions returns every version of a record, oldest first.
func (s *Store) Versions(ctx context.Context, v Viewer, id string) ([]domain.Record, error) {
	w, args := visible(v, 2)
	rows, err := s.DB.Query(ctx, `SELECT `+recCols+` FROM records WHERE id=$1 AND `+w+` ORDER BY version`, append([]any{id}, args...)...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// RecordByRef resolves an organisation's own reference to its latest record.
func (s *Store) RecordByRef(ctx context.Context, orgID string, t domain.RecordType, ref string) (domain.Record, error) {
	r, err := scanRecord(s.DB.QueryRow(ctx, `SELECT `+recCols+` FROM records WHERE org_id=$1 AND type=$2 AND ref=$3 AND latest`, orgID, string(t), ref))
	return r, notFound(err)
}

// HashExists reports whether another latest record already has this content hash.
func (s *Store) HashExists(ctx context.Context, hash, exceptID string) (string, bool, error) {
	var id string
	err := s.DB.QueryRow(ctx, `SELECT id FROM records WHERE content_hash=$1 AND latest AND id<>$2 LIMIT 1`, hash, exceptID).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	return id, err == nil, err
}

// Filter narrows a record listing.
type Filter struct {
	Types    []domain.RecordType
	OrgID    string // records issued by, or received by, this organisation
	Status   string
	Query    string // id or reference prefix
	From, To time.Time
	Before   *Cursor // keyset pagination
	Limit    int
}

// Cursor is the position after the last row of a page.
type Cursor struct {
	At time.Time
	ID string
}

// CountRecords counts what Records would return without pagination.
func (s *Store) CountRecords(ctx context.Context, v Viewer, f Filter) (int64, error) {
	where, args := filterSQL(v, f)
	var n int64
	err := s.DB.QueryRow(ctx, `SELECT count(*) FROM records WHERE `+strings.Join(where, " AND "), args...).Scan(&n)
	return n, err
}

// likeEscape makes user text literal inside LIKE/ILIKE: % and _ are not wildcards.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func filterSQL(v Viewer, f Filter) ([]string, []any) {
	where, args := []string{"latest"}, []any{}
	add := func(cond string, a ...any) {
		for _, x := range a {
			args = append(args, x)
			cond = strings.Replace(cond, "?", fmt.Sprintf("$%d", len(args)), 1)
		}
		where = append(where, cond)
	}
	if !v.All() {
		add("(org_id = ? OR counterparty = ?)", v.OrgID, v.OrgID)
	}
	if len(f.Types) > 0 {
		ts := make([]string, len(f.Types))
		for i, t := range f.Types {
			ts[i] = string(t)
		}
		add("type = ANY(?)", ts)
	}
	if f.OrgID != "" {
		add("(org_id = ? OR counterparty = ?)", f.OrgID, f.OrgID)
	}
	if f.Status != "" {
		add("status = ?", f.Status)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		// A passport id or your own reference matches from its start; with three or more
		// characters it matches anywhere ("26-000003", "ALERT-TEST"), and also the road
		// of a section, the mix of a lot and the name or town of one of your own sites.
		esc := likeEscape(q)
		conds, a := []string{"id LIKE ?", "ref ILIKE ?"}, []any{strings.ToUpper(esc) + "%", esc + "%"}
		if len([]rune(q)) >= 3 {
			conds, a = []string{"id LIKE ?", "ref ILIKE ?", "body->>'road' ILIKE ?", "body->>'mix' ILIKE ?"},
				[]any{"%" + strings.ToUpper(esc) + "%", "%" + esc + "%", "%" + esc + "%", "%" + esc + "%"}
			site := "site_id IN (SELECT id FROM sites WHERE (name ILIKE ? OR city ILIKE ?)"
			a = append(a, "%"+esc+"%", "%"+esc+"%")
			if !v.All() {
				site += " AND org_id = ?"
				a = append(a, v.OrgID)
			}
			conds = append(conds, site+")")
		}
		add("("+strings.Join(conds, " OR ")+")", a...)
	}
	if !f.From.IsZero() {
		add("occurred_at >= ?", f.From)
	}
	if !f.To.IsZero() {
		add("occurred_at < ?", f.To)
	}
	if f.Before != nil {
		add("(occurred_at, id) < (?, ?)", f.Before.At, f.Before.ID)
	}
	return where, args
}

// Records lists latest versions, newest first, with keyset pagination.
func (s *Store) Records(ctx context.Context, v Viewer, f Filter) ([]domain.Record, *Cursor, error) {
	where, args := filterSQL(v, f)
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	args = append(args, limit+1)
	q := fmt.Sprintf(`SELECT %s FROM records WHERE %s ORDER BY occurred_at DESC, id DESC LIMIT $%d`, recCols, strings.Join(where, " AND "), len(args))
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	out, err := collect(rows)
	if err != nil {
		return nil, nil, err
	}
	var next *Cursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = &Cursor{At: last.OccurredAt, ID: last.ID}
	}
	return out, next, nil
}

// SetStatus updates the status of a record's latest version.
func (s *Store) SetStatus(ctx context.Context, id string, st domain.Status) error {
	if _, err := s.DB.Exec(ctx, `UPDATE records SET status=$2 WHERE id=$1 AND latest`, id, string(st)); err != nil {
		return err
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO reproject(id) VALUES ($1) ON CONFLICT (id) DO UPDATE SET queued_at = now()`, id)
	return err
}

// FinalizeLatest marks the newest version of every record latest (after CopyRecords).
func (s *Store) FinalizeLatest(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `UPDATE records r SET latest = true FROM (
			SELECT id, max(version) AS v FROM records GROUP BY id) m
		WHERE r.id = m.id AND r.version = m.v AND NOT r.latest`)
	return err
}

// SetReceipt stores the receiver's signed receipt of a shipment, once.
func (s *Store) SetReceipt(ctx context.Context, id string, rc domain.Receipt) error {
	ak, as, _ := sigParts(rc.ActorSignature)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	tag, err := tx.Exec(ctx, `UPDATE records SET received_at=$2, received_mass_kg=$3, rcpt_key_id=$4, rcpt_signature=$5,
			rcpt_decision=$6, rcpt_note=$7, received_by=$8, rcpt_actor_key_id=$9, rcpt_actor_signature=$10
		WHERE id=$1 AND latest AND type IN ('shipment','delivery') AND received_at IS NULL`,
		id, rc.ReceivedAt, rc.MassKg, rc.Signature.KeyID, rc.Signature.Value, nullIf(rc.Decision), rc.Note, nullIf(rc.ReceivedBy), ak, as)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s is not an open shipment", ErrConflict, id)
	}
	// The receipt changes what the record counts for in the projection (a
	// rejected delivery was not laid), but writes no new version: queue it.
	if _, err := tx.Exec(ctx, `INSERT INTO reproject(id) VALUES ($1) ON CONFLICT (id) DO UPDATE SET queued_at = now()`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RecordsAfter returns record versions with seq > after, in seq order (projector).
func (s *Store) RecordsAfter(ctx context.Context, after int64, limit int) ([]domain.Record, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+recCols+` FROM records WHERE seq > $1 ORDER BY seq LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// MaxSeq is the newest record sequence number.
func (s *Store) MaxSeq(ctx context.Context) (int64, error) {
	var n int64
	err := s.DB.QueryRow(ctx, `SELECT COALESCE(max(seq),0) FROM records`).Scan(&n)
	return n, err
}

// NextID allocates the next human id for a prefix and year, e.g. ASL-25-004214.
func NextID(ctx context.Context, q Querier, prefix string, year int) (string, error) {
	key := fmt.Sprintf("%s-%02d", prefix, year%100)
	var n int64
	err := q.QueryRow(ctx, `INSERT INTO id_counters(key, n) VALUES ($1, 1)
		ON CONFLICT (key) DO UPDATE SET n = id_counters.n + 1 RETURNING n`, key).Scan(&n)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%06d", key, n), nil
}

// SyncCounters moves id counters past every existing id (after a bulk load).
func (s *Store) SyncCounters(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO id_counters(key, n)
		SELECT left(id, 6), max(substring(id from 8)::bigint) FROM records WHERE id ~ '^[A-Z]{3}-[0-9]{2}-[0-9]+$' GROUP BY 1
		ON CONFLICT (key) DO UPDATE SET n = GREATEST(id_counters.n, EXCLUDED.n)`)
	return err
}
