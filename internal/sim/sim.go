// Package sim generates the ROADPASS demonstration dataset: a day-by-day,
// discrete-event simulation of recycled reject film travelling from Spanish
// packaging sorting plants to asphalt roads, January 2024 – September 2026.
//
// Companies are fictional; geography, volumes, process parameters, test ranges and
// emission factors come from public sources (docs/research/SPAIN_DATA_SOURCES.md).
// Mass is conserved: every kilogram used was produced and shipped before it was
// used, except where a defect is injected on purpose. Every record is hashed,
// signed and judged by the same checks engine that judges partner data. All
// records carry provenance "Simulated".
//
// The output is deterministic for a given Config.Seed.
package sim

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/checks"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/signing"
)

// Config controls one run.
type Config struct {
	Seed      uint64
	Start     time.Time
	End       time.Time
	Now       time.Time // findings older than this minus 30 days get closed
	Scale     float64   // 1.0 ≈ 2.7M rows; smaller values shrink demand and telemetry
	Telemetry bool      // generate extruder readings and mixer cycles
	Defects   Defects
}

// Defects are the rates at which errors are injected on purpose, so the data
// checks have something real to find. Rates are per eligible record.
type Defects struct {
	MissingField  float64 // C1
	DuplicateTkt  float64 // C2 (deliveries)
	DateTypo      float64 // C3
	UnshippedLot  float64 // C4 (additive shipment never recorded)
	Overstated    float64 // C5 (recycled output)
	WeightGap     float64 // C5 (shipment receipt)
	Unsigned      float64 // C7
	DosageTypo    float64 // C8
	EarlyDispatch float64 // C9 (shipped before quality release)
	ExtruderGlich float64 // telemetry glitches (×10 / ÷100 scaling)
	SensorDrop    float64 // per reading: the control system goes silent for 20–90 min mid-run
}

// DefaultDefects are the documented injection rates.
var DefaultDefects = Defects{MissingField: 0.025, DuplicateTkt: 0.0015, DateTypo: 0.0015, UnshippedLot: 0.004,
	Overstated: 0.004, WeightGap: 0.008, Unsigned: 0.0005, DosageTypo: 0.003, EarlyDispatch: 0.01, ExtruderGlich: 0.002,
	SensorDrop: 0.0015}

// DefaultConfig is the full demonstration dataset.
func DefaultConfig() Config {
	return Config{Seed: 20260915, Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		End: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), Now: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
		Scale: 1, Telemetry: true, Defects: DefaultDefects}
}

// Finding is a data-check result with its lifecycle.
type Finding struct {
	checks.Finding
	OrgID     string
	CreatedAt time.Time
	Status    string
	ClosedAt  *time.Time
	ClosedBy  string
	Reason    string
}

// MixerCycle and Reading are the plant control-log rows.
type (
	MixerCycle = domain.MixerCycle
	Reading    = domain.Reading
)

// Sink receives the output. Telemetry streams during the run; records and
// findings arrive once at the end, in time order, with their final status.
type Sink interface {
	Cycles(ctx context.Context, cs []MixerCycle) error
	Readings(ctx context.Context, rs []Reading) error
	Records(ctx context.Context, rs []domain.Record) error
	Findings(ctx context.Context, fs []Finding) error
}

// World is the cast: organisations, sites and their signing keys.
type World struct {
	Orgs  []domain.Org
	Sites []domain.Site
	Keys  map[string]*signing.Key // by org id
}

// Stats summarises a run.
type Stats struct {
	Records   map[domain.RecordType]int
	Findings  map[string]int
	Cycles    int
	Readings  int
	AsphaltT  float64
	RecycledT float64
}

// ---------------------------------------------------------------- engine types

type org struct {
	domain.Org
	spec  actorSpec
	key   *signing.Key
	sites []*site
}

type stock struct {
	id string
	kg float64
	at time.Time // available from
}

