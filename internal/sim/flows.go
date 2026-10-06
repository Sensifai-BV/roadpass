package sim

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
)

// Paving seasonality by month (Jan..Dec): low in winter and in the August
// holidays (research §4, EST).
var season = [12]float64{0.45, 0.6, 0.9, 1.1, 1.2, 1.25, 1.2, 0.55, 1.15, 1.1, 0.85, 0.45}

var northern = map[string]bool{rGAL: true, rAST: true, rCNB: true, rPV: true, rCYL: true, rNAV: true, rRIO: true}

// adoption is the share of an asphalt plant's eligible output made with the
// recycled-plastic additive; it ramps up as the scheme grows.
func (e *Engine) adoption(t time.Time) float64 {
	span := e.cfg.End.Sub(e.cfg.Start).Hours()
	x := t.Sub(e.cfg.Start).Hours() / span
	return 0.15 + 0.45*x
}

// closed reports plant holidays: two weeks in August and Christmas.
func closed(t time.Time) bool {
	m, d := t.Month(), t.Day()
	return (m == time.August && d >= 5 && d <= 20) || (m == time.December && d >= 24) || (m == time.January && d <= 6)
}

type mixSpec struct {
	name          string
	layer         string
	thLo, thHi    float64
	binder, binSD float64
	binMin        float64
}

var mixes = struct {
	surface, binder, base []struct {
		w float64
		m mixSpec
	}
}{
	surface: []struct {
		w float64
		m mixSpec
	}{
		{0.57, mixSpec{"AC 16 surf 50/70 S", "surface", 4, 5, 4.9, 0.2, 4.5}},
		{0.25, mixSpec{"AC 22 surf 50/70 S", "surface", 5, 6, 4.8, 0.2, 4.5}},
		{0.14, mixSpec{"BBTM 11B PMB 45/80-60", "surface", 2, 3, 5.2, 0.15, 4.75}},
		{0.03, mixSpec{"SMA 11 surf PMB 45/80-65", "surface", 3, 4, 6.0, 0.2, 5.6}},
		{0.01, mixSpec{"PA 16 PMB 45/80-60", "surface", 4, 5, 4.4, 0.15, 4.3}},
	},
	binder: []struct {
		w float64
		m mixSpec
	}{{1, mixSpec{"AC 22 bin 50/70 S", "binder", 5, 10, 4.4, 0.2, 4.0}}},
	base: []struct {
		w float64
		m mixSpec
	}{{1, mixSpec{"AC 32 base 50/70 G", "base", 7, 15, 4.1, 0.15, 4.0}}},
}

func (e *Engine) pickMix() mixSpec {
	r := e.rng.Float64()
	list := mixes.surface
	switch {
	case r < 0.05:
		list = mixes.base
	case r < 0.25:
		list = mixes.binder
	}
	x := e.rng.Float64()
	for _, c := range list {
		if x < c.w {
			return c.m
		}
		x -= c.w
	}
	return list[0].m
}

// plan creates the road projects that use the additive, and books them into
// plant calendars.
func (e *Engine) plan() {
	n := e.days()
	for _, s := range e.asphalt {
		region := s.Region
		roads := roadsByRegion[region]
		contractors := regionContractor[region]
		// Road works start two months in: the upstream chain needs that long to fill
		// recyclers' and producers' stock (a cold start would show as dosage findings).
		for m := e.cfg.Start.AddDate(0, 2, 0); m.Before(e.cfg.End); m = m.AddDate(0, 1, 0) {
			f := season[m.Month()-1] / 10.8
			if northern[region] && (m.Month() == time.December || m.Month() <= time.February) {
				f *= 0.6
			}
			target := s.CapacityT * f * e.adoption(m) * 0.8 * e.cfg.Scale
			for target > 50 {
				p := e.newProject(s, roads, contractors, region)
				start := int(m.Sub(e.cfg.Start).Hours()/24) + e.rng.IntN(28)
				e.schedule(p, start, n)
				if len(p.days) == 0 {
					break
				}
				e.projects = append(e.projects, p)
				target -= p.plannedT
			}
		}
	}
}

