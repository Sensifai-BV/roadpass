package analytics

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
)

// The ClickHouse row of a delivery rejected at the paver carries no asphalt and no
// plastic, so the impact KPIs (asphalt with additive, recycled plastic laid, CO₂e
// avoided) and the map's flows, which sum delivery rows, leave it out. The
// resolver is never asked: a rejected load gets no figures however it was made.
func TestRejectedDeliveryProjectsNoMass(t *testing.T) {
	p := &Projector{orgs: map[string]domain.Org{"org_plant": {ID: "org_plant", Kind: domain.OrgKind("asphalt_plant")}},
		sites: map[string]domain.Site{}, owners: map[string]string{"RSC-1": "org_owner"}}
	r := &domain.Record{Seq: 7, ID: "DLV-1", Version: 1, Type: domain.TypeDelivery, OrgID: "org_plant", Counterparty: "org_contractor",
		OccurredAt: time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), MassKg: 25000, Provenance: domain.ProvOperational,
		Body:    map[string]any{"section_id": "RSC-1", "arrival_temp_c": 118.0},
		Receipt: &domain.Receipt{Decision: domain.Rejected, Note: "118 °C at the paver"}}
	row, err := p.fact(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	// mass, recycled, reject, avoided, virgin, footprint, chain, t·km
	for i := 13; i <= 20; i++ {
		if v, _ := row[i].(float64); v != 0 {
			t.Fatalf("column %d = %v, want 0 for a rejected load", i, v)
		}
	}
	if row[10] != "RSC-1" || row[11] != "org_owner" {
		t.Fatalf("section/owner %v/%v: the row must still say where it was meant to go", row[10], row[11])
	}
}

// A record with an open blocker is projected with its status, and every dashboard
// read goes through `final`, which leaves blocked rows out: a blocked lot
// must not appear in the funnel or the KPIs until it is corrected or accepted.
func TestBlockedRecordsAreLeftOutOfEveryRead(t *testing.T) {
	p := &Projector{orgs: map[string]domain.Org{"org_rec": {ID: "org_rec", Kind: domain.KindRecycler}},
		sites: map[string]domain.Site{}, owners: map[string]string{}}
	r := &domain.Record{Seq: 9, ID: "LAB-9", Version: 1, Type: domain.TypeLabTest, OrgID: "org_rec", MassKg: 0, Status: domain.StatusBlocked,
		OccurredAt: time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), Provenance: domain.ProvOperational, Body: map[string]any{"subject_id": "RPL-9", "test": "mfi", "value": 9.2, "result": "fail"}}
	row, err := p.fact(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if got := row[len(row)-1]; got != "blocked" {
		t.Fatalf("projected status = %v, want blocked", got)
	}
	if !strings.Contains(final, "status != 'blocked'") {
		t.Fatalf("final must exclude blocked rows: %s", final)
	}
	sql, _ := build(t, Query{Source: "records", Measure: "mass_kg", Agg: "sum"}, Scope{})
	if !strings.Contains(sql, "status != 'blocked'") {
		t.Fatalf("the explorer must read through final: %s", sql)
	}
}
