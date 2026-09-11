package lca

import (
	"github.com/sensifai/roadpass/internal/domain"
)

// Intensity is what one kilogram of a record carries from upstream.
type Intensity struct {
	RecycledKg  float64 // recycled plastic
	RejectKg    float64 // reject film diverted from disposal
	AvoidedKg   float64 // disposal emissions avoided (kg CO₂e)
	FootprintKg float64 // process + transport emissions so far (kg CO₂e)
	ChainKg     float64 // the part of FootprintKg caused by the plastic chain (sorting → additive)
	Tkm         float64 // transport work so far (t·km)
}

func (a Intensity) scale(k float64) Intensity {
	return Intensity{a.RecycledKg * k, a.RejectKg * k, a.AvoidedKg * k, a.FootprintKg * k, a.ChainKg * k, a.Tkm * k}
}

func (a Intensity) add(b Intensity) Intensity {
	return Intensity{a.RecycledKg + b.RecycledKg, a.RejectKg + b.RejectKg, a.AvoidedKg + b.AvoidedKg,
		a.FootprintKg + b.FootprintKg, a.ChainKg + b.ChainKg, a.Tkm + b.Tkm}
}

// inChain reports whether a record type belongs to the plastic chain.
func inChain(t domain.RecordType) bool {
	return t == domain.TypeRejectLot || t == domain.TypeRecycledLot || t == domain.TypeAdditiveBatch
}

// Result is the passport figure set for one record.
type Result struct {
	Scenario     string  `json:"scenario"`
	MassKg       float64 `json:"massKg"`
	RecycledKg   float64 `json:"recycledKg"`
	RejectKg     float64 `json:"rejectKg"`
	DisposalKg   float64 `json:"avoidedDisposalKg"`  // kg CO₂e avoided by not landfilling/burning
	VirginKg     float64 `json:"avoidedVirginKg"`    // kg CO₂e avoided by not making virgin polymer
	AvoidedKg    float64 `json:"avoidedKg"`          // total avoided
	FootprintKg  float64 `json:"footprintKg"`        // process + transport of the recycled-plastic chain and the asphalt
	TransportTkm float64 `json:"transportTkm"`       //
	RecycledPerT float64 `json:"recycledKgPerTonne"` // kg recycled plastic per tonne of this record
	NetBenefitKg float64 `json:"netBenefitKg"`       // avoided − footprint of the plastic chain only
	ChainFootKg  float64 `json:"chainFootprintKg"`   // footprint of the plastic chain (sorting→additive), excl. asphalt
}

// Flow computes intensities over a lineage graph by mass allocation: a record's
// intensity is the mass-weighted sum of its inputs' intensities (by link mass),
// plus its own process footprint, divided by its mass. Reject lots enter
// burden-free (cut-off approach for waste). shipFoot gives the shipment footprint
// and t·km allocated to each record, keyed by record id.
// Weighted is an input's intensity and the mass taken from it.
type Weighted struct {
	Intensity Intensity
	MassKg    float64
}

// Node computes the per-kg intensity of r from its weighted inputs and the
// transport footprint of the shipments that carried it (ship). It is the single
// definition used by Flow (passport pages) and by the analytics resolver, so a
// chart and a passport can never disagree.
func Node(r *domain.Record, inputs []Weighted, ship Intensity, sc Scenario) Intensity {
	var tot Intensity
	mass := r.MassKg
	switch r.Type {
	case domain.TypeRejectLot:
		route := domain.Str(r.Body, "avoided_route")
		if sc.DisposalOverride != "" {
			route = sc.DisposalOverride
		}
		// The recycler consumes the lot by mass (link mass); what it screens out
		// shows up as a recycling yield below 100 %, not as avoided disposal (the
		// recycled lot scales the credit by its yield). On the reject lot's own
		// passport the figure is therefore a potential, until it is recycled.
		tot = Intensity{RejectKg: mass, AvoidedKg: mass * DisposalFactor(route, countryOf(r.OrgID))}
	default:
		var in float64
		for _, w := range inputs {
			tot = tot.add(w.Intensity.scale(w.MassKg))
			in += w.MassKg
		}
		if r.Type == domain.TypeRecycledLot {
			tot.RecycledKg = mass // everything that leaves a recycler is recycled plastic
			// Only the film that came out as recycled plastic was diverted from
			// disposal; what the recycler screens out (washing losses, labels,
			// other polymers) is itself disposed. Credit avoided disposal by yield,
			// as EN 15804 module D and the PEF Circular Footprint Formula do.
			// RejectKg stays the film taken in (the passport's origins add up to it).
			if in > 0 && mass < in {
				tot.AvoidedKg *= mass / in
			}
		}
		if r.Type == domain.TypeAdditiveBatch {
			// The signed declaration caps the recycled plastic an additive carries:
			// inputs above it are a material-balance error (check C5), and a
			// passport never claims more than the product declares.
			if rc := domain.Num(r.Body, "recycled_content_pct"); rc > 0 && tot.RecycledKg > mass*rc/100 {
				tot.RecycledKg = mass * rc / 100
			}
		}
		if r.Type == domain.TypeRoadSection && mass <= 0 {
			mass = in
		}
		own := RecordFootprint(r, sc)
		tot.FootprintKg += own
		if inChain(r.Type) {
			tot.ChainKg += own
		}
		tot.Tkm += Tkm(r)
	}
	if inChain(r.Type) {
		ship.ChainKg = ship.FootprintKg
	} else {
		ship.ChainKg = 0
	}
	tot = tot.add(ship)
	if mass <= 0 {
		return Intensity{}
	}
	return tot.scale(1 / mass)
}