func (e *Engine) newProject(s *site, roads []struct {
	Road string
	Len  float64
}, contractors []string, region string) *project {
	mx := e.pickMix()
	road := pick(e, roads)
	length := e.lognorm(1.6, 0.7, 0.3, 12)
	lanes := 2.0
	switch x := e.rng.Float64(); {
	case x < 0.25:
		lanes = 1
	case x > 0.85:
		lanes = 3
	}
	th := round(e.u(mx.thLo, mx.thHi), 0)
	area := length * 1000 * lanes * 3.5
	t := math.Min(area*th/100*e.norm(2.42, 0.04, 2.3, 2.55), 25000)
	from := round(e.u(0, math.Max(0.5, road.Len-length)), 1)
	bearing := e.u(0, 2*math.Pi)
	dist := e.u(8, 42)
	lat := s.Lat + dist/111*math.Cos(bearing)
	lon := s.Lon + dist/(111*math.Cos(s.Lat*math.Pi/180))*math.Sin(bearing)
	return &project{plant: s, contractor: e.byOrg["org_"+pick(e, contractors)], owner: e.byOrg["org_"+regionOwner[region]],
		road: road.Road, kmFrom: from, kmTo: round(from+length, 1), layer: mx.layer, mix: mx.name, thickCm: th,
		areaM2: round(area, 0), lat: round(lat, 4), lon: round(lon, 4), region: region, plannedT: round(t, 0),
		distKm: round(dist*1.25+2, 1), warm: e.chance(0.08)}
}

// schedule books daily tonnages from start, skipping weekends, holidays and days
// on which the plant is already full.
func (e *Engine) schedule(p *project, start, n int) {
	const dayCap = 2200.0
	left := p.plannedT
	rate := e.u(600, 1800)
	for d := start; left > 1 && d < n; d++ {
		t := e.day(d)
		if !workday(t) || closed(t) {
			continue
		}
		free := dayCap - p.plant.calendar[d]
		if free < 150 {
			continue
		}
		take := math.Min(math.Min(rate*e.norm(1, 0.1, 0.6, 1.3), free), left)
		if left-take < 120 { // never leave a tiny last day
			take = left
		}
		take = round(take, 1)
		p.plant.calendar[d] += take
		p.days = append(p.days, planDay{d, take})
		e.byDay[d] = append(e.byDay[d], p)
		left -= take
	}
	p.plannedT = round(p.plannedT-left, 1)
	// Capped or partly scheduled works pave less: keep area × thickness in line
	// with the tonnage (check C6 compares them at about 2.4 t/m³).
	if full := p.areaM2 * p.thickCm / 100 * 2.42; p.thickCm > 0 && p.plannedT < 0.8*full {
		p.areaM2 = round(p.plannedT/(p.thickCm/100*2.42), 0)
	}
}

// ---------------------------------------------------------------- stage 1: sorting

func (e *Engine) sortingDay(i int) {
	t := e.day(i)
	wd := t.Weekday()
	if wd == time.Sunday {
		return
	}
	for _, s := range e.sorting {
		f := e.norm(1, 0.15, 0.4, 1.6)
		if wd == time.Saturday {
			f *= 0.5
		}
		s.acc += s.rateKgDay * f
		for s.acc >= 15000 {
			size := round(math.Min(s.acc, e.u(15000, 22000)), -1)
			s.acc -= size
			at := e.at(i, e.u(7, 20))
			route := "landfill"
			if e.chance(incinerationShare[s.Region]) {
				route = "incineration"
			}
			source := "municipal_packaging"
			if e.chance(0.15) {
				source = "commercial_film"
			}
			lot := e.record(&domain.Record{ID: e.nextID(domain.TypeRejectLot, at), Type: domain.TypeRejectLot, OrgID: s.org.ID,
				SiteID: s.ID, Ref: fmt.Sprintf("%s-%s-%03d", code(s), at.Format("060102"), e.rng.IntN(1000)), OccurredAt: at, MassKg: size,
				Body: map[string]any{"source": source, "ldpe_share_pct": round(e.norm(74, 7, 55, 90), 1),
					"moisture_pct": round(e.norm(15, 5, 5, 30), 1), "contamination_pct": round(e.norm(14, 4, 4, 30), 1),
					"avoided_route": route, "bales": float64(int(size / e.u(800, 1100)))}})
			s.outStock = append(s.outStock, stock{lot.ID, lot.MassKg, at})
		}
		// Ship waiting lots: one truck per lot, usually within two days.
		var keep []stock
		for _, l := range s.outStock {
			if !l.at.Before(e.day(i)) || !e.chance(0.6) {
				keep = append(keep, l)
				continue
			}
			to := s.targets[0]
			if len(s.targets) > 1 && e.chance(0.3) {
				to = s.targets[1]
			}
			e.ship(i, s, to, []stock{l}, 6, 14, false)
		}
		s.outStock = keep
	}
}

