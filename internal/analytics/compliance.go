package analytics

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Limit reporting (compliance): which values broke a limit in a period, the
// period's statistics, and how complete the plant data were. Columns come from
// the catalogue only; values are bound.

// LimitSpec is the part of a limit the queries need.
type LimitSpec struct {
	Source  string
	Measure string
	Types   []string
	SiteID  string
	Basis   string // instant | daily_mean | monthly_mean
	Min     *float64
	Max     *float64
}

// Exceedance is one value outside a limit.
type Exceedance struct {
	At     time.Time `json:"at"`
	Ref    string    `json:"ref"` // record id, line or lot; or the day/month for means
	Site   string    `json:"site,omitempty"`
	Value  float64   `json:"value"`
	Kind   string    `json:"kind"` // above | below
	Excess float64   `json:"excess"`
}

// Stat is a limit's measure over the period.
type Stat struct {
	Min   float64 `json:"min"`
	Avg   float64 `json:"avg"`
	Max   float64 `json:"max"`
	Count uint64  `json:"count"`
}

func (l LimitSpec) resolve(s Scope, r Range) (src *Source, col string, where string, args []any, err error) {
	src, ok := SourceByKey(l.Source)
	if !ok {
		return nil, "", "", nil, fmt.Errorf("%w: unknown source %q", ErrQuery, l.Source)
	}
	f, ok := src.field(l.Measure)
	if !ok || f.Kind != Measure || f.col == "1" {
		return nil, "", "", nil, fmt.Errorf("%w: %q is not a measurable value of %s", ErrQuery, l.Measure, src.Label)
	}
	w, wa := src.scoped(s)
	conds := []string{w, src.timeCol + " >= ?", src.timeCol + " < ?"}
	args = append(slices.Clone(wa), r.From.UTC(), r.To.UTC())
	if len(l.Types) > 0 && len(src.Types) > 0 {
		conds = append(conds, "type IN ?")
		args = append(args, l.Types)
	}
	if l.SiteID != "" {
		conds = append(conds, "site_id = ?")
		args = append(args, l.SiteID)
	}
	if l.Source == "records" {
		conds = append(conds, "toFloat64("+f.col+") != 0") // unset values are 0 in the projection
	}
	return src, f.col, strings.Join(conds, " AND "), args, nil
}

// ValidateLimit checks a limit against the catalogue.
func (l LimitSpec) Validate() error {
	_, _, _, _, err := l.resolve(Scope{}, Range{})
	if err == nil && l.Basis != "instant" && l.Basis != "daily_mean" && l.Basis != "monthly_mean" {
		err = fmt.Errorf("%w: basis must be instant, daily_mean or monthly_mean", ErrQuery)
	}
	return err
}

// Exceedances lists values outside the limit in r (at most max), and the
// period's statistics.
func (d *DB) Exceedances(ctx context.Context, s Scope, r Range, l LimitSpec, max int) ([]Exceedance, Stat, error) {
	src, col, where, args, err := l.resolve(s, r)
	if err != nil {
		return nil, Stat{}, err
	}
	var st []struct {
		Min, Avg, Max float64
		N             uint64
	}
	if err := d.C.Select(ctx, &st, `SELECT toFloat64(min(`+col+`)) AS Min, ifNotFinite(toFloat64(avg(`+col+`)), 0) AS Avg, toFloat64(max(`+col+`)) AS Max, count() AS N FROM `+src.table+` WHERE `+where, args...); err != nil {
		return nil, Stat{}, err
	}
	stat := Stat{}
	if len(st) > 0 && st[0].N > 0 {
		stat = Stat{Min: finite(st[0].Min), Avg: finite(st[0].Avg), Max: finite(st[0].Max), Count: st[0].N}
	}
	outside := []string{}
	var bounds []any
	val := col
	if l.Basis != "instant" {
		val = "v"
	}
	if l.Max != nil {
		outside = append(outside, val+" > ?")
		bounds = append(bounds, *l.Max)
	}
	if l.Min != nil {
		outside = append(outside, val+" < ?")
		bounds = append(bounds, *l.Min)
	}
	cond := "(" + strings.Join(outside, " OR ") + ")"
	var q string
	switch l.Basis {
	case "instant":
		ref := map[string]string{"records": "id", "extruder_readings": "line", "mixer_cycles": "lot_id"}[l.Source]
		q = `SELECT toDateTime(` + src.timeCol + `) AS at, toString(` + ref + `) AS ref, toString(site_id) AS site, toFloat64(` + col + `) AS value
			FROM ` + src.table + ` WHERE ` + where + ` AND ` + cond + ` ORDER BY at LIMIT ` + fmt.Sprint(max)
	default:
		fn := map[string]string{"daily_mean": "toStartOfDay", "monthly_mean": "toStartOfMonth"}[l.Basis]
		q = `SELECT at, toString(at) AS ref, '' AS site, v AS value FROM (
				SELECT toDateTime(` + fn + `(` + src.timeCol + `)) AS at, toFloat64(avg(` + col + `)) AS v FROM ` + src.table + ` WHERE ` + where + ` GROUP BY at)
			WHERE ` + cond + ` ORDER BY at LIMIT ` + fmt.Sprint(max)
	}
	var rows []struct {
		At    time.Time `ch:"at"`
		Ref   string    `ch:"ref"`
		Site  string    `ch:"site"`
		Value float64   `ch:"value"`
	}
	if err := d.C.Select(ctx, &rows, q, append(slices.Clone(args), bounds...)...); err != nil {
		return nil, stat, err
	}
	out := make([]Exceedance, 0, len(rows))
	for _, x := range rows {
		e := Exceedance{At: x.At.UTC(), Ref: x.Ref, Site: x.Site, Value: x.Value}
		if l.Basis != "instant" {
			e.Ref = x.At.UTC().Format(map[string]string{"daily_mean": "2 Jan 2006", "monthly_mean": "January 2006"}[l.Basis])
		}
		switch {
		case l.Max != nil && x.Value > *l.Max:
			e.Kind, e.Excess = "above", x.Value-*l.Max
		case l.Min != nil && x.Value < *l.Min:
			e.Kind, e.Excess = "below", *l.Min-x.Value
		}
		out = append(out, e)
	}
	return out, stat, nil
}

