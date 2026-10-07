package demo

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"strings"
	"time"
)

const shortTon = 907.18474 // kg; the MoDOT report's "tons" are US short tons

// Wire types of the records API.
type (
	input struct {
		ID     string  `json:"id"`
		MassKg float64 `json:"massKg"`
	}
	rec struct {
		ID         string         `json:"id,omitempty"`
		Type       string         `json:"type"`
		Ref        string         `json:"ref"`
		SiteID     string         `json:"siteId,omitempty"`
		OccurredAt string         `json:"occurredAt"`
		MassKg     float64        `json:"massKg"`
		Inputs     []input        `json:"inputs,omitempty"`
		Data       map[string]any `json:"data"`
		Provenance string         `json:"provenance,omitempty"`
		Note       string         `json:"note,omitempty"`
	}
	result struct {
		ID       string `json:"id"`
		Ref      string `json:"ref"`
		Status   string `json:"status"`
		Findings []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"findings"`
	}
	lotInfo struct {
		id, site, mix              string
		kg, additiveKg, binder, tc float64
		hoursAgo                   float64
	}
)

// build is one run of the scenario.
type build struct {
	m    *Manager
	ctx  context.Context
	now  time.Time
	A    map[string]*actor
	site map[string]string // site kind (and "yard") → id
	rng  *rand.Rand
	lots []lotInfo // asphalt lots, for the mixer log
	n    int       // delivery ticket counter
}

func (b *build) at(hoursAgo float64) string {
	return b.now.Add(-time.Duration(hoursAgo * float64(time.Hour))).Format(time.RFC3339)
}

func (b *build) submit(a *actor, rows ...rec) ([]result, error) {
	var out []result
	for i := 0; i < len(rows); i += 200 {
		chunk := rows[i:min(i+200, len(rows))]
		var res struct {
			Results []result `json:"results"`
		}
		if err := b.m.call(b.ctx, a, "POST", "/api/v1/records", map[string]any{"records": chunk}, &res); err != nil {
			return nil, err
		}
		if len(res.Results) != len(chunk) {
			return nil, fmt.Errorf("%s: %d results for %d records", a.Key, len(res.Results), len(chunk))
		}
		out = append(out, res.Results...)
	}
	return out, nil
}

// clean submits rows that must not be blocked by a data check.
func (b *build) clean(a *actor, rows ...rec) ([]result, error) {
	res, err := b.submit(a, rows...)
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		if r.Status == "blocked" {
			return nil, fmt.Errorf("%s (%s) is blocked: %+v", r.Ref, a.Key, r.Findings)
		}
	}
	return res, nil
}

func (b *build) receipt(a *actor, shipmentID string, hoursAgo, kg float64, decision, note string) error {
	return b.m.call(b.ctx, a, "POST", "/api/v1/shipments/"+shipmentID+"/receipt",
		map[string]any{"receivedAt": b.at(hoursAgo), "massKg": kg, "decision": decision, "note": note}, nil)
}

// ---------------------------------------------------------------- projects

type section struct {
	key, name string
	kg        float64
	dosage    float64 // % of mix
	mix       string
	process   string  // hot | warm
	binder    float64 // % of mix
	thickCm   float64
	widthM    float64
	coords    [4]float64 // lat from, lon from, lat to, lon to
}

type project struct {
	code, road string
	startHours float64 // hours ago the first record is made
	sections   []section
	real       bool    // quantities published by MoDOT: PublicReference
	open       bool    // the last section is still being paved
	loadsDone  float64 // open projects: share of loads delivered
	defect     string  // which data-quality story this project tells
}

func (s section) area() float64 { return math.Round(s.kg / 2450 / (s.thickCm / 100)) }
func (s section) lenKm() float64 {
	return math.Round(s.area()/s.widthM) / 1000
}
func (s section) addKg() float64 { return math.Round(s.kg * s.dosage / 100) }

func st(tons float64) float64 { return math.Round(tons * shortTon) }
func t(tons float64) float64  { return tons * 1000 }

var projects = []project{
	{code: "R740", road: "Route 740 (Stadium Blvd)", startHours: 1500, real: true, sections: []section{
		{"25PE", "Route 740 section 25PE", st(1370.8), 0.25, "Route 740 surface mix 25PE", "hot", 4.7, 4, 7, [4]float64{38.9446, -92.3330, 38.9395, -92.3329}},
		{"50PE", "Route 740 section 50PE", st(835.9), 0.5, "Route 740 surface mix 50PE", "hot", 4.7, 4, 7, [4]float64{38.9395, -92.3329, 38.9362, -92.3326}},
		{"50PEL", "Route 740 section 50PEL", st(1370.8), 0.5, "Route 740 surface mix 50PEL", "hot", 4.7, 4, 7, [4]float64{38.9362, -92.3326, 38.9310, -92.3324}},
	}},
	{code: "PROV", road: "Providence Rd", startHours: 1100, defect: "temperature", sections: []section{
		{"N", "Providence Rd, Business Loop to Worley", t(520), 0.5, "SP125C surface 64-22", "hot", 5.0, 4, 7.4, [4]float64{38.9560, -92.3262, 38.9470, -92.3262}},
		{"S", "Providence Rd, Worley to Stadium", t(610), 0.5, "SP125C surface 64-22", "hot", 5.0, 4, 7.4, [4]float64{38.9470, -92.3262, 38.9380, -92.3262}},
	}},
	{code: "BL70", road: "Business Loop 70", startHours: 800, defect: "reject", sections: []section{
		{"A", "Business Loop 70 overlay", t(780), 0.4, "SP125C surface 64-22", "hot", 5.1, 4, 7.4, [4]float64{38.9519, -92.3370, 38.9519, -92.3150}},
	}},
	{code: "NIF", road: "Nifong Blvd", startHours: 520, defect: "early-release", sections: []section{
		{"W", "Nifong Blvd west", t(430), 0.6, "SP125C surface 64-22", "hot", 5.0, 4, 7.4, [4]float64{38.9120, -92.3400, 38.9120, -92.3230}},
		{"E", "Nifong Blvd east (warm mix)", t(390), 0.5, "SP125C WMA surface", "warm", 5.1, 4, 7.4, [4]float64{38.9120, -92.3230, 38.9120, -92.3060}},
	}},
	{code: "O63", road: "Old 63", startHours: 330, defect: "dosage-typo", sections: []section{
		{"A", "Old 63, Rock Quarry to Grindstone", t(560), 0.5, "SP190C binder 64-22", "hot", 4.6, 6, 7.4, [4]float64{38.9000, -92.3250, 38.9250, -92.3250}},
	}},
	{code: "SCOTT", road: "Scott Blvd", startHours: 100, open: true, loadsDone: 0.55, sections: []section{
		{"A", "Scott Blvd resurfacing", t(380), 0.5, "SP125C surface 64-22", "hot", 5.0, 4, 7.4, [4]float64{38.8990, -92.3800, 38.8800, -92.3800}},
	}},
}

func round(x float64, step float64) float64 { return math.Round(x/step) * step }

func (b *build) project(p project) error {
	dt := func(h float64) float64 { return p.startHours - h }
	prov := ""
	if p.real {
		prov = "PublicReference"
	}
	note := ""
	if p.real {
		note = "Published: section quantity, dosage and binder content (MoDOT final report cmr 23-009, Route 740). Dates and all other values are demo placeholders."
	}
	var need float64
	for _, s := range p.sections {
		need += s.addKg()
	}
	batchKg := round(need*1.15/2, 100) // two additive batches
	pellets := batchKg * 2
	recycled := round(pellets*1.25, 100)
	rejects := round(recycled/0.655, 100)
	rA, rB := round(rejects*0.52, 100), round(rejects*0.48, 100)
	pre := p.code + "-"
	own := b.site
	sorter, recycler, comp, mixer, site := b.A["sorter"], b.A["recycler"], b.A["compounder"], b.A["mixer"], b.A["site"]
	dispatch, intake, lab, quality := b.A["dispatch"], b.A["intake"], b.A["lab"], b.A["quality"]

	// 1. reject film is sorted (stage 1)
	rej, err := b.clean(sorter,
		rec{Type: "reject_lot", Ref: pre + "REJ-A", SiteID: own["sorting_plant"], OccurredAt: b.at(dt(1)), MassKg: rA,
			Data: map[string]any{"source": "municipal_packaging", "ldpe_share_pct": 78, "moisture_pct": 10, "contamination_pct": 10, "avoided_route": "landfill", "bales": math.Round(rA / 810)}},
		rec{Type: "reject_lot", Ref: pre + "REJ-B", SiteID: own["sorting_plant"], OccurredAt: b.at(dt(3)), MassKg: rB,
			Data: map[string]any{"source": "commercial_film", "ldpe_share_pct": 84, "moisture_pct": 7, "contamination_pct": 8, "avoided_route": "incineration", "bales": math.Round(rB / 810)}})
	if err != nil {
		return err
	}
	// 2. shipped to the recycler and weighed in
	recvKg := rA + rB - 20
	if p.defect == "reject" || p.defect == "dosage-typo" {
		recvKg = (rA + rB) * 0.955 // a weighbridge difference the intake officer notes
	}
	sh1, err := b.submit(dispatch, rec{Type: "shipment", Ref: pre + "SHP-1", SiteID: own["sorting_plant"], OccurredAt: b.at(dt(6)), MassKg: rA + rB,
		Inputs: []input{{rej[0].ID, rA}, {rej[1].ID, rB}}, Data: map[string]any{"to_site_id": own["recycler"], "carrier": "Own fleet", "vehicle": "CP-14", "distance_km": 6}})
	if err != nil {
		return err
	}
	decision, rnote := "accepted", ""
	if recvKg < rA+rB-100 {
		decision, rnote = "accepted_with_note", "Bales wetter than declared; weighbridge shows less than the delivery note."
	}
	if err := b.receipt(intake, sh1[0].ID, dt(8), math.Round(recvKg), decision, rnote); err != nil {
		return err
	}
	// 3. recycled into pellets (stage 2); tests; release
	rpMoist := 0.2
	if p.defect == "dosage-typo" {
		rpMoist = 3.6 // outside the 3 % window: a warning that stays visible
	}
	rl, err := b.submit(recycler, rec{Type: "recycled_lot", Ref: pre + "PEL-1", SiteID: own["recycler"], OccurredAt: b.at(dt(12)), MassKg: recycled,
		Inputs: []input{{rej[0].ID, rA}, {rej[1].ID, rB}},
		Data:   map[string]any{"form": "regranulate", "process": "washing,shredding,melt filtration,pelletising", "mfi_g10min": 1.2, "moisture_pct": rpMoist, "ash_pct": 1.5, "energy_kwh": math.Round(recycled * 0.33)}})
	if err != nil {
		return err
	}
	if rl[0].Status == "blocked" {
		return fmt.Errorf("%s blocked: %+v", rl[0].Ref, rl[0].Findings)
	}
	if _, err := b.submit(lab,
		rec{Type: "lab_test", Ref: pre + "LAB-PEL-MFI", SiteID: own["recycler"], OccurredAt: b.at(dt(14)), Data: map[string]any{"subject_id": rl[0].ID, "test": "mfi", "value": 1.2, "result": "pass", "lab": "Materials laboratory Columbia"}},
		rec{Type: "lab_test", Ref: pre + "LAB-PEL-MOI", SiteID: own["recycler"], OccurredAt: b.at(dt(14)), Data: map[string]any{"subject_id": rl[0].ID, "test": "moisture", "value": rpMoist, "result": "pass", "lab": "Materials laboratory Columbia"}}); err != nil {
		return err
	}
	release := func(subject, ref string, h float64, why string) error {
		_, err := b.submit(quality, rec{Type: "release", Ref: ref, SiteID: "", OccurredAt: b.at(h), Data: map[string]any{"subject_id": subject, "decision": "released", "note": why}})
		return err
	}
	early := p.defect == "early-release"
	// 4. quality releases the pellets, then they go to additive handling (one project ships first: C9)
	if !early {
		if err := release(rl[0].ID, pre+"REL-PEL", dt(16), "Melt flow index within film grade"); err != nil {
			return err
		}
	}
	sh2At := dt(18)
	if early {
		sh2At = dt(15) // shipped before quality released it
	}
	sh2, err := b.submit(dispatch, rec{Type: "shipment", Ref: pre + "SHP-2", SiteID: own["recycler"], OccurredAt: b.at(sh2At), MassKg: pellets,
		Inputs: []input{{rl[0].ID, pellets}}, Data: map[string]any{"to_site_id": own["additive_producer"], "carrier": "Own fleet", "vehicle": "CP-14", "distance_km": 3}})
	if err != nil {
		return err
	}
	if early {
		if err := release(rl[0].ID, pre+"REL-PEL", dt(16), "Melt flow index within film grade (released after dispatch)"); err != nil {
			return err
		}
	}
	if err := b.receipt(intake, sh2[0].ID, dt(19), pellets-8, "accepted", ""); err != nil {
		return err
	}
	// 5. additive batches (stage 3), tests, release, shipment to the asphalt plant
	product := "CP-Mod LDPE pellets"
	if p.real {
		product = "Post-consumer LLDPE pellets (supplier per MoDOT report: Avangard Innovative)"
	}
	ab, err := b.clean(comp,
		rec{Type: "additive_batch", Ref: pre + "ADD-1", SiteID: own["additive_producer"], OccurredAt: b.at(dt(22)), MassKg: batchKg, Inputs: []input{{rl[0].ID, batchKg}},
			Data: map[string]any{"product": product, "form": "pellets", "recycled_content_pct": 100, "mfi_g10min": 1.2, "melting_point_c": 122, "energy_kwh": 120}},
		rec{Type: "additive_batch", Ref: pre + "ADD-2", SiteID: own["additive_producer"], OccurredAt: b.at(dt(23)), MassKg: batchKg, Inputs: []input{{rl[0].ID, batchKg}},
			Data: map[string]any{"product": product, "form": "pellets", "recycled_content_pct": 100, "mfi_g10min": 1.2, "melting_point_c": 122, "energy_kwh": 120}})
	if err != nil {
		return err
	}
	for i, x := range ab {
		if _, err := b.submit(lab, rec{Type: "lab_test", Ref: fmt.Sprintf("%sLAB-ADD-%d", pre, i+1), SiteID: own["additive_producer"], OccurredAt: b.at(dt(24)),
			Data: map[string]any{"subject_id": x.ID, "test": "dsc_melting", "value": 122, "result": "pass", "lab": "Materials laboratory Columbia"}}); err != nil {
			return err
		}
		if err := release(x.ID, fmt.Sprintf("%sREL-ADD-%d", pre, i+1), dt(25), "Melting peak 122 °C"); err != nil {
			return err
		}
	}
	sh3, err := b.submit(dispatch, rec{Type: "shipment", Ref: pre + "SHP-3", SiteID: own["additive_producer"], OccurredAt: b.at(dt(26)), MassKg: batchKg * 2,
		Inputs: []input{{ab[0].ID, batchKg}, {ab[1].ID, batchKg}}, Data: map[string]any{"to_site_id": own["asphalt_plant"], "carrier": "Own fleet", "vehicle": "CP-09", "distance_km": 8}})
	if err != nil {
		return err
	}
	if err := b.receipt(intake, sh3[0].ID, dt(28), batchKg*2-5, "accepted", ""); err != nil {
		return err
	}
	// 6. asphalt lots (stage 4): additive drawn from the batches in order
	pool := []input{{ab[0].ID, batchKg}, {ab[1].ID, batchKg}}
	lotIDs := make([]string, len(p.sections))
	for i, s := range p.sections {
		var ins []input
		need := s.addKg()
		for k := range pool {
			take := math.Min(pool[k].MassKg, need)
			if take > 0 {
				ins = append(ins, input{pool[k].ID, take})
				pool[k].MassKg -= take
				need -= take
			}
		}
		tc := 150.0
		if s.process == "warm" {
			tc = 128
		}
		data := map[string]any{"mix": s.mix, "process": s.process, "binder_pct": s.binder, "additive_kg": s.addKg(), "dosage_pct_mix": s.dosage, "mixing_temp_c": tc, "gas_m3": math.Round(s.kg / 1000 * 6)}
		hours := dt(40 + 24*float64(i))
		typo := p.defect == "dosage-typo"
		if typo {
			data["dosage_pct_mix"] = s.dosage * 10 // a keying slip, corrected below
		}
		lot := rec{Type: "asphalt_lot", Ref: fmt.Sprintf("%sASL-%s", pre, s.key), SiteID: own["asphalt_plant"], OccurredAt: b.at(hours), MassKg: s.kg, Inputs: ins, Data: data, Provenance: prov, Note: note}
		res, err := b.submit(mixer, lot)
		if err != nil {
			return err
		}
		if res[0].Status == "blocked" {
			return fmt.Errorf("%s blocked: %+v", res[0].Ref, res[0].Findings)
		}
		if typo {
			lot.ID, lot.Data = res[0].ID, cloneMap(data)
			lot.Data["dosage_pct_mix"] = s.dosage
			if _, err := b.clean(mixer, lot); err != nil {
				return err
			}
		}
		lotIDs[i] = res[0].ID
		b.lots = append(b.lots, lotInfo{res[0].ID, own["asphalt_plant"], s.mix, s.kg, s.addKg(), s.binder, tc, hours})
		// laboratory tests on the mix
		tests := []rec{
			{Type: "lab_test", Ref: fmt.Sprintf("%sLAB-%s-VOIDS", pre, s.key), Data: map[string]any{"subject_id": res[0].ID, "test": "air_voids", "value": round(4.2+b.rng.Float64()*1.4, 0.1), "result": "pass", "lab": "Materials laboratory Columbia"}},
			{Type: "lab_test", Ref: fmt.Sprintf("%sLAB-%s-BIND", pre, s.key), Data: map[string]any{"subject_id": res[0].ID, "test": "binder_content", "value": s.binder, "result": "pass", "lab": "Materials laboratory Columbia"}},
			{Type: "lab_test", Ref: fmt.Sprintf("%sLAB-%s-ITSR", pre, s.key), Data: map[string]any{"subject_id": res[0].ID, "test": "itsr", "value": round(85+b.rng.Float64()*7, 0.1), "result": "pass", "lab": "Materials laboratory Columbia"}},
			{Type: "lab_test", Ref: fmt.Sprintf("%sLAB-%s-WTS", pre, s.key), Data: map[string]any{"subject_id": res[0].ID, "test": "wheel_tracking", "value": round(0.05+b.rng.Float64()*0.06, 0.01), "result": "pass", "lab": "Materials laboratory Columbia"}},
			{Type: "lab_test", Ref: fmt.Sprintf("%sLAB-%s-MARSH", pre, s.key), Data: map[string]any{"subject_id": res[0].ID, "test": "marshall_stability", "value": round(9+b.rng.Float64()*3, 0.1), "result": "pass", "lab": "Materials laboratory Columbia"}},
		}
		if p.defect == "reject" && i == 0 {
			tests[3].Data["value"], tests[3].Data["result"] = 0.19, "fail" // wheel tracking over the limit
		}
		for k := range tests {
			tests[k].SiteID, tests[k].OccurredAt = own["asphalt_plant"], b.at(hours-3)
			if p.real && s.key == "50PE" && k < 2 {
				tests[k].Provenance, tests[k].Note = "PublicReference", "Published design value, MoDOT cmr 23-009 Table 5-2."
				if k == 0 {
					tests[k].Data["value"] = 4.9
				}
			}
		}
		if _, err := b.submit(lab, tests...); err != nil {
			return err
		}
	}
	// 7. road sections opened by the paving crew (stage 5), loads delivered and signed at the paver, sections completed (stage 6)
	secStart := func(i int) float64 { return 60 + 48*float64(i) }
	secData := func(s section, extra map[string]any) map[string]any {
		d := map[string]any{"road": p.road, "km_from": 0, "km_to": s.lenKm(), "carriageway": "Both", "layer": "surface", "thickness_cm": s.thickCm, "area_m2": s.area(),
			"works": "rehabilitation", "mix": s.mix, "owner_org_id": OrgID, "region": "Missouri",
			"lat": (s.coords[0] + s.coords[2]) / 2, "lon": (s.coords[1] + s.coords[3]) / 2, "lat_from": s.coords[0], "lon_from": s.coords[1], "lat_to": s.coords[2], "lon_to": s.coords[3]}
		if s.thickCm >= 6 {
			d["layer"] = "binder"
		}
		for k, v := range extra {
			d[k] = v
		}
		return d
	}
	var secRows []rec
	for i, s := range p.sections {
		secRows = append(secRows, rec{Type: "road_section", Ref: fmt.Sprintf("%sSEC-%s", pre, s.key), SiteID: own["contractor"], OccurredAt: b.at(dt(secStart(i))), MassKg: s.kg, Data: secData(s, nil), Provenance: prov, Note: note})
	}
	secs, err := b.clean(site, secRows...)
	if err != nil {
		return err
	}
	for i, s := range p.sections {
		var rows []rec
		var recvAt []float64
		var kgs []float64
		need := s.kg
		for k := 0; need > 0; k++ {
			kg := math.Min(25000, need)
			f0 := 1 - need/s.kg
			need -= kg
			f1 := 1 - need/s.kg
			if p.open && i == len(p.sections)-1 && f0 >= p.loadsDone {
				break
			}
			hours := dt(secStart(i)) - 1 - float64(k)*0.3
			b.n++
			ticket := fmt.Sprintf("T-%s-%04d", p.code, b.n)
			arrival := 144 + b.rng.Float64()*8
			d := map[string]any{"ticket": ticket, "section_id": secs[i].ID, "vehicle": fmt.Sprintf("HT-%02d", 1+k%9), "distance_km": 12, "departure_temp_c": 158, "arrival_temp_c": round(arrival, 0.1),
				"lat_from": round(s.coords[0]+(s.coords[2]-s.coords[0])*f0, 1e-6), "lon_from": round(s.coords[1]+(s.coords[3]-s.coords[1])*f0, 1e-6),
				"lat_to": round(s.coords[0]+(s.coords[2]-s.coords[0])*f1, 1e-6), "lon_to": round(s.coords[1]+(s.coords[3]-s.coords[1])*f1, 1e-6)}
			if s.process == "warm" {
				d["departure_temp_c"], d["arrival_temp_c"] = 130, round(118+b.rng.Float64()*6, 0.1)
			}
			if p.defect == "temperature" && i == 0 && k == 3 {
				d["arrival_temp_c"] = 118.0 // cold load: warning under 130 °C
			}
			if p.defect == "reject" && k == 5 {
				d["arrival_temp_c"] = 146.0
			}
			if p.defect == "temperature" && i == 1 && k == 2 {
				delete(d, "distance_km") // recommended value missing
				delete(d, "arrival_temp_c")
			}
			rows = append(rows, rec{Type: "delivery", Ref: ticket, SiteID: own["asphalt_plant"], OccurredAt: b.at(hours), MassKg: kg, Inputs: []input{{lotIDs[i], kg}}, Data: d})
			recvAt = append(recvAt, hours-0.4)
			kgs = append(kgs, kg)
		}
		res, err := b.submit(dispatch, rows...)
		if err != nil {
			return err
		}
		laid := 0.0
		for k, r := range res {
			if r.Status == "blocked" {
				return fmt.Errorf("%s blocked: %+v", r.Ref, r.Findings)
			}
			// Open section: the three latest loads are still on the road, not yet signed at the paver.
			if p.open && i == len(p.sections)-1 && k >= len(res)-3 {
				continue
			}
			if p.defect == "reject" && k == 5 {
				if err := b.receipt(intake, r.ID, recvAt[k], 0, "rejected", "Load rejected at the paver: segregation and a cold tail."); err != nil {
					return err
				}
				continue
			}
			if err := b.receipt(intake, r.ID, recvAt[k], kgs[k], "accepted", ""); err != nil {
				return err
			}
			laid += kgs[k]
		}
		if p.open && i == len(p.sections)-1 {
			continue // still being paved: no completion
		}
		done := rec{ID: secs[i].ID, Type: "road_section", Ref: fmt.Sprintf("%sSEC-%s", pre, s.key), SiteID: own["contractor"], OccurredAt: b.at(dt(secStart(i)) - 20), MassKg: laid,
			Data: secData(s, map[string]any{"completed_at": b.at(dt(secStart(i)) - 19)}), Provenance: prov, Note: note}
		if p.real {
			done.MassKg = s.kg
		}
		if _, err := b.submit(site, done); err != nil {
			return err
		}
	}
	slog.Info("demo: project built", "project", p.code)
	return nil
}

func cloneMap(m map[string]any) map[string]any {
	o := make(map[string]any, len(m))
	for k, v := range m {
		o[k] = v
	}
	return o
}

// floating builds the unfinished work that fills every work queue: a lot waiting
// to be shipped, one waiting at intake, one to be processed, one to be tested and released.
func (b *build) floating() error {
	own := b.site
	sorter, recycler, dispatch, intake := b.A["sorter"], b.A["recycler"], b.A["dispatch"], b.A["intake"]
	mk := func(ref string, kg float64, hours float64) (string, error) {
		r, err := b.clean(sorter, rec{Type: "reject_lot", Ref: ref, SiteID: own["sorting_plant"], OccurredAt: b.at(hours), MassKg: kg,
			Data: map[string]any{"source": "municipal_packaging", "ldpe_share_pct": 80, "moisture_pct": 9, "contamination_pct": 9, "avoided_route": "landfill", "bales": math.Round(kg / 810)}})
		if err != nil {
			return "", err
		}
		return r[0].ID, nil
	}
	send := func(ref string, lot string, kg float64, hours float64) (string, error) {
		r, err := b.clean(dispatch, rec{Type: "shipment", Ref: ref, SiteID: own["sorting_plant"], OccurredAt: b.at(hours), MassKg: kg, Inputs: []input{{lot, kg}},
			Data: map[string]any{"to_site_id": own["recycler"], "carrier": "Own fleet", "vehicle": "CP-14", "distance_km": 6}})
		if err != nil {
			return "", err
		}
		return r[0].ID, nil
	}
	// waiting to be shipped
	if _, err := mk("LIVE-REJ-1", 9800, 3); err != nil {
		return err
	}
	// shipped, not yet weighed in at intake
	l4, err := mk("LIVE-REJ-4", 8600, 6)
	if err != nil {
		return err
	}
	if _, err := send("LIVE-SHP-4", l4, 8600, 2); err != nil {
		return err
	}
	// received, waiting for the recycler
	l2, err := mk("LIVE-REJ-2", 10400, 20)
	if err != nil {
		return err
	}
	s2, err := send("LIVE-SHP-2", l2, 10400, 18)
	if err != nil {
		return err
	}
	if err := b.receipt(intake, s2, 16, 10380, "accepted", ""); err != nil {
		return err
	}
	// recycled, waiting for tests and quality release
	l3, err := mk("LIVE-REJ-3", 12000, 30)
	if err != nil {
		return err
	}
	s3, err := send("LIVE-SHP-3", l3, 12000, 28)
	if err != nil {
		return err
	}
	if err := b.receipt(intake, s3, 26, 11990, "accepted", ""); err != nil {
		return err
	}
	_, err = b.clean(recycler, rec{Type: "recycled_lot", Ref: "LIVE-PEL-3", SiteID: own["recycler"], OccurredAt: b.at(10), MassKg: 7800, Inputs: []input{{l3, 12000}},
		Data: map[string]any{"form": "agglomerate", "process": "washing,shredding,agglomeration", "mfi_g10min": 1.4, "moisture_pct": 0.6, "ash_pct": 2.1, "energy_kwh": 2600}})
	return err
}

// telemetry pushes the extruder line log and the mixer log of every asphalt lot.
func (b *build) telemetry() error {
	admin := b.A["admin"]
	type reading struct {
		TS            string  `json:"ts"`
		SiteID        string  `json:"siteId"`
		Line          string  `json:"line"`
		MeltC         float64 `json:"meltC"`
		PressureBar   float64 `json:"pressureBar"`
		ThroughputKgH float64 `json:"throughputKgH"`
		RPM           float64 `json:"rpm"`
		LoadPct       float64 `json:"loadPct"`
	}
	var rs []reading
	for h := 72.0; h >= 0.1; h -= 5.0 / 60 {
		stopped := h > 30 && h < 30.7 // a short maintenance stop
		x := reading{TS: b.at(h), SiteID: b.site["recycler"], Line: "L1", MeltC: 224 + b.rng.NormFloat64()*3, PressureBar: 68 + b.rng.NormFloat64()*2.5,
			ThroughputKgH: 840 + b.rng.NormFloat64()*25, RPM: 100 + b.rng.NormFloat64()*1.5, LoadPct: 77 + b.rng.NormFloat64()*3}
		if stopped {
			x.ThroughputKgH, x.RPM, x.LoadPct = 0, 0, 0
		}
		rs = append(rs, x)
	}
	if err := b.m.call(b.ctx, admin, "POST", "/api/v1/telemetry", map[string]any{"readings": rs}, nil); err != nil {
		return err
	}
	type cycle struct {
		TS         string  `json:"ts"`
		SiteID     string  `json:"siteId"`
		LotID      string  `json:"lotId"`
		Mix        string  `json:"mix"`
		Cycle      int     `json:"cycle"`
		BatchKg    float64 `json:"batchKg"`
		AdditiveKg float64 `json:"additiveKg"`
		BinderKg   float64 `json:"binderKg"`
		TempC      float64 `json:"tempC"`
	}
	var cs []cycle
	flush := func() error {
		if len(cs) == 0 {
			return nil
		}
		err := b.m.call(b.ctx, admin, "POST", "/api/v1/telemetry", map[string]any{"cycles": cs}, nil)
		cs = cs[:0]
		return err
	}
	for _, l := range b.lots {
		const batch = 3000.0
		n := int(math.Ceil(l.kg / batch))
		for i := 0; i < n; i++ {
			kg := math.Min(batch, l.kg-float64(i)*batch)
			ts := b.now.Add(-time.Duration(l.hoursAgo * float64(time.Hour))).Add(-time.Duration(n-i) * 3 * time.Minute)
			cs = append(cs, cycle{TS: ts.Format(time.RFC3339), SiteID: l.site, LotID: l.id, Mix: l.mix, Cycle: i + 1, BatchKg: kg,
				AdditiveKg: round(kg*l.additiveKg/l.kg, 0.01), BinderKg: round(kg*l.binder/100, 0.1), TempC: round(l.tc+b.rng.NormFloat64()*2, 0.1)})
			if len(cs) >= 2000 {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	return flush()
}

// plans fills the calendar with upcoming and overdue work.
func (b *build) plans() {
	own := b.site
	in := func(h float64) string { return b.now.Add(time.Duration(h * float64(time.Hour))).Format(time.RFC3339) }
	plan := func(who string, p map[string]any) {
		if err := b.m.call(b.ctx, b.A[who], "POST", "/api/v1/plans", p, nil); err != nil {
			slog.Warn("demo: plan skipped", "title", p["title"], "err", err)
		}
	}
	plan("dispatch", map[string]any{"recordType": "shipment", "title": "Pellets to additive handling", "siteId": own["recycler"], "toSiteId": own["additive_producer"], "startsAt": in(20), "massKg": 3000, "note": "Second pellet shipment for Scott Blvd."})
	plan("mixer", map[string]any{"recordType": "asphalt_lot", "title": "Scott Blvd remaining mix", "siteId": own["asphalt_plant"], "startsAt": in(16), "endsAt": in(24), "massKg": 160000, "note": "0.5 % additive, SP125C."})
	plan("site", map[string]any{"recordType": "road_section", "title": "Finish Scott Blvd paving", "siteId": own["contractor"], "ownerOrgId": OrgID, "startsAt": in(28), "endsAt": in(40), "massKg": 170000})
	plan("sorter", map[string]any{"recordType": "reject_lot", "title": "Collect film bales from the county drop-off", "siteId": own["sorting_plant"], "startsAt": in(-20), "massKg": 11000, "note": "Postponed: truck in service."})
	plan("recycler", map[string]any{"recordType": "recycled_lot", "title": "Pellet run for Route 740 maintenance", "siteId": own["recycler"], "startsAt": in(72), "endsAt": in(80), "massKg": 9000})
	plan("dispatch", map[string]any{"recordType": "delivery", "title": "Asphalt loads to Scott Blvd", "siteId": own["asphalt_plant"], "ownerOrgId": OrgID, "startsAt": in(30), "endsAt": in(38), "massKg": 170000})
}

// extras sets up what the other dashboard pages show: alert rules, compliance
// limits and statements, calibration notes, a signed report, work assignments
// and a shared passport. None of it is essential, so a failure is logged only.
func (b *build) extras(sections map[string]string) {
	warn := func(what string, err error) {
		if err != nil {
			slog.Warn("demo: skipped "+what, "err", err)
		}
	}
	admin, quality, site := b.A["admin"], b.A["quality"], b.A["site"]
	warn("setup guide", b.m.call(b.ctx, admin, "PUT", "/api/v1/org/setup", map[string]any{"dismissed": true}, nil))
	warn("alert rule", b.m.call(b.ctx, admin, "POST", "/api/v1/alerts/rules", map[string]any{
		"name": "New recycled pellet lot", "kind": "metric", "severity": "info", "audience": []string{"role:admin"}, "channelIds": []string{}, "forMinutes": 0,
		"repeatMinutes": 0, "notifyResolved": false, "escalation": []any{}, "enabled": true,
		"params": map[string]any{"query": map[string]any{"source": "records", "types": []string{"recycled_lot"}, "measure": "records", "agg": "count"}, "windowMinutes": 1440, "op": ">=", "value": 1}}, nil))
	warn("compliance limit", b.m.call(b.ctx, quality, "POST", "/api/v1/compliance/limits", map[string]any{
		"name": "Additive dosage window", "source": "records", "measure": "dosage", "types": []string{"asphalt_lot"}, "basis": "instant", "min": 0.4, "max": 0.6,
		"note": "Mix-design target; the 25PE section was dosed lower by design.", "enabled": true}, nil))
	for _, c := range []map[string]any{
		{"parameter": "Plant weighbridge", "kind": "calibration", "at": b.at(24 * 20), "by": "Show-Me Scales Inc. (demo)", "note": "Certified, ±20 kg."},
		{"parameter": "Mixer additive doser", "kind": "check", "at": b.at(24 * 9), "by": "Taylor Brooks", "note": "Dosed 10 test batches; mean error 0.8 %."},
		{"parameter": "Extruder melt thermocouple", "kind": "replacement", "at": b.at(24 * 40), "by": "Riley Chen", "note": "Replaced after drift of 6 °C."},
	} {
		warn("calibration", b.m.call(b.ctx, quality, "POST", "/api/v1/compliance/calibrations", c, nil))
	}
	var reg struct {
		Register struct {
			Items []struct {
				ID     string `json:"id"`
				Attest bool   `json:"attest"`
				Title  string `json:"title"`
			} `json:"items"`
		} `json:"register"`
	}
	if err := b.m.call(b.ctx, admin, "GET", "/api/v1/compliance/register", nil, &reg); err != nil {
		warn("register", err)
	} else {
		done := 0
		for _, it := range reg.Register.Items {
			if !it.Attest || done >= 4 {
				continue
			}
			status, note := "met", "Demo statement: "+strings.ToLower(it.Title)+"; evidence is filed with the quality manager."
			if done == 3 {
				status, note = "partial", "Demo statement: in progress; the remaining sites are being brought in line."
			}
			warn("statement", b.m.call(b.ctx, admin, "POST", "/api/v1/compliance/attestations", map[string]any{"requirementId": it.ID, "status": status, "note": note}, nil))
			done++
		}
	}
	// the previous month's report, signed
	period := b.now.AddDate(0, -1, 0).Format("2006-01")
	var st struct {
		CanSign     bool   `json:"canSign"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := b.m.call(b.ctx, admin, "GET", "/api/v1/compliance/report?period="+period, nil, &st); err == nil && st.CanSign {
		warn("monthly report", b.m.call(b.ctx, admin, "POST", "/api/v1/compliance/report/sign", map[string]any{"period": period, "note": "Demo sign-off", "fingerprint": st.Fingerprint}, nil))
	}
	// resolve one warning the way a site manager would
	var fl struct {
		Items []struct {
			ID       int64  `json:"id"`
			Code     string `json:"code"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"items"`
	}
	if err := b.m.call(b.ctx, admin, "GET", "/api/v1/findings?status=open&limit=200", nil, &fl); err == nil {
		for _, f := range fl.Items {
			if f.Code == "C6" && f.Severity == "warning" && strings.Contains(strings.ToLower(f.Message), "temperature") {
				warn("finding", b.m.call(b.ctx, site, "POST", fmt.Sprintf("/api/v1/findings/%d/resolve", f.ID),
					map[string]any{"status": "resolved", "reason": "Cold tail of the load; compaction checked with the core density test."}, nil))
				break
			}
		}
	}
	// work handed to a member
	if id := b.liveID("LIVE-PEL-3"); id != "" {
		warn("assignment", b.m.call(b.ctx, admin, "POST", "/api/v1/work/assign", map[string]any{"recordId": id, "duty": "test", "userId": b.A["lab"].userID, "note": "Please run MFI and moisture before the end of the shift."}, nil))
	}
	// a passport anyone can open by link (Route 740, 50PE)
	if id := sections["R740-SEC-50PE"]; id != "" {
		warn("passport share", b.m.call(b.ctx, admin, "POST", "/api/v1/passports/"+id+"/share", nil, nil))
	}
}

func (b *build) liveID(ref string) string {
	var id string
	_ = b.m.St.DB.QueryRow(b.ctx, `SELECT id FROM records WHERE org_id=$1 AND ref=$2 AND latest`, OrgID, ref).Scan(&id)
	return id
}

// populate builds the whole scenario.
func (m *Manager) populate(ctx context.Context) error {
	acts, err := m.sessions(ctx)
	if err != nil {
		return err
	}
	b := &build{m: m, ctx: ctx, now: time.Now().UTC().Truncate(time.Minute), A: acts, site: map[string]string{
		"sorting_plant": "site_capdemo_sorting", "recycler": "site_capdemo_recycler", "additive_producer": "site_capdemo_additive",
		"asphalt_plant": "site_capdemo_asphalt", "contractor": "site_capdemo_paving", "road_owner": "site_capdemo_assets", "lab": "site_capdemo_lab"},
		rng: rand.New(rand.NewPCG(uint64(time.Now().YearDay()), 20261020))}
	for _, p := range projects {
		if err := b.project(p); err != nil {
			return fmt.Errorf("project %s: %w", p.code, err)
		}
	}
	if err := b.floating(); err != nil {
		return fmt.Errorf("work in progress: %w", err)
	}
	if err := b.telemetry(); err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	b.plans()
	secIDs := map[string]string{}
	rows, err := m.St.DB.Query(ctx, `SELECT ref, id FROM records WHERE org_id=$1 AND type='road_section' AND latest`, OrgID)
	if err == nil {
		for rows.Next() {
			var ref, id string
			if rows.Scan(&ref, &id) == nil {
				secIDs[ref] = id
			}
		}
		rows.Close()
	}
	b.extras(secIDs)
	return nil
}