// ship records one truck from s to dst carrying lots, and moves them into dst's
// inbound inventory at the receipt time.
func (e *Engine) ship(i int, s, dst *site, lots []stock, hLo, hHi float64, unrecorded bool) {
	var mass float64
	inputs := make([]domain.Input, len(lots))
	for k, l := range lots {
		inputs[k] = domain.Input{ID: l.id, MassKg: round(l.kg, 1)}
		mass += round(l.kg, 1)
	}
	at := e.at(i, e.u(hLo, hHi))
	for _, l := range lots {
		if !at.After(l.at) {
			at = l.at.Add(time.Duration(30+e.rng.IntN(90)) * time.Minute)
		}
	}
	dist := roadKm(s, dst)
	arrive := at.Add(time.Duration((dist/62 + e.u(0.3, 1.5)) * float64(time.Hour))).Truncate(time.Second)
	recvKg := round(mass*e.norm(1, 0.003, 0.99, 1.01), 0)
	rc := &domain.Receipt{ReceivedAt: arrive, MassKg: recvKg, Decision: domain.Accepted}
	if e.chance(e.cfg.Defects.WeightGap) {
		recvKg = round(mass*e.u(0.93, 0.97), 0)
		// The intake desk notices the gap and says so in the signed receipt.
		rc.MassKg, rc.Decision = recvKg, domain.AcceptedWithNote
		rc.Note = fmt.Sprintf("Weighbridge %.0f kg against %.0f kg on the delivery note (%.1f %%). Reported to the sender.", recvKg, mass, (recvKg-mass)/mass*100)
	}
	if !unrecorded {
		e.record(&domain.Record{ID: e.nextID(domain.TypeShipment, at), Type: domain.TypeShipment, OrgID: s.org.ID, SiteID: s.ID,
			Counterparty: dst.org.ID, Ref: fmt.Sprintf("ALB-%s-%05d", code(s), e.rng.IntN(100000)), OccurredAt: at, MassKg: mass,
			Inputs: inputs, Body: map[string]any{"to_org_id": dst.org.ID, "to_site_id": dst.ID, "carrier": pick(e, carriers),
				"vehicle": e.plate(), "distance_km": dist},
			Receipt: rc})
	}
	for _, l := range lots {
		dst.inbound = append(dst.inbound, stock{l.id, l.kg, arrive})
	}
}

// code is a short site code used in references.
func code(s *site) string {
	w := strings.Fields(strings.ToUpper(strings.NewReplacer("Á", "A", "É", "E", "Í", "I", "Ó", "O", "Ú", "U", "Ñ", "N", "'", "").Replace(s.town.Name)))
	c := ""
	for _, x := range w {
		if len(x) > 2 {
			c += x[:1]
		}
	}
	for _, x := range w {
		if len(c) >= 3 {
			break
		}
		c += x[1:min(len(x), 3-len(c)+1)]
	}
	return fmt.Sprintf("%s%d", c[:min(3, len(c))], s.n)
}

// take removes up to kg from the FIFO inventory, using only stock available by t.
func take(inv *[]stock, kg float64, t time.Time) ([]domain.Input, float64) {
	var out []domain.Input
	var got float64
	keep := (*inv)[:0]
	for _, l := range *inv {
		if got >= kg-0.5 || l.at.After(t) {
			keep = append(keep, l)
			continue
		}
		use := math.Min(l.kg, kg-got)
		out = append(out, domain.Input{ID: l.id, MassKg: round(use, 1)})
		got += round(use, 1)
		if l.kg-use > 50 {
			keep = append(keep, stock{l.id, l.kg - use, l.at})
		}
	}
	*inv = keep
	return out, got
}

func available(inv []stock, t time.Time) float64 {
	var s float64
	for _, l := range inv {
		if !l.at.After(t) {
			s += l.kg
		}
	}
	return s
}

func merge(a []domain.Input, b []domain.Input) []domain.Input {
	for _, x := range b {
		found := false
		for k := range a {
			if a[k].ID == x.ID {
				a[k].MassKg = round(a[k].MassKg+x.MassKg, 1)
				found = true
				break
			}
		}
		if !found {
			a = append(a, x)
		}
	}
	return a
}

// split takes fraction f of every slice (the inputs that made one output lot).
func split(acc []domain.Input, f float64) (took, rest []domain.Input) {
	for _, x := range acc {
		t := round(x.MassKg*f, 1)
		if t > 0.5 {
			took = append(took, domain.Input{ID: x.ID, MassKg: t})
		}
		if r := round(x.MassKg-t, 1); r > 0.5 {
			rest = append(rest, domain.Input{ID: x.ID, MassKg: r})
		}
	}
	return took, rest
}

// ---------------------------------------------------------------- stage 2: recycling

