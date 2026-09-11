package lca

import (
	"math"
	"testing"

	"github.com/sensifai/roadpass/internal/domain"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6*math.Max(1, math.Abs(b)) }

// A 1 t reject lot becomes 700 kg recycled plastic, then a 760 kg additive batch
// (700 kg recycled + 60 kg carrier), of which 500 kg goes into a 100 t asphalt lot.
func TestFlowConservesRecycledMass(t *testing.T) {
	nodes := []domain.Record{
		{ID: "PRL", Type: domain.TypeRejectLot, MassKg: 1000, Body: map[string]any{"avoided_route": "incineration"}},
		{ID: "RPL", Type: domain.TypeRecycledLot, MassKg: 700, Body: map[string]any{"energy_kwh": 300.0}},
		{ID: "ADB", Type: domain.TypeAdditiveBatch, MassKg: 760, Body: map[string]any{}},
		{ID: "ASL", Type: domain.TypeAsphaltLot, MassKg: 100000, Body: map[string]any{}},
	}
	links := []domain.Link{
		{Parent: "PRL", Child: "RPL", Kind: "input", MassKg: 1000},
		{Parent: "RPL", Child: "ADB", Kind: "input", MassKg: 700},
		{Parent: "ADB", Child: "ASL", Kind: "input", MassKg: 500},
	}
	SetOrgCountries(map[string]string{"es": "ES"})
	for i := range nodes {
		nodes[i].OrgID = "es"
	}
	sc := DefaultScenario
	res := Summarise(&nodes[3], Flow(nodes, links, nil, sc), sc)
	share := 500.0 / 760
	if !near(res.RecycledKg, 700*share) {
		t.Fatalf("recycled in asphalt %v, want %v", res.RecycledKg, 700*share)
	}
	if !near(res.RejectKg, 1000*share) {
		t.Fatalf("reject film taken in %v, want %v", res.RejectKg, 1000*share)
	}
	// 70 % yield: disposal is avoided for the 700 kg recycled; the 300 kg screened out were disposed.
	if !near(res.DisposalKg, 700*share*2.3) {
		t.Fatalf("avoided disposal %v, want %v", res.DisposalKg, 700*share*2.3)
	}
	if !near(res.ChainFootKg, 300*0.154*share) {
		t.Fatalf("chain footprint %v, want %v", res.ChainFootKg, 300*0.154*share)
	}
	if res.FootprintKg <= res.ChainFootKg {
		t.Fatal("asphalt footprint missing from total")
	}
	cons := Scenarios[2]
	low := Summarise(&nodes[3], Flow(nodes, links, nil, cons), cons)
	if low.AvoidedKg >= res.AvoidedKg {
		t.Fatal("conservative scenario must avoid less")
	}
}

// A load the contractor rejected at the paver was not laid: the section gets
// nothing from it, while the delivery's own figures stay as they were.
func TestFlowSkipsRejectedDelivery(t *testing.T) {
	nodes := []domain.Record{
		{ID: "PRL", Type: domain.TypeRejectLot, MassKg: 1000, Body: map[string]any{"avoided_route": "landfill"}},
		{ID: "RPL", Type: domain.TypeRecycledLot, MassKg: 1000, Body: map[string]any{}},
		{ID: "ADB", Type: domain.TypeAdditiveBatch, MassKg: 1000, Body: map[string]any{}},
		{ID: "ASL", Type: domain.TypeAsphaltLot, MassKg: 50000, Body: map[string]any{}},
		{ID: "DLV1", Type: domain.TypeDelivery, MassKg: 25000, Body: map[string]any{}},
		{ID: "DLV2", Type: domain.TypeDelivery, MassKg: 25000, Body: map[string]any{},
			Receipt: &domain.Receipt{Decision: domain.Rejected, Note: "118 °C at the paver"}},
		{ID: "RSC", Type: domain.TypeRoadSection, MassKg: 50000, Body: map[string]any{}},
	}
	links := []domain.Link{
		{Parent: "PRL", Child: "RPL", Kind: "input", MassKg: 1000},
		{Parent: "RPL", Child: "ADB", Kind: "input", MassKg: 1000},
		{Parent: "ADB", Child: "ASL", Kind: "input", MassKg: 1000},
		{Parent: "ASL", Child: "DLV1", Kind: "delivered", MassKg: 25000},
		{Parent: "ASL", Child: "DLV2", Kind: "delivered", MassKg: 25000},
		{Parent: "DLV1", Child: "RSC", Kind: "laid", MassKg: 25000},
		{Parent: "DLV2", Child: "RSC", Kind: "laid", MassKg: 25000},
	}
	sc := DefaultScenario
	in := Flow(nodes, links, nil, sc)
	sec := Summarise(&nodes[6], in, sc)
	if !near(sec.RecycledKg, 500) {
		t.Fatalf("section counts %v kg recycled, want 500 (the accepted load only)", sec.RecycledKg)
	}
	if dlv := Summarise(&nodes[5], in, sc); !near(dlv.RecycledKg, 500) {
		t.Fatalf("rejected delivery's own passport shows %v kg, want 500", dlv.RecycledKg)
	}
	nodes[5].Receipt.Decision = domain.Accepted
	if all := Summarise(&nodes[6], Flow(nodes, links, nil, sc), sc); !near(all.RecycledKg, 1000) {
		t.Fatalf("both loads accepted: %v kg, want 1000", all.RecycledKg)
	}
}

// An additive batch counts no more recycled plastic than it declares: 14 t of
// recycled input in a 14 t batch declared 92 % recycled carries 12.88 t.
func TestAdditiveRecycledCappedByDeclaration(t *testing.T) {
	nodes := []domain.Record{
		{ID: "RPL", Type: domain.TypeRecycledLot, MassKg: 14000, Body: map[string]any{}},
		{ID: "ADB", Type: domain.TypeAdditiveBatch, MassKg: 14000, Body: map[string]any{"recycled_content_pct": 92.0}},
	}
	links := []domain.Link{{Parent: "RPL", Child: "ADB", Kind: "input", MassKg: 14000}}
	sc := DefaultScenario
	res := Summarise(&nodes[1], Flow(nodes, links, nil, sc), sc)
	if !near(res.RecycledKg, 12880) {
		t.Fatalf("recycled %v, want 12880 (the declared 92 %%)", res.RecycledKg)
	}
}
