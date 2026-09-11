// Package lca computes carbon footprints and avoided emissions for passports, and
// writes the GRETA-ready scenario export.
//
// Every factor names its source. Factors that depend on where a plant operates
// (electricity, natural gas, the asphalt plant, landfill, haulage) are looked up
// by the recording organisation's country (ISO 3166-1 alpha-2): Regional holds
// the national values, Factors the fallback for a country without one, and each
// fallback's source says so. Numbers are kg CO₂e unless stated otherwise. The
// model is deliberately simple and transparent: a passport shows each term, so
// a reviewer can redo the sum.
package lca

import (
	"github.com/sensifai/roadpass/internal/domain"
)

// Factor is one emission factor with its source.
type Factor struct {
	Key    string  `json:"key"`
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Year   int     `json:"year"`
	Geo    string  `json:"geo"`
	Source string  `json:"source"`
}

// Sources shared by several national factors.
const (
	srcDESNZ = "DESNZ 2024 GHG conversion factors (flat file v1.1)"
	srcEPA   = "US EPA GHG Emission Factors Hub 2024 (AR5 GWPs; waste factors AR4)"
)

// Factors are the fallback values, used where Regional has no national value.
var Factors = map[string]Factor{
	"incineration": {"incineration", "Incineration of mixed plastic rejects (as received)", 2.3, "kg CO₂/kg", 2006, "global",
		"IPCC 2006 GL Vol.5 Ch.2 Tab.2.4: 75 % fossil C → 2.75 kg CO₂/kg dry; ×0.85 for moisture and dirt. The film's fossil carbon is the same in every country, so there are no national values"},
	"landfill": {"landfill", "Landfill of plastics (operations, transport)", 0.00888, "kg CO₂e/kg", 2024, "fallback: GB",
		srcDESNZ + ", average plastic film, landfill (no national value tabled for this country); IPCC DOC = 0 for plastics (no landfill methane)"},
	"virgin_ldpe": {"virgin_ldpe", "Virgin LDPE, cradle-to-gate (avoided polymer)", 1.92, "kg CO₂e/kg", 2020, "EU",
		"BAFA (DE) via Climatiq; PlasticsEurope-based EPD reports 2.13"},
	"grid": {"grid", "Electricity, location-based: each plant's own country", GridWorld, "kg CO₂e/kWh", 2025, "per country",
		"Per country: " + GridSource + ". This value: world average, same source, for a country not in the table"},
	"grid_residual": {"grid_residual", "Electricity, residual mix (sensitivity scenario)", GridWorld, "kg CO₂e/kWh", 2024, "per country",
		"Per country: " + ResidualSource + ". Each country keeps the higher of its residual mix and its location-based factor; " +
			"where no residual mix is published the location-based factor is used, as the GHG Protocol Scope 2 Guidance prescribes"},
	"natural_gas": {"natural_gas", "Natural gas combustion", 0.182, "kg CO₂e/kWh (PCS)", 2006, "fallback: IPCC default",
		"IPCC 2006 GL Vol.2 Ch.2 Tab.2.2 defaults (56 100 kg CO₂, 1 kg CH₄, 0.1 kg N₂O per TJ NCV; AR5 GWPs), converted to gross CV with NCV ≈ 0.9 × GCV (IPCC 2006 Vol.2 Ch.1)"},
	"gas_kwh_per_m3": {"gas_kwh_per_m3", "Natural gas energy content", 11.63, "kWh (PCS)/m³", 2025, "fallback: ES",
		"Enagás standard PCS ≈ 11.63 kWh/Nm³ (Spanish network gas; no national value tabled for this country — record the supplier's value as gas energy where you can)"},
	"truck": {"truck", "Articulated HGV, average laden", 0.0745, "kg CO₂e/t·km", 2024, "fallback: GB",
		srcDESNZ + ", HGV (all diesel) articulated >33 t, average laden (no national value tabled for this country)"},
	"hma_plant": {"hma_plant", "Hot mix asphalt, manufacturing stage (when the plant records no gas use)", 30.4, "kg CO₂e/t", 2019, "fallback: ES",
		"LIFE BattleCO2 layman report (ASEFMA), fossil-fuelled plant (no national value tabled for this country)"},
	"hma_cradle_site": {"hma_cradle_site", "Hot mix asphalt, cradle to construction (reference only)", 62, "kg CO₂e/t", 2019, "ES",
		"LIFE BattleCO2 layman report (ASEFMA)"},
	"bitumen": {"bitumen", "Bitumen, cradle-to-gate (reference only)", 0.530, "kg CO₂e/kg", 2025, "EU",
		"Eurobitume LCA 4.0 (via Nynas / Agg-Net)"},
}

