// Package ingest turns partner submissions (JSON, NGSI-LD, CSV) into stored,
// signed, checked records. Every channel ends in Service.Process, so a record's
// fingerprint, signature and findings do not depend on how it arrived.
package ingest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/checks"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/keyring"
	"github.com/sensifai/roadpass/internal/signing"
	"github.com/sensifai/roadpass/internal/store"
)

// MaxSync is the largest submission processed inside the request.
const MaxSync = 1000

// Input is one submitted record before the server assigns identity and trust.
type Input struct {
	// ID is set only for a correction (a new version of an existing record).
	ID         string         `json:"id,omitempty"`
	Type       string         `json:"type"`
	Ref        string         `json:"ref"`
	SiteID     string         `json:"siteId,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
	MassKg     float64        `json:"massKg"`
	Inputs     []domain.Input `json:"inputs,omitempty"`
	Data       map[string]any `json:"data"`
	Provenance string         `json:"provenance,omitempty"`
	// Note is the signer's remark, signed with the record (optional).
	Note string `json:"note,omitempty"`
	// Signature, when present, is the organisation's own Ed25519 signature
	// (base64) over the content hash: a customer-held key. KeyID names it.
	Signature string `json:"signature,omitempty"`
	KeyID     string `json:"keyId,omitempty"`
}

// RowError is a rejected row.
type RowError struct {
	Row    int                 `json:"row"`
	Ref    string              `json:"ref,omitempty"`
	Errors []domain.FieldError `json:"errors"`
}

// Result is the outcome of one record.
type Result struct {
	Row         int              `json:"row"`
	ID          string           `json:"id"`
	Version     int              `json:"version"`
	Ref         string           `json:"ref"`
	ContentHash string           `json:"contentHash"`
	Status      domain.Status    `json:"status"`
	Findings    []checks.Finding `json:"findings,omitempty"`
}

// ErrSuspended is returned when a suspended organisation writes.
var ErrSuspended = errors.New("this organisation is suspended and cannot submit records")

// Service processes submissions.
type Service struct {
	St    *store.Store
	Cache *cache.Cache
	Keys  *keyring.Keyring
}

// Actor is who submits: a member, who also signs with their personal key, or an
// API key (UserID empty), which signs with the company key only.
type Actor struct {
	ID     string // user or API key id, for created_by and the audit log
	UserID string
	Steps  []string // sections of the chain the member works in; empty = all
}

// signer pairs: the company key, and the member's personal key when there is one.
func (s *Service) signers(ctx context.Context, orgID string, a Actor) (org, person signing.Signer, err error) {
	if org, err = s.Keys.OrgSigner(ctx, orgID); err != nil {
		return nil, nil, err
	}
	if a.UserID != "" {
		if person, err = s.Keys.UserSigner(ctx, orgID, a.UserID); err != nil {
			return nil, nil, fmt.Errorf("personal signing key: %w", err)
		}
	}
	return org, person, nil
}

// Validate checks rows without writing anything (dry run, and step one of Process).
func Validate(rows []Input) ([]*domain.Record, []RowError) {
	recs := make([]*domain.Record, len(rows))
	var errs []RowError
	for i, in := range rows {
		r := &domain.Record{ID: in.ID, Type: domain.RecordType(in.Type), Ref: in.Ref, SiteID: in.SiteID,
			OccurredAt: in.OccurredAt.UTC().Truncate(time.Second), MassKg: in.MassKg, Inputs: in.Inputs,
			Body: in.Data, Provenance: in.Provenance, Note: strings.TrimSpace(in.Note)}
		if r.Provenance == "" {
			r.Provenance = domain.ProvOperational
		}
		fe := domain.Validate(r)
		if len([]rune(r.Note)) > domain.MaxNote {
			fe = append(fe, domain.FieldError{Field: "note", Message: fmt.Sprintf("keep the note under %d characters", domain.MaxNote)})
		}
		if len(fe) > 0 {
			errs = append(errs, RowError{Row: i + 1, Ref: in.Ref, Errors: fe})
		}
		recs[i] = r
	}
	return recs, errs
}

// ErrReadOnly is returned when a read-only organisation (a reporter) writes.
var ErrReadOnly = errors.New("this organisation reads the chain but does not submit records")

// Process stores rows for an organisation in one transaction: all rows or none.
// duties are the caller's duties (user or API key); each row needs the duty of
// its record type. It returns per-row results, or row errors (nothing written).
func (s *Service) Process(ctx context.Context, org domain.Org, actor Actor, duties []string, ingestID string, rows []Input) ([]Result, []RowError, error) {
	if org.Status == domain.OrgSuspended {
		return nil, nil, ErrSuspended
	}
	if org.Kind.ReadOnly() {
		return nil, nil, ErrReadOnly
	}
	// A row without a provenance takes its organisation's: a test organisation's
	// records are Simulated. It may never file Operational ones.
	rows = slices.Clone(rows)
	for i := range rows {
		if rows[i].Provenance == "" {
			rows[i].Provenance = org.Provenance
		}
	}
	recs, errs := Validate(rows)
	for i, r := range recs {
		if org.Provenance == domain.ProvSimulated && r.Provenance == domain.ProvOperational && !r.Void() { // a void keeps the original's label
			errs = append(errs, RowError{Row: i + 1, Ref: r.Ref, Errors: []domain.FieldError{{Field: "provenance",
				Message: "this is a test organisation: its records are Simulated, not Operational"}}})
			continue
		}
		if d := domain.DutyFor(r.Type); r.Type.Valid() && !domain.HasDuty(duties, d) {
			errs = append(errs, RowError{Row: i + 1, Ref: r.Ref, Errors: []domain.FieldError{{Field: "type",
				Message: fmt.Sprintf("%s records need the %s duty, which you do not hold", domain.Types[r.Type].Name, domain.DutyInfo[d].Name)}}})
		} else if r.Type.Valid() && r.Type != domain.TypeAcceptance && !domain.InSteps(r.Type, actor.Steps) {
			errs = append(errs, RowError{Row: i + 1, Ref: r.Ref, Errors: []domain.FieldError{{Field: "type",
				Message: fmt.Sprintf("%s records belong to another section of the chain than the ones your role covers", domain.Types[r.Type].Name)}}})
		}
	}
	if len(errs) > 0 {
		return nil, errs, nil
	}
	orgKey, personKey, err := s.signers(ctx, org.ID, actor)
	if err != nil {
		return nil, nil, err
	}
	tx, err := s.St.DB.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	env := &dbEnv{q: tx}
	results := make([]Result, 0, len(recs))
	var allFindings []store.Finding
	// 1. Assign ids and fingerprint every row.
	ready := make([]bool, len(recs))
	for i, r := range recs {
		if fe := s.prepare(ctx, tx, org, r); len(fe) > 0 {
			errs = append(errs, RowError{Row: i + 1, Ref: r.Ref, Errors: fe})
			continue
		}
		h, err := r.ComputeHash()
		if err != nil {
			return nil, nil, err
		}
		r.ContentHash, ready[i] = h, true
	}
	// 2. Sign them all: one batch per key (a single vault round trip each), the
	// company key unless the organisation signed itself, and the member's own key.
	now := time.Now().UTC().Truncate(time.Second)
	var orgMsgs, personMsgs [][]byte
	var orgIdx, personIdx []int
	for i, r := range recs {
		if !ready[i] {
			continue
		}
		msg := signing.SigningMessage(r.ContentHash)
		if in := rows[i]; in.Signature != "" {
			sig, err := base64.StdEncoding.DecodeString(in.Signature)
			if err != nil {
				errs = append(errs, RowError{Row: i + 1, Ref: r.Ref, Errors: []domain.FieldError{{Field: "signature", Message: "must be base64"}}})
				ready[i] = false
				continue
			}
			r.Signature = &domain.Signature{KeyID: in.KeyID, OrgID: org.ID, Value: sig, SignedAt: now}
		} else {
			orgMsgs, orgIdx = append(orgMsgs, msg), append(orgIdx, i)
		}
		if personKey != nil {
			personMsgs, personIdx = append(personMsgs, msg), append(personIdx, i)
		}
	}
	if len(orgMsgs) > 0 {
		sigs, err := keyring.SignAll(ctx, orgKey, orgMsgs)
		if err != nil {
			return nil, nil, fmt.Errorf("company signature: %w", err)
		}
		for j, i := range orgIdx {
			recs[i].Signature = &domain.Signature{KeyID: orgKey.KeyID(), OrgID: org.ID, Value: sigs[j], SignedAt: now}
		}
	}
	if len(personMsgs) > 0 {
		sigs, err := keyring.SignAll(ctx, personKey, personMsgs)
		if err != nil {
			return nil, nil, fmt.Errorf("personal signature: %w", err)
		}
		for j, i := range personIdx {
			recs[i].ActorSignature = &domain.Signature{KeyID: personKey.KeyID(), OrgID: org.ID, UserID: actor.UserID, Value: sigs[j], SignedAt: now}
		}
	}
	// 3. Check and store in order (a later row may use an earlier one).
	for i, r := range recs {
		if !ready[i] {
			continue
		}
		row := i + 1
		fs, err := checks.Run(ctx, env, r)
		if err != nil {
			return nil, nil, err
		}
		r.Status = checks.StatusFor(fs)
		r.CreatedBy = actor.ID
		if err := store.InsertRecord(ctx, tx, r, ingestID); err != nil {
			if errors.Is(err, store.ErrConflict) {
				errs = append(errs, RowError{Row: row, Ref: r.Ref, Errors: []domain.FieldError{{Field: "ref", Message: err.Error()}}})
				continue
			}
			return nil, nil, err
		}
		for _, f := range fs {
			allFindings = append(allFindings, store.Finding{RecordID: r.ID, Code: f.Code, Severity: f.Severity, Message: f.Message,
				Fix: f.Fix, Detail: f.Detail, OrgID: org.ID})
		}
		results = append(results, Result{Row: row, ID: r.ID, Version: r.Version, Ref: r.Ref, ContentHash: r.ContentHash, Status: r.Status, Findings: fs})
	}
	if len(errs) > 0 {
		return nil, errs, nil
	}
	if err := store.PutFindings(ctx, tx, allFindings); err != nil {
		return nil, nil, err
	}
	// A correction that no longer trips a check closes the finding it raised.
	for _, res := range results {
		if res.Version <= 1 {
			continue
		}
		still := make([]string, 0, len(res.Findings))
		for _, f := range res.Findings {
			still = append(still, f.Code)
		}
		if err := store.CloseCorrected(ctx, tx, res.ID, res.Version, still, actor.ID); err != nil {
			return nil, nil, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(org_id,actor,action,subject,detail) VALUES ($1,$2,'records.submit',$3,$4)`,
		org.ID, actor.ID, ingestID, map[string]any{"count": len(results)}); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	// A delivery that was voided or corrected may leave its section claiming more
	// than was laid on it.
	for i, r := range recs {
		if ready[i] && r.Type == domain.TypeDelivery && r.Version > 1 {
			if err := s.recheckLaid(context.WithoutCancel(ctx), domain.Str(r.Body, "section_id")); err != nil {
				slog.Warn("as-laid recheck", "delivery", r.ID, "err", err)
			}
		}
	}
	if s.Cache != nil {
		orgs := []string{org.ID}
		for _, r := range recs {
			if r.Counterparty != "" {
				orgs = append(orgs, r.Counterparty)
			}
		}
		s.Cache.Publish(context.WithoutCancel(ctx), cache.Event{Kind: "records", Orgs: orgs, Count: len(results), Ref: ingestID})
	}
	slog.Info("records stored", "org", org.ID, "count", len(results), "ingest", ingestID)
	return results, nil, nil
}

