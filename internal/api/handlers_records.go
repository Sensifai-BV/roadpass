package api

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/auth"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/ingest"
	"github.com/sensifai/roadpass/internal/store"
)

func encodeCursor(c *store.Cursor) string {
	if c == nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.At.Unix(), 10) + "|" + c.ID))
}

func decodeCursor(s string) *store.Cursor {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return nil
	}
	n, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil
	}
	return &store.Cursor{At: time.Unix(n, 0).UTC(), ID: parts[1]}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

func (s *Server) listRecords(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	q := r.URL.Query()
	f := store.Filter{OrgID: q.Get("org"), Status: q.Get("status"), Query: q.Get("q"), From: parseTime(q.Get("from")),
		To: parseTime(q.Get("to")), Before: decodeCursor(q.Get("cursor"))}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	for _, t := range strings.Split(q.Get("type"), ",") {
		if t != "" {
			f.Types = append(f.Types, domain.RecordType(t))
		}
	}
	recs, next, err := s.St.Records(r.Context(), p.Viewer, f)
	if err != nil {
		fail(w, r, err)
		return
	}
	if recs == nil {
		recs = []domain.Record{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": recs, "next": encodeCursor(next)})
}

func (s *Server) getRecord(w http.ResponseWriter, r *http.Request) {
	rec, err := s.St.Record(r.Context(), principal(r.Context()).Viewer, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) recordVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := s.St.Versions(r.Context(), principal(r.Context()).Viewer, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	if len(vs) == 0 {
		fail(w, r, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, vs)
}

// recordProof returns everything needed to verify a record independently: the
// hashed document, the signature with its public key, and the Merkle path to the
// root anchored on the ledger.
func (s *Server) recordProof(w http.ResponseWriter, r *http.Request) {
	rec, err := s.St.Record(r.Context(), principal(r.Context()).Viewer, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	pp, err := s.Passport.Build(r.Context(), rec.ID, &principal(r.Context()).Viewer)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pp.Trust)
}

func (s *Server) submitRecords(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var body struct {
		Records []ingest.Input `json:"records"`
	}
	if !decode(w, r, &body, 8<<20) {
		return
	}
	if len(body.Records) == 0 || len(body.Records) > ingest.MaxSync {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Batch size", "Send between 1 and 1,000 records per request.")
		return
	}
	s.process(w, r, p, "api", "", body.Records)
}

// process runs one submission end to end, with idempotency.
func (s *Server) process(w http.ResponseWriter, r *http.Request, p *Principal, channel, recType string, rows []ingest.Input) {
	if !s.demoRecords(w, r, p, len(rows)) {
		return
	}
	in := store.Ingest{ID: auth.ID("ing_"), OrgID: p.Org.ID, Channel: channel, RecordType: recType, Status: "running",
		Total: len(rows), IdempotencyKey: r.Header.Get("Idempotency-Key"), CreatedBy: p.Actor()}
	prev, err := s.St.CreateIngest(r.Context(), in)
	if errors.Is(err, store.ErrConflict) {
		w.Header().Set("Idempotent-Replay", "true")
		writeJSON(w, http.StatusOK, prev)
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	// The outcome is written even when the client has gone away: a cancelled
	// request must not leave the batch "running" for ever.
	done := context.WithoutCancel(r.Context())
	results, rowErrs, err := s.Ingest.Process(r.Context(), p.Org, p.IngestActor(), p.Duties(), in.ID, rows)
	if err != nil {
		in.Status = "failed"
		in.Errors = append(in.Errors, map[string]any{"row": 0, "ref": "", "errors": []string{"The request ended before the batch finished; nothing was stored. Submit it again."}})
		_ = s.St.FinishIngest(done, in)
		fail(w, r, err)
		return
	}
	if len(rowErrs) > 0 {
		in.Status, in.Rejected = "failed", len(rowErrs)
		for _, e := range rowErrs {
			in.Errors = append(in.Errors, map[string]any{"row": e.Row, "ref": e.Ref, "errors": e.Errors})
		}
		_ = s.St.FinishIngest(done, in)
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Some rows are invalid",
			"Nothing was stored. Fix the rows listed and submit again.", rowErrs)
		return
	}
	in.Status, in.Accepted = "done", len(results)
	_ = s.St.FinishIngest(done, in)
	writeJSON(w, http.StatusCreated, map[string]any{"ingest": in.ID, "results": results})
}

func (s *Server) receipt(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in ingest.ReceiptIn
	if !decode(w, r, &in, 8192) {
		return
	}
	if in.ReceivedAt.IsZero() {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Receipt incomplete", "Send receivedAt, massKg and your decision.")
		return
	}
	rc, err := s.Ingest.Receipt(r.Context(), p.Org, p.IngestActor(), p.Duties(), r.PathValue("id"), in)
	if errors.Is(err, ingest.ErrReview) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Receipt not signed", strings.TrimPrefix(err.Error(), "review: "))
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "shipment.receipt", r.PathValue("id"), map[string]any{"decision": rc.Decision})
	writeJSON(w, http.StatusOK, rc)
}

// voidRecord writes a void version of one of the caller's records (signed, checked
// and sealed like any correction; the old versions stay).
func (s *Server) voidRecord(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in ingest.VoidIn
	if !decode(w, r, &in, 8192) {
		return
	}
	row, err := s.Ingest.VoidInput(r.Context(), p.Org, r.PathValue("id"), in)
	if errors.Is(err, ingest.ErrReview) {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Record not voided", strings.TrimPrefix(err.Error(), "review: "))
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	s.process(w, r, p, "api", row.Type, []ingest.Input{row})
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	recs, _, err := s.St.Records(r.Context(), p.Viewer, store.Filter{Limit: 25,
		Types: []domain.RecordType{domain.TypeRejectLot, domain.TypeRecycledLot, domain.TypeAdditiveBatch, domain.TypeAsphaltLot,
			domain.TypeRoadSection, domain.TypeShipment, domain.TypeAcceptance}})
	if err != nil {
		fail(w, r, err)
		return
	}
	if recs == nil {
		recs = []domain.Record{}
	}
	writeJSON(w, http.StatusOK, recs)
}

// ---------------------------------------------------------------- findings

func (s *Server) listFindings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.FindingFilter{Status: q.Get("status"), Code: q.Get("code"), RecordID: q.Get("record")}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Offset, _ = strconv.Atoi(q.Get("offset"))
	items, total, err := s.St.Findings(r.Context(), principal(r.Context()).Viewer, f)
	if err != nil {
		fail(w, r, err)
		return
	}
	if items == nil {
		items = []store.Finding{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) findingSummary(w http.ResponseWriter, r *http.Request) {
	counts, err := s.St.FindingCounts(r.Context(), principal(r.Context()).Viewer)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

func (s *Server) resolveFinding(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Status string `json:"status"` // resolved | accepted
		Reason string `json:"reason"`
	}
	if !decode(w, r, &in, 4096) {
		return
	}
	if in.Status != "resolved" && in.Status != "accepted" {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Invalid status", "Use resolved or accepted.")
		return
	}
	if len(strings.TrimSpace(in.Reason)) < 5 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Reason required", "Say why, in a sentence; it is kept in the audit trail.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, r, store.ErrNotFound)
		return
	}
	// Who may close what: an owner explains a warning away; a blocker is fixed by a
	// correction (new version), or accepted by an independent party with a signed
	// acceptance record.
	cur, err := s.St.FindingByID(r.Context(), store.SystemViewer, id)
	if err != nil {
		fail(w, r, err)
		return
	}
	independent := p.Viewer.Oversight
	if !independent && p.Org.Kind == domain.KindRoadOwner {
		// A road owner is independent for its own roads only.
		if independent, err = s.St.FindingParty(r.Context(), p.Org.ID, id); err != nil {
			fail(w, r, err)
			return
		}
	}
	switch {
	case cur.Status != "open":
		writeProblem(w, r, http.StatusConflict, "Already closed", "")
		return
	case in.Status == "resolved" && (cur.Severity != "warning" || (cur.OrgID != p.Org.ID && !p.Viewer.Oversight)):
		writeProblem(w, r, http.StatusForbidden, "Cannot resolve", "Only the record owner can resolve a warning; blockers need a correction or an independent acceptance.")
		return
	case in.Status == "accepted" && !independent:
		writeProblem(w, r, http.StatusForbidden, "Independent acceptance required", "Auditors, the operator, or the owner of the road it concerns accept findings.")
		return
	}
	if in.Status == "accepted" {
		// The acceptance is itself signed evidence, stored before the finding closes.
		_, rowErrs, err := s.Ingest.Process(r.Context(), p.Org, p.IngestActor(), p.Duties(), "", []ingest.Input{{Type: string(domain.TypeAcceptance),
			Ref: "ACC-" + cur.Code + "-" + cur.RecordID + "-" + strconv.FormatInt(time.Now().Unix(), 36), OccurredAt: time.Now().UTC(),
			Data: map[string]any{"subject_id": cur.RecordID, "finding_code": cur.Code, "reason": in.Reason}}})
		if err != nil {
			fail(w, r, err)
			return
		}
		if len(rowErrs) > 0 {
			writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Acceptance rejected", "", rowErrs)
			return
		}
	}
	f, err := s.St.CloseFinding(r.Context(), store.SystemViewer, id, in.Status, p.Actor(), in.Reason)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "finding."+in.Status, f.RecordID, map[string]any{"code": f.Code, "reason": in.Reason})
	s.Cache.Publish(r.Context(), cacheEvent("finding", p.Org.ID, f.RecordID))
	writeJSON(w, http.StatusOK, f)
}