// Regional are national values by factor key and ISO 3166-1 alpha-2 code. The
// residual mixes are added from residualByCountry (grid.go).
var Regional = map[string]map[string]Factor{
	"landfill": {
		"GB": {"landfill", "Landfill of plastics", 0.00888, "kg CO₂e/kg", 2024, "GB", srcDESNZ + ", average plastic film, landfill (8.884 kg CO₂e/t)"},
		"US": {"landfill", "Landfill of plastics", 0.0220, "kg CO₂e/kg", 2024, "US", srcEPA + ", Table 9: LDPE landfilled 0.02 t CO₂e per short ton"},
	},
	"natural_gas": {
		"ES": {"natural_gas", "Natural gas combustion", 0.182, "kg CO₂/kWh (PCS)", 2025, "ES", "MITECO Calculadora de huella de carbono v32 (2025)"},
		"GB": {"natural_gas", "Natural gas combustion", 0.1829, "kg CO₂e/kWh (PCS)", 2024, "GB", srcDESNZ + ", natural gas, kWh (gross CV)"},
		"US": {"natural_gas", "Natural gas combustion", 0.1812, "kg CO₂e/kWh (PCS)", 2024, "US", srcEPA + ", Table 1: 53.06 kg CO₂ + 1.0 g CH₄ + 0.10 g N₂O per mmBtu (HHV)"},
	},
	"gas_kwh_per_m3": {
		"ES": {"gas_kwh_per_m3", "Natural gas energy content", 11.63, "kWh (PCS)/m³", 2025, "ES", "Enagás standard PCS ≈ 11.63 kWh/Nm³"},
		"GB": {"gas_kwh_per_m3", "Natural gas energy content", 11.18, "kWh (PCS)/m³", 2024, "GB", srcDESNZ + ": 2.04542 kg CO₂e/m³ ÷ 0.1829 kg CO₂e/kWh (gross CV)"},
		"US": {"gas_kwh_per_m3", "Natural gas energy content", 10.62, "kWh (PCS)/m³", 2024, "US", srcEPA + ", Table 1: 0.001026 mmBtu per scf (HHV, 60 °F)"},
	},
	"truck": {
		"GB": {"truck", "Articulated HGV, average laden", 0.0745, "kg CO₂e/t·km", 2024, "GB", srcDESNZ + ", HGV (all diesel) articulated >33 t, average laden"},
		"US": {"truck", "Medium- and heavy-duty truck", 0.1160, "kg CO₂e/t·km", 2024, "US", srcEPA + ", Table 8: 0.168 kg CO₂ + 0.0015 g CH₄ + 0.0047 g N₂O per short ton-mile"},
	},
	"hma_plant": {
		"ES": {"hma_plant", "Hot mix asphalt, manufacturing stage", 30.4, "kg CO₂e/t", 2019, "ES", "LIFE BattleCO2 layman report (ASEFMA), fossil-fuelled plant"},
		"US": {"hma_plant", "Hot mix asphalt, manufacturing stage", 22.6, "kg CO₂e/t", 2024, "US",
			"NAPA SIP 109 (2024), baseline plant operations (A3) 20.5 kg CO₂e per short ton, natural-gas burner and grid electricity"},
	},
}

// In is the factor that applies in a country: its national value, else the fallback.
func In(key, country string) Factor {
	if f, ok := Regional[key][country]; ok {
		return f
	}
	return Factors[key]
}

// Scenario selects the assumptions of one calculation.
type Scenario struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	About string `json:"about"`
	// SubstituteVirgin credits avoided virgin polymer for recycled plastic in the mix.
	SubstituteVirgin bool `json:"substituteVirgin"`
	// GridKey chooses the electricity factor.
	GridKey string `json:"gridKey"`
	// DisposalOverride forces every avoided route ("" keeps each lot's own route).
	DisposalOverride string `json:"disposalOverride,omitempty"`
}

// Scenarios are the three GRETA-ready variants required by KPI 6b.
var Scenarios = []Scenario{
	{"baseline", "Disposal baseline", "Reject film is landfilled or incinerated as each lot records; no asphalt additive.", false, "grid", ""},
	{"additive", "Recycled-plastic additive", "Reject film becomes asphalt additive and replaces a virgin polymer modifier; each plant's location-based electricity.", true, "grid", ""},
	{"sensitivity", "Conservative sensitivity", "No virgin-polymer credit, each plant's national residual-mix electricity, every lot assumed landfilled (the lowest avoided emissions).", false, "grid_residual", "landfill"},
}

// DefaultScenario is what passports show.
var DefaultScenario = Scenarios[1]

// DisposalFactor is the avoided emissions per kg of reject film for a route, in
// the country where the film was sorted.
func DisposalFactor(route, country string) float64 {
	if route == "incineration" {
		return In("incineration", country).Value
	}
	return In("landfill", country).Value
}

// RecordFootprint is the direct process footprint of one record (kg CO₂e):
// electricity, gas and haulage. Upstream footprints are summed by the passport.
func RecordFootprint(r *domain.Record, sc Scenario) float64 {
	c := countryOf(r.OrgID) // factors of the country where the plant is, not one country's
	grid := gridFor(c, sc)
	switch r.Type {
	case domain.TypeRecycledLot, domain.TypeAdditiveBatch:
		return domain.Num(r.Body, "energy_kwh") * grid
	case domain.TypeAsphaltLot:
		if gas := domain.Num(r.Body, "gas_m3"); gas > 0 {
			// Burner gas plus electricity at ~8 kWh/t (drum, mixer, conveyors).
			return gas*In("gas_kwh_per_m3", c).Value*In("natural_gas", c).Value + r.MassKg/1000*8*grid
		}
		return r.MassKg / 1000 * In("hma_plant", c).Value
	case domain.TypeShipment, domain.TypeDelivery:
		return r.MassKg / 1000 * domain.Num(r.Body, "distance_km") * In("truck", c).Value
	}
	return 0
}

// Tkm is the transport work of a shipment or delivery.
func Tkm(r *domain.Record) float64 {
	if r.Type != domain.TypeShipment && r.Type != domain.TypeDelivery {
		return 0
	}
	return r.MassKg / 1000 * domain.Num(r.Body, "distance_km")
}

// Avoided returns the emissions avoided by putting recycledKg of plastic into
// asphalt instead of disposing of rejectKg of reject film by route in country.
func Avoided(recycledKg, rejectKg float64, route, country string, sc Scenario) float64 {
	if sc.DisposalOverride != "" {
		route = sc.DisposalOverride
	}
	out := rejectKg * DisposalFactor(route, country)
	if sc.SubstituteVirgin {
		out += recycledKg * Factors["virgin_ldpe"].Value
	}
	return out
}