// producedAt is the site kind a production record is made at ("" for records
// that are not production lots).
func producedAt(t domain.RecordType) string {
	switch t {
	case domain.TypeRejectLot, domain.TypeRecycledLot, domain.TypeAdditiveBatch, domain.TypeAsphaltLot:
		return string(domain.Types[t].CreatedBy[0])
	}
	return ""
}

// prepare assigns identity (id, version, prev hash), checks ownership of a
// correction and derives server-side fields (counterparty).
func (s *Service) prepare(ctx context.Context, tx pgx.Tx, org domain.Org, r *domain.Record) []domain.FieldError {
	r.OrgID = org.ID
	if r.SiteID != "" {
		var owner, kind string
		if err := tx.QueryRow(ctx, `SELECT org_id, kind FROM sites WHERE id=$1`, r.SiteID).Scan(&owner, &kind); err != nil || owner != org.ID {
			return []domain.FieldError{{Field: "siteId", Message: "is not a site of your organisation"}}
		}
		// A production lot is made at a site of that step: an asphalt lot at an
		// asphalt plant, not at the company's compounding site. "other" is the
		// admin's own description, so it is not second-guessed; nor is a site
		// whose kind was never set.
		if want := producedAt(r.Type); want != "" && kind != "" && kind != want && kind != "other" && !r.Void() {
			return []domain.FieldError{{Field: "siteId", Message: fmt.Sprintf("is a %s site; a %s is made at a %s site",
				strings.ReplaceAll(kind, "_", " "), strings.ToLower(domain.Types[r.Type].Name), strings.ReplaceAll(want, "_", " "))}}
		}
	}
	if !typeAllowed(r.Type, org) {
		return []domain.FieldError{{Field: "type", Message: fmt.Sprintf("a %s cannot create %s records",
			string(org.Kind), domain.Types[r.Type].Plural)}}
	}
	switch r.Type {
	case domain.TypeLabTest:
		// A polymer test on asphalt (or the reverse) is a data-entry mistake.
		var typ string
		if err := tx.QueryRow(ctx, `SELECT type FROM records WHERE id=$1 AND latest`, domain.Str(r.Body, "subject_id")).Scan(&typ); err == nil {
			if t, ok := domain.LabTests[domain.Str(r.Body, "test")]; ok {
				if m := domain.MaterialOf(domain.RecordType(typ)); m != "" && m != t.Material {
					return []domain.FieldError{{Field: "test", Message: fmt.Sprintf("%s is a %s test; %s is %s", t.Name, t.Material, domain.Str(r.Body, "subject_id"), m)}}
				}
			}
		}
	case domain.TypeShipment:
		// Receivers come from the sender's own address book of sites. A shipment to
		// one of its own sites is confirmed by its own receiving team; one to a
		// partner's site is recorded as external (no receiver signature).
		if legacy := domain.Str(r.Body, "to_org_id"); legacy != "" {
			// Only corrections of shipments made before sites were private keep naming a company.
			var prev string
			if r.ID == "" || tx.QueryRow(ctx, `SELECT COALESCE(counterparty,'') FROM records WHERE id=$1 AND latest`, r.ID).Scan(&prev) != nil || prev != legacy {
				return []domain.FieldError{{Field: "to_org_id", Message: "name the receiving site with to_site_id, one of your own or partner sites"}}
			}
			r.Counterparty = legacy
			break
		}
		to := domain.Str(r.Body, "to_site_id")
		var owner, partner string
		if err := tx.QueryRow(ctx, `SELECT org_id, partner FROM sites WHERE id=$1`, to).Scan(&owner, &partner); err != nil || owner != org.ID {
			return []domain.FieldError{{Field: "to_site_id", Message: "is not one of your sites; add the receiver under Organisation → Sites"}}
		}
		if to == r.SiteID {
			return []domain.FieldError{{Field: "to_site_id", Message: "is the site the shipment leaves from"}}
		}
		r.Counterparty = ""
		if partner == "" {
			r.Counterparty = org.ID
		}
	case domain.TypeDelivery:
		sec := domain.Str(r.Body, "section_id")
		var contractor, typ string
		if err := tx.QueryRow(ctx, `SELECT org_id, type FROM records WHERE id=$1 AND latest`, sec).Scan(&contractor, &typ); err != nil || typ != string(domain.TypeRoadSection) {
			return []domain.FieldError{{Field: "section_id", Message: "is not a registered road section"}}
		}
		r.Counterparty = contractor
	}
	if r.ID == "" {
		id, err := store.NextID(ctx, tx, domain.Types[r.Type].Prefix, r.OccurredAt.Year())
		if err != nil {
			return []domain.FieldError{{Field: "id", Message: err.Error()}}
		}
		r.ID, r.Version = id, 1
		return nil
	}
	// A correction: the caller owns the record and the type does not change.
	var prevOrg, prevType, prevHash string
	var prevVersion int
	err := tx.QueryRow(ctx, `SELECT org_id, type, version, content_hash FROM records WHERE id=$1 AND latest FOR UPDATE`, r.ID).
		Scan(&prevOrg, &prevType, &prevVersion, &prevHash)
	if err != nil {
		return []domain.FieldError{{Field: "id", Message: "unknown record"}}
	}
	if prevOrg != org.ID {
		return []domain.FieldError{{Field: "id", Message: "only the issuing organisation can correct a record"}}
	}
	if prevType != string(r.Type) {
		return []domain.FieldError{{Field: "type", Message: "a correction cannot change the record type"}}
	}
	r.Version, r.PrevHash = prevVersion+1, prevHash
	return nil
}