func (e *Engine) recyclingDay(i int) {
	t := e.day(i)
	if !workday(t) || closed(t) {
		return
	}
	for _, s := range e.recyclers {
		if e.chance(0.04) { // breakdown
			continue
		}
		yieldToday := e.norm(s.yield, 0.03, 0.5, 0.8)
		need := s.capKgDay * e.norm(1, 0.08, 0.6, 1.25) / yieldToday
		ins, got := take(&s.inbound, need, e.at(i, 6))
		if got < 500 {
			continue
		}
		s.accSlices = merge(s.accSlices, ins)
		s.accOut += got * yieldToday
		e.extruderDay(i, s, 6, 22, s.capKgDay/16/float64(s.lines))
		for s.accOut >= 18000 {
			size := round(math.Min(s.accOut, e.u(18000, 24000)), -1)
			f := size / s.accOut
			took, rest := split(s.accSlices, f)
			s.accSlices, s.accOut = rest, s.accOut-size
			at := e.at(i, e.u(14, 22))
			regran := e.chance(0.3)
			body := map[string]any{"form": "agglomerate", "process": "washing,shredding,agglomeration",
				"mfi_g10min": round(e.lognorm(1.4, 0.45, 0.3, 4.8), 2), "moisture_pct": round(e.norm(1.2, 0.6, 0.3, 3.6), 2),
				"ash_pct": round(e.norm(4.5, 1.5, 1.5, 9.5), 2), "energy_kwh": round(size/1000*e.u(450, 700), 0)}
			if regran {
				body["form"], body["process"] = "regranulate", "washing,shredding,extrusion,melt filtration"
				body["mfi_g10min"] = round(e.lognorm(1.2, 0.35, 0.3, 2.5), 2)
				body["moisture_pct"] = round(e.u(0.05, 0.3), 2)
				body["ash_pct"] = round(e.u(0.3, 1.0), 2)
			}
			mass := size
			if e.chance(e.cfg.Defects.Overstated) {
				mass = round(size*e.u(1.6, 1.9), -1)
			}
			lot := e.record(&domain.Record{ID: e.nextID(domain.TypeRecycledLot, at), Type: domain.TypeRecycledLot, OrgID: s.org.ID,
				SiteID: s.ID, Ref: fmt.Sprintf("R%s%s%02d", code(s), at.Format("0601"), e.rng.IntN(100)), OccurredAt: at, MassKg: mass,
				Inputs: took, Body: body})
			tests := e.labTests(lot, s.org, []string{"mfi", "moisture"}, map[string]float64{"mfi": domain.Num(body, "mfi_g10min"),
				"moisture": domain.Num(body, "moisture_pct"), "ash": domain.Num(body, "ash_pct")}, 0.3, "ash")
			if ready := e.release(lot, s.org, tests); !ready.IsZero() {
				s.outStock = append(s.outStock, stock{lot.ID, lot.MassKg, ready})
			}
			s.lastLot = lot.ID
		}
		// Lots not bought by additive producers within 60 days are sold outside the chain.
		var keep []stock
		for _, l := range s.outStock {
			if t.Sub(l.at) < 60*24*time.Hour {
				keep = append(keep, l)
			}
		}
		s.outStock = keep
	}
}

// labTests records lab tests on a subject and returns them. extraP is the
// probability of the extra test.
func (e *Engine) labTests(subj *domain.Record, owner *org, tests []string, values map[string]float64, extraP float64, extra string) []*domain.Record {
	var out []*domain.Record
	if extra != "" && e.chance(extraP) {
		tests = append(tests, extra)
	}
	for _, test := range tests {
		by := owner
		if e.chance(0.2) {
			by = pick(e, e.labs)
		}
		v := values[test]
		switch test {
		case "mfi":
			v = round(v*e.norm(1, 0.04, 0.8, 1.2), 2)
		case "moisture", "ash":
			v = round(v*e.norm(1, 0.06, 0.7, 1.3), 2)
		}
		at := subj.OccurredAt.Add(time.Duration(e.u(2, 30) * float64(time.Hour))).Truncate(time.Second)
		out = append(out, e.record(&domain.Record{ID: e.nextID(domain.TypeLabTest, at), Type: domain.TypeLabTest, OrgID: by.ID,
			Ref: fmt.Sprintf("INF-%s-%s", strings.ToUpper(test[:min(3, len(test))]), subj.ID), OccurredAt: at,
			Body: map[string]any{"subject_id": subj.ID, "test": test, "value": v, "result": verdict(test, v, subj),
				"lab": by.Name}}))
	}
	return out
}

