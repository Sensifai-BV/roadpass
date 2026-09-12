package passport

import (
	"math"
	"testing"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/lca"
	"github.com/sensifai/roadpass/internal/store"
)

// The origins a passport shows must add up to the reject film the carbon model
// counts: both allocate by mass along the same links.
func TestSharesAgreeWithCarbonModel(t *testing.T) {
	g := &store.Graph{
		Nodes: []domain.Record{
			{ID: "PRL1", Type: domain.TypeRejectLot, MassKg: 20000, Body: map[string]any{"avoided_route": "landfill"}},
			{ID: "PRL2", Type: domain.TypeRejectLot, MassKg: 18000, Body: map[string]any{"avoided_route": "incineration"}},
			{ID: "RPL", Type: domain.TypeRecycledLot, MassKg: 22000, Body: map[string]any{}},
			{ID: "ADB", Type: domain.TypeAdditiveBatch, MassKg: 10000, Body: map[string]any{}},
			{ID: "ASL", Type: domain.TypeAsphaltLot, MassKg: 200000, Body: map[string]any{}},
			{ID: "DLV", Type: domain.TypeDelivery, MassKg: 25000, Body: map[string]any{}},
			{ID: "RSC", Type: domain.TypeRoadSection, MassKg: 25000, Body: map[string]any{}},
		},
		Links: []domain.Link{
			{Parent: "PRL1", Child: "RPL", Kind: "input", MassKg: 20000},
			{Parent: "PRL2", Child: "RPL", Kind: "input", MassKg: 14000},
			{Parent: "RPL", Child: "ADB", Kind: "input", MassKg: 9300},
			{Parent: "ADB", Child: "ASL", Kind: "input", MassKg: 1000},
			{Parent: "ASL", Child: "DLV", Kind: "delivered", MassKg: 25000},
			{Parent: "DLV", Child: "RSC", Kind: "laid", MassKg: 25000},
		},
	}
	share := shareOf("RSC", g)
	var reject float64
	for _, n := range g.Nodes {
		if n.Type == domain.TypeRejectLot {
			reject += share[n.ID] * n.MassKg
		}
	}
	res := lca.Summarise(&g.Nodes[6], lca.Flow(g.Nodes, g.Links, nil, lca.DefaultScenario), lca.DefaultScenario)
	if math.Abs(reject-res.RejectKg) > 1e-6 {
		t.Fatalf("origins add up to %.3f kg, carbon model counts %.3f kg", reject, res.RejectKg)
	}
	if got := share["ADB"] * 10000; math.Abs(got-125) > 1e-9 { // 1000 kg × 25/200 t
		t.Fatalf("additive contribution %.3f kg, want 125", got)
	}
}

// A section passport leaves out a load rejected at the paver: no lineage, no
// share, no origins and no carbon from it, but the rejection stays visible.
func TestRejectedDeliveryLeavesSectionLineage(t *testing.T) {
	g := &store.Graph{
		Nodes: []domain.Record{
			{ID: "PRL", Type: domain.TypeRejectLot, MassKg: 1000, Body: map[string]any{"avoided_route": "landfill"}},
			{ID: "RPL", Type: domain.TypeRecycledLot, MassKg: 1000, Body: map[string]any{}},
			{ID: "ADB", Type: domain.TypeAdditiveBatch, MassKg: 1000, Body: map[string]any{}},
			{ID: "ASL1", Type: domain.TypeAsphaltLot, MassKg: 25000, Body: map[string]any{}},
			{ID: "ASL2", Type: domain.TypeAsphaltLot, MassKg: 25000, Body: map[string]any{}},
			{ID: "DLV1", Type: domain.TypeDelivery, MassKg: 25000, Body: map[string]any{}},
			{ID: "DLV2", Type: domain.TypeDelivery, MassKg: 25000, Body: map[string]any{},
				Receipt: &domain.Receipt{Decision: domain.Rejected, Note: "118 °C"}},
			{ID: "RSC", Type: domain.TypeRoadSection, MassKg: 25000, Body: map[string]any{}},
		},
		Links: []domain.Link{
			{Parent: "PRL", Child: "RPL", Kind: "input", MassKg: 1000},
			{Parent: "RPL", Child: "ADB", Kind: "input", MassKg: 1000},
			{Parent: "ADB", Child: "ASL1", Kind: "input", MassKg: 500},
			{Parent: "ADB", Child: "ASL2", Kind: "input", MassKg: 500},
			{Parent: "ASL1", Child: "DLV1", Kind: "delivered", MassKg: 25000},
			{Parent: "ASL2", Child: "DLV2", Kind: "delivered", MassKg: 25000},
			{Parent: "DLV1", Child: "RSC", Kind: "laid", MassKg: 25000},
			{Parent: "DLV2", Child: "RSC", Kind: "laid", MassKg: 25000},
		},
	}
	pg, dropped := counted(g, "RSC", true)
	ids := map[string]bool{}
	for _, n := range pg.Nodes {
		ids[n.ID] = true
	}
	if ids["DLV2"] || ids["ASL2"] || !ids["DLV1"] || !ids["PRL"] {
		t.Fatalf("lineage kept %v", ids)
	}
	if len(dropped) != 1 || dropped[0].ID != "DLV2" {
		t.Fatalf("dropped %v, want the rejected delivery", dropped)
	}
	for _, l := range pg.Links {
		if l.Parent == "DLV2" || l.Child == "DLV2" {
			t.Fatalf("link %v kept", l)
		}
	}
	share := shareOf("RSC", pg)
	if got := share["PRL"] * 1000; math.Abs(got-500) > 1e-9 {
		t.Fatalf("reject film from the accepted load %.3f kg, want 500", got)
	}
	res := lca.Summarise(&pg.Nodes[len(pg.Nodes)-1], lca.Flow(pg.Nodes, pg.Links, nil, lca.DefaultScenario), lca.DefaultScenario)
	if math.Abs(res.RecycledKg-500) > 1e-6 || math.Abs(res.RejectKg-500) > 1e-6 {
		t.Fatalf("section carbon counts %.3f kg recycled / %.3f kg reject, want 500 / 500", res.RecycledKg, res.RejectKg)
	}
	// The rejected delivery's own passport still shows where it came from, and
	// downstream it reaches no section.
	if own, _ := counted(g, "DLV2", true); len(own.Nodes) != 5 { // DLV2, ASL2, ADB, RPL, PRL
		t.Fatalf("the rejected delivery's own lineage has %d records, want 5", len(own.Nodes))
	}
	down := &store.Graph{Nodes: []domain.Record{g.Nodes[4], g.Nodes[6], g.Nodes[7]},
		Links: []domain.Link{g.Links[5], g.Links[7]}}
	if d, _ := counted(down, "ASL2", false); len(d.Nodes) != 2 || len(d.Links) != 1 {
		t.Fatalf("downstream of ASL2: %d nodes, %d links; want the delivery but not the section", len(d.Nodes), len(d.Links))
	}
}
