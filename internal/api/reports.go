package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sensifai/roadpass/internal/compliance"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/report"
	"github.com/sensifai/roadpass/internal/store"
)

// Downloads: every page's content as PDF, Word, Excel or CSV. A builder turns
// the same data the page shows, in the same viewer scope, into a report.Report;
// the renderer is shared, so every download looks and checks the same.

type reportBuilder func(s *Server, r *http.Request) (*report.Report, error)

var reportBuilders = map[string]reportBuilder{
	"overview":   (*Server).reportOverview,
	"passports":  (*Server).reportPassports,
	"passport":   (*Server).reportPassport,
	"quality":    (*Server).reportQuality,
	"carbon":     (*Server).reportCarbon,
	"ledger":     (*Server).reportLedger,
	"status":     (*Server).reportStatus,
	"roads":      (*Server).reportRoads,
	"team":       (*Server).reportTeam,
	"alerts":     (*Server).reportAlerts,
	"dashboard":  (*Server).reportDashboard,
	"invoices":   (*Server).reportInvoices,
	"compliance": (*Server).reportCompliance,
	"register":   (*Server).reportRegister,
}

var errReportForbidden = errors.New("not allowed")

// downloadReport serves GET /api/v1/reports/{kind}?format=pdf|docx|xlsx|csv|json&…
func (s *Server) downloadReport(w http.ResponseWriter, r *http.Request) {
	b, ok := reportBuilders[r.PathValue("kind")]
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "No such report", "")
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "pdf"
	}
	ft, ok := report.Formats[format]
	if !ok {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Unknown format", "Use pdf, docx, xlsx, csv or json.")
		return
	}
	// The same caller asking for the same rendering within a minute gets it from
	// Redis (reports can be heavy).
	rep, err := b(s, r)
	if errors.Is(err, errReportForbidden) {
		writeProblem(w, r, http.StatusForbidden, "Not allowed", "")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	p := principal(r.Context())
	rep.Generated, rep.By = time.Now().UTC(), p.Actor()
	if p.User != nil {
		rep.By = p.User.Name
	}
	if rep.Org == "" {
		rep.Org = p.Org.Name
	}
	// The downloading organisation's letterhead (it issues the document).
	rep.Brand = compliance.Letterhead(r.Context(), s.St, p.Org)
	w.Header().Set("Content-Type", ft.Type)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, rep.FileName(format)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Report-Fingerprint", rep.Fingerprint())
	if format == "csv" {
		idx, _ := strconv.Atoi(r.URL.Query().Get("table"))
		_ = rep.CSV(w, idx)
		return
	}
	if err := rep.Render(w, format); err != nil {
		slogReportError(r, err)
	}
	_ = s.St.Audit(context.WithoutCancel(r.Context()), p.Org.ID, p.Actor(), "report.download", r.PathValue("kind"), map[string]string{"format": format, "fingerprint": rep.Fingerprint()})
}

func slogReportError(r *http.Request, err error) {
	fmt.Println("report render failed:", r.URL.Path, err) // the response has started; nothing else to tell the client
}

// ---------------------------------------------------------------- helpers

func tonnes(kg float64) string {
	switch a := kg; {
	case a >= 1e9 || a <= -1e9:
		return fmt.Sprintf("%.2f Mt", kg/1e9)
	case a >= 1e6 || a <= -1e6:
		return fmt.Sprintf("%.1f kt", kg/1e6)
	case a >= 1e3 || a <= -1e3:
		return fmt.Sprintf("%.1f t", kg/1e3)
	}
	return fmt.Sprintf("%.0f kg", kg)
}

func periodOf(rg analytics.Range) string {
	return rg.From.Format("2 Jan 2006") + " – " + rg.To.Add(-time.Second).Format("2 Jan 2006")
}