// release records quality's decision after the tests: released, or quarantined
// when a test failed. It returns when the lot may leave the plant (zero time:
// never, it is held). A small rate of lots is dispatched before the release.
func (e *Engine) release(lot *domain.Record, owner *org, tests []*domain.Record) time.Time {
	at := lot.OccurredAt
	failed := ""
	for _, t := range tests {
		if t.OccurredAt.After(at) {
			at = t.OccurredAt
		}
		if domain.Str(t.Body, "result") == "fail" {
			failed = domain.Str(t.Body, "test")
		}
	}
	at = at.Add(time.Duration(e.u(1, 8) * float64(time.Hour))).Truncate(time.Second)
	decision, note := domain.Released, "Within specification; certificate of analysis issued."
	if failed != "" {
		decision, note = domain.Quarantined, fmt.Sprintf("Held: %s out of specification, pending re-test.", failed)
	}
	// Drawn every time so the random stream, and with it the dataset, does not depend
	// on the date; a late release must still fall inside the simulated period.
	early := e.chance(e.cfg.Defects.EarlyDispatch) && !at.Add(30*24*time.Hour).After(e.cfg.End)
	if early {
		at = at.Add(30 * 24 * time.Hour) // the paperwork caught up after the truck left
	}
	e.record(&domain.Record{ID: e.nextID(domain.TypeRelease, at), Type: domain.TypeRelease, OrgID: owner.ID, SiteID: lot.SiteID,
		Ref: "REL-" + lot.ID, OccurredAt: at, Body: map[string]any{"subject_id": lot.ID, "decision": decision, "note": note}})
	switch {
	case decision == domain.Quarantined:
		return time.Time{}
	case early:
		return lot.OccurredAt.Add(2 * time.Hour)
	}
	return at
}

func verdict(test string, v float64, subj *domain.Record) string {
	lo, hi := math.Inf(-1), math.Inf(1)
	switch test {
	case "mfi":
		lo, hi = 0.2, 4
	case "moisture":
		hi = 3
	case "ash":
		hi = 8
	case "dsc_melting":
		lo, hi = 100, 135
	case "itsr":
		lo = 80
		if strings.Contains(domain.Str(subj.Body, "mix"), "surf") {
			lo = 85
		}
	case "air_voids":
		lo, hi = 3, 8
	case "wheel_tracking":
		hi = 0.15
	case "binder_content":
		lo, hi = 3.5, 6.5
	}
	if v < lo || v > hi {
		return "fail"
	}
	return "pass"
}

// extruderDay emits 5-minute line readings between hours h0 and h1.
func (e *Engine) extruderDay(i int, s *site, h0, h1 float64, kgH float64) {
	if !e.cfg.Telemetry {
		return
	}
	for line := 1; line <= s.lines; line++ {
		ln := fmt.Sprintf("L%d", line)
		meltSet := 200 + 5*float64(line)
		for m := h0 * 60; m < h1*60; m += 5 {
			if e.chance(e.cfg.Defects.SensorDrop) {
				m += float64(20 + 5*e.rng.IntN(15)) // a dropout: the PLC link goes quiet, the line keeps running
				continue
			}
			ts := e.day(i).Add(time.Duration(m) * time.Minute)
			r := Reading{TS: ts, OrgID: s.org.ID, SiteID: s.ID, Line: ln, LotID: s.lastLot,
				MeltC: float32(e.norm(meltSet, 3, 170, 240)), PressureBar: float32(e.norm(160, 12, 90, 240)),
				ThroughputKgH: float32(kgH * e.norm(1, 0.06, 0.5, 1.4)), RPM: float32(e.norm(85, 3, 60, 110)),
				LoadPct: float32(e.norm(72, 6, 30, 98))}
			if e.chance(0.03) { // micro-stop
				r.ThroughputKgH, r.RPM, r.LoadPct = 0, 0, 5
			}
			if e.chance(e.cfg.Defects.ExtruderGlich) {
				if e.chance(0.7) {
					r.MeltC *= 10
				} else {
					r.PressureBar /= 100
				}
			}
			e.readings = append(e.readings, r)
		}
	}
}

// ---------------------------------------------------------------- stage 3: additive

func (e *Engine) demand(s *site, from, days int) float64 {
	var kg float64
	for d := from; d < from+days; d++ {
		for _, p := range e.byDay[d] {
			if p.plant != s {
				continue
			}
			for _, pd := range p.days {
				if pd.day == d {
					kg += pd.t * 1000 * s.dosage / 100
				}
			}
		}
	}
	return kg
}

