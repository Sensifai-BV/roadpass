package analytics

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The explorer answers dashboard widgets: one measure, aggregated over an
// optional time bucket and an optional dimension, with filters. Every column
// name comes from the catalogue below, never from the request; every value is a
// bound parameter; every query carries the viewer's scope.

// FieldKind says how a field may be used.
type FieldKind string

// Field kinds.
const (
	Measure   FieldKind = "measure"
	Dimension FieldKind = "dimension"
)

// DataType is a field's value type, so widgets can format and validate it.
type DataType string

// Data types.
const (
	TypeNumber DataType = "number"
	TypeString DataType = "string"
	TypeBool   DataType = "bool"
)

// Field is one queryable column.
type Field struct {
	Key   string    `json:"key"`
	Label string    `json:"label"`
	Kind  FieldKind `json:"kind"`
	Type  DataType  `json:"type"`
	// Unit tells the dashboard how to format a measure: kg, kgco2e, pct, c, bar,
	// km, m2, tkm, kg_h, rpm, count, or "" for a plain number.
	Unit string `json:"unit,omitempty"`
	Help string `json:"help,omitempty"`
	col  string // SQL expression; trusted, from this file only
}

// Source is one table a widget can read.
type Source struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Help   string  `json:"help"`
	Fields []Field `json:"fields"`
	// Types lists the record types the source can be narrowed to (records only).
	Types   []string `json:"types,omitempty"`
	table   string
	timeCol string
	scoped  func(Scope) (string, []any)
}

func m(key, label, unit, help string) Field {
	return Field{Key: key, Label: label, Kind: Measure, Type: TypeNumber, Unit: unit, Help: help, col: key}
}
func dim(key, label string) Field {
	return Field{Key: key, Label: label, Kind: Dimension, Type: TypeString, col: key}
}

// Catalogue lists every source and field a widget may use.
var Catalogue = []Source{
	{
		Key: "records", Label: "Passport records", table: final, timeCol: "occurred_at",
		Help:   "Every record in the chain (latest version): lots, shipments, deliveries, tests, road sections.",
		Types:  []string{"reject_lot", "recycled_lot", "additive_batch", "asphalt_lot", "shipment", "delivery", "lab_test", "road_section"},
		scoped: func(s Scope) (string, []any) { return s.where() },
		Fields: []Field{
			{Key: "records", Label: "Number of records", Kind: Measure, Type: TypeNumber, Unit: "count", col: "1"},
			m("mass_kg", "Mass", "kg", "Mass of the lot, shipment or delivery."),
			m("recycled_kg", "Recycled plastic", "kg", "Recycled plastic contained, traced through the lineage."),
			m("reject_kg", "Plastic rejects diverted", "kg", "Reject film from sorting plants behind this record."),
			m("avoided_kg", "CO₂e avoided", "kgco2e", "Disposal and virgin polymer avoided, net."),
			m("virgin_kg", "Virgin polymer avoided", "kg", ""),
			m("footprint_kg", "Footprint", "kgco2e", "Emissions of this step."),
			m("chain_kg", "Chain footprint", "kgco2e", "Emissions of the recycling chain behind it."),
			m("tkm", "Transport", "tkm", "Tonne-kilometres moved."),
			m("dosage", "Additive dosage", "pct", "Additive share of the asphalt mix."),
			m("temp_c", "Temperature", "c", "Production or laying temperature."),
			m("test_value", "Test result", "", "Measured value of a lab test (unit depends on the test)."),
			{Key: "test_pass_rate", Label: "Test pass rate", Kind: Measure, Type: TypeNumber, Unit: "pct", col: "test_pass * 100", Help: "Share of lab tests that passed (use Average)."},
			m("distance_km", "Distance", "km", ""),
			m("length_km", "Road length", "km", ""),
			m("area_m2", "Road area", "m2", ""),
			dim("type", "Record type"),
			dim("org_id", "Organisation"),
			dim("org_kind", "Organisation kind"),
			dim("site_id", "Site"),
			dim("region", "Region"),
			dim("counterparty", "Counterparty"),
			dim("mix", "Asphalt mix"),
			dim("process", "Process"),
			dim("layer", "Road layer"),
			dim("road", "Road"),
			dim("test", "Lab test"),
			dim("route", "Avoided disposal route"),
			dim("provenance", "Provenance"),
			{Key: "test_pass", Label: "Test passed", Kind: Dimension, Type: TypeBool, col: "test_pass"},
		},
	},
	{
		Key: "mixer_cycles", Label: "Asphalt mixer cycles", table: "mixer_cycles", timeCol: "ts",
		Help:   "One row per batch cycle of an asphalt plant mixer (plant control log).",
		scoped: func(s Scope) (string, []any) { return s.orgOnly() },
		Fields: []Field{
			{Key: "cycles", Label: "Number of cycles", Kind: Measure, Type: TypeNumber, Unit: "count", col: "1"},
			m("batch_kg", "Batch mass", "kg", ""),
			m("additive_kg", "Additive dosed", "kg", ""),
			m("binder_kg", "Binder", "kg", ""),
			m("temp_c", "Mix temperature", "c", ""),
			{Key: "missed", Label: "Missed dosing", Kind: Measure, Type: TypeNumber, Unit: "count", col: "toUInt8(additive_kg = 0)", Help: "Cycles with no additive dosed (use Sum)."},
			dim("org_id", "Organisation"),
			dim("site_id", "Site"),
			dim("mix", "Asphalt mix"),
		},
	},
	{
		Key: "extruder_readings", Label: "Extruder readings", table: "extruder_readings", timeCol: "ts",
		Help:   "One-minute sensor readings from recycler extrusion lines (plant control log).",
		scoped: func(s Scope) (string, []any) { return s.orgOnly() },
		Fields: []Field{
			{Key: "readings", Label: "Number of readings", Kind: Measure, Type: TypeNumber, Unit: "count", col: "1"},
			m("melt_c", "Melt temperature", "c", ""),
			m("pressure_bar", "Melt pressure", "bar", ""),
			m("throughput_kg_h", "Throughput", "kg_h", ""),
			m("rpm", "Screw speed", "rpm", ""),
			m("load_pct", "Motor load", "pct", ""),
			dim("org_id", "Organisation"),
			dim("site_id", "Site"),
			dim("line", "Line"),
		},
	},
}

// Aggregations a widget may ask for.
var aggregations = map[string]func(col string) string{
	"sum":    func(c string) string { return "sum(" + c + ")" },
	"avg":    func(c string) string { return "ifNotFinite(avg(" + c + "), 0)" }, // avg of nothing is NaN, which JSON cannot carry
	"min":    func(c string) string { return "min(" + c + ")" },
	"max":    func(c string) string { return "max(" + c + ")" },
	"median": func(c string) string { return "quantile(0.5)(" + c + ")" },
	"p90":    func(c string) string { return "quantile(0.9)(" + c + ")" },
	"count":  func(string) string { return "count()" },
}

// Intervals a time series may be bucketed by.
var intervals = map[string]string{
	"hour": "toStartOfHour", "day": "toStartOfDay", "week": "toMonday", "month": "toStartOfMonth",
}

// Filter narrows a query: Field Op Value, where Op is eq, neq, in, gt, gte, lt, lte.
type Filter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// Query is a widget's data request.
type Query struct {
	Source   string   `json:"source"`
	Types    []string `json:"types,omitempty"`
	Measure  string   `json:"measure"`
	Agg      string   `json:"agg"`
	GroupBy  string   `json:"groupBy,omitempty"`
	Interval string   `json:"interval,omitempty"` // "", auto, hour, day, week, month
	Filters  []Filter `json:"filters,omitempty"`
	Limit    int      `json:"limit,omitempty"`   // groups kept (top N by value)
	Compare  bool     `json:"compare,omitempty"` // also compute the previous window (single value only)

	scope Scope // set by Explore, never by the request
}

// Row is one result cell.
type Row struct {
	T     *time.Time `json:"t,omitempty"`
	Group string     `json:"group,omitempty"`
	Value float64    `json:"v"`
	Count uint64     `json:"n"`
}

// Result is a widget's data.
type Result struct {
	Unit     string    `json:"unit"`
	Interval string    `json:"interval,omitempty"`
	Rows     []Row     `json:"rows"`
	Previous *float64  `json:"previous,omitempty"`
	Range    Range     `json:"range"`
	Groups   []string  `json:"groups,omitempty"`
	Field    Field     `json:"field"`
	GroupBy  *Field    `json:"groupBy,omitempty"`
	Took     float64   `json:"tookMs"`
	At       time.Time `json:"at"`
}

// ErrQuery is a request the explorer cannot run; the message is safe to show.
var ErrQuery = errors.New("query")

func qerr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrQuery, fmt.Sprintf(format, a...))
}

const (
	maxGroups  = 50
	maxBuckets = 1500
	maxFilters = 10
	maxIn      = 100
)

func (s *Source) field(key string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// SourceByKey finds a catalogue source.
func SourceByKey(key string) (*Source, bool) {
	for i := range Catalogue {
		if Catalogue[i].Key == key {
			return &Catalogue[i], true
		}
	}
	return nil, false
}

// plan is a validated query, ready to build.
type plan struct {
	src      *Source
	measure  Field
	agg      string
	group    *Field
	interval string
	where    []string
	args     []any
	limit    int
}

// plan checks q against the catalogue and resolves the bucket for r.
func (q Query) plan(r Range) (plan, error) {
	src, ok := SourceByKey(q.Source)
	if !ok {
		return plan{}, qerr("unknown data source %q", q.Source)
	}
	p := plan{src: src}
	if p.measure, ok = src.field(q.Measure); !ok || p.measure.Kind != Measure {
		return plan{}, qerr("%q is not a measure of %s", q.Measure, src.Label)
	}
	agg := q.Agg
	if agg == "" {
		agg = "sum"
	}
	if p.measure.Unit == "count" && p.measure.col == "1" {
		agg = "count" // "number of records" is always a count
	}
	if _, ok := aggregations[agg]; !ok {
		return plan{}, qerr("unknown aggregation %q", q.Agg)
	}
	p.agg = agg
	if q.GroupBy != "" {
		g, ok := src.field(q.GroupBy)
		if !ok || g.Kind != Dimension {
			return plan{}, qerr("cannot group %s by %q", src.Label, q.GroupBy)
		}
		p.group = &g
	}
	switch q.Interval {
	case "":
	case "auto":
		p.interval = string(GrainFor(r))
		if r.To.Sub(r.From) <= 4*24*time.Hour {
			p.interval = "hour"
		}
	default:
		if _, ok := intervals[q.Interval]; !ok {
			return plan{}, qerr("unknown interval %q", q.Interval)
		}
		p.interval = q.Interval
	}
	if p.interval != "" {
		if n := buckets(r, p.interval); n > maxBuckets {
			return plan{}, qerr("%d %s buckets is too many for one chart; pick a longer interval", n, p.interval)
		}
	}
	p.limit = q.Limit
	if p.limit <= 0 || p.limit > maxGroups {
		p.limit = 10
		if p.interval == "" {
			p.limit = maxGroups
		}
	}

	w, wa := src.scoped(q.scope)
	p.where = []string{w, src.timeCol + " >= ?", src.timeCol + " < ?"}
	p.args = append(slices.Clone(wa), r.From.UTC(), r.To.UTC())
	if len(q.Types) > 0 {
		if len(src.Types) == 0 {
			return plan{}, qerr("%s has no record types", src.Label)
		}
		for _, t := range q.Types {
			if !slices.Contains(src.Types, t) {
				return plan{}, qerr("unknown record type %q", t)
			}
		}
		p.where = append(p.where, "type IN ?")
		p.args = append(p.args, q.Types)
	}
	if len(q.Filters) > maxFilters {
		return plan{}, qerr("at most %d filters", maxFilters)
	}
	for _, f := range q.Filters {
		fl, ok := src.field(f.Field)
		if !ok {
			return plan{}, qerr("cannot filter %s by %q", src.Label, f.Field)
		}
		sql, arg, err := filterSQL(fl, f)
		if err != nil {
			return plan{}, err
		}
		p.where = append(p.where, sql)
		p.args = append(p.args, arg)
	}
	return p, nil
}

func filterSQL(f Field, x Filter) (string, any, error) {
	ops := map[string]string{"eq": "=", "neq": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
	if x.Op == "in" {
		vals, ok := x.Value.([]any)
		if !ok || len(vals) == 0 || len(vals) > maxIn {
			return "", nil, qerr("filter %s: 'in' needs a list of 1–%d values", f.Label, maxIn)
		}
		if f.Type == TypeNumber {
			out := make([]float64, 0, len(vals))
			for _, v := range vals {
				n, ok := toFloat(v)
				if !ok {
					return "", nil, qerr("filter %s: %v is not a number", f.Label, v)
				}
				out = append(out, n)
			}
			return f.col + " IN ?", out, nil
		}
		out := make([]string, 0, len(vals))
		for _, v := range vals {
			out = append(out, fmt.Sprint(v))
		}
		return "toString(" + f.col + ") IN ?", out, nil
	}
	op, ok := ops[x.Op]
	if !ok {
		return "", nil, qerr("unknown filter operator %q", x.Op)
	}
	switch f.Type {
	case TypeNumber:
		n, ok := toFloat(x.Value)
		if !ok {
			return "", nil, qerr("filter %s: %v is not a number", f.Label, x.Value)
		}
		return f.col + " " + op + " ?", n, nil
	case TypeBool:
		if op != "=" && op != "!=" {
			return "", nil, qerr("filter %s: yes/no fields take eq or neq", f.Label)
		}
		b := x.Value == true || x.Value == "true" || x.Value == 1.0
		v := 0
		if b {
			v = 1
		}
		return f.col + " " + op + " ?", v, nil
	}
	if op != "=" && op != "!=" {
		return "", nil, qerr("filter %s: text fields take eq, neq or in", f.Label)
	}
	return "toString(" + f.col + ") " + op + " ?", fmt.Sprint(x.Value), nil
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		var f float64
		_, err := fmt.Sscan(x, &f)
		return f, err == nil
	}
	return 0, false
}

func buckets(r Range, interval string) int {
	h := r.To.Sub(r.From).Hours()
	switch interval {
	case "hour":
		return int(h)
	case "day":
		return int(h / 24)
	case "week":
		return int(h / 24 / 7)
	}
	return int(h / 24 / 30)
}

// sql builds the statement for p.
func (p plan) sql() (string, []any) {
	val := aggregations[p.agg](p.measure.col)
	cols := []string{}
	groupBy := []string{}
	if p.interval != "" {
		cols = append(cols, "toDateTime("+intervals[p.interval]+"("+p.src.timeCol+")) AS t")
		groupBy = append(groupBy, "t")
	}
	if p.group != nil {
		cols = append(cols, "toString("+p.group.col+") AS g")
		groupBy = append(groupBy, "g")
	}
	cols = append(cols, "toFloat64("+val+") AS v", "count() AS n")
	where := strings.Join(p.where, " AND ")
	args := slices.Clone(p.args)
	q := "SELECT " + strings.Join(cols, ", ") + " FROM " + p.src.table + " WHERE " + where
	switch {
	case p.interval != "" && p.group != nil:
		// A series per group, for the top groups only.
		q += " AND toString(" + p.group.col + ") IN (SELECT toString(" + p.group.col + ") FROM " + p.src.table +
			" WHERE " + where + " GROUP BY 1 ORDER BY " + val + " DESC LIMIT " + fmt.Sprint(p.limit) + ")"
		args = append(args, p.args...)
		q += " GROUP BY " + strings.Join(groupBy, ", ") + " ORDER BY t, g"
	case p.interval != "":
		q += " GROUP BY t ORDER BY t"
	case p.group != nil:
		q += " GROUP BY g ORDER BY v DESC LIMIT " + fmt.Sprint(p.limit)
	}
	return q, args
}

// Explore runs a widget query for the viewer.
func (d *DB) Explore(ctx context.Context, s Scope, r Range, q Query) (Result, error) {
	start := time.Now()
	q.scope = s
	p, err := q.plan(r)
	if err != nil {
		return Result{}, err
	}
	stmt, args := p.sql()
	var rows []struct {
		T time.Time `ch:"t"`
		G string    `ch:"g"`
		V float64   `ch:"v"`
		N uint64    `ch:"n"`
	}
	if err := d.C.Select(ctx, &rows, stmt, args...); err != nil {
		return Result{}, err
	}
	out := Result{Unit: p.measure.Unit, Interval: p.interval, Range: r, Field: p.measure, GroupBy: p.group, Rows: make([]Row, 0, len(rows)), At: time.Now().UTC()}
	if p.agg == "count" {
		out.Unit = "count"
	}
	seen := map[string]bool{}
	for _, x := range rows {
		row := Row{Group: x.G, Value: finite(x.V), Count: x.N}
		if p.interval != "" {
			t := x.T.UTC()
			row.T = &t
		}
		if p.group != nil && !seen[x.G] {
			seen[x.G] = true
			out.Groups = append(out.Groups, x.G)
		}
		out.Rows = append(out.Rows, row)
	}
	if q.Compare && p.interval == "" && p.group == nil {
		span := r.To.Sub(r.From)
		prev, err := d.Explore(ctx, s, Range{r.From.Add(-span), r.From}, Query{Source: q.Source, Types: q.Types, Measure: q.Measure, Agg: q.Agg, Filters: q.Filters})
		if err != nil {
			return Result{}, err
		}
		if len(prev.Rows) > 0 && prev.Rows[0].Count > 0 {
			v := prev.Rows[0].Value
			out.Previous = &v
		}
	}
	out.Took = float64(time.Since(start).Microseconds()) / 1000
	return out, nil
}

// finite maps NaN/Inf (avg over nothing) to 0 so JSON stays valid.
func finite(v float64) float64 {
	if v != v || v > 1e300 || v < -1e300 {
		return 0
	}
	return v
}

// Validate checks a stored widget query against the catalogue, without the
// window-dependent bucket limit (checked again when the query runs).
func (q Query) Validate() error {
	day := Range{From: time.Unix(0, 0).UTC(), To: time.Unix(86400, 0).UTC()}
	_, err := q.plan(day)
	return err
}