func (s *Server) simulatedNotice(ctx context.Context) string {
	var operational bool
	_ = s.St.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM records WHERE provenance = 'Operational' LIMIT 1)`).Scan(&operational)
	if operational {
		return ""
	}
	return "Simulated demonstration data. The figures describe a simulated supply chain, not real operations."
}

func (s *Server) orgNames(ctx context.Context) map[string]string {
	orgs, _ := s.St.Orgs(ctx)
	m := make(map[string]string, len(orgs))
	for _, o := range orgs {
		m[o.ID] = o.Name
	}
	return m
}

func typeName(t domain.RecordType) string {
	if i, ok := domain.Types[t]; ok {
		return i.Name
	}
	return string(t)
}

// ---------------------------------------------------------------- builders

func (s *Server) reportOverview(r *http.Request) (*report.Report, error) {
	ctx, sc, rg := r.Context(), scope(r), window(r)
	ov, err := s.CH.Overview(ctx, sc, rg)
	if err != nil {
		return nil, err
	}
	series, err := s.CH.ImpactSeries(ctx, sc, rg, analytics.GrainFor(rg))
	if err != nil {
		return nil, err
	}
	rep := &report.Report{Title: "Overview", Subtitle: "Recycled plastic from sorting plant to road", Period: periodOf(rg), Notice: s.simulatedNotice(ctx)}
	rep.KPIs = []report.KPI{
		{Label: "Recycled plastic laid", Value: tonnes(ov.Impact.RecycledKg), Hint: "in delivered asphalt"},
		{Label: "CO2e avoided", Value: tonnes(ov.Impact.AvoidedKg), Hint: "net of " + tonnes(ov.Impact.ChainKg) + " chain footprint"},
		{Label: "Asphalt with additive", Value: tonnes(ov.Impact.AsphaltKg), Hint: fmt.Sprintf("%d deliveries", ov.Impact.Deliveries)},
		{Label: "Road surfaced", Value: fmt.Sprintf("%.1f km", ov.Roads.LengthKm), Hint: fmt.Sprintf("%d sections", ov.Roads.Sections)},
	}
	stages := &report.Table{Name: "Records by type", Columns: []report.Column{{Title: "Record type", Weight: 2}, {Title: "Records", Align: report.Right}, {Title: "Mass (t)", Align: report.Right, Format: "#,##0.0"}}}
	for _, t := range ov.Types {
		stages.Rows = append(stages.Rows, []any{typeName(domain.RecordType(t.Type)), int(t.Count), t.MassKg / 1000})
	}
	var labels []string
	var values []float64
	trend := &report.Table{Name: "Recycled plastic over time", Columns: []report.Column{{Title: "Period"}, {Title: "Recycled plastic (t)", Align: report.Right, Format: "#,##0.0"}, {Title: "CO2e avoided (t)", Align: report.Right, Format: "#,##0.0"}}}
	byT := map[time.Time][2]float64{}
	var ts []time.Time
	for _, p := range series {
		v, seen := byT[p.T]
		if !seen {
			ts = append(ts, p.T)
		}
		switch p.Series {
		case "recycled":
			v[0] = p.Value
		case "avoided":
			v[1] = p.Value
		}
		byT[p.T] = v
	}
	slices.SortFunc(ts, func(a, b time.Time) int { return a.Compare(b) })
	for _, t := range ts {
		labels = append(labels, t.Format("Jan 06"))
		values = append(values, byT[t][0])
		trend.Rows = append(trend.Rows, []any{t, byT[t][0], byT[t][1]})
	}
	rep.Sections = []report.Section{
		{Heading: "Recycled plastic reaching roads", Text: []string{"Tonnes of recycled plastic in the asphalt delivered, by period, from signed delivery tickets."},
			Chart: &report.Chart{Kind: "bars", Labels: labels, Values: values, Unit: "t"}, Table: trend},
		{Heading: "What each step produced", Table: stages},
	}
	return rep, nil
}

func (s *Server) reportPassports(r *http.Request) (*report.Report, error) {
	p := principal(r.Context())
	q := r.URL.Query()
	f := store.Filter{Status: q.Get("status"), Query: q.Get("q"), OrgID: q.Get("org"), Limit: 5000}
	if t := domain.RecordType(q.Get("type")); t.Valid() {
		f.Types = []domain.RecordType{t}
	}
	recs, _, err := s.St.Records(r.Context(), p.Viewer, f)
	if err != nil {
		return nil, err
	}
	names := s.orgNames(r.Context())
	t := &report.Table{Name: "Passports", Columns: []report.Column{{Title: "Passport", Weight: 1.3}, {Title: "Type", Weight: 1.4}, {Title: "Company", Weight: 2}, {Title: "Date"},
		{Title: "Mass (t)", Align: report.Right, Format: "#,##0.00"}, {Title: "Status"}, {Title: "Sealed"}, {Title: "Fingerprint", Weight: 1.6}}}
	for _, x := range recs {
		t.Rows = append(t.Rows, []any{x.ID, typeName(x.Type), names[x.OrgID], x.OccurredAt, x.MassKg / 1000, string(x.Status), x.Sealed, x.ContentHash[:16] + "…"})
	}
	title := "Passports"
	if len(f.Types) == 1 {
		title = domain.Types[f.Types[0]].Name + " passports"
	}
	return &report.Report{Title: title, Subtitle: fmt.Sprintf("%d records", len(recs)), Notice: s.simulatedNotice(r.Context()),
		Sections: []report.Section{{Heading: "Records", Table: t}}}, nil
}

func (s *Server) reportPassport(r *http.Request) (*report.Report, error) {
	p := principal(r.Context())
	id := r.URL.Query().Get("id")
	pp, err := s.Passport.Build(r.Context(), id, &p.Viewer)
	if err != nil {
		return nil, err
	}
	org := pp.Orgs[pp.Root.OrgID].Name
	name := func(id string) string {
		if o, ok := pp.Orgs[id]; ok {
			return o.Name
		}
		return id
	}
	rep := &report.Report{Title: "Passport " + pp.Root.ID, Subtitle: typeName(pp.Root.Type) + " · " + org, Org: org}
	if pp.Simulated {
		rep.Notice = "Simulated demonstration data. This passport describes a simulated supply chain."
	}
	rep.KPIs = []report.KPI{
		{Label: "Mass", Value: tonnes(pp.Root.MassKg)},
		{Label: "Recycled plastic", Value: tonnes(pp.Carbon.RecycledKg), Hint: fmt.Sprintf("%.1f kg per tonne", pp.Carbon.RecycledPerT)},
		avoidedKPI(pp.Root.Type, pp.Carbon.AvoidedKg, pp.Carbon.NetBenefitKg),
		{Label: "Data checks", Value: map[domain.Status]string{"ok": "Passed", "flagged": "Warnings", "blocked": "Blocked", "accepted": "Accepted"}[pp.Root.Status], Hint: map[bool]string{true: "sealed on the ledger", false: "waiting for a seal"}[pp.Root.Sealed]},
	}
	// The QR link is the passport's random share link, created on first use by its maker.
	if pp.Root.OrgID == p.Org.ID && domain.Types[pp.Root.Type].Passport {
		if tok, err := s.St.EnsureShare(r.Context(), pp.Root.ID, p.Org.ID, p.Actor()); err == nil {
			rep.Link = &report.Link{URL: s.shareURL(tok), Caption: "Scan to open the public passport"}
		}
	}
	// The item itself: identity, then every field of its record that this viewer may see.
	det := &report.Table{Name: "Details", Columns: []report.Column{{Title: "Field", Weight: 1.4}, {Title: "Value", Weight: 3}}}
	det.Rows = append(det.Rows, []any{"Identifier", pp.Root.ID}, []any{"Type", typeName(pp.Root.Type)}, []any{"Made by", org},
		[]any{"Date", pp.Root.OccurredAt}, []any{"Mass (t)", pp.Root.MassKg / 1000})
	if pp.Root.Ref != "" {
		det.Rows = append(det.Rows, []any{"Company reference", pp.Root.Ref})
	}
	if pp.Root.SiteID != "" {
		if site, err := s.St.Site(r.Context(), pp.Root.SiteID); err == nil {
			det.Rows = append(det.Rows, []any{"Site", strings.Join(nonEmptyStr(site.Name, site.City), ", ")})
		}
	}
	for _, f := range domain.Fields[pp.Root.Type] {
		v, ok := pp.Root.Data[f.Name]
		if !ok || v == nil || v == "" {
			continue
		}
		label := f.Label
		if f.Unit != "" {
			label += " (" + f.Unit + ")"
		}
		switch {
		case f.Kind == domain.FOrg:
			v = name(fmt.Sprint(v))
		case f.Kind == domain.FSite:
			if site, err := s.St.Site(r.Context(), fmt.Sprint(v)); err == nil {
				v = strings.Join(nonEmptyStr(site.Name, site.Partner, site.City), ", ")
			}
		case f.Kind == domain.FTime:
			if t, err := time.Parse(time.RFC3339, fmt.Sprint(v)); err == nil {
				v = t
			}
		case f.Name == "lat" || f.Name == "lon":
			if x, ok := v.(float64); ok {
				v = fmt.Sprintf("%.5f", x) // about one metre
			}
		}
		det.Rows = append(det.Rows, []any{label, v})
	}
	rep.Sections = append(rep.Sections, report.Section{Heading: "Details", Table: det})
	c := pp.Carbon
	co := &report.Table{Name: "Carbon", Columns: []report.Column{{Title: "Figure", Weight: 2.2}, {Title: "kg CO2e", Align: report.Right, Format: "#,##0"}}}
	co.Rows = [][]any{{"Avoided by not landfilling or burning the plastic", c.DisposalKg}, {"Avoided by replacing virgin polymer", c.VirginKg},
		{"Total avoided", c.AvoidedKg}, {"Footprint of the recycling chain", c.ChainFootKg}, {"Net benefit", c.NetBenefitKg}}
	rep.Sections = append(rep.Sections, report.Section{Heading: "Carbon", Text: []string{fmt.Sprintf(
		"Scenario: %s. Computed from the material that ended up in this item, following each input by its share of mass; %.1f t·km of transport.", c.Scenario, c.TransportTkm)}, Table: co})
	stages := &report.Table{Name: "Journey", Columns: []report.Column{{Title: "Step", Weight: 1.4}, {Title: "Records", Align: report.Right}, {Title: "Mass (t)", Align: report.Right, Format: "#,##0.00"}, {Title: "Companies", Weight: 3}, {Title: "From"}, {Title: "To"}}}
	for _, st := range pp.Stages {
		var os []string
		for _, o := range st.Orgs {
			os = append(os, name(o))
		}
		stages.Rows = append(stages.Rows, []any{st.Name, st.Records, st.MassKg / 1000, strings.Join(os, ", "), st.First, st.Last})
	}
	origins := &report.Table{Name: "Origins", Columns: []report.Column{{Title: "Sorting plant", Weight: 2}, {Title: "Region"}, {Title: "Lots", Align: report.Right}, {Title: "Plastic (t)", Align: report.Right, Format: "#,##0.00"}, {Title: "Would have gone to"}}}
	for _, o := range pp.Origins {
		origins.Rows = append(origins.Rows, []any{name(o.OrgID), o.Region, o.Lots, o.Kg / 1000, o.Route})
	}
	rep.Sections = append(rep.Sections, report.Section{Heading: "Journey", Text: []string{"Each step that contributed material to this passport, with the share that ended up here."}, Table: stages},
		report.Section{Heading: "Where the plastic came from", Table: origins})
	if len(pp.Timeline) > 0 {
		tl := &report.Table{Name: "Timeline", Columns: []report.Column{{Title: "When"}, {Title: "Event", Weight: 2.5}, {Title: "Record", Weight: 1.2}, {Title: "Company", Weight: 1.8}}}
		for _, e := range pp.Timeline {
			tl.Rows = append(tl.Rows, []any{e.At, e.Title, e.Record, name(e.OrgID)})
		}
		rep.Sections = append(rep.Sections, report.Section{Heading: "Timeline", Table: tl})
	}
	if len(pp.Tests) > 0 {
		lt := &report.Table{Name: "Lab tests", Columns: []report.Column{{Title: "When"}, {Title: "Test", Weight: 1.6}, {Title: "Result", Align: report.Right}, {Title: "Verdict"}, {Title: "Laboratory", Weight: 1.8}}}
		for _, n := range pp.Tests {
			lt.Rows = append(lt.Rows, []any{n.OccurredAt, n.Data["test"], n.Data["value"], n.Data["result"], n.Data["lab"]})
		}
		rep.Sections = append(rep.Sections, report.Section{Heading: "Lab tests", Table: lt})
	}
	if t := pp.Trust; t != nil {
		tr := &report.Table{Name: "Verification", Columns: []report.Column{{Title: "Check", Weight: 1.4}, {Title: "Detail", Weight: 4}}}
		tr.Rows = append(tr.Rows, []any{"Fingerprint", t.ContentHash + " (" + t.Algorithm + ")"})
		if t.Signature != nil {
			tr.Rows = append(tr.Rows, []any{"Company signature", fmt.Sprintf("%s, key %s, valid: %v", name(t.Signature.OrgID), t.Signature.KeyID, t.Signature.Valid)})
		}
		if t.ActorSignature != nil {
			tr.Rows = append(tr.Rows, []any{"Personal signature", fmt.Sprintf("key %s, valid: %v", t.ActorSignature.KeyID, t.ActorSignature.Valid)})
		}
		if t.Seal != nil {
			tr.Rows = append(tr.Rows, []any{"Ledger seal", fmt.Sprintf("%s, Merkle root %s, transaction %s, block %d", t.Seal.ID, t.Seal.MerkleRoot, t.Seal.TxID, t.Seal.BlockNumber)})
		}
		rep.Sections = append(rep.Sections, report.Section{Heading: "How to verify", Text: []string{
			"Recompute the SHA-256 of the record's canonical JSON (RFC 8785), check the Ed25519 signatures against the public keys registered on the ledger, and replay the Merkle path to the seal's root."}, Table: tr})
	}
	return rep, nil
}

func (s *Server) reportQuality(r *http.Request) (*report.Report, error) {
	p := principal(r.Context())
	status := r.URL.Query().Get("status")
	fs, total, err := s.St.Findings(r.Context(), p.Viewer, store.FindingFilter{Status: status, Code: r.URL.Query().Get("code"), Limit: 5000})
	if err != nil {
		return nil, err
	}
	names := s.orgNames(r.Context())
	t := &report.Table{Name: "Findings", Columns: []report.Column{{Title: "Rule"}, {Title: "Severity"}, {Title: "Record", Weight: 1.3}, {Title: "Company", Weight: 1.8}, {Title: "Finding", Weight: 3.5}, {Title: "Status"}, {Title: "Raised"}}}
	open, blockers := 0, 0
	for _, f := range fs {
		t.Rows = append(t.Rows, []any{f.Code, f.Severity, f.RecordID, names[f.OrgID], f.Message, f.Status, f.CreatedAt})
		if f.Status == "open" {
			open++
			if f.Severity == "blocker" {
				blockers++
			}
		}
	}
	return &report.Report{Title: "Data checks", Subtitle: "Findings of the automatic checks C1–C9", Notice: s.simulatedNotice(r.Context()),
		KPIs:     []report.KPI{{Label: "Findings", Value: strconv.Itoa(total)}, {Label: "Open", Value: strconv.Itoa(open)}, {Label: "Blocking (open)", Value: strconv.Itoa(blockers)}},
		Sections: []report.Section{{Heading: "Findings", Table: t}}}, nil
}

func (s *Server) reportCarbon(r *http.Request) (*report.Report, error) {
	ctx, sc, rg := r.Context(), scope(r), window(r)
	rows, err := s.CH.GRETARows(ctx, sc, rg)
	if err != nil {
		return nil, err
	}
	t := &report.Table{Name: "GRETA", Columns: []report.Column{{Title: "Section", Weight: 1.3}, {Title: "Road"}, {Title: "Region", Weight: 1.3}, {Title: "Layer"}, {Title: "Completed"},
		{Title: "Asphalt (t)", Align: report.Right, Format: "#,##0.0"}, {Title: "Recycled plastic (kg)", Align: report.Right, Format: "#,##0"}, {Title: "CO2e avoided (kg)", Align: report.Right, Format: "#,##0"},
		{Title: "Chain footprint (kg)", Align: report.Right, Format: "#,##0"}, {Title: "Transport (t·km)", Align: report.Right, Format: "#,##0"}, {Title: "Provenance"}}}
	var rec, av, ch float64
	for _, x := range rows {
		t.Rows = append(t.Rows, []any{x.ID, x.Road, x.Region, x.Layer, x.At, x.MassKg / 1000, x.RecycledKg, x.AvoidedKg, x.ChainKg, x.Tkm, x.Provenance})
		rec, av, ch = rec+x.RecycledKg, av+x.AvoidedKg, ch+x.ChainKg
	}
	t.Total = []any{"Total", "", "", "", nil, nil, rec, av, ch, nil, ""}
	return &report.Report{Title: "Carbon and impact", Subtitle: "Mass-allocated LCA per finished road section (GRETA-ready)", Period: periodOf(rg), Notice: s.simulatedNotice(ctx),
		KPIs:     []report.KPI{{Label: "Road sections", Value: strconv.Itoa(len(rows))}, {Label: "Recycled plastic", Value: tonnes(rec)}, {Label: "CO2e avoided", Value: tonnes(av)}, {Label: "Chain footprint", Value: tonnes(ch)}},
		Sections: []report.Section{{Heading: "Per road section", Text: []string{"Factors and sources are listed under Carbon & impact in ROADPASS. Allocation is by mass along each passport's lineage."}, Table: t}}}, nil
}

func (s *Server) reportLedger(r *http.Request) (*report.Report, error) {
	p := principal(r.Context())
	org := p.Org.ID
	if p.Viewer.Oversight {
		org = r.URL.Query().Get("org")
	}
	seals, err := s.St.SealsFor(r.Context(), org, 2000)
	if err != nil {
		return nil, err
	}
	t := &report.Table{Name: "Seals", Columns: []report.Column{{Title: "Seal", Weight: 1.8}, {Title: "Merkle root", Weight: 2.4}, {Title: "Records", Align: report.Right}, {Title: "Yours", Align: report.Right},
		{Title: "Transaction", Weight: 2}, {Title: "Block", Align: report.Right}, {Title: "Status"}, {Title: "Sealed"}}}
	for _, x := range seals {
		t.Rows = append(t.Rows, []any{x.ID, x.MerkleRoot, x.LeafCount, x.Mine, x.TxID, int(x.BlockNumber), x.Status, x.SealedAt})
	}
	return &report.Report{Title: "Ledger seals", Subtitle: "Merkle seals committed on the ledger, with the records each holds", Sections: []report.Section{{Heading: "Seals", Table: t}}}, nil
}

// stepNames names a step of the chain by the activity that does it (as the dashboard does).
var stepNames = map[string]string{"sorting_plant": "Sort", "recycler": "Recycle", "additive_producer": "Make additive",
	"asphalt_plant": "Mix asphalt", "contractor": "Lay", "road_owner": "Own the road", "lab": "Testing (lab)"}

func kindName(k string) string {
	if n, ok := stepNames[k]; ok {
		return n
	}
	return k
}

func (s *Server) reportStatus(r *http.Request) (*report.Report, error) {
	p := principal(r.Context())
	if !p.Viewer.Oversight {
		return nil, errReportForbidden
	}
	st, err := s.St.ChainStatus(r.Context())
	if err != nil {
		return nil, err
	}
	names := s.orgNames(r.Context())
	t := &report.Table{Name: "Chain status", Columns: []report.Column{{Title: "Organisation", Weight: 2.2}, {Title: "Activity", Weight: 1.2}, {Title: "Records", Align: report.Right}, {Title: "Sealed", Align: report.Right}, {Title: "In transit", Align: report.Right},
		{Title: "Awaiting release", Align: report.Right}, {Title: "Quarantined", Align: report.Right}, {Title: "Blocked", Align: report.Right}, {Title: "Last activity"}}}
	for _, x := range st {
		t.Rows = append(t.Rows, []any{names[x.OrgID], kindName(x.Activity), x.Records, x.Sealed, x.InTransitN, x.AwaitingReleaseN, x.QuarantinedN, x.Blocked, x.LastActivity})
	}
	return &report.Report{Title: "Chain status", Subtitle: "Every organisation's state over the last 90 days", Notice: s.simulatedNotice(r.Context()), Sections: []report.Section{{Heading: "Organisations", Table: t}}}, nil
}

func (s *Server) reportRoads(r *http.Request) (*report.Report, error) {
	ctx, sc, rg := r.Context(), scope(r), window(r)
	secs, err := s.CH.Sections(ctx, sc, rg, 20000)
	if err != nil {
		return nil, err
	}
	t := &report.Table{Name: "Road sections", Columns: []report.Column{{Title: "Section", Weight: 1.3}, {Title: "Road"}, {Title: "Region", Weight: 1.4}, {Title: "Layer"}, {Title: "Length (km)", Align: report.Right, Format: "0.00"},
		{Title: "Asphalt (t)", Align: report.Right, Format: "#,##0.0"}, {Title: "Recycled plastic (kg)", Align: report.Right, Format: "#,##0"}, {Title: "Latitude", Align: report.Right, Format: "0.0000"}, {Title: "Longitude", Align: report.Right, Format: "0.0000"}}}
	km := 0.0
	for _, x := range secs {
		t.Rows = append(t.Rows, []any{x.ID, x.Road, x.Region, x.Layer, float64(x.LengthKm), x.MassKg / 1000, x.RecycledKg, float64(x.Lat), float64(x.Lon)})
		km += float64(x.LengthKm)
	}
	return &report.Report{Title: "Roads", Subtitle: "Road sections surfaced with recycled-plastic asphalt", Period: periodOf(rg), Notice: s.simulatedNotice(ctx),
		KPIs: []report.KPI{{Label: "Sections", Value: strconv.Itoa(len(secs))}, {Label: "Length", Value: fmt.Sprintf("%.1f km", km)}}, Sections: []report.Section{{Heading: "Sections", Table: t}}}, nil
}

func (s *Server) reportTeam(r *http.Request) (*report.Report, error) {
	p := principal(r.Context())
	if !p.can("admin") {
		return nil, errReportForbidden
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 365 {
		days = 30
	}
	members, err := s.St.Users(r.Context(), p.Org.ID, false)
	if err != nil {
		return nil, err
	}
	act, err := s.St.TeamActivity(r.Context(), p.Org.ID, days)
	if err != nil {
		return nil, err
	}
	t := &report.Table{Name: "Team", Columns: []report.Column{{Title: "Member", Weight: 1.8}, {Title: "Role"}, {Title: "Duties", Weight: 1.6}, {Title: "Records", Align: report.Right},
		{Title: "Intake reviews", Align: report.Right}, {Title: "Rejected", Align: report.Right}, {Title: "Corrections", Align: report.Right}, {Title: "Open findings", Align: report.Right}, {Title: "Last action"}}}
	sum := func(m map[string]int) int {
		n := 0
		for _, v := range m {
			n += v
		}
		return n
	}
	for _, u := range members {
		a := act[u.ID]
		if a == nil {
			a = &store.MemberActivity{Created: map[string]int{}, Receipts: map[string]int{}}
		}
		t.Rows = append(t.Rows, []any{u.Name, domain.RoleTitleIn(append([]domain.OrgKind{p.Org.Kind}, p.Org.Kinds...), u.Steps, u.Role, u.Duties), dutyNames(u.Duties), sum(a.Created), sum(a.Receipts), a.Receipts["rejected"], a.Corrections, a.OpenFindings, a.LastAction})
	}
	return &report.Report{Title: "Team activity", Subtitle: fmt.Sprintf("Work signed by each member over the last %d days", days), Sections: []report.Section{{Heading: "Members", Table: t}}}, nil
}

func (s *Server) reportAlerts(r *http.Request) (*report.Report, error) {
	p := principal(r.Context())
	ev, err := s.Notify.DB.History(r.Context(), p.Org.ID, "", 5000)
	if err != nil {
		return nil, err
	}
	t := &report.Table{Name: "Alert history", Columns: []report.Column{{Title: "When"}, {Title: "Alert", Weight: 3}, {Title: "Event"}, {Title: "Severity"}, {Title: "Delivered to", Weight: 2}, {Title: "Acknowledged"}}}
	for _, e := range ev {
		var to []string
		for _, d := range e.Deliveries {
			to = append(to, fmt.Sprintf("%s%s", d.Channel, map[bool]string{true: "", false: " (failed)"}[d.OK]))
		}
		t.Rows = append(t.Rows, []any{e.At, e.Title, e.State, e.Severity, strings.Join(to, ", "), e.AckAt})
	}
	return &report.Report{Title: "Alert history", Sections: []report.Section{{Heading: "Events", Table: t}}}, nil
}

func (s *Server) reportDashboard(r *http.Request) (*report.Report, error) {
	p := principal(r.Context())
	d, err := s.St.Dashboard(r.Context(), p.Org.ID, r.URL.Query().Get("id"))
	if err != nil {
		return nil, err
	}
	if !p.can("admin") && !d.Sees(p.Role(), p.Duties()) {
		return nil, store.ErrNotFound
	}
	var ws []widget
	if err := json.Unmarshal(d.Widgets, &ws); err != nil {
		return nil, err
	}
	slices.SortFunc(ws, func(a, b widget) int {
		if a.Y != b.Y {
			return a.Y - b.Y
		}
		return a.X - b.X
	})
	rg := window(r)
	rep := &report.Report{Title: d.Name, Subtitle: d.Description, Period: periodOf(rg), Notice: s.simulatedNotice(r.Context())}
	names := s.orgNames(r.Context())
	for _, w := range ws {
		if w.Type == "text" {
			var o struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(w.Options, &o)
			rep.Sections = append(rep.Sections, report.Section{Heading: w.Title, Text: strings.Split(strings.TrimSpace(o.Text), "\n")})
			continue
		}
		if w.Query == nil {
			continue
		}
		res, err := s.CH.Explore(r.Context(), scope(r), widgetRange(w.Range, rg), *w.Query)
		if err != nil {
			rep.Sections = append(rep.Sections, report.Section{Heading: w.Title, Text: []string{"Could not compute: " + err.Error()}})
			continue
		}
		label := func(g string) string {
			if n, ok := names[g]; ok {
				return n
			}
			return g
		}
		if w.Type == "kpi" || w.Type == "gauge" {
			v := 0.0
			for _, row := range res.Rows {
				v += row.Value
			}
			rep.KPIs = append(rep.KPIs, report.KPI{Label: w.Title, Value: fmtUnit(v, res.Unit), Hint: res.Field.Label})
			continue
		}
		tbl := &report.Table{Name: w.Title}
		if res.Interval != "" {
			tbl.Columns = []report.Column{{Title: "Period"}}
			if res.GroupBy != nil {
				tbl.Columns = append(tbl.Columns, report.Column{Title: res.GroupBy.Label, Weight: 2})
			}
		} else if res.GroupBy != nil {
			tbl.Columns = []report.Column{{Title: res.GroupBy.Label, Weight: 2}}
		}
		tbl.Columns = append(tbl.Columns, report.Column{Title: res.Field.Label + unitSuffix(res.Unit), Align: report.Right, Format: "#,##0.00"}, report.Column{Title: "Records", Align: report.Right})
		var chart *report.Chart
		if res.Interval != "" && res.GroupBy == nil {
			chart = &report.Chart{Kind: "line", Unit: res.Unit}
		}
		for _, row := range res.Rows {
			var cells []any
			if row.T != nil {
				cells = append(cells, *row.T)
				if chart != nil {
					chart.Labels, chart.Values = append(chart.Labels, row.T.Format("2 Jan")), append(chart.Values, row.Value)
				}
			}
			if res.GroupBy != nil {
				cells = append(cells, label(row.Group))
			}
			cells = append(cells, row.Value, int(row.Count))
			tbl.Rows = append(tbl.Rows, cells)
		}
		rep.Sections = append(rep.Sections, report.Section{Heading: w.Title, Table: tbl, Chart: chart})
	}
	return rep, nil
}

func widgetRange(r string, page analytics.Range) analytics.Range {
	to := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	switch r {
	case "7d":
		return analytics.Range{From: to.AddDate(0, 0, -7), To: to}
	case "30d":
		return analytics.Range{From: to.AddDate(0, 0, -30), To: to}
	case "90d":
		return analytics.Range{From: to.AddDate(0, 0, -90), To: to}
	case "12m":
		return analytics.Range{From: to.AddDate(-1, 0, 0), To: to}
	case "all":
		return analytics.Range{From: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), To: to}
	}
	return page
}

func unitSuffix(u string) string {
	if s := map[string]string{"kg": " (kg)", "kgco2e": " (kg CO2e)", "pct": " (%)", "c": " (°C)", "bar": " (bar)", "km": " (km)", "m2": " (m²)", "tkm": " (t·km)", "kg_h": " (kg/h)", "rpm": " (rpm)"}[u]; s != "" {
		return s
	}
	return ""
}

func fmtUnit(v float64, u string) string {
	switch u {
	case "kg":
		return tonnes(v)
	case "kgco2e":
		return tonnes(v) + " CO2e"
	case "pct":
		return fmt.Sprintf("%.1f %%", v)
	case "count":
		return strconv.Itoa(int(v))
	}
	return strings.TrimSpace(fmt.Sprintf("%.2f%s", v, strings.Trim(unitSuffix(u), " ()")))
}

func (s *Server) reportInvoices(r *http.Request) (*report.Report, error) {
	p := principal(r.Context())
	org := p.Org.ID
	if p.Operator() && p.can("admin") {
		org = r.URL.Query().Get("org") // the platform manager sees every organisation's
	} else if !p.can("admin") {
		return nil, errReportForbidden
	}
	m, err := monthParam(r.URL.Query().Get("month"))
	if err != nil {
		return nil, err
	}
	inv, err := s.St.Invoices(r.Context(), org, m)
	if err != nil {
		return nil, err
	}
	names := s.orgNames(r.Context())
	t := &report.Table{Name: "Invoices", Columns: []report.Column{{Title: "Number"}, {Title: "Organisation", Weight: 2}, {Title: "Month"}, {Title: "Plan"}, {Title: "Amount", Align: report.Right, Format: "#,##0.00"}, {Title: "Currency"}, {Title: "Status"}, {Title: "Issued"}, {Title: "Paid"}}}
	var total float64
	for _, x := range inv {
		if !p.Operator() && x.Status == "draft" {
			continue
		}
		t.Rows = append(t.Rows, []any{x.Number, names[x.OrgID], x.Period, x.PlanID, float64(x.AmountCents) / 100, x.Currency, x.Status, x.IssuedAt, x.PaidAt})
		if x.Status != "void" {
			total += float64(x.AmountCents) / 100
		}
	}
	t.Total = []any{"", "Total (excluding void)", nil, "", total, "", "", nil, nil}
	return &report.Report{Title: "Invoices", Sections: []report.Section{{Heading: "Invoices", Table: t}}}, nil
}

// reportCompliance is defined with the compliance module.

func dutyNames(ds []string) string {
	out := make([]string, 0, len(ds))
	for _, d := range domain.Duties {
		if slices.Contains(ds, string(d)) {
			out = append(out, domain.DutyInfo[d].Name)
		}
	}
	return strings.Join(out, ", ")
}

func nonEmptyStr(xs ...string) []string {
	out := xs[:0:0]
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// avoidedKPI labels a reject lot's figure as a potential: disposal is avoided
// only for the plastic the recycler actually turns out.
func avoidedKPI(t domain.RecordType, avoided, net float64) report.KPI {
	if t == domain.TypeRejectLot {
		return report.KPI{Label: "CO2e avoidable", Value: tonnes(avoided), Hint: "potential, until it is recycled"}
	}
	return report.KPI{Label: "CO2e avoided", Value: tonnes(avoided), Hint: "net " + tonnes(net)}
}