func (e *Engine) additiveDay(i int) {
	t := e.day(i)
	for _, s := range e.additive {
		var need float64
		for _, c := range s.customers {
			need += e.demand(c, i, 21)
		}
		var have float64
		for _, l := range s.outStock {
			have += l.kg
		}
		target := need*1.1 + 5000
		// Buy recycled plastic: keep ~15 days of production in stock.
		recycledNeed := math.Max(20000, need*s.rcPct/100/21*15)
		if available(s.inbound, t.Add(48*time.Hour)) < recycledNeed && workday(t) {
			for _, r := range s.targets {
				if len(r.outStock) == 0 {
					continue
				}
				var lots []stock
				var kg float64
				for len(r.outStock) > 0 && kg < 20000 {
					lots = append(lots, r.outStock[0])
					kg += r.outStock[0].kg
					r.outStock = r.outStock[1:]
				}
				e.ship(i, r, s, lots, 6, 12, false)
				break
			}
		}
		if !workday(t) || closed(t) || have >= target {
			continue
		}
		hours := 0.0
		for k := 0; k < 3 && have < target; k++ {
			at := e.at(i, 7+float64(k)*3.5+e.u(0, 0.5))
			size := round(e.u(8000, 12000), -1)
			rc := round(e.norm(s.rcPct, 1, 85, 97), 1)
			ins, got := take(&s.inbound, size*rc/100, at)
			if got < 2000 {
				break
			}
			size = round(got/(rc/100), -1)
			body := map[string]any{"product": s.product, "form": "pellets", "recycled_content_pct": rc,
				"mfi_g10min": round(e.lognorm(2.0, 0.25, 0.8, 4), 2), "melting_point_c": round(e.norm(111, 3, 100, 132), 1),
				"energy_kwh": round(size/1000*e.u(280, 380), 0)}
			b := e.record(&domain.Record{ID: e.nextID(domain.TypeAdditiveBatch, at), Type: domain.TypeAdditiveBatch, OrgID: s.org.ID,
				SiteID: s.ID, Ref: fmt.Sprintf("%s-%s-%d", strings.Fields(s.product)[0], at.Format("060102"), k+1), OccurredAt: at,
				MassKg: size, Inputs: ins, Body: body})
			tests := e.labTests(b, s.org, []string{"mfi", "dsc_melting"}, map[string]float64{"mfi": domain.Num(body, "mfi_g10min"),
				"dsc_melting": domain.Num(body, "melting_point_c")}, 0, "")
			if ready := e.release(b, s.org, tests); !ready.IsZero() {
				s.outStock = append(s.outStock, stock{b.ID, b.MassKg, ready})
			}
			s.rc[b.ID] = rc
			have += size
			hours += 3
			s.lastLot = b.ID
		}
		if hours > 0 {
			e.extruderDay(i, s, 7, 7+hours, 1200)
		}
	}
}

// ---------------------------------------------------------------- stages 4–6: asphalt, delivery, road

func (e *Engine) asphaltDay(i int) {
	t := e.day(i)
	for _, s := range e.asphalt {
		// Keep ten days of additive in the silo.
		upcoming := e.demand(s, i, 10)
		sup := s.supplier
		for available(s.inbound, t.Add(72*time.Hour)) < upcoming && len(sup.outStock) > 0 && workday(t) {
			var lots []stock
			var kg float64
			for len(sup.outStock) > 0 && kg+sup.outStock[0].kg <= 24500 {
				lots = append(lots, sup.outStock[0])
				kg += sup.outStock[0].kg
				sup.outStock = sup.outStock[1:]
			}
			if len(lots) == 0 {
				lots = []stock{sup.outStock[0]}
				sup.outStock = sup.outStock[1:]
			}
			e.ship(i, sup, s, lots, 6, 10, e.chance(e.cfg.Defects.UnshippedLot))
		}
	}
	for _, p := range e.byDay[i] {
		for k, pd := range p.days {
			if pd.day == i {
				e.produce(i, p, pd.t, k == 0, k == len(p.days)-1)
			}
		}
	}
	for _, p := range e.completions[i] {
		e.complete(i, p)
	}
}