// Coverage is how complete a site's plant data were in a period.
type Coverage struct {
	Site       string  `json:"site"`
	Source     string  `json:"source"` // extruder_readings | mixer_cycles
	Samples    uint64  `json:"samples"`
	Gaps       uint64  `json:"gaps"`
	GapMinutes float64 `json:"gapMinutes"`
	Stops      uint64  `json:"stops"`   // silences long enough to be a shutdown, not missing data
	Percent    float64 `json:"percent"` // share of operating time without gaps
}

// ShutdownAfter separates missing data from a plant that is not running: a
// silence longer than this is a stop (night, weekend, maintenance).
const ShutdownAfter = 6 * time.Hour

// Completeness finds silences longer than three usual intervals (at least five
// minutes) in each site's plant data, as AquaSense does for sensors. Unlike a
// sensor a plant stops between campaigns, so silences beyond ShutdownAfter are
// counted as stops and the share is taken over operating time only.
func (d *DB) Completeness(ctx context.Context, s Scope, r Range) ([]Coverage, error) {
	w, wa := s.orgOnly()
	var out []Coverage
	for _, src := range []string{"extruder_readings", "mixer_cycles"} {
		var rows []struct {
			Site    string  `ch:"site_id"`
			Samples uint64  `ch:"samples"`
			Gaps    uint64  `ch:"gaps"`
			GapSec  float64 `ch:"gap_s"`
			SpanSec float64 `ch:"span_s"`
			Stops   uint64  `ch:"stops"`
		}
		// The usual interval is the median gap between consecutive samples.
		q := `WITH deltas AS (
				SELECT site_id, toFloat64(toUnixTimestamp64Milli(toDateTime64(ts, 3)) - toUnixTimestamp64Milli(toDateTime64(lagInFrame(ts) OVER (PARTITION BY site_id ORDER BY ts), 3))) / 1000 AS dt,
					row_number() OVER (PARTITION BY site_id ORDER BY ts) AS rn
				FROM ` + src + ` WHERE ` + w + ` AND ts >= ? AND ts < ?),
			usual AS (SELECT site_id, greatest(300, 3 * quantile(0.5)(dt)) AS limit_s FROM deltas WHERE rn > 1 GROUP BY site_id)
			SELECT d.site_id AS site_id, count() AS samples,
				countIf(d.rn > 1 AND d.dt > u.limit_s AND d.dt <= ?) AS gaps, sumIf(d.dt, d.rn > 1 AND d.dt > u.limit_s AND d.dt <= ?) AS gap_s,
				sumIf(d.dt, d.rn > 1 AND d.dt <= ?) AS span_s, countIf(d.rn > 1 AND d.dt > ?) AS stops
			FROM deltas d JOIN usual u ON u.site_id = d.site_id GROUP BY d.site_id ORDER BY d.site_id`
		off := ShutdownAfter.Seconds()
		if err := d.C.Select(ctx, &rows, q, append(slices.Clone(wa), r.From.UTC(), r.To.UTC(), off, off, off, off)...); err != nil {
			return nil, err
		}
		for _, x := range rows {
			pct := 100.0
			if x.SpanSec > 0 {
				pct = 100 * (1 - x.GapSec/x.SpanSec)
			}
			out = append(out, Coverage{Site: x.Site, Source: src, Samples: x.Samples, Gaps: x.Gaps, GapMinutes: x.GapSec / 60, Stops: x.Stops, Percent: pct})
		}
	}
	return out, nil
}

// Field describes the limit's measure (label and unit) for reports.
func (l LimitSpec) Field() (Field, bool) {
	src, ok := SourceByKey(l.Source)
	if !ok {
		return Field{}, false
	}
	return src.field(l.Measure)
}
