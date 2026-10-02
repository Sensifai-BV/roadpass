package api

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/ingest"
)

// MaxCSVRows is the largest CSV accepted in one upload (processed in one
// transaction: all rows or none).
const MaxCSVRows = 5000

func csvColumns(t domain.RecordType) []string {
	cols := []string{"ref", "site_id", "occurred_at", "mass_kg"}
	if t != domain.TypeRoadSection && t != domain.TypeLabTest && t != domain.TypeAcceptance && t != domain.TypeRejectLot {
		cols = append(cols, "inputs")
	}
	for _, f := range domain.Fields[t] {
		if !f.Legacy {
			cols = append(cols, f.Name)
		}
	}
	return cols
}

// templateExample is a worked example per record type: inputs of the type it
// consumes, and masses that agree with the fields' examples (an asphalt lot of
// 64 t with 320 kg of additive is dosed at 0.5 %; a 10 t additive batch declared
// 92 % recycled takes 9.2 t of recycled plastic). The ids are placeholders.
var templateExample = map[domain.RecordType]struct{ mass, inputs string }{
	domain.TypeRejectLot:     {"18000", ""},
	domain.TypeRecycledLot:   {"12000", "PRL-26-000123:10000;PRL-26-000124:8000"},
	domain.TypeAdditiveBatch: {"10000", "RPL-26-000123:6000;RPL-26-000124:3200"},
	domain.TypeAsphaltLot:    {"64000", "ADB-26-000123:200;ADB-26-000124:120"},
	domain.TypeDelivery:      {"24000", "ASL-26-000123:24000"},
	domain.TypeShipment:      {"12000", "RPL-26-000123:12000"},
	domain.TypeRoadSection:   {"343000", ""},
	domain.TypeLabTest:       {"0", ""},
	domain.TypeRelease:       {"0", ""},
}