func (e *Engine) produce(i int, p *project, tonnes float64, first, last bool) {
	s := p.plant
	if first {
		a, b := p.along(0), p.along(1)
		at := e.at(i, 6.5)
		works := "rehabilitation"
		if x := e.rng.Float64(); x < 0.1 {
			works = "new_construction"
		} else if x < 0.3 {
			works = "maintenance"
		}
		sec := e.record(&domain.Record{ID: e.nextID(domain.TypeRoadSection, at), Type: domain.TypeRoadSection, OrgID: p.contractor.ID,
			Ref: fmt.Sprintf("OB-%s-%s-%.0f", p.road, at.Format("0601"), p.kmFrom*10), OccurredAt: at, MassKg: p.plannedT * 1000,
			Body: map[string]any{"road": p.road, "km_from": p.kmFrom, "km_to": p.kmTo, "carriageway": pick(e, []string{"Both", "Right", "Left"}),
				"layer": p.layer, "thickness_cm": p.thickCm, "area_m2": p.areaM2, "works": works, "mix": p.mix,
				"owner_org_id": p.owner.ID, "lat": p.lat, "lon": p.lon, "region": p.region,
				"lat_from": a.lat, "lon_from": a.lon, "lat_to": b.lat, "lon_to": b.lon}})
		p.sectionID = sec.ID
	}
	at := e.at(i, 6.5)
	mass := round(tonnes*1000, 0)
	dosage := s.dosage * e.norm(1, 0.05, 0.85, 1.15)
	ins, got := take(&s.inbound, mass*dosage/100, at)
	actual := got / mass * 100
	mx := e.mixSpec(p.mix)
	binder := round(e.norm(mx.binder, mx.binSD, mx.binMin-0.1, mx.binder+0.8), 2)
	process, temp := "hot", round(e.norm(156, 5, 140, 172), 0)
	if p.warm {
		process, temp = "warm", round(e.norm(125, 5, 110, 142), 0)
	}
	dose := round(actual, 2)
	if e.chance(e.cfg.Defects.DosageTypo) {
		dose = round(actual*10, 2)
	}
	lot := e.record(&domain.Record{ID: e.nextID(domain.TypeAsphaltLot, at), Type: domain.TypeAsphaltLot, OrgID: s.org.ID, SiteID: s.ID,
		Ref:        fmt.Sprintf("%s-%s-%s", code(s), at.Format("060102"), strings.ReplaceAll(strings.Fields(p.mix)[0]+strings.Fields(p.mix)[1], " ", "")),
		OccurredAt: at, MassKg: mass, Inputs: ins,
		Body: map[string]any{"mix": p.mix, "process": process, "binder_pct": binder, "additive_kg": round(got, 1),
			"dosage_pct_mix": dose, "mixing_temp_c": temp, "gas_m3": round(tonnes*e.norm(7.2, 0.6, 5.5, 9.5), 0)}})
	for _, in := range ins {
		e.stats.RecycledT += in.MassKg * s.supplier.rc[in.ID] / 100 / 1000
	}
	e.stats.AsphaltT += tonnes
	// QC: binder content and voids on every lot; water sensitivity and rutting weekly.
	tests := []string{"binder_content", "air_voids"}
	if e.chance(0.2) {
		tests = append(tests, "itsr", "wheel_tracking")
	}
	vals := map[string]float64{"binder_content": round(binder+e.norm(0, 0.12, -0.5, 0.5), 2),
		"air_voids": round(e.norm(5.0, 0.6, 2.5, 8.5), 1), "itsr": round(e.norm(91, 4, 72, 99), 1),
		"wheel_tracking": round(e.lognorm(0.048, 0.25, 0.02, 0.2), 3)}
	e.labTests(lot, s.org, tests, vals, 0, "")
	e.mixer(lot, s, at, binder, temp, actual)
	e.deliver(i, p, lot, temp)
	if last {
		done := i + 1 + e.rng.IntN(3)
		if e.completions == nil {
			e.completions = map[int][]*project{}
		}
		e.completions[done] = append(e.completions[done], p)
	}
}

func (e *Engine) mixSpec(name string) mixSpec {
	for _, group := range [][]struct {
		w float64
		m mixSpec
	}{mixes.surface, mixes.binder, mixes.base} {
		for _, c := range group {
			if c.m.name == name {
				return c.m
			}
		}
	}
	return mixes.surface[0].m
}

func (e *Engine) mixer(lot *domain.Record, s *site, start time.Time, binder, temp, dosage float64) {
	if !e.cfg.Telemetry {
		return
	}
	n := int(math.Ceil(lot.MassKg / s.batchKg))
	ts := start
	for c := 1; c <= n; c++ {
		b := s.batchKg * e.norm(1, 0.01, 0.95, 1.05)
		add := b * dosage / 100 * e.norm(1, 0.015, 0.9, 1.1)
		if e.chance(0.002) {
			add = 0 // doser missed a cycle
		}
		e.cycles = append(e.cycles, MixerCycle{TS: ts, OrgID: s.org.ID, SiteID: s.ID, LotID: lot.ID, Mix: domain.Str(lot.Body, "mix"),
			Cycle: uint32(c), BatchKg: float32(b), AdditiveKg: float32(add), BinderKg: float32(b * binder / 100 * e.norm(1, 0.01, 0.95, 1.05)),
			TC: float32(e.norm(temp, 3, temp-12, temp+12))})
		step := e.u(62, 80)
		if e.chance(0.01) {
			step += e.u(300, 1800) // waiting for trucks
		}
		ts = ts.Add(time.Duration(step * float64(time.Second)))
	}
}