type site struct {
	domain.Site
	org  *org
	town Town
	n    int // plant number within the company

	// sorting plant
	rateKgDay float64
	acc       float64
	targets   []*site

	// recycler / additive producer / asphalt plant inventories
	inbound   []stock // material received (reject lots, recycled lots, additive batches)
	outStock  []stock // own lots waiting to be shipped
	capKgDay  float64
	yield     float64
	accOut    float64
	accSlices []domain.Input
	lines     int
	product   string
	rcPct     float64
	customers []*site
	supplier  *site

	// asphalt plant
	batchKg  float64
	dosage   float64
	calendar map[int]float64 // day index -> tonnes reserved
	ticket   int

	lastLot string             // newest lot of the site, tagged on its telemetry
	rc      map[string]float64 // recycled content of each additive batch made here
}

type project struct {
	sectionID         string
	plant             *site
	contractor, owner *org
	road              string
	kmFrom, kmTo      float64
	layer, mix        string
	thickCm, areaM2   float64
	lat, lon          float64
	region            string
	plannedT          float64
	days              []planDay
	laidKg            float64
	distKm            float64
	warm              bool
}

type planDay struct {
	day int
	t   float64
}

type decision struct {
	decision string
	at       time.Time
}

// Engine runs one simulation.
type Engine struct {
	cfg  Config
	rng  *rand.Rand
	sink Sink

	orgs                                  []*org
	byOrg                                 map[string]*org
	sites                                 []*site
	sorting, recyclers, additive, asphalt []*site
	labs                                  []*org
	auditor                               *org

	projects    []*project
	byDay       map[int][]*project
	completions map[int][]*project

	recs     map[string]*domain.Record
	order    []*domain.Record
	findings []Finding
	consumed map[string]float64
	laid     map[string]float64 // road section -> deliveries onto it that count
	received map[string]int64
	dups     map[string]string
	keys     map[string][]byte
	seq      map[string]int
	refs     map[string]bool
	releases map[string]decision // lot id -> latest quality decision

	cycles   []MixerCycle
	readings []Reading
	stats    Stats
}

// New builds the world for cfg.
func New(cfg Config, sink Sink) *Engine {
	if cfg.Scale <= 0 {
		cfg.Scale = 1
	}
	e := &Engine{cfg: cfg, sink: sink, rng: rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15)),
		byOrg: map[string]*org{}, byDay: map[int][]*project{}, recs: map[string]*domain.Record{},
		consumed: map[string]float64{}, laid: map[string]float64{}, received: map[string]int64{}, dups: map[string]string{},
		keys: map[string][]byte{}, seq: map[string]int{}, refs: map[string]bool{}, releases: map[string]decision{},
		stats: Stats{Records: map[domain.RecordType]int{}, Findings: map[string]int{}}}
	e.cast()
	return e
}

// World returns the organisations, sites and keys.
func (e *Engine) World() World {
	w := World{Keys: map[string]*signing.Key{}}
	for _, o := range e.orgs {
		w.Orgs = append(w.Orgs, o.Org)
		w.Keys[o.ID] = o.key
		for _, s := range o.sites {
			w.Sites = append(w.Sites, s.Site)
		}
	}
	return w
}

func (e *Engine) day(i int) time.Time { return e.cfg.Start.AddDate(0, 0, i) }

func (e *Engine) days() int { return int(e.cfg.End.Sub(e.cfg.Start).Hours() / 24) }

// ---------------------------------------------------------------- random helpers

func (e *Engine) u(lo, hi float64) float64 { return lo + e.rng.Float64()*(hi-lo) }

func (e *Engine) norm(mean, sd, lo, hi float64) float64 {
	return math.Min(hi, math.Max(lo, mean+sd*e.rng.NormFloat64()))
}

func (e *Engine) lognorm(median, sigma, lo, hi float64) float64 {
	return math.Min(hi, math.Max(lo, median*math.Exp(sigma*e.rng.NormFloat64())))
}

func (e *Engine) chance(p float64) bool { return e.rng.Float64() < p }

