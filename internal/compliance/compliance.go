package compliance

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/report"
	"github.com/sensifai/roadpass/internal/store"
)

// ReportRequirement is the attestation id of a month's report sign-off.
const ReportRequirement = "report.monthly"

// MaxLimits caps the limits an organisation can define (each runs a query).
const MaxLimits = 40

// MaxExceedances is how many exceedances a limit lists per period.
const MaxExceedances = 500

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// AttestationMessage is the exact text a member signs for a statement.
func AttestationMessage(a store.Attestation) string {
	return strings.Join([]string{"roadpass/v1/attestation", a.OrgID, a.RequirementID, a.Period, a.Status,
		a.At.UTC().Format(time.RFC3339), sha(a.Note), sha(a.EvidenceURL)}, "\n")
}

// Month parses "2026-09" into the first instant of that month (UTC).
func Month(period string) (time.Time, error) {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return time.Time{}, fmt.Errorf("period must look like 2026-09")
	}
	return t.UTC(), nil
}

// PreviousMonth is the last complete month before now.
func PreviousMonth(now time.Time) string {
	y, m, _ := now.UTC().Date()
	return time.Date(y, m-1, 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
}

// Spec turns a stored limit into the query spec.
func Spec(l store.Limit) analytics.LimitSpec {
	return analytics.LimitSpec{Source: l.Source, Measure: l.Measure, Types: l.Types, SiteID: l.SiteID, Basis: l.Basis, Min: l.Min, Max: l.Max}
}

// LimitStatus is one limit's result over a period.
type LimitStatus struct {
	Limit       store.Limit            `json:"limit"`
	Label       string                 `json:"label"`
	Unit        string                 `json:"unit"`
	Stat        analytics.Stat         `json:"stat"`
	Exceedances []analytics.Exceedance `json:"exceedances"`
	Truncated   bool                   `json:"truncated"`
	Error       string                 `json:"error,omitempty"`
}

// CheckLimits runs every enabled limit of an organisation over r. A limit whose
// query fails is reported with its error instead of failing the whole check.
func CheckLimits(ctx context.Context, ch *analytics.DB, orgID string, limits []store.Limit, r analytics.Range) []LimitStatus {
	out := make([]LimitStatus, 0, len(limits))
	for _, l := range limits {
		if !l.Enabled {
			continue
		}
		s := LimitStatus{Limit: l, Exceedances: []analytics.Exceedance{}}
		spec := Spec(l)
		if f, ok := spec.Field(); ok {
			s.Label, s.Unit = f.Label, f.Unit
		}
		ex, st, err := ch.Exceedances(ctx, analytics.Scope{OrgID: orgID}, r, spec, MaxExceedances+1)
		if err != nil {
			s.Error = err.Error()
		} else {
			if len(ex) > MaxExceedances {
				ex, s.Truncated = ex[:MaxExceedances], true
			}
			s.Exceedances, s.Stat = ex, st
		}
		out = append(out, s)
	}
	return out
}

// Deps are what the report needs.
type Deps struct {
	St *store.Store
	CH *analytics.DB
}

var basisName = map[string]string{"instant": "each value", "daily_mean": "daily mean", "monthly_mean": "monthly mean"}
var statusName = map[string]string{Met: "Met", Partial: "Partly met", Gap: "Gap", Open: "Statement needed", NotApplicable: "Not applicable"}

// StatusName is a register status in words.
func StatusName(s string) string { return statusName[s] }

func bound(v *float64) any {
	if v == nil {
		return "—"
	}
	return *v
}

// Report builds an organisation's monthly compliance report for period (2026-09).
func Report(ctx context.Context, d Deps, org domain.Org, period string) (*report.Report, error) {
	from, err := Month(period)
	if err != nil {
		return nil, err
	}
	to := from.AddDate(0, 1, 0)
	rg := analytics.Range{From: from, To: to}
	limits, err := d.St.Limits(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	statuses := CheckLimits(ctx, d.CH, org.ID, limits, rg)
	cov, err := d.CH.Completeness(ctx, analytics.Scope{OrgID: org.ID}, rg)
	if err != nil {
		return nil, err
	}
	cals, err := d.St.Calibrations(ctx, org.ID, from, to)
	if err != nil {
		return nil, err
	}
	latest, err := d.St.LatestAttestations(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	operational, err := d.St.HasOperational(ctx, org.ID, from, to)
	if err != nil {
		return nil, err
	}
	sites, _ := d.St.Sites(ctx, org.ID)
	siteName := map[string]string{}
	for _, s := range sites {
		siteName[s.ID] = s.Name
	}

	rep := &report.Report{
		Title:    "Compliance report",
		Subtitle: "Limits, exceedances, plant data completeness and calibrations",
		Org:      org.Name,
		Period:   from.Format("January 2006"),
	}
	if !operational {
		any, err := d.St.HasRecords(ctx, org.ID, from, to)
		if err != nil {
			return nil, err
		}
		if any {
			rep.Notice = "Simulated demonstration data: no operational records in this period."
		} else {
			rep.Notice = "No records in this period."
		}
	}
	nEx := 0
	for _, s := range statuses {
		nEx += len(s.Exceedances)
	}
	var covSum float64
	for _, c := range cov {
		covSum += c.Percent
	}
	covText := "—"
	if len(cov) > 0 {
		covText = fmt.Sprintf("%.1f %%", covSum/float64(len(cov)))
	}
	rep.KPIs = []report.KPI{
		{Label: "Limits checked", Value: fmt.Sprint(len(statuses))},
		{Label: "Exceedances", Value: fmt.Sprint(nEx)},
		{Label: "Plant data completeness", Value: covText, Hint: "operating time without gaps"},
	}

	lt := &report.Table{Name: "Limits", Columns: []report.Column{{Title: "Limit", Weight: 2}, {Title: "Measure", Weight: 1.6}, {Title: "Basis"},
		{Title: "Min", Align: report.Right, Format: "#,##0.00"}, {Title: "Max", Align: report.Right, Format: "#,##0.00"},
		{Title: "Lowest", Align: report.Right, Format: "#,##0.00"}, {Title: "Average", Align: report.Right, Format: "#,##0.00"}, {Title: "Highest", Align: report.Right, Format: "#,##0.00"},
		{Title: "Values", Align: report.Right, Format: "#,##0"}, {Title: "Exceedances", Align: report.Right, Format: "#,##0"}}}
	et := &report.Table{Name: "Exceedances", Columns: []report.Column{{Title: "When"}, {Title: "Limit", Weight: 2}, {Title: "Reference", Weight: 1.4}, {Title: "Site", Weight: 1.4},
		{Title: "Value", Align: report.Right, Format: "#,##0.00"}, {Title: "Direction"}, {Title: "Excess", Align: report.Right, Format: "#,##0.00"}}}
	for _, s := range statuses {
		row := []any{s.Limit.Name, s.Label, basisName[s.Limit.Basis], bound(s.Limit.Min), bound(s.Limit.Max), s.Stat.Min, s.Stat.Avg, s.Stat.Max, float64(s.Stat.Count), float64(len(s.Exceedances))}
		if s.Error != "" {
			row = []any{s.Limit.Name, s.Label, basisName[s.Limit.Basis], bound(s.Limit.Min), bound(s.Limit.Max), "error", "", "", "", ""}
		}
		lt.Rows = append(lt.Rows, row)
		for _, e := range s.Exceedances {
			et.Rows = append(et.Rows, []any{e.At, s.Limit.Name, e.Ref, siteName[e.Site], e.Value, e.Kind, e.Excess})
		}
	}
	limitsText := []string{"Each limit is checked on every value, or on the daily or monthly mean, as set for it."}
	if len(statuses) == 0 {
		limitsText = []string{"No limits are set. Add them under Compliance → Limits."}
	}
	rep.Sections = append(rep.Sections, report.Section{Heading: "Limits", Text: limitsText, Table: lt})
	exText := []string{}
	if nEx == 0 {
		exText = []string{"No value broke a limit in this period."}
	}
	rep.Sections = append(rep.Sections, report.Section{Heading: "Exceedances", Text: exText, Table: et})

	ct := &report.Table{Name: "Completeness", Columns: []report.Column{{Title: "Site", Weight: 2}, {Title: "Data"}, {Title: "Samples", Align: report.Right, Format: "#,##0"},
		{Title: "Gaps", Align: report.Right, Format: "#,##0"}, {Title: "Gap minutes", Align: report.Right, Format: "#,##0"}, {Title: "Stops", Align: report.Right, Format: "#,##0"},
		{Title: "Complete", Align: report.Right, Format: "0.0"}}}
	for _, c := range cov {
		ct.Rows = append(ct.Rows, []any{orDefault(siteName[c.Site], c.Site), map[string]string{"extruder_readings": "Extruder", "mixer_cycles": "Mixer"}[c.Source],
			float64(c.Samples), float64(c.Gaps), c.GapMinutes, float64(c.Stops), c.Percent})
	}
	rep.Sections = append(rep.Sections, report.Section{Heading: "Plant data completeness", Text: []string{
		"A gap is a silence longer than three usual intervals (at least five minutes). Silences over six hours are plant stops, not missing data, and are left out of the share."}, Table: ct})

	kt := &report.Table{Name: "Calibrations", Columns: []report.Column{{Title: "When"}, {Title: "Parameter", Weight: 1.6}, {Title: "Site", Weight: 1.4}, {Title: "Kind"}, {Title: "By"}, {Title: "Note", Weight: 2}}}
	for _, c := range cals {
		kt.Rows = append(kt.Rows, []any{c.At, c.Parameter, siteName[c.SiteID], c.Kind, c.By, c.Note})
	}
	rep.Sections = append(rep.Sections, report.Section{Heading: "Calibrations and checks", Table: kt})

	// The sign-off counts only if it was signed over exactly these figures; the
	// caller learns from SignState when the data changed after signing.
	fp := rep.Fingerprint()
	for _, a := range latest {
		if a.RequirementID == ReportRequirement && a.Period == period && a.Message == string(report.SigningMessage(fp)) {
			rep.SignOff = &report.SignOff{Statement: orDefault(a.Note, "I confirm this report is complete and correct."), Name: a.ByName, Role: "Signed with a personal key",
				At: a.At, KeyID: a.KeyID, Signature: base64.StdEncoding.EncodeToString(a.Signature)}
		}
	}