func typeAllowed(t domain.RecordType, org domain.Org) bool {
	kinds := append([]domain.OrgKind{org.Kind}, org.Kinds...)
	for _, k := range kinds {
		if k == domain.KindOperator {
			return true
		}
		for _, x := range domain.Types[t].CreatedBy {
			if x == k {
				return true
			}
		}
	}
	return false
}

// dbEnv answers the checks from Postgres, inside the ingest transaction so a
// batch sees its own earlier rows.
type dbEnv struct{ q store.Querier }

func (e *dbEnv) Record(ctx context.Context, id string) (*domain.Record, bool, error) {
	var r domain.Record
	var typ string
	var body []byte
	err := e.q.QueryRow(ctx, `SELECT id, version, type, org_id, occurred_at, mass_kg, body FROM records WHERE id=$1 AND latest`, id).
		Scan(&r.ID, &r.Version, &typ, &r.OrgID, &r.OccurredAt, &r.MassKg, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	r.Type = domain.RecordType(typ)
	if err := jsonUnmarshal(body, &r.Body); err != nil {
		return nil, false, err
	}
	return &r, true, nil
}

func (e *dbEnv) ReceivedShipment(ctx context.Context, lot, org string) (bool, int64, error) {
	var n, rejected int
	var at *time.Time
	err := e.q.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE r.rcpt_decision = 'rejected'),
			max(r.received_at) FILTER (WHERE r.rcpt_decision IS DISTINCT FROM 'rejected')
		FROM links l JOIN records r ON r.id = l.child_id AND r.latest
		WHERE l.parent_id=$1 AND l.kind='shipped' AND r.counterparty=$2`, lot, org).Scan(&n, &rejected, &at)
	switch {
	case err != nil:
		return false, 0, err
	case n == 0:
		return false, 0, nil
	case at != nil:
		return true, at.Unix(), nil
	case rejected == n:
		return true, -1, nil
	}
	return true, 0, nil
}

func (e *dbEnv) ConsumedKg(ctx context.Context, parent, except string) (float64, error) {
	var kg float64
	err := e.q.QueryRow(ctx, `SELECT COALESCE(sum(mass_kg),0) FROM links WHERE parent_id=$1 AND kind IN ('input','delivered') AND child_id<>$2`,
		parent, except).Scan(&kg)
	return kg, err
}

func (e *dbEnv) LaidKg(ctx context.Context, section string) (float64, error) {
	var kg float64
	err := e.q.QueryRow(ctx, `SELECT COALESCE(sum(r.mass_kg),0) FROM links l JOIN records r ON r.id = l.parent_id AND r.latest
		WHERE l.child_id=$1 AND l.kind='laid' AND r.rcpt_decision IS DISTINCT FROM 'rejected'
			AND NOT COALESCE((r.body->>'void')::boolean, false)`, section).Scan(&kg)
	return kg, err
}

func (e *dbEnv) DuplicateOf(ctx context.Context, dup, except string) (string, bool, error) {
	var id string
	err := e.q.QueryRow(ctx, `SELECT id FROM records WHERE dup_hash=$1 AND latest AND id<>$2 LIMIT 1`, dup, except).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

func (e *dbEnv) Release(ctx context.Context, lotID string) (string, time.Time, bool, error) {
	var decision string
	var at time.Time
	err := e.q.QueryRow(ctx, `SELECT r.body->>'decision', r.occurred_at FROM links l JOIN records r ON r.id = l.child_id AND r.latest
		WHERE l.parent_id=$1 AND l.kind='releases' ORDER BY r.occurred_at DESC LIMIT 1`, lotID).Scan(&decision, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	return decision, at, err == nil, err
}

func (e *dbEnv) PublicKey(ctx context.Context, keyID string) ([]byte, bool, error) {
	var pub []byte
	err := e.q.QueryRow(ctx, `SELECT public_key FROM signing_keys WHERE id=$1`, keyID).Scan(&pub)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return pub, err == nil, err
}

// ErrDuty is returned when the caller lacks the duty for an action.
var ErrDuty = errors.New("you do not hold the duty for this action")

// Receipt records a receiver's signed confirmation of a shipment (intake duty).
// ReceiptIn is the receiver's intake review of a shipment.
type ReceiptIn struct {
	ReceivedAt time.Time `json:"receivedAt"`
	MassKg     float64   `json:"massKg"`
	// Decision: accepted (default), accepted_with_note or rejected. A note is
	// required unless the load is accepted as it is.
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

// ErrReview is an intake review that cannot be signed; the message is for people.
var ErrReview = errors.New("review")

func (s *Service) Receipt(ctx context.Context, org domain.Org, actor Actor, duties []string, shipmentID string, in ReceiptIn) (domain.Receipt, error) {
	if org.Status == domain.OrgSuspended {
		return domain.Receipt{}, ErrSuspended
	}
	if !domain.HasDuty(duties, domain.DutyReceive) {
		return domain.Receipt{}, ErrDuty
	}
	in.Note = strings.TrimSpace(in.Note)
	if in.Decision == "" {
		in.Decision = domain.Accepted
	}
	switch {
	case in.Decision != domain.Accepted && in.Decision != domain.AcceptedWithNote && in.Decision != domain.Rejected:
		return domain.Receipt{}, fmt.Errorf("%w: decision must be accepted, accepted_with_note or rejected", ErrReview)
	case in.Decision != domain.Accepted && in.Note == "":
		return domain.Receipt{}, fmt.Errorf("%w: say why in a note", ErrReview)
	case len([]rune(in.Note)) > domain.MaxNote:
		return domain.Receipt{}, fmt.Errorf("%w: keep the note under %d characters", ErrReview, domain.MaxNote)
	case in.MassKg < 0 || (in.Decision != domain.Rejected && in.MassKg == 0):
		return domain.Receipt{}, fmt.Errorf("%w: enter the weighbridge mass", ErrReview)
	}
	r, err := s.St.RecordAny(ctx, shipmentID)
	if err != nil {
		return domain.Receipt{}, err
	}
	// Shipments are confirmed at the receiver's weighbridge; asphalt deliveries
	// by the contractor at the paver (mix, temperature, ticket).
	if (r.Type != domain.TypeShipment && r.Type != domain.TypeDelivery) || r.Counterparty != org.ID {