// template serves an example CSV for a record type: /api/v1/templates/asphalt_lot.csv
func (s *Server) template(w http.ResponseWriter, r *http.Request) {
	t := domain.RecordType(strings.TrimSuffix(r.PathValue("file"), ".csv"))
	if !t.Valid() || t == domain.TypeAcceptance {
		writeProblem(w, r, http.StatusNotFound, "Unknown record type", "")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="roadpass-%s-template.csv"`, t))
	cw := csv.NewWriter(w)
	cols := csvColumns(t)
	_ = cw.Write(cols)
	ex := make([]string, len(cols))
	for i, c := range cols {
		switch c {
		case "ref":
			ex[i] = "YOUR-REF-001"
		case "site_id":
			ex[i] = "site_…"
		case "occurred_at":
			ex[i] = "2026-09-14 08:30"
		case "mass_kg":
			ex[i] = templateExample[t].mass
		case "inputs":
			ex[i] = templateExample[t].inputs
		default:
			for _, f := range domain.Fields[t] {
				if f.Name == c {
					ex[i] = f.Example
				}
			}
		}
	}
	_ = cw.Write(ex)
	cw.Flush()
}

// parseCSV turns an upload into ingest inputs. It accepts comma or semicolon
// separators (many European Excel exports use ";"), a UTF-8 BOM, decimal commas, and
// times in RFC 3339 or "YYYY-MM-DD HH:MM" (local time in loc).
func parseCSV(t domain.RecordType, body io.Reader, loc *time.Location) ([]ingest.Input, []ingest.RowError, error) {
	br := bufio.NewReader(body)
	head, _ := br.Peek(4096)
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	sep := ','
	if first, _, _ := bytes.Cut(head, []byte("\n")); bytes.Count(first, []byte(";")) > bytes.Count(first, []byte(",")) {
		sep = ';'
	}
	if bom, _ := br.Peek(3); bytes.Equal(bom, []byte("\xef\xbb\xbf")) {
		_, _ = br.Discard(3)
	}
	cr := csv.NewReader(br)
	cr.Comma = sep
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read the header row: %w", err)
	}
	known := map[string]bool{}
	for _, c := range csvColumns(t) {
		known[c] = true
	}
	known["provenance"] = true
	idx := map[string]int{}
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(h))
		if !known[h] {
			return nil, nil, fmt.Errorf("unknown column %q for %s; download the template to see the columns", h, domain.Types[t].Plural)
		}
		idx[h] = i
	}
	for _, need := range []string{"ref", "occurred_at"} {
		if _, ok := idx[need]; !ok {
			return nil, nil, fmt.Errorf("missing column %q", need)
		}
	}
	var rows []ingest.Input
	var errs []ingest.RowError
	for n := 2; ; n++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", n, err)
		}
		if len(rows) >= MaxCSVRows {
			return nil, nil, fmt.Errorf("more than %d rows; split the file or use the API", MaxCSVRows)
		}
		get := func(c string) string {
			if i, ok := idx[c]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		in := ingest.Input{Type: string(t), Ref: get("ref"), SiteID: get("site_id"), Provenance: get("provenance"), Data: map[string]any{}}
		var fe []domain.FieldError
		if at, err := parseLocalTime(get("occurred_at"), loc); err != nil {
			fe = append(fe, domain.FieldError{Field: "occurred_at", Message: "use 2026-09-14 08:30 or RFC 3339"})
		} else {
			in.OccurredAt = at
		}
		if m := get("mass_kg"); m != "" {
			v, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64)
			if err != nil {
				fe = append(fe, domain.FieldError{Field: "mass_kg", Message: "must be a number"})
			}
			in.MassKg = v
		}
		if s := get("inputs"); s != "" {
			for _, part := range strings.Split(s, ";") {
				id, kg, ok := strings.Cut(strings.TrimSpace(part), ":")
				v, err := strconv.ParseFloat(strings.ReplaceAll(kg, ",", "."), 64)
				if !ok || err != nil {
					fe = append(fe, domain.FieldError{Field: "inputs", Message: "write inputs as ID:kg;ID:kg"})
					break
				}
				in.Inputs = append(in.Inputs, domain.Input{ID: strings.TrimSpace(id), MassKg: v})
			}
		}
		for _, f := range domain.Fields[t] {
			v := get(f.Name)
			if v == "" {
				continue
			}
			if f.Kind == domain.FTime {
				at, err := parseLocalTime(v, loc)
				if err != nil {
					fe = append(fe, domain.FieldError{Field: f.Name, Message: "use 2026-09-14 08:30 or RFC 3339"})
					continue
				}
				v = at.Format(time.RFC3339)
			}
			in.Data[f.Name] = v
		}
		if len(fe) > 0 {
			errs = append(errs, ingest.RowError{Row: n, Ref: in.Ref, Errors: fe})
		}
		rows = append(rows, in)
	}
	return rows, errs, nil
}

func parseLocalTime(s string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02", "02/01/2006 15:04", "02/01/2006"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("bad time %q", s)
}

// importCSV handles one upload: ?type=…&dryRun=1 validates only.
func (s *Server) importCSV(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	t := domain.RecordType(r.URL.Query().Get("type"))
	if !t.Valid() || t == domain.TypeAcceptance {
		writeProblem(w, r, http.StatusBadRequest, "Choose a record type", "")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	f, _, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "Attach a CSV file", "Send multipart/form-data with a file field.")
		return
	}
	defer f.Close()
	// Times without an offset are the uploader's local time: ?tz=<IANA zone>
	// (the dashboard sends the browser's), else UTC.
	loc := time.UTC
	if tz := r.URL.Query().Get("tz"); tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown time zone", fmt.Sprintf("%q is not an IANA time zone such as Europe/Berlin.", tz))
			return
		}
		loc = l
	}
	rows, rowErrs, err := parseCSV(t, f, loc)
	if err != nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Cannot read the CSV", err.Error())
		return
	}
	if len(rows) == 0 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "The CSV has no rows", "")
		return
	}
	if r.URL.Query().Get("dryRun") == "1" {
		_, verrs := ingest.Validate(rows)
		for i := range verrs {
			verrs[i].Row++ // header line
		}
		all := append(rowErrs, verrs...)
		preview := rows
		if len(preview) > 20 {
			preview = preview[:20]
		}
		writeJSON(w, http.StatusOK, map[string]any{"rows": len(rows), "valid": len(all) == 0, "errors": nonNil(all),
			"columns": csvColumns(t), "preview": preview})
		return
	}
	if len(rowErrs) > 0 {
		writeProblemErrs(w, r, http.StatusUnprocessableEntity, "Some rows are invalid", "Nothing was stored.", rowErrs)
		return
	}
	s.process(w, r, p, "csv", string(t), rows)
}

func (s *Server) listImports(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.Ingests(r.Context(), principal(r.Context()).Org.ID, 50)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) getImport(w http.ResponseWriter, r *http.Request) {
	in, err := s.St.GetIngest(r.Context(), principal(r.Context()).Org.ID, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}
