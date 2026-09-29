package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/ingest"
	"github.com/sensifai/roadpass/internal/ngsild"
	"github.com/sensifai/roadpass/internal/store"
)

// NGSI-LD API (ETSI GS CIM 009) over the same records and pipeline as /api/v1.
func (s *Server) ngsiRoutes(mux *http.ServeMux) {
	read := guard{scope: "records:read"}
	write := guard{role: "editor", scope: "records:write"}
	mux.HandleFunc("GET "+ngsild.ContextPath, s.ngsiContext)
	mux.HandleFunc("GET /ngsi-ld/v1/types", s.with(read, s.ngsiTypes))
	mux.HandleFunc("GET /ngsi-ld/v1/entities", s.with(read, s.ngsiQuery))
	mux.HandleFunc("GET /ngsi-ld/v1/entities/{id}", s.with(read, s.ngsiGet))
	mux.HandleFunc("POST /ngsi-ld/v1/entities", s.with(write, s.ngsiCreate))
	mux.HandleFunc("PATCH /ngsi-ld/v1/entities/{id}/attrs", s.with(write, s.ngsiPatch))
	mux.HandleFunc("POST /ngsi-ld/v1/entityOperations/create", s.with(write, s.ngsiBatch(false)))
	mux.HandleFunc("POST /ngsi-ld/v1/entityOperations/upsert", s.with(write, s.ngsiBatch(true)))
}

func (s *Server) contextURL() string { return s.Cfg.PublicURL + ngsild.ContextPath }

func ngsiError(w http.ResponseWriter, status int, typ, title, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ngsild.Error{Type: typ, Title: title, Detail: detail})
}

func (s *Server) ngsiFail(w http.ResponseWriter, err error) {
	var e *ngsild.Error
	switch {
	case errors.As(err, &e):
		code := http.StatusBadRequest
		if e.Type == ngsild.ErrLdContextNotAvail {
			code = http.StatusServiceUnavailable
		}
		ngsiError(w, code, e.Type, e.Title, e.Detail)
	case errors.Is(err, store.ErrNotFound):
		ngsiError(w, http.StatusNotFound, ngsild.ErrResourceNotFound, "Resource not found", "")
	case errors.Is(err, store.ErrConflict):
		ngsiError(w, http.StatusConflict, ngsild.ErrAlreadyExists, "Already exists", err.Error())
	default:
		ngsiError(w, http.StatusInternalServerError, ngsild.ErrInvalidRequest, "Internal error", "")
	}
}