func (e *Engine) deliver(i int, p *project, lot *domain.Record, temp float64) {
	s := p.plant
	left := lot.MassKg
	trucks := int(math.Ceil(left / 25000))
	span := 9.0
	for k := 0; left > 1; k++ {
		load := math.Min(left, round(e.u(24000, 26000), -1))
		left -= load
		at := lot.OccurredAt.Add(time.Duration((0.5 + span*float64(k)/float64(trucks)) * float64(time.Hour))).Truncate(time.Second)
		s.ticket++
		dep := round(temp-e.u(2, 6), 0)
		arr := round(dep-9*p.distKm/50-e.u(2, 6), 0)
		body := map[string]any{"ticket": fmt.Sprintf("T-%s-%06d", code(s), s.ticket), "section_id": p.sectionID,
			"vehicle": e.plate(), "distance_km": p.distKm, "departure_temp_c": dep, "arrival_temp_c": arr}
		// The paver lays the loads one after another from km Start, each covering its share of the planned tonnes.
		if p.plannedT > 0 && p.laidKg < p.plannedT*1000 {
			a, b := p.along(p.laidKg/(p.plannedT*1000)), p.along((p.laidKg+load)/(p.plannedT*1000))
			body["lat_from"], body["lon_from"], body["lat_to"], body["lon_to"] = a.lat, a.lon, b.lat, b.lon
		}
		// Custody passes to the contractor at the paver, so the delivery names them.
		rec := &domain.Record{ID: e.nextID(domain.TypeDelivery, at), Type: domain.TypeDelivery, OrgID: s.org.ID, SiteID: s.ID,
			Counterparty: p.contractor.ID,
			Ref:          body["ticket"].(string), OccurredAt: at, MassKg: round(load, 0),
			Inputs: []domain.Input{{ID: lot.ID, MassKg: round(load, 0)}}, Body: body}
		e.record(rec)
		p.laidKg += rec.MassKg
		if e.chance(e.cfg.Defects.DuplicateTkt) {
			dup := *rec
			dup.ID = e.nextID(domain.TypeDelivery, at)
			dup.Ref = rec.Ref + "/2"
			dup.Body = cloneBody(rec.Body)
			dup.Signature, dup.Status, dup.ContentHash = nil, "", ""
			e.record(&dup)
		}
	}
}

func cloneBody(b map[string]any) map[string]any {
	c := make(map[string]any, len(b))
	for k, v := range b {
		c[k] = v
	}
	return c
}

// complete records version 2 of a road section: completion date and as-laid mass.
func (e *Engine) complete(i int, p *project) {
	v1 := e.recs[p.sectionID]
	if v1 == nil {
		return
	}
	at := e.at(i, e.u(16, 19))
	body := cloneBody(v1.Body)
	body["completed_at"] = at.Format(time.RFC3339)
	v2 := &domain.Record{ID: v1.ID, Version: 2, PrevHash: v1.ContentHash, Type: domain.TypeRoadSection, OrgID: v1.OrgID,
		Ref: v1.Ref, OccurredAt: at, MassKg: round(p.laidKg, 0), Body: body}
	// The duplicate check must not compare v2 with its own v1.
	e.record(v2)
	lab := pick(e, e.labs)
	tat := at.Add(time.Duration(e.u(48, 240) * float64(time.Hour))).Truncate(time.Second)
	e.record(&domain.Record{ID: e.nextID(domain.TypeLabTest, tat), Type: domain.TypeLabTest, OrgID: lab.ID,
		Ref: "TES-" + v1.ID, OccurredAt: tat,
		Body: map[string]any{"subject_id": v1.ID, "test": "density", "value": round(e.norm(2.42, 0.03, 2.3, 2.55), 3), "result": "pass", "lab": lab.Name}})
}

type latLon struct{ lat, lon float64 }

// along is the point at fraction f (0 = km Start, 1 = km End) of the section, laid
// as a straight line through its mid-point. The heading comes from the road and
// chainage, not the random stream, so adding geometry leaves the rest of the data unchanged.
func (p *project) along(f float64) latLon {
	f = math.Max(0, math.Min(1, f))
	h := fnv.New32a()
	fmt.Fprintf(h, "%s/%.1f", p.road, p.kmFrom)
	heading := float64(h.Sum32()%360) * math.Pi / 180
	d := (f - 0.5) * (p.kmTo - p.kmFrom) // km from the mid-point
	return latLon{
		lat: round(p.lat+d/111*math.Cos(heading), 5),
		lon: round(p.lon+d/(111*math.Cos(p.lat*math.Pi/180))*math.Sin(heading), 5),
	}
}
