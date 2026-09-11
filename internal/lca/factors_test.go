package lca

import (
	"strings"
	"testing"

	"github.com/sensifai/roadpass/internal/domain"
)

func TestEveryFactorNamesItsSource(t *testing.T) {
	check := func(f Factor, where string) {
		if f.Source == "" || f.Unit == "" || f.Year == 0 || f.Geo == "" || f.Value <= 0 {
			t.Errorf("%s: incomplete factor %+v", where, f)
		}
	}
	for k, f := range Factors {
		check(f, "fallback "+k)
		if f.Key != k {
			t.Errorf("fallback %s carries key %s", k, f.Key)
		}
	}
	for k, m := range Regional {
		if _, ok := Factors[k]; !ok {
			t.Errorf("national values for %s have no fallback", k)
		}
		for c, f := range m {
			check(f, k+" "+c)
			if f.Geo != c || f.Key != k {
				t.Errorf("%s %s labelled %s/%s", k, c, f.Key, f.Geo)
			}
		}
	}
	// A fallback taken from one country must say it is a fallback.
	for _, k := range []string{"landfill", "truck", "hma_plant", "gas_kwh_per_m3"} {
		if f := Factors[k]; !strings.HasPrefix(f.Geo, "fallback") || !strings.Contains(f.Source, "no national value") {
			t.Errorf("fallback %s does not say so: %q / %q", k, f.Geo, f.Source)
		}
	}
}

func TestFactorsFollowTheCountry(t *testing.T) {
	if In("natural_gas", "US").Geo != "US" || In("natural_gas", "NL") != Factors["natural_gas"] {
		t.Error("natural gas lookup")
	}
	if DisposalFactor("landfill", "US") != 0.0220 || DisposalFactor("landfill", "BR") != Factors["landfill"].Value {
		t.Error("landfill lookup")
	}
	if DisposalFactor("incineration", "US") != DisposalFactor("incineration", "ES") {
		t.Error("incineration is chemistry: the same everywhere")
	}
	// Residual mix: the higher of AIB and the location-based factor; location-based where none is published.
	if ResidualFactor("NL") != 0.382 {
		t.Errorf("NL residual %v", ResidualFactor("NL"))
	}
	if ResidualFactor("FR") != GridFactor("FR") { // AIB 0.024 is below the life-cycle grid 0.041
		t.Errorf("FR residual %v, want the location-based %v", ResidualFactor("FR"), GridFactor("FR"))
	}
	for _, c := range []string{"AT", "CH", "US", "BR"} {
		if ResidualFactor(c) != GridFactor(c) {
			t.Errorf("%s has no residual mix: want the location-based factor", c)
		}
	}
	if ResidualFactor("") != GridWorld {
		t.Error("unknown country should fall back to the world average")
	}
}

func TestRecordFootprintByCountry(t *testing.T) {
	SetOrgCountries(map[string]string{"tx": "US", "es": "ES", "nl": "NL"})
	lot := func(org string, body map[string]any) *domain.Record {
		return &domain.Record{Type: domain.TypeAsphaltLot, OrgID: org, MassKg: 100_000, Body: body}
	}
	// No gas recorded: the national plant factor (or the fallback).
	if got := RecordFootprint(lot("tx", map[string]any{}), DefaultScenario); !near(got, 100*22.6) {
		t.Errorf("US plant %v", got)
	}
	if got := RecordFootprint(lot("nl", map[string]any{}), DefaultScenario); !near(got, 100*Factors["hma_plant"].Value) {
		t.Errorf("NL plant (fallback) %v", got)
	}
	// Gas recorded: burner gas at the national factor plus electricity on the local grid.
	got := RecordFootprint(lot("tx", map[string]any{"gas_m3": 1000.0}), DefaultScenario)
	if want := 1000*10.62*0.1812 + 100*8*GridFactor("US"); !near(got, want) {
		t.Errorf("US gas %v, want %v", got, want)
	}
	ship := &domain.Record{Type: domain.TypeShipment, OrgID: "tx", MassKg: 20_000, Body: map[string]any{"distance_km": 100.0}}
	if got := RecordFootprint(ship, DefaultScenario); !near(got, 20*100*0.1160) {
		t.Errorf("US truck %v", got)
	}
	// Sensitivity uses each plant's residual mix.
	rl := &domain.Record{Type: domain.TypeRecycledLot, OrgID: "nl", MassKg: 1000, Body: map[string]any{"energy_kwh": 100.0}}
	if got := RecordFootprint(rl, Scenarios[2]); !near(got, 100*0.382) {
		t.Errorf("NL residual footprint %v", got)
	}
	reject := domain.Record{ID: "R", Type: domain.TypeRejectLot, OrgID: "tx", MassKg: 1000, Body: map[string]any{"avoided_route": "incineration"}}
	if it := Node(&reject, nil, Intensity{}, Scenarios[2]); !near(it.AvoidedKg, 0.0220) {
		t.Errorf("sensitivity landfill in the US %v", it.AvoidedKg)
	}
}