// respond writes NGSI-LD JSON honouring Accept: ld+json embeds @context, plain
// JSON gets it in a Link header.
func (s *Server) ngsiRespond(w http.ResponseWriter, r *http.Request, status int, v any) {
	if strings.Contains(r.Header.Get("Accept"), ngsild.MediaLD) {
		switch x := v.(type) {
		case ngsild.Entity:
			x["@context"] = s.contextURL()
		case []ngsild.Entity:
			for _, e := range x {
				e["@context"] = s.contextURL()
			}
		}
		w.Header().Set("Content-Type", ngsild.MediaLD)
	} else {
		w.Header().Set("Content-Type", ngsild.MediaJSON)
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="http://www.w3.org/ns/json-ld#context"; type="application/ld+json"`, s.contextURL()))
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) ngsiContext(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", ngsild.MediaLD)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(w).Encode(ngsild.Context())
}

func (s *Server) ngsiTypes(w http.ResponseWriter, r *http.Request) {
	type typ struct {
		ID       string   `json:"id"`
		Type     string   `json:"type"`
		TypeName string   `json:"typeName"`
		Attrs    []string `json:"attributeNames"`
	}
	var out []typ
	for _, rt := range domain.RecordTypes {
		et := ngsild.EntityTypes[rt]
		attrs := []string{ngsild.AttrReference, ngsild.AttrDate, ngsild.AttrMass, ngsild.AttrIssuedBy, ngsild.AttrProvenance}
		for _, f := range domain.Fields[rt] {
			attrs = append(attrs, ngsild.Camel(f.Name))
		}
		out = append(out, typ{ID: ngsild.Vocab + et, Type: "EntityTypeInfo", TypeName: et, Attrs: attrs})
	}
	s.ngsiRespond(w, r, http.StatusOK, out)
}

// readEntities decodes a body of one entity or an array, checking content type
// and @context.
func (s *Server) readEntities(w http.ResponseWriter, r *http.Request, many bool) ([]ngsild.Entity, bool) {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != ngsild.MediaJSON && mt != ngsild.MediaLD {
		ngsiError(w, http.StatusUnsupportedMediaType, ngsild.ErrInvalidRequest, "Unsupported media type", "Use application/json or application/ld+json.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		ngsiError(w, http.StatusBadRequest, ngsild.ErrInvalidRequest, "Invalid JSON", err.Error())
		return nil, false
	}
	var es []ngsild.Entity
	if many {
		if err := json.Unmarshal(raw, &es); err != nil {
			ngsiError(w, http.StatusBadRequest, ngsild.ErrInvalidRequest, "Expected an array of entities", err.Error())
			return nil, false
		}
	} else {
		var e ngsild.Entity
		if err := json.Unmarshal(raw, &e); err != nil {
			ngsiError(w, http.StatusBadRequest, ngsild.ErrInvalidRequest, "Expected an entity", err.Error())
			return nil, false
		}
		es = []ngsild.Entity{e}
	}
	// The context comes from the body (ld+json) or the Link header (json), never both.
	link := r.Header.Get("Link")
	for _, e := range es {
		ctx, inBody := e["@context"]
		if mt == ngsild.MediaLD && !inBody {
			ngsiError(w, http.StatusBadRequest, ngsild.ErrBadRequestData, "Missing @context", "application/ld+json bodies carry @context.")
			return nil, false
		}
		if mt == ngsild.MediaJSON && inBody {
			ngsiError(w, http.StatusBadRequest, ngsild.ErrBadRequestData, "Unexpected @context", "With application/json, send the context in a Link header.")
			return nil, false
		}
		if mt == ngsild.MediaJSON && link != "" {
			if i, j := strings.Index(link, "<"), strings.Index(link, ">"); i >= 0 && j > i {
				ctx = link[i+1 : j]
			}
		}
		if err := ngsild.CheckContext(ctx, s.contextURL()); err != nil {
			s.ngsiFail(w, err)
			return nil, false
		}
	}
	return es, true
}

func toInputs(es []ngsild.Entity, orgID string) ([]ingest.Input, error) {
	out := make([]ingest.Input, len(es))
	for i, e := range es {
		rec, err := ngsild.ToRecord(e, orgID)
		if err != nil {
			return nil, err
		}
		out[i] = ingest.Input{ID: rec.ID, Type: string(rec.Type), Ref: rec.Ref, SiteID: rec.SiteID, OccurredAt: rec.OccurredAt,
			MassKg: rec.MassKg, Inputs: rec.Inputs, Data: rec.Body, Provenance: rec.Provenance}
	}
	return out, nil
}

// ngsiCreate creates one entity. A client-chosen id is not accepted for a new
// record (ids are issued by the platform); use upsert with the issued id to correct.
func (s *Server) ngsiCreate(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	es, ok := s.readEntities(w, r, false)
	if !ok {
		return
	}
	if id, _ := es[0]["id"].(string); id != "" {
		if _, err := s.St.RecordAny(r.Context(), ngsild.RecordIDOf(id)); err == nil {
			ngsiError(w, http.StatusConflict, ngsild.ErrAlreadyExists, "Already exists", id)
			return
		}
		delete(es[0], "id")
	}
	ins, err := toInputs(es, p.Org.ID)
	if err != nil {
		s.ngsiFail(w, err)
		return
	}
	res, rowErrs, err := s.Ingest.Process(r.Context(), p.Org, p.IngestActor(), p.Duties(), "", ins)
	if err != nil {
		fail(w, r, err)
		return
	}
	if len(rowErrs) > 0 {
		b, _ := json.Marshal(rowErrs[0].Errors)
		ngsiError(w, http.StatusBadRequest, ngsild.ErrBadRequestData, "Invalid entity", string(b))
		return
	}
	w.Header().Set("Location", "/ngsi-ld/v1/entities/"+ngsild.EntityID(domain.RecordType(ins[0].Type), res[0].ID))
	w.WriteHeader(http.StatusCreated)
}

// ngsiPatch adds a new version with the patched attributes.
func (s *Server) ngsiPatch(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	id := ngsild.RecordIDOf(r.PathValue("id"))
	cur, err := s.St.Record(r.Context(), p.Viewer, id)
	if err != nil {
		s.ngsiFail(w, err)
		return
	}
	es, ok := s.readEntities(w, r, false)
	if !ok {
		return
	}
	merged := ngsild.FromRecord(&cur, nil, nil, nil)
	for k, v := range es[0] {
		if k == "@context" || k == "id" || k == "type" {
			continue
		}
		merged[k] = v
	}
	merged["id"] = ngsild.EntityID(cur.Type, cur.ID)
	ins, err := toInputs([]ngsild.Entity{roundTrip(merged)}, p.Org.ID)
	if err != nil {
		s.ngsiFail(w, err)
		return
	}
	_, rowErrs, err := s.Ingest.Process(r.Context(), p.Org, p.IngestActor(), p.Duties(), "", ins)
	if err != nil {
		fail(w, r, err)
		return
	}
	if len(rowErrs) > 0 {
		b, _ := json.Marshal(rowErrs[0].Errors)
		ngsiError(w, http.StatusBadRequest, ngsild.ErrBadRequestData, "Invalid attributes", string(b))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func roundTrip(e ngsild.Entity) ngsild.Entity {
	b, _ := json.Marshal(e)
	var out ngsild.Entity
	_ = json.Unmarshal(b, &out)
	return out
}

// ngsiBatch implements entityOperations/create and /upsert (all-or-nothing; a
// failure is reported per entity with 400, success with 201/204).
func (s *Server) ngsiBatch(upsert bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := principal(r.Context())
		es, ok := s.readEntities(w, r, true)
		if !ok {
			return
		}
		if len(es) == 0 || len(es) > ingest.MaxSync {
			ngsiError(w, http.StatusBadRequest, ngsild.ErrBadRequestData, "Batch size", "1–1,000 entities per request.")
			return
		}
		for _, e := range es {
			if id, _ := e["id"].(string); id != "" {
				_, err := s.St.RecordAny(r.Context(), ngsild.RecordIDOf(id))
				switch {
				case err == nil && !upsert:
					ngsiError(w, http.StatusConflict, ngsild.ErrAlreadyExists, "Already exists", id)
					return
				case errors.Is(err, store.ErrNotFound):
					delete(e, "id") // new record: the platform issues the id
				}
			}
		}
		ins, err := toInputs(es, p.Org.ID)
		if err != nil {
			s.ngsiFail(w, err)
			return
		}
		res, rowErrs, err := s.Ingest.Process(r.Context(), p.Org, p.IngestActor(), p.Duties(), "", ins)
		if err != nil {
			fail(w, r, err)
			return
		}
		if len(rowErrs) > 0 {
			type fail struct {
				EntityID string       `json:"entityId"`
				Error    ngsild.Error `json:"error"`
			}
			out := struct {
				Success []string `json:"success"`
				Errors  []fail   `json:"errors"`
			}{Success: []string{}}
			for _, e := range rowErrs {
				b, _ := json.Marshal(e.Errors)
				out.Errors = append(out.Errors, fail{EntityID: fmt.Sprintf("row %d (%s)", e.Row, e.Ref),
					Error: ngsild.Error{Type: ngsild.ErrBadRequestData, Title: "Invalid entity", Detail: string(b)}})
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		ids := make([]string, len(res))
		for i, x := range res {
			ids[i] = ngsild.EntityID(domain.RecordType(ins[i].Type), x.ID)
		}
		s.ngsiRespond(w, r, http.StatusCreated, ids)
	}
}

func (s *Server) entity(r *http.Request, rec *domain.Record) ngsild.Entity {
	var loc *ngsild.Location
	if lat := domain.Num(rec.Body, "lat"); lat != 0 {
		loc = &ngsild.Location{Lat: lat, Lon: domain.Num(rec.Body, "lon")}
	}
	pr := &ngsild.Proof{ContentHash: rec.ContentHash, Algorithm: "sha256/jcs-rfc8785/roadpass-record-v1", Status: string(rec.Status),
		SealID: rec.SealID}
	if rec.SealID != "" {
		li := rec.LeafIndex
		pr.LeafIndex = &li
	}
	if rec.Signature != nil {
		pr.KeyID, pr.Signature = rec.Signature.KeyID, b64(rec.Signature.Value)
	}
	if rec.Receipt != nil {
		pr.CoKeyID, pr.CoSignature = rec.Receipt.Signature.KeyID, b64(rec.Receipt.Signature.Value)
	}
	e := ngsild.FromRecord(rec, loc, pr, func(id string) (domain.RecordType, bool) {
		for _, t := range domain.RecordTypes {
			if strings.HasPrefix(id, domain.Types[t].Prefix+"-") {
				return t, true
			}
		}
		return "", false
	})
	if r.URL.Query().Get("options") == "keyValues" || strings.Contains(r.URL.Query().Get("format"), "simplified") {
		return ngsild.KeyValues(e)
	}
	return e
}

func (s *Server) ngsiGet(w http.ResponseWriter, r *http.Request) {
	rec, err := s.St.Record(r.Context(), principal(r.Context()).Viewer, ngsild.RecordIDOf(r.PathValue("id")))
	if err != nil {
		s.ngsiFail(w, err)
		return
	}
	s.ngsiRespond(w, r, http.StatusOK, s.entity(r, &rec))
}

// ngsiQuery supports type, id, q on mass/dateObserved/reference, limit/offset and
// count — the subset partners use to synchronise.
func (s *Server) ngsiQuery(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	q := r.URL.Query()
	f := store.Filter{}
	for _, t := range strings.Split(q.Get("type"), ",") {
		if t == "" {
			continue
		}
		rt, ok := ngsild.RecordTypeOf(t)
		if !ok {
			ngsiError(w, http.StatusBadRequest, ngsild.ErrBadRequestData, "Unknown type", t)
			return
		}
		f.Types = append(f.Types, rt)
	}
	if ids := q.Get("id"); ids != "" && len(f.Types) == 0 {
		var out []ngsild.Entity
		for _, id := range strings.Split(ids, ",") {
			if rec, err := s.St.Record(r.Context(), p.Viewer, ngsild.RecordIDOf(id)); err == nil {
				out = append(out, s.entity(r, &rec))
			}
		}
		s.ngsiRespond(w, r, http.StatusOK, nonNil(out))
		return
	}
	if len(f.Types) == 0 {
		ngsiError(w, http.StatusBadRequest, ngsild.ErrBadRequestData, "type or id required", "NGSI-LD queries need a type or an id list.")
		return
	}
	for _, cond := range strings.Split(q.Get("q"), ";") {
		cond = strings.TrimSpace(cond)
		switch {
		case cond == "":
		case strings.HasPrefix(cond, "dateObserved>="):
			f.From = parseTime(strings.Trim(strings.TrimPrefix(cond, "dateObserved>="), `"`))
		case strings.HasPrefix(cond, "dateObserved<"):
			f.To = parseTime(strings.Trim(strings.TrimPrefix(cond, "dateObserved<"), `"`))
		case strings.HasPrefix(cond, "reference=="):
			f.Query = strings.Trim(strings.TrimPrefix(cond, "reference=="), `"`)
		default:
			ngsiError(w, http.StatusBadRequest, ngsild.ErrOperationNotSupport, "Query not supported",
				"Supported: dateObserved>=…, dateObserved<…, reference==…")
			return
		}
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		ngsiError(w, http.StatusForbidden, ngsild.ErrBadRequestData, "Too many results requested", "limit ≤ 500")
		return
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	// Offset pagination is emulated over the keyset cursor (bounded by 10,000).
	if offset > 10000 {
		ngsiError(w, http.StatusBadRequest, ngsild.ErrBadRequestData, "Offset too large", "Narrow the query with dateObserved.")
		return
	}
	f.Limit = min(500, offset+limit)
	var recs []domain.Record
	for len(recs) < offset+limit {
		page, next, err := s.St.Records(r.Context(), p.Viewer, f)
		if err != nil {
			fail(w, r, err)
			return
		}
		recs = append(recs, page...)
		if next == nil {
			break
		}
		f.Before = next
	}
	if offset < len(recs) {
		recs = recs[offset:min(len(recs), offset+limit)]
	} else {
		recs = nil
	}
	out := make([]ngsild.Entity, 0, len(recs))
	for i := range recs {
		out = append(out, s.entity(r, &recs[i]))
	}
	if q.Get("count") == "true" {
		f.Before, f.Limit = nil, 0
		n, err := s.St.CountRecords(r.Context(), p.Viewer, f)
		if err != nil {
			fail(w, r, err)
			return
		}
		w.Header().Set("NGSILD-Results-Count", strconv.FormatInt(n, 10))
	}
	s.ngsiRespond(w, r, http.StatusOK, out)
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