// Flow computes intensities over a lineage graph by mass allocation: a record's
// intensity is the mass-weighted sum of its inputs' intensities (by link mass),
// plus its own process footprint, divided by its mass. Reject lots enter
// burden-free (cut-off approach for waste). shipFoot gives the shipment footprint
// and t·km allocated to each record, keyed by record id.
func Flow(nodes []domain.Record, links []domain.Link, shipFoot map[string]Intensity, sc Scenario) map[string]Intensity {
	byID := make(map[string]*domain.Record, len(nodes))
	for i := range nodes {
		byID[nodes[i].ID] = &nodes[i]
	}
	parents := map[string][]domain.Link{}
	for _, l := range links {
		parents[l.Child] = append(parents[l.Child], l)
	}
	memo := map[string]Intensity{}
	visiting := map[string]bool{}
	var visit func(id string) Intensity
	visit = func(id string) Intensity {
		if v, ok := memo[id]; ok {
			return v
		}
		r := byID[id]
		if r == nil || visiting[id] {
			return Intensity{}
		}
		visiting[id] = true
		defer delete(visiting, id)
		var ins []Weighted
		for _, l := range parents[id] {
			// A void, or a load rejected at the paver, gives nothing to its child.
			if p := byID[l.Parent]; p != nil && p.MassKg > 0 && p.CountsMass() {
				ins = append(ins, Weighted{visit(l.Parent), l.MassKg})
			}
		}
		out := Node(r, ins, shipFoot[id], sc)
		memo[id] = out
		return out
	}
	for id := range byID {
		visit(id)
	}
	return memo
}

// ShipmentShare is the footprint of shipment s allocated to one carried item of
// itemKg: transport scales with the item's share of the load.
func ShipmentShare(s *domain.Record, itemKg float64, sc Scenario) Intensity {
	if s.MassKg <= 0 {
		return Intensity{}
	}
	f := itemKg / s.MassKg
	return Intensity{FootprintKg: RecordFootprint(s, sc) * f, Tkm: Tkm(s) * f}
}

// Add exposes Intensity addition to other packages.
func (a Intensity) Add(b Intensity) Intensity { return a.add(b) }

// Summarise turns the intensity of root into passport figures.
func Summarise(root *domain.Record, in map[string]Intensity, sc Scenario) Result {
	m := root.MassKg
	it := in[root.ID]
	res := Result{Scenario: sc.Key, MassKg: m, RecycledKg: it.RecycledKg * m, RejectKg: it.RejectKg * m,
		DisposalKg: it.AvoidedKg * m, FootprintKg: it.FootprintKg * m, TransportTkm: it.Tkm * m}
	if sc.SubstituteVirgin {
		res.VirginKg = res.RecycledKg * Factors["virgin_ldpe"].Value
	}
	res.AvoidedKg = res.DisposalKg + res.VirginKg
	if m > 0 {
		res.RecycledPerT = res.RecycledKg / (m / 1000)
	}
	res.ChainFootKg = it.ChainKg * m
	res.NetBenefitKg = res.AvoidedKg - res.ChainFootKg
	return res
}
