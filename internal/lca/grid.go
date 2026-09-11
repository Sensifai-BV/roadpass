package lca

import "sync"

// Electricity is counted where it is used: each plant's grid, not one country's.

// GridSource names the per-country electricity factors.
const GridSource = "Ember, carbon intensity of electricity generation (lifecycle gCO₂e/kWh), latest year 2024–2025, via Our World in Data (grapher/carbon-intensity-electricity)"

// GridWorld is the fallback for a country not in the table (world, 2025, same source).
const GridWorld = 0.458

// gridByCountry is kg CO₂e per kWh by ISO 3166-1 alpha-2 code.
var gridByCountry = map[string]float64{
	"AF": 0.131, "AL": 0.025, "DZ": 0.633, "AO": 0.185, "AR": 0.346, "AM": 0.212, "AU": 0.525, "AT": 0.117, "AZ": 0.632,
	"BS": 0.653, "BH": 0.902, "BD": 0.696, "BB": 0.595, "BY": 0.309, "BE": 0.150, "BZ": 0.170, "BJ": 0.584, "BT": 0.024,
	"BO": 0.481, "BA": 0.571, "BW": 0.851, "BR": 0.110, "BN": 0.892, "BG": 0.276, "BF": 0.562, "BI": 0.184, "KH": 0.499,
	"CM": 0.226, "CA": 0.191, "CV": 0.462, "TD": 0.622, "CL": 0.289, "CN": 0.525, "CO": 0.187, "CG": 0.716, "CR": 0.024,
	"CI": 0.405, "HR": 0.159, "CU": 0.643, "CY": 0.489, "CZ": 0.401, "CD": 0.028, "DK": 0.114, "DJ": 0.450, "DO": 0.537,
	"TL": 0.667, "EC": 0.159, "EG": 0.563, "SV": 0.139, "GQ": 0.644, "ER": 0.578, "EE": 0.319, "SZ": 0.131, "ET": 0.023,
	"FJ": 0.278, "FI": 0.057, "FR": 0.041, "GA": 0.523, "GM": 0.667, "GE": 0.146, "DE": 0.330, "GH": 0.469, "GR": 0.315,
	"GT": 0.301, "GN": 0.181, "GW": 0.625, "GY": 0.645, "HT": 0.535, "HN": 0.322, "HK": 0.675, "HU": 0.163, "IS": 0.028,
	"IN": 0.670, "ID": 0.680, "IR": 0.660, "IQ": 0.683, "IE": 0.257, "IL": 0.493, "IT": 0.285, "JM": 0.563, "JP": 0.477,
	"JO": 0.530, "KZ": 0.805, "KE": 0.095, "KW": 0.635, "KG": 0.153, "LA": 0.232, "LB": 0.390, "LS": 0.021, "LR": 0.316,
	"LY": 0.827, "LT": 0.138, "LU": 0.123, "MG": 0.432, "MW": 0.055, "MY": 0.602, "MV": 0.612, "ML": 0.539, "MT": 0.484,
	"MR": 0.512, "MU": 0.642, "MX": 0.474, "MD": 0.633, "MN": 0.816, "ME": 0.264, "MA": 0.596, "MZ": 0.129, "MM": 0.503,
	"NA": 0.049, "NP": 0.024, "NL": 0.254, "NZ": 0.093, "NI": 0.301, "NE": 0.674, "NG": 0.456, "KP": 0.341, "MK": 0.441,
	"NO": 0.028, "OM": 0.544, "PK": 0.347, "PS": 0.414, "PA": 0.221, "PG": 0.514, "PY": 0.025, "PE": 0.238, "PH": 0.588,
	"PL": 0.589, "PT": 0.128, "QA": 0.582, "RO": 0.251, "RU": 0.450, "RW": 0.354, "SA": 0.692, "SN": 0.540, "RS": 0.696,
	"SL": 0.048, "SG": 0.497, "SK": 0.095, "SI": 0.183, "SO": 0.512, "ZA": 0.699, "KR": 0.417, "SS": 0.643, "ES": 0.154,
	"LK": 0.329, "SD": 0.154, "SR": 0.322, "SE": 0.035, "CH": 0.039, "SY": 0.706, "TW": 0.633, "TJ": 0.073, "TZ": 0.345,
	"TH": 0.546, "TG": 0.423, "TT": 0.682, "TN": 0.560, "TR": 0.475, "TM": 1.306, "UG": 0.059, "UA": 0.250, "AE": 0.468,
	"GB": 0.217, "US": 0.384, "UY": 0.080, "UZ": 1.000, "VE": 0.086, "VN": 0.461, "YE": 0.592, "ZM": 0.120, "ZW": 0.384,
}

// ResidualSource names the national residual mixes.
const ResidualSource = "AIB European Residual Mixes 2024 (Grexel for AIB, results of 30 May 2025), direct CO₂ of the residual mix. " +
	"No residual mix is published for AT and CH (full disclosure), outside the AIB area, or nationally for the US (Green-e publishes per eGRID subregion only)"

// residualByCountry is the 2024 residual mix in kg CO₂/kWh by ISO 3166-1 alpha-2 code.
var residualByCountry = map[string]float64{
	"BA": 0.777, "BE": 0.132, "BG": 0.380, "CY": 0.613, "CZ": 0.584, "DE": 0.725, "DK": 0.422, "EE": 0.612, "ES": 0.292,
	"FI": 0.406, "FR": 0.024, "GB": 0.421, "GR": 0.367, "HR": 0.573, "HU": 0.319, "IE": 0.366, "IS": 0.506, "IT": 0.441,
	"LT": 0.568, "LU": 0.213, "LV": 0.504, "ME": 0.623, "MT": 0.398, "NL": 0.382, "NO": 0.535, "PL": 0.808, "PT": 0.502,
	"RO": 0.233, "RS": 0.896, "SE": 0.086, "SI": 0.429, "SK": 0.334,
}

func init() {
	m := make(map[string]Factor, len(residualByCountry))
	for c, v := range residualByCountry {
		m[c] = Factor{"grid_residual", "Electricity, residual mix", v, "kg CO₂/kWh", 2024, c, "AIB European Residual Mixes 2024 (Grexel for AIB, 30 May 2025)"}
	}
	Regional["grid_residual"] = m
}

var (
	countryMu  sync.RWMutex
	orgCountry = map[string]string{}
)

// SetOrgCountries tells the model where each organisation operates (callers
// that load organisations refresh it).
func SetOrgCountries(m map[string]string) {
	countryMu.Lock()
	defer countryMu.Unlock()
	for k, v := range m {
		orgCountry[k] = v
	}
}

// GridFactor is the location-based electricity factor of a country (kg CO₂e/kWh).
func GridFactor(country string) float64 {
	if v, ok := gridByCountry[country]; ok {
		return v
	}
	return GridWorld
}

// ResidualFactor is the sensitivity scenario's electricity factor of a country:
