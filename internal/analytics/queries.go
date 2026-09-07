package analytics

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
)

// final is the deduplicating read of the fact table (latest version per record).
//
// Records with an open blocker are left out: they are not sealed and not yet part
// of the chain, so no KPI, funnel or map counts them until they are corrected or
// accepted (the projector re-projects a record when its status changes).
const final = "(SELECT * FROM records FINAL WHERE status != 'blocked')"

// TypeTotal is one record type's count and mass in a window.
type TypeTotal struct {
	Type   string  `ch:"type" json:"type"`
	Count  uint64  `ch:"n" json:"count"`
	MassKg float64 `ch:"mass" json:"massKg"`
}

// Impact is what reached the road in a window (from deliveries, so each kilogram
// is counted once). Void deliveries and loads rejected at the paver are projected
// with zero mass and are not counted.
type Impact struct {
	AsphaltKg   float64 `ch:"asphalt" json:"asphaltKg"`
	RecycledKg  float64 `ch:"recycled" json:"recycledKg"`
	RejectKg    float64 `ch:"reject" json:"rejectKg"`
	AvoidedKg   float64 `ch:"avoided" json:"avoidedKg"`
	VirginKg    float64 `ch:"virgin" json:"avoidedVirginKg"`
	ChainKg     float64 `ch:"chain" json:"chainFootprintKg"`
	FootprintKg float64 `ch:"footprint" json:"footprintKg"`
	Deliveries  uint64  `ch:"deliveries" json:"deliveries"`
}

// RoadTotals summarises finished road sections.
type RoadTotals struct {
	Sections uint64  `ch:"sections" json:"sections"`
	LengthKm float64 `ch:"km" json:"lengthKm"`
	AreaM2   float64 `ch:"area" json:"areaM2"`
}

// Overview is the headline set for one window, with the previous window for deltas.
type Overview struct {
	Range    Range       `json:"range"`
	Types    []TypeTotal `json:"types"`
	Impact   Impact      `json:"impact"`
	Previous Impact      `json:"previous"`
	Roads    RoadTotals  `json:"roads"`
	LabPass  float64     `json:"labPassRate"`
	LabTests uint64      `json:"labTests"`
}

// Overview computes the headline figures.
func (d *DB) Overview(ctx context.Context, s Scope, r Range) (Overview, error) {
	w, wa := s.where()
	out := Overview{Range: r, Types: []TypeTotal{}} // [] not null for a company with no records yet
	args := append(wa, r.args()...)
	if err := d.C.Select(ctx, &out.Types, `SELECT type, count() AS n, sum(mass_kg) AS mass FROM `+final+`
		WHERE `+w+` AND occurred_at >= ? AND occurred_at < ? GROUP BY type ORDER BY type`, args...); err != nil {
		return out, err
	}
	imp := func(rr Range) (Impact, error) {
		var x []Impact
		err := d.C.Select(ctx, &x, `SELECT sum(mass_kg) AS asphalt, sum(recycled_kg) AS recycled, sum(reject_kg) AS reject,
			sum(avoided_kg) AS avoided, sum(virgin_kg) AS virgin, sum(chain_kg) AS chain, sum(footprint_kg) AS footprint, countIf(mass_kg > 0) AS deliveries
			FROM `+final+` WHERE type = 'delivery' AND `+w+` AND occurred_at >= ? AND occurred_at < ?`, append(slices.Clone(wa), rr.args()...)...)
		if err != nil || len(x) == 0 {
			return Impact{}, err
		}
		return x[0], nil
	}
	var err error
	if out.Impact, err = imp(r); err != nil {
		return out, err
	}
	span := r.To.Sub(r.From)
	if out.Previous, err = imp(Range{r.From.Add(-span), r.From}); err != nil {
		return out, err
	}
	var roads []RoadTotals
	if err := d.C.Select(ctx, &roads, `SELECT count() AS sections, sum(length_km) AS km, sum(area_m2) AS area FROM `+final+`
		WHERE type = 'road_section' AND `+w+` AND occurred_at >= ? AND occurred_at < ?`, args...); err != nil {
		return out, err
	}
	if len(roads) > 0 {
		out.Roads = roads[0]
	}
	var lab []struct {
		N    uint64  `ch:"n"`
		Pass float64 `ch:"pass"`
	}
	if err := d.C.Select(ctx, &lab, `SELECT count() AS n, ifNotFinite(avg(test_pass), 0) AS pass FROM `+final+`
		WHERE type = 'lab_test' AND `+w+` AND occurred_at >= ? AND occurred_at < ?`, args...); err != nil {
		return out, err
	}
	if len(lab) > 0 {
		out.LabTests, out.LabPass = lab[0].N, lab[0].Pass
	}
	return out, nil
}

// Point is one value of a time series.
type Point struct {
	T      time.Time `ch:"t" json:"t"`
	Series string    `ch:"series" json:"series"`
	Value  float64   `ch:"v" json:"v"`
}

// Grain is a time bucket.
type Grain string

// Grains.
const (
	Day   Grain = "day"
	Week  Grain = "week"
	Month Grain = "month"
)

func (g Grain) fn() string {
	switch g {
	case Day:
		return "toStartOfDay"
	case Week:
		return "toMonday"
	}
	return "toStartOfMonth"
}

// GrainFor picks a bucket that keeps a chart under ~120 points.
func GrainFor(r Range) Grain {
	days := r.To.Sub(r.From).Hours() / 24
	switch {
	case days <= 120:
		return Day
	case days <= 730:
		return Week
	}
	return Month
}

// MaterialFlow is tonnes per stage per bucket: what each stage produced.
func (d *DB) MaterialFlow(ctx context.Context, s Scope, r Range, g Grain) ([]Point, error) {
	w, wa := s.where()
	var out []Point
	err := d.C.Select(ctx, &out, `SELECT toDateTime(`+g.fn()+`(occurred_at)) AS t, type AS series, sum(mass_kg)/1000 AS v
		FROM `+final+` WHERE type IN ('reject_lot','recycled_lot','additive_batch','asphalt_lot') AND `+w+`
		AND occurred_at >= ? AND occurred_at < ? GROUP BY t, series ORDER BY t, series`, append(wa, r.args()...)...)
	return out, err
}

// ImpactSeries is recycled plastic laid, avoided and footprint per bucket (tonnes).
func (d *DB) ImpactSeries(ctx context.Context, s Scope, r Range, g Grain) ([]Point, error) {
	w, wa := s.where()
	var out []Point
	err := d.C.Select(ctx, &out, `SELECT t, series, v FROM (
			SELECT toDateTime(`+g.fn()+`(occurred_at)) AS t, sum(recycled_kg)/1000 AS recycled, sum(avoided_kg)/1000 AS avoided,
				sum(chain_kg)/1000 AS chain, sum(mass_kg)/1000 AS asphalt
			FROM `+final+` WHERE type = 'delivery' AND `+w+` AND occurred_at >= ? AND occurred_at < ? GROUP BY t)
		ARRAY JOIN ['recycled','avoided','chain','asphalt'] AS series, [recycled, avoided, chain, asphalt] AS v
		ORDER BY t, series`, append(wa, r.args()...)...)
	return out, err
}

// FlowLink is one edge of the chain Sankey, in tonnes of plastic.
type FlowLink struct {
	Stage  uint8   `ch:"stage" json:"stage"`
	From   string  `ch:"src" json:"from"`
	To     string  `ch:"dst" json:"to"`
	Tonnes float64 `ch:"t" json:"tonnes"`
}

// Flows returns organisation-to-organisation flows of plastic for the Sankey:
// reject film shipped to recyclers (1), recycled plastic to additive producers (2),
// recycled plastic in additive to asphalt companies (3), in asphalt to contractors
// (4), and in finished sections to road owners (5).
func (d *DB) Flows(ctx context.Context, s Scope, r Range) ([]FlowLink, error) {
	w, wa := s.where()
	var out []FlowLink
	// A shipment's stage is the material it carries, read from the id of its first
	// input (PRL = reject lot, RPL = recycled lot, ADB = additive batch), not from
	// the sender's main kind: a company that sorts, recycles and makes additive
	// ships all three. The sender's kind is only the fallback for ids without a
	// known prefix.
	pre := func(t domain.RecordType) string { return domain.Types[t].Prefix }
	stage := fmt.Sprintf(`multiIf(startsWith(parent_id,'%s'),1, startsWith(parent_id,'%s'),2, startsWith(parent_id,'%s'),3,
				org_kind='sorting_plant',1, org_kind='recycler',2, org_kind='additive_producer',3, 0)`,
		pre(domain.TypeRejectLot), pre(domain.TypeRecycledLot), pre(domain.TypeAdditiveBatch))
	// A step done without a shipment (a company's own sites hand material on with no
	// haulier) still moves plastic: derive that link from the lots' own inputs, but only
	// for producing lots that were never shipped, so nothing is counted twice.
	hop := func(n int, consumer, producer domain.RecordType, tonnes string) string {
		return fmt.Sprintf(`SELECT %d AS stage, p.org_id AS src, c.org_id AS dst, sum(%s)/1000 AS t
		FROM `+final+` AS c INNER JOIN (SELECT id, org_id FROM `+final+` WHERE type='%s') AS p ON p.id = c.parent_id
		WHERE c.type='%s' AND c.parent_id NOT IN (SELECT parent_id FROM `+final+` WHERE type='shipment')
			AND `+strings.ReplaceAll(w, "org_id", "c.org_id")+` AND c.occurred_at >= ? AND c.occurred_at < ? GROUP BY stage, src, dst`,
			n, tonnes, producer, consumer)
	}
	inHouse := hop(1, domain.TypeRecycledLot, domain.TypeRejectLot, "c.reject_kg") + `
		UNION ALL ` + hop(2, domain.TypeAdditiveBatch, domain.TypeRecycledLot, "c.recycled_kg") + `
		UNION ALL ` + hop(3, domain.TypeAsphaltLot, domain.TypeAdditiveBatch, "c.recycled_kg")
	q := `SELECT stage, src, dst, t FROM (
		SELECT ` + stage + ` AS stage,
			org_id AS src, counterparty AS dst, sum(if(stage = 1, mass_kg, recycled_kg))/1000 AS t
		FROM ` + final + ` WHERE type='shipment' AND ` + w + ` AND occurred_at >= ? AND occurred_at < ? GROUP BY stage, src, dst
		UNION ALL
		` + inHouse + `
		UNION ALL
		SELECT 4 AS stage, org_id AS src, counterparty AS dst, sum(recycled_kg)/1000 AS t
		FROM ` + final + ` WHERE type='delivery' AND counterparty != '' AND ` + w + ` AND occurred_at >= ? AND occurred_at < ? GROUP BY src, dst
		UNION ALL
		SELECT 5 AS stage, org_id AS src, owner_org AS dst, sum(recycled_kg)/1000 AS t
		FROM ` + final + ` WHERE type='road_section' AND owner_org != '' AND ` + w + ` AND occurred_at >= ? AND occurred_at < ? GROUP BY src, dst
	) WHERE stage > 0 AND t > 0 ORDER BY stage, t DESC
	SETTINGS prefer_column_name_to_alias = 1`
	var args []any
	for i := 0; i < 6; i++ { // shipments, three in-house hops, deliveries, sections
		args = append(append(args, wa...), r.args()...)
	}
	err := d.C.Select(ctx, &out, q, args...)
	return out, err
}

// FunnelStep is the plastic that reached one step of the chain.
type FunnelStep struct {
	Type   string  `ch:"type" json:"type"`
	Tonnes float64 `ch:"t" json:"tonnes"`
	Count  uint64  `ch:"n" json:"count"`
}

// Funnel returns the plastic at each step: the reject film sorted, the recycled
// plastic made from it, the additive made from that, the plastic inside asphalt
// that was mixed, and the plastic in road sections actually laid. Each is a
// different stock, so the drop between steps is plastic still waiting or sold
// outside the road chain.
func (d *DB) Funnel(ctx context.Context, s Scope, r Range) ([]FunnelStep, error) {
	w, wa := s.where()
	var out []FunnelStep
	err := d.C.Select(ctx, &out, `SELECT type, sum(if(type='reject_lot', mass_kg, recycled_kg))/1000 AS t, count() AS n
		FROM `+final+` WHERE type IN ('reject_lot','recycled_lot','additive_batch','asphalt_lot','road_section') AND `+w+`
			AND occurred_at >= ? AND occurred_at < ? GROUP BY type
		SETTINGS prefer_column_name_to_alias = 1`, append(wa, r.args()...)...)
	return out, err
}

// SiteVolume is the mass a site handled in a window, for the map.
type SiteVolume struct {
	SiteID string  `ch:"site_id" json:"siteId"`
	OrgID  string  `ch:"org_id" json:"orgId"`
	Type   string  `ch:"type" json:"type"`
	MassKg float64 `ch:"mass" json:"massKg"`
	Count  uint64  `ch:"n" json:"count"`
}

// SiteVolumes returns per-site production by record type.
func (d *DB) SiteVolumes(ctx context.Context, s Scope, r Range) ([]SiteVolume, error) {
	w, wa := s.where()
	var out []SiteVolume
	err := d.C.Select(ctx, &out, `SELECT site_id, org_id, type, sum(mass_kg) AS mass, count() AS n FROM `+final+`
		WHERE site_id != '' AND type IN ('reject_lot','recycled_lot','additive_batch','asphalt_lot') AND `+w+`
		AND occurred_at >= ? AND occurred_at < ? GROUP BY site_id, org_id, type`, append(wa, r.args()...)...)
	return out, err
}

// SectionPoint is a road section on the map.
type SectionPoint struct {
	ID         string    `ch:"id" json:"id"`
	Road       string    `ch:"road" json:"road"`
	Region     string    `ch:"region" json:"region"`
	Lat        float32   `ch:"lat" json:"lat"`
	Lon        float32   `ch:"lon" json:"lon"`
	LengthKm   float32   `ch:"length_km" json:"lengthKm"`
	MassKg     float64   `ch:"mass_kg" json:"massKg"`
	RecycledKg float64   `ch:"recycled_kg" json:"recycledKg"`
	AvoidedKg  float64   `ch:"avoided_kg" json:"avoidedKg"`
	Layer      string    `ch:"layer" json:"layer"`
	At         time.Time `ch:"occurred_at" json:"at"`
}

// Sections returns road sections for the map, newest first.
func (d *DB) Sections(ctx context.Context, s Scope, r Range, limit int) ([]SectionPoint, error) {
	w, wa := s.where()
	var out []SectionPoint
	err := d.C.Select(ctx, &out, `SELECT id, road, region, lat, lon, length_km, mass_kg, recycled_kg, avoided_kg, layer, occurred_at
		FROM `+final+` WHERE type='road_section' AND lat != 0 AND `+w+` AND occurred_at >= ? AND occurred_at < ?
		ORDER BY occurred_at DESC LIMIT ?`, append(append(wa, r.args()...), limit)...)
	return out, err
}

// Breakdown is a labelled value.
type Breakdown struct {
	Key   string  `ch:"k" json:"key"`
	Value float64 `ch:"v" json:"value"`
	Count uint64  `ch:"n" json:"count"`
}

// ByRegion returns recycled plastic laid per region (tonnes), from road sections.
func (d *DB) ByRegion(ctx context.Context, s Scope, r Range) ([]Breakdown, error) {
	w, wa := s.where()
	var out []Breakdown
	err := d.C.Select(ctx, &out, `SELECT region AS k, sum(recycled_kg)/1000 AS v, count() AS n FROM `+final+`
		WHERE type='road_section' AND `+w+` AND occurred_at >= ? AND occurred_at < ? GROUP BY k ORDER BY v DESC`,
		append(wa, r.args()...)...)
	return out, err
}

// ByMix returns asphalt tonnes per mix designation.
func (d *DB) ByMix(ctx context.Context, s Scope, r Range) ([]Breakdown, error) {
	w, wa := s.where()
	var out []Breakdown
	err := d.C.Select(ctx, &out, `SELECT mix AS k, sum(mass_kg)/1000 AS v, count() AS n FROM `+final+`
		WHERE type='asphalt_lot' AND `+w+` AND occurred_at >= ? AND occurred_at < ? GROUP BY k ORDER BY v DESC`,
		append(wa, r.args()...)...)
	return out, err
}

// LabSeries is the pass rate per test per bucket.
func (d *DB) LabSeries(ctx context.Context, s Scope, r Range, g Grain) ([]Point, error) {
	w, wa := s.where()
	var out []Point
	err := d.C.Select(ctx, &out, `SELECT toDateTime(`+g.fn()+`(occurred_at)) AS t, test AS series, avg(test_pass)*100 AS v
		FROM `+final+` WHERE type='lab_test' AND `+w+` AND occurred_at >= ? AND occurred_at < ?
		GROUP BY t, series ORDER BY t, series`, append(wa, r.args()...)...)
	return out, err
}

// TestStats summarises one lab test.
type TestStats struct {
	Test string  `ch:"test" json:"test"`
	N    uint64  `ch:"n" json:"count"`
	Pass float64 `ch:"pass" json:"passRate"`
	P10  float64 `ch:"p10" json:"p10"`
	P50  float64 `ch:"p50" json:"p50"`
	P90  float64 `ch:"p90" json:"p90"`
}

// LabStats returns per-test distributions.
func (d *DB) LabStats(ctx context.Context, s Scope, r Range) ([]TestStats, error) {
	w, wa := s.where()
	var out []TestStats
	err := d.C.Select(ctx, &out, `SELECT test, count() AS n, avg(test_pass) AS pass,
			quantile(0.1)(test_value) AS p10, quantile(0.5)(test_value) AS p50, quantile(0.9)(test_value) AS p90
		FROM `+final+` WHERE type='lab_test' AND `+w+` AND occurred_at >= ? AND occurred_at < ? GROUP BY test ORDER BY n DESC`,
		append(wa, r.args()...)...)
	return out, err
}

// PlantDosing compares declared and measured dosage per asphalt plant.
type PlantDosing struct {
	SiteID      string  `ch:"site_id" json:"siteId"`
	OrgID       string  `ch:"org_id" json:"orgId"`
	Lots        uint64  `ch:"lots" json:"lots"`
	Cycles      uint64  `ch:"cycles" json:"cycles"`
	Missed      uint64  `ch:"missed" json:"missedDoses"`
	MeasuredPct float64 `ch:"measured" json:"measuredPct"`
	AsphaltT    float64 `ch:"asphalt" json:"asphaltTonnes"`
	AdditiveT   float64 `ch:"additive" json:"additiveTonnes"`
}

// Dosing returns plant-level dosing accuracy from the mixer control logs.
func (d *DB) Dosing(ctx context.Context, s Scope, r Range) ([]PlantDosing, error) {
	w, wa := s.orgOnly()
	var out []PlantDosing
	err := d.C.Select(ctx, &out, `SELECT site_id, org_id, uniqExact(lot_id) AS lots, sum(cycles) AS cycles,
			sum(missed) AS missed, sum(additive_kg)/sum(batch_kg)*100 AS measured, sum(batch_kg)/1000 AS asphalt,
			sum(additive_kg)/1000 AS additive
		FROM lot_dosing WHERE `+w+` AND day >= toDate(?) AND day < toDate(?) GROUP BY site_id, org_id ORDER BY asphalt DESC`,
		append(wa, r.args()...)...)
	return out, err
}

// LotCycle is one mixer cycle (a lot's dosing trace).
type LotCycle struct {
	TS         time.Time `ch:"ts" json:"ts"`
	Cycle      uint32    `ch:"cycle" json:"cycle"`
	BatchKg    float32   `ch:"batch_kg" json:"batchKg"`
	AdditiveKg float32   `ch:"additive_kg" json:"additiveKg"`
	BinderKg   float32   `ch:"binder_kg" json:"binderKg"`
	TempC      float32   `ch:"temp_c" json:"tempC"`
}

// LotCycles returns every mixer cycle of an asphalt lot.
func (d *DB) LotCycles(ctx context.Context, s Scope, lotID string) ([]LotCycle, error) {
	w, wa := s.orgOnly()
	var out []LotCycle
	err := d.C.Select(ctx, &out, `SELECT ts, cycle, batch_kg, additive_kg, binder_kg, temp_c FROM mixer_cycles
		WHERE lot_id = ? AND `+w+` ORDER BY ts`, append([]any{lotID}, wa...)...)
	return out, err
}

// LineHour is an hourly extruder summary.
type LineHour struct {
	Hour       time.Time `ch:"hour" json:"t"`
	SiteID     string    `ch:"site_id" json:"siteId"`
	Line       string    `ch:"line" json:"line"`
	Samples    uint64    `ch:"samples" json:"samples"`
	Stopped    uint64    `ch:"stopped" json:"stopped"`
	Suspect    uint64    `ch:"suspect" json:"suspect"`
	MeltC      float64   `ch:"melt" json:"meltC"`
	Pressure   float64   `ch:"press" json:"pressureBar"`
	Throughput float64   `ch:"thr" json:"throughputKgH"`
	Load       float64   `ch:"load" json:"loadPct"`
}

// Extruder returns hourly line data for a site.
func (d *DB) Extruder(ctx context.Context, s Scope, siteID string, r Range) ([]LineHour, error) {
	w, wa := s.orgOnly()
	var out []LineHour
	err := d.C.Select(ctx, &out, `SELECT hour, site_id, line, sum(samples) AS samples, sum(stopped) AS stopped, sum(suspect) AS suspect,
			sum(melt_sum)/greatest(1, sum(samples)-sum(suspect)) AS melt, sum(press_sum)/greatest(1, sum(samples)-sum(suspect)) AS press,
			sum(thr_sum)/greatest(1, sum(samples)) AS thr, sum(load_sum)/greatest(1, sum(samples)) AS load
		FROM extruder_hourly WHERE site_id = ? AND `+w+` AND hour >= ? AND hour < ?
		GROUP BY hour, site_id, line ORDER BY hour, line
		SETTINGS prefer_column_name_to_alias = 1`, // the aliases reuse column names inside other aggregates
		append(append([]any{siteID}, wa...), r.args()...)...)
	return out, err
}

// LineHealth summarises every extruder line in a window.
type LineHealth struct {
	SiteID  string  `ch:"site_id" json:"siteId"`
	OrgID   string  `ch:"org_id" json:"orgId"`
	Line    string  `ch:"line" json:"line"`
	Hours   uint64  `ch:"hours" json:"hours"`
	Samples uint64  `ch:"samples" json:"samples"`
	Stopped uint64  `ch:"stopped" json:"stopped"`
	Suspect uint64  `ch:"suspect" json:"suspect"`
	OutputT float64 `ch:"output" json:"outputTonnes"`
}

// Lines returns per-line health: run hours, stops and suspect readings. Output
// integrates each hour's mean throughput over that hour, so it does not depend on
// how often the plant samples; a plant that sends every minute and one that sends
// hourly report the same tonnes.
func (d *DB) Lines(ctx context.Context, s Scope, r Range) ([]LineHealth, error) {
	w, wa := s.orgOnly()
	var out []LineHealth
	err := d.C.Select(ctx, &out, `SELECT site_id, org_id, line, countIf(n > st) AS hours, sum(n) AS samples,
			sum(st) AS stopped, sum(sp) AS suspect, sum(avg_thr) / 1000 AS output
		FROM (SELECT site_id, org_id, line, hour, sum(samples) AS n, sum(stopped) AS st, sum(suspect) AS sp,
				sum(thr_sum) / greatest(1, sum(samples)) AS avg_thr
			FROM extruder_hourly WHERE `+w+` AND hour >= ? AND hour < ? GROUP BY site_id, org_id, line, hour)
		GROUP BY site_id, org_id, line ORDER BY site_id, line
		SETTINGS prefer_column_name_to_alias = 1`,
		append(wa, r.args()...)...)
	if err != nil || len(out) == 0 {
		return out, err
	}
	// A stopped line reads zero melt and pressure; that is a stop, not a suspect reading.
	// The hourly roll-up cannot tell them apart, so count the suspects from the readings
	// themselves, the same rule as the "Readings to check" list.
	var sus []struct {
		SiteID string `ch:"site_id"`
		Line   string `ch:"line"`
		N      uint64 `ch:"n"`
	}
	if err := d.C.Select(ctx, &sus, `SELECT site_id, line, count() AS n FROM extruder_readings
		WHERE `+w+` AND ts >= ? AND ts < ? AND throughput_kg_h > 0 AND (melt_c > 400 OR melt_c < 50 OR pressure_bar < 10)
		GROUP BY site_id, line`, append(wa, r.args()...)...); err != nil {
		return nil, err
	}
	n := map[[2]string]uint64{}
	for _, x := range sus {
		n[[2]string{x.SiteID, x.Line}] = x.N
	}
	for i := range out {
		out[i].Suspect = n[[2]string{out[i].SiteID, out[i].Line}]
	}
	return out, nil
}

// Suspect is one raw reading that fails a physical plausibility test.
type Suspect struct {
	TS       time.Time `ch:"ts" json:"ts"`
	SiteID   string    `ch:"site_id" json:"siteId"`
	Line     string    `ch:"line" json:"line"`
	LotID    string    `ch:"lot_id" json:"lotId"`
	MeltC    float32   `ch:"melt_c" json:"meltC"`
	Pressure float32   `ch:"pressure_bar" json:"pressureBar"`
	Reason   string    `ch:"reason" json:"reason"`
}

// Suspects returns the newest implausible extruder readings, ranked so the most
// likely scaling errors (×10, ÷100) come first.
func (d *DB) Suspects(ctx context.Context, s Scope, r Range, limit int) ([]Suspect, error) {
	w, wa := s.orgOnly()
	var out []Suspect
	err := d.C.Select(ctx, &out, `SELECT ts, site_id, line, lot_id, melt_c, pressure_bar,
			multiIf(melt_c > 1000, 'Melt temperature ×10 (unit or scaling error)', melt_c > 400, 'Melt temperature above physical range',
				pressure_bar < 10, 'Pressure ÷100 (scaling error)', 'Out of range') AS reason
		FROM extruder_readings WHERE `+w+` AND ts >= ? AND ts < ? AND (melt_c > 400 OR melt_c < 50 OR pressure_bar < 10)
		AND throughput_kg_h > 0 ORDER BY ts DESC LIMIT ?`, append(append(wa, r.args()...), limit)...)
	return out, err
}

// Count returns the number of rows in a table (ops, status page).
func (d *DB) Count(ctx context.Context, table string) (uint64, error) {
	if !slices.Contains([]string{"records", "mixer_cycles", "extruder_readings"}, table) {
		return 0, fmt.Errorf("unknown table %q", table)
	}
	var n uint64
	err := d.C.QueryRow(ctx, "SELECT count() FROM "+table).Scan(&n)
	return n, err
}

// GRETARow is one finished road section for the LCA export.
type GRETARow struct {
	ID         string    `ch:"id"`
	Road       string    `ch:"road"`
	Region     string    `ch:"region"`
	Layer      string    `ch:"layer"`
	At         time.Time `ch:"occurred_at"`
	MassKg     float64   `ch:"mass_kg"`
	RecycledKg float64   `ch:"recycled_kg"`
	RejectKg   float64   `ch:"reject_kg"`
	AvoidedKg  float64   `ch:"avoided_kg"`
	VirginKg   float64   `ch:"virgin_kg"`
	ChainKg    float64   `ch:"chain_kg"`
	Tkm        float64   `ch:"tkm"`
	Provenance string    `ch:"provenance"`
}

// GRETARows returns finished road sections (version ≥ 2 carries completed_at).
func (d *DB) GRETARows(ctx context.Context, s Scope, r Range) ([]GRETARow, error) {
	w, wa := s.where()
	var out []GRETARow
	err := d.C.Select(ctx, &out, `SELECT id, road, region, layer, occurred_at, mass_kg, recycled_kg, reject_kg, avoided_kg, virgin_kg,
			chain_kg, tkm, provenance
		FROM `+final+` WHERE type='road_section' AND version >= 2 AND `+w+` AND occurred_at >= ? AND occurred_at < ?
		ORDER BY occurred_at`, append(wa, r.args()...)...)
	return out, err
}