func (e *Engine) tri(lo, mode, hi float64) float64 {
	u := e.rng.Float64()
	c := (mode - lo) / (hi - lo)
	if u < c {
		return lo + math.Sqrt(u*(hi-lo)*(mode-lo))
	}
	return hi - math.Sqrt((1-u)*(hi-lo)*(hi-mode))
}

func pick[T any](e *Engine, xs []T) T { return xs[e.rng.IntN(len(xs))] }

func round(x float64, d int) float64 {
	p := math.Pow(10, float64(d))
	return math.Round(x*p) / p
}

// at returns a time on day i at hour h (fractional).
func (e *Engine) at(i int, h float64) time.Time {
	return e.day(i).Add(time.Duration(h * float64(time.Hour))).Truncate(time.Second)
}

func workday(t time.Time) bool { wd := t.Weekday(); return wd != time.Saturday && wd != time.Sunday }

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := p2-p1, (lon2-lon1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// roadKm turns a straight-line distance into a road distance (Spanish network
// circuity ≈ 1.25 for inter-city trips).
func roadKm(a, b *site) float64 {
	return round(haversine(a.Lat, a.Lon, b.Lat, b.Lon)*1.25+3, 1)
}

var plateLetters = []rune("BCDFGHJKLMNPRSTVWXYZ")

func (e *Engine) plate() string {
	return fmt.Sprintf("%04d %c%c%c", e.rng.IntN(10000), pick(e, plateLetters), pick(e, plateLetters), pick(e, plateLetters))
}

var carriers = []string{"Transportes Ruta Verde S.L.", "Logística Bética de Graneles S.L.", "Cargas del Cierzo S.L.",
	"Transportes Meseta Norte S.L.", "Trans-Levante Volquetes S.L.", "Garraf Transport Logístic S.L.", "Galitrans Carga S.L."}

// nextID returns a human id such as ASL-25-004213.
func (e *Engine) nextID(t domain.RecordType, at time.Time) string {
	p := domain.Types[t].Prefix
	key := fmt.Sprintf("%s-%02d", p, at.Year()%100)
	e.seq[key]++
	return fmt.Sprintf("%s-%06d", key, e.seq[key])
}

// ---------------------------------------------------------------- world

func (e *Engine) cast() {
	add := func(spec actorSpec) *org {
		var town Town
		if len(spec.Towns) > 0 {
			town = towns[spec.Towns[0]]
		}
		o := &org{spec: spec, Org: domain.Org{ID: "org_" + spec.Slug, Name: spec.Name, Kind: spec.Kind,
			TaxID: e.cif(), City: town.Name, Province: town.Province, Region: town.Region, Country: "ES",
			Lat: town.Lat, Lon: town.Lon, Description: spec.Desc, Provenance: domain.ProvSimulated,
			Status: domain.OrgVerified, CreatedAt: e.cfg.Start.AddDate(0, -2, 0)}}
		if spec.Kind == domain.KindOperator {
			o.City, o.Province, o.Region, o.Country, o.Lat, o.Lon, o.TaxID = "Brussels", "Brussels", "Brussels-Capital", "BE", 50.85, 4.35, ""
			o.Provenance = domain.ProvOperational
		}
		seed := sha256.Sum256(fmt.Appendf(nil, "roadpass-sim-key:%d:%s", e.cfg.Seed, o.ID))
		o.key = signing.KeyFromSeed(signing.KeyID(o.ID, 1), o.ID, seed[:])
		e.keys[o.key.ID] = o.key.Public
		for i, tn := range spec.Towns {
			t := towns[tn]
			s := &site{org: o, town: t, n: i + 1, calendar: map[int]float64{}}
			s.Site = domain.Site{ID: fmt.Sprintf("site_%s_%d", spec.Slug, i+1), OrgID: o.ID,
				Name: siteName(spec, t), City: t.Name, Province: t.Province, Region: t.Region,
				Lat: round(t.Lat+e.u(-0.03, 0.03), 4), Lon: round(t.Lon+e.u(-0.03, 0.03), 4)}
			o.sites = append(o.sites, s)
			e.sites = append(e.sites, s)
		}
		e.orgs = append(e.orgs, o)
		e.byOrg[o.ID] = o
		return o
	}
	scale := e.cfg.Scale
	for _, sp := range sortingPlants {
		o := add(sp)
		for _, s := range o.sites {
			// Film-rich reject per plant: input 8–60 kt/yr (lognormal, median 20 kt),
			// 36 % rejected, of which ~40 % is film-rich enough for film recyclers.
			input := e.lognorm(20000, 0.55, 8000, 60000)
			s.CapacityT = round(input, -2)
			s.rateKgDay = input * 1000 * e.norm(0.36, 0.06, 0.25, 0.5) * 0.4 / 247 * scale
			s.Equipment = fmt.Sprintf("%d t/h sorting line, ballistic separators, optical sorters", []int{2, 4, 6, 8}[e.rng.IntN(4)])
			e.sorting = append(e.sorting, s)
		}
	}
	for _, sp := range recyclers {
		o := add(sp)
		for _, s := range o.sites {
			outT := e.tri(5000, 11000, 18000) * scale
			s.CapacityT = round(outT/scale, -2)
			s.capKgDay = outT * 1000 / 245
			s.yield = e.u(0.58, 0.72)
			s.lines = 1 + e.rng.IntN(2)
			s.Equipment = fmt.Sprintf("%d washing/agglomeration line(s), %d kg/h extruder", s.lines, 800+100*e.rng.IntN(8))
			e.recyclers = append(e.recyclers, s)
		}
	}
	for _, sp := range additiveProducers {
		o := add(sp)
		for _, s := range o.sites {
			s.product = sp.Extra
			s.rcPct = e.u(88, 95)
			s.capKgDay = 30000
			s.CapacityT = 7000
			s.lines = 1
			s.rc = map[string]float64{}
			s.Equipment = "twin-screw compounding line, 1.2 t/h"
			e.additive = append(e.additive, s)
		}
	}
	for _, sp := range asphaltCompanies {
		o := add(sp)
		for _, s := range o.sites {
			s.CapacityT = round(e.lognorm(60000, 0.45, 25000, 150000), -3)
			s.batchKg = []float64{3000, 3500, 4000}[e.rng.IntN(3)]
			s.dosage = e.tri(0.35, 0.5, 0.9)
			s.Equipment = fmt.Sprintf("%d t/h batch plant, %.1f t mixer, additive doser", 160+40*e.rng.IntN(5), s.batchKg/1000)
			e.asphalt = append(e.asphalt, s)
		}
	}
	for _, sp := range contractors {
		add(sp)
	}
	for _, sp := range roadOwners {
		add(sp)
	}
	for _, sp := range labs {
		e.labs = append(e.labs, add(sp))
	}
	for _, sp := range oversight {
		if o := add(sp); sp.Kind == domain.KindAuditor {
			e.auditor = o
		}
	}
	add(operatorSpec)

	// Supply relationships: nearest partners.
	for _, s := range e.sorting {
		s.targets = nearest(s, e.recyclers, 2)
	}
	for _, s := range e.asphalt {
		s.supplier = nearest(s, e.additive, 1)[0]
		s.supplier.customers = append(s.supplier.customers, s)
	}
	for _, a := range e.additive {
		a.targets = nearest(a, e.recyclers, 3)
	}
}

func nearest(from *site, pool []*site, n int) []*site {
	c := slices.Clone(pool)
	slices.SortStableFunc(c, func(a, b *site) int {
		return cmp.Compare(haversine(from.Lat, from.Lon, a.Lat, a.Lon), haversine(from.Lat, from.Lon, b.Lat, b.Lon))
	})
	return c[:min(n, len(c))]
}

func siteName(spec actorSpec, t Town) string {
	switch spec.Kind {
	case domain.KindAsphaltPlant:
		return "Planta de aglomerado " + t.Name
	case domain.KindRecycler:
		return "Planta de reciclado " + t.Name
	case domain.KindAdditiveProducer:
		return "Planta de aditivos " + t.Name
	case domain.KindSortingPlant:
		return "Planta de selección de envases " + t.Name
	}
	return t.Name
}

// cif returns a syntactically valid-looking but fictional Spanish company tax id.
func (e *Engine) cif() string {
	return fmt.Sprintf("%c%08d", "AB"[e.rng.IntN(2)], e.rng.IntN(100000000))
}

// ---------------------------------------------------------------- recording

// record finalises a new record: defects, hash, signature, checks, bookkeeping.
func (e *Engine) record(r *domain.Record) *domain.Record {
	r.Provenance = domain.ProvSimulated
	if r.Version == 0 {
		r.Version = 1
	}
	if r.Body == nil {
		r.Body = map[string]any{}
	}
	o := e.byOrg[r.OrgID]
	if r.Version == 1 {
		// References are unique per company and record type, as an ERP would number them.
		base := r.Ref
		for n := 2; e.refs[r.OrgID+"|"+string(r.Type)+"|"+r.Ref]; n++ {
			r.Ref = fmt.Sprintf("%s-%d", base, n)
		}
		e.refs[r.OrgID+"|"+string(r.Type)+"|"+r.Ref] = true
	}
	if errs := domain.Validate(r); len(errs) > 0 {
		panic(fmt.Sprintf("sim produced an invalid %s %s: %v", r.Type, r.ID, errs))
	}
	if e.chance(e.cfg.Defects.MissingField) {
		e.dropRecommended(r)
	}
	if len(r.Inputs) > 0 && r.Type != domain.TypeShipment && e.chance(e.cfg.Defects.DateTypo) {
		// A day/month mix-up on a paper ticket typed in later.
		r.OccurredAt = r.OccurredAt.AddDate(0, 0, -(2 + e.rng.IntN(5)))
	}
	h, err := r.ComputeHash()
	if err != nil {
		panic(err)
	}
	r.ContentHash = h
	if !e.chance(e.cfg.Defects.Unsigned) {
		sig, _ := o.key.Sign(h)
		r.Signature = &domain.Signature{KeyID: o.key.ID, OrgID: o.ID, Value: sig, SignedAt: r.OccurredAt.Add(time.Duration(e.rng.IntN(600)) * time.Second)}
	}
	if r.Receipt != nil {
		recv := e.byOrg[r.Counterparty]
		sig, _ := recv.key.SignReceipt(h, signing.ReceiptFacts{ReceivedAt: r.Receipt.ReceivedAt, MassKg: r.Receipt.MassKg, Decision: r.Receipt.Decision, Note: r.Receipt.Note})
		r.Receipt.Signature = domain.Signature{KeyID: recv.key.ID, OrgID: recv.ID, Value: sig, SignedAt: r.Receipt.ReceivedAt}
	}
	fs, err := checks.Run(context.Background(), (*env)(e), r)
	if err != nil {
		panic(err)
	}
	r.Status = checks.StatusFor(fs)
	r.CreatedAt = r.OccurredAt.Add(time.Duration(30+e.rng.IntN(3600)) * time.Second)
	for _, f := range fs {
		e.findings = append(e.findings, Finding{Finding: f, OrgID: r.OrgID, CreatedAt: r.CreatedAt, Status: "open"})
	}
	if r.Type != domain.TypeShipment {
		for _, in := range r.Inputs {
			e.consumed[in.ID] += in.MassKg
		}
	}
	if r.Type == domain.TypeDelivery && r.CountsMass() {
		e.laid[domain.Str(r.Body, "section_id")] += r.MassKg
	}
	if r.Type == domain.TypeRelease {
		e.releases[domain.Str(r.Body, "subject_id")] = decision{domain.Str(r.Body, "decision"), r.OccurredAt}
	}
	if r.Type == domain.TypeShipment && r.Receipt != nil {
		for _, in := range r.Inputs {
			e.received[in.ID+"|"+r.Counterparty] = r.Receipt.ReceivedAt.Unix()
		}
	}
	if dh, err := r.DupHash(); err == nil {
		if _, seen := e.dups[dh]; !seen {
			e.dups[dh] = r.ID
		}
	}
	e.recs[r.ID] = r
	e.order = append(e.order, r)
	e.stats.Records[r.Type]++
	return r
}

func (e *Engine) dropRecommended(r *domain.Record) {
	opts := map[domain.RecordType][]string{
		domain.TypeRejectLot: {"moisture_pct", "contamination_pct"}, domain.TypeRecycledLot: {"mfi_g10min", "energy_kwh"},
		domain.TypeAdditiveBatch: {"melting_point_c"}, domain.TypeAsphaltLot: {"gas_m3"}, domain.TypeDelivery: {"arrival_temp_c"},
	}[r.Type]
	if len(opts) > 0 {
		delete(r.Body, pick(e, opts))
	}
}

// env adapts the engine to checks.Env.
type env Engine

func (v *env) Record(_ context.Context, id string) (*domain.Record, bool, error) {
	r, ok := v.recs[id]
	return r, ok, nil
}

func (v *env) ReceivedShipment(_ context.Context, lot, org string) (bool, int64, error) {
	t, ok := v.received[lot+"|"+org]
	return ok, t, nil
}

func (v *env) ConsumedKg(_ context.Context, p, _ string) (float64, error) { return v.consumed[p], nil }

func (v *env) LaidKg(_ context.Context, section string) (float64, error) { return v.laid[section], nil }

func (v *env) DuplicateOf(_ context.Context, dh, except string) (string, bool, error) {
	id, ok := v.dups[dh]
	return id, ok && id != except, nil
}

func (v *env) Release(_ context.Context, id string) (string, time.Time, bool, error) {
	d, ok := v.releases[id]
	return d.decision, d.at, ok, nil
}

func (v *env) PublicKey(_ context.Context, id string) ([]byte, bool, error) {
	k, ok := v.keys[id]
	return k, ok, nil
}

// ---------------------------------------------------------------- run

// Run simulates every day and flushes everything to the sink.
func (e *Engine) Run(ctx context.Context) (Stats, error) {
	e.plan()
	n := e.days()
	for i := range n {
		if err := ctx.Err(); err != nil {
			return e.stats, err
		}
		e.sortingDay(i)
		e.recyclingDay(i)
		e.additiveDay(i)
		e.asphaltDay(i)
		if err := e.flushTelemetry(ctx, false); err != nil {
			return e.stats, err
		}
	}
	if err := e.flushTelemetry(ctx, true); err != nil {
		return e.stats, err
	}
	e.closeFindings()
	slices.SortStableFunc(e.order, func(a, b *domain.Record) int { return a.OccurredAt.Compare(b.OccurredAt) })
	const batch = 5000
	out := make([]domain.Record, 0, batch)
	for _, r := range e.order {
		out = append(out, *r)
		if len(out) == batch {
			if err := e.sink.Records(ctx, out); err != nil {
				return e.stats, err
			}
			out = out[:0]
		}
	}
	if len(out) > 0 {
		if err := e.sink.Records(ctx, out); err != nil {
			return e.stats, err
		}
	}
	for _, f := range e.findings {
		e.stats.Findings[f.Code]++
	}
	return e.stats, e.sink.Findings(ctx, e.findings)
}

func (e *Engine) flushTelemetry(ctx context.Context, all bool) error {
	if len(e.cycles) >= 50000 || (all && len(e.cycles) > 0) {
		if err := e.sink.Cycles(ctx, e.cycles); err != nil {
			return err
		}
		e.stats.Cycles += len(e.cycles)
		e.cycles = e.cycles[:0]
	}
	if len(e.readings) >= 50000 || (all && len(e.readings) > 0) {
		if err := e.sink.Readings(ctx, e.readings); err != nil {
			return err
		}
		e.stats.Readings += len(e.readings)
		e.readings = e.readings[:0]
	}
	return nil
}

// closeFindings ages findings: most warnings older than 30 days have been dealt
// with; most old blockers were accepted by the auditor with a signed reason.
func (e *Engine) closeFindings() {
	cut := e.cfg.Now.AddDate(0, 0, -30)
	affected := map[string]bool{}
	for i := range e.findings {
		f := &e.findings[i]
		if f.CreatedAt.After(cut) {
			continue
		}
		switch {
		case f.Severity == checks.Warning && e.chance(0.8):
			t := f.CreatedAt.Add(time.Duration(1+e.rng.IntN(20*24)) * time.Hour).Truncate(time.Second)
			f.Status, f.ClosedAt, f.ClosedBy = "resolved", &t, f.OrgID
			f.Reason = pick(e, []string{"Supplier sent the missing certificate.", "Re-test passed; lot released.",
				"Weighbridge ticket attached; difference within contract tolerance.", "Explained in the batch report."})
		case f.Severity == checks.Blocker && e.chance(0.65):
			t := f.CreatedAt.Add(time.Duration(2+e.rng.IntN(25*24)) * time.Hour).Truncate(time.Second)
			reason := pick(e, []string{"Paper delivery note found and checked on site.",
				"Duplicate ticket withdrawn; the remaining record is correct.", "Date corrected on the original ticket; material verified.",
				"Shipment confirmed by the carrier's CMR document.", "Mass difference explained by moisture loss; verified by lab report."})
			acc := e.record(&domain.Record{ID: e.nextID(domain.TypeAcceptance, t), Type: domain.TypeAcceptance, OrgID: e.auditor.ID,
				Ref: fmt.Sprintf("ACC-%s-%s", f.Code, f.RecordID), OccurredAt: t,
				Body: map[string]any{"subject_id": f.RecordID, "finding_code": f.Code, "reason": reason}})
			f.Status, f.ClosedAt, f.ClosedBy, f.Reason = "accepted", &t, acc.ID, reason
		}
		affected[f.RecordID] = true
	}
	// Recompute record status from the findings that remain open.
	open := map[string]string{}
	accepted := map[string]bool{}
	for _, f := range e.findings {
		switch f.Status {
		case "open":
			if f.Severity == checks.Blocker || open[f.RecordID] == "" {
				open[f.RecordID] = f.Severity
			}
		case "accepted":
			accepted[f.RecordID] = true
		}
	}
	for id := range affected {
		r := e.recs[id]
		switch {
		case open[id] == checks.Blocker:
			r.Status = domain.StatusBlocked
		case open[id] == checks.Warning:
			r.Status = domain.StatusFlagged
		case accepted[id]:
			r.Status = domain.StatusAccepted
		default:
			r.Status = domain.StatusOK
		}
	}
}

// Summary is a short human description of a run.
func (s Stats) Summary() string {
	var b strings.Builder
	total := 0
	types := make([]string, 0, len(s.Records))
	for t, n := range s.Records {
		types = append(types, fmt.Sprintf("%s=%d", t, n))
		total += n
	}
	slices.Sort(types)
	fmt.Fprintf(&b, "passport records %d (%s)\n", total, strings.Join(types, ", "))
	fmt.Fprintf(&b, "mixer cycles %d, extruder readings %d, total rows %d\n", s.Cycles, s.Readings, total+s.Cycles+s.Readings)
	fmt.Fprintf(&b, "asphalt with additive %.0f t, recycled plastic in roads %.1f t\n", s.AsphaltT, s.RecycledT)
	fs := make([]string, 0, len(s.Findings))
	for c, n := range s.Findings {
		fs = append(fs, fmt.Sprintf("%s=%d", c, n))
	}
	slices.Sort(fs)
	fmt.Fprintf(&b, "findings %s\n", strings.Join(fs, ", "))
	return b.String()
}
