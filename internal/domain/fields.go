package domain

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// FieldKind is the value type of a type-specific field.
type FieldKind string

const (
	FNumber FieldKind = "number"
	FText   FieldKind = "text"
	FEnum   FieldKind = "enum"
	FTime   FieldKind = "time"
	FRef    FieldKind = "ref"  // id of another record
	FOrg    FieldKind = "org"  // id of an organisation
	FSite   FieldKind = "site" // id of one of the sender's sites
)

// Field describes one type-specific field. The same table drives validation, the
// CSV templates, the API reference and the UI forms, so they cannot disagree.
type Field struct {
	Name     string    `json:"name"`
	Label    string    `json:"label"`
	Kind     FieldKind `json:"kind"`
	Unit     string    `json:"unit,omitempty"`
	Required bool      `json:"required"`
	// Min/Max are hard validity limits (a value outside is rejected). Spec limits,
	// which only raise a finding, live in the checks package.
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Enum    []string `json:"enum,omitempty"`
	Help    string   `json:"help,omitempty"`
	Example string   `json:"example"`
	// Legacy fields are accepted on corrections of older records only; forms and
	// templates leave them out.
	Legacy bool `json:"legacy,omitempty"`
}

func f(v float64) *float64 { return &v }

func num(name, label, unit string, req bool, lo, hi float64, help, ex string) Field {
	return Field{Name: name, Label: label, Kind: FNumber, Unit: unit, Required: req, Min: f(lo), Max: f(hi), Help: help, Example: ex}
}

func enum(name, label string, req bool, vals []string, help string) Field {
	return Field{Name: name, Label: label, Kind: FEnum, Required: req, Enum: vals, Help: help, Example: vals[0]}
}

func text(name, label string, req bool, help, ex string) Field {
	return Field{Name: name, Label: label, Kind: FText, Required: req, Help: help, Example: ex}
}

// Enumerations shared with the simulation and the UI.
var (
	RejectSources  = []string{"municipal_packaging", "commercial_film", "agricultural_film"}
	DisposalRoutes = []string{"landfill", "incineration"}
	RecycledForms  = []string{"agglomerate", "regranulate", "flakes"}
	AdditiveForms  = []string{"pellets", "granules"}
	MixProcesses   = []string{"hot", "warm"}
	Layers         = []string{"surface", "binder", "base"}
	WorksKinds     = []string{"rehabilitation", "new_construction", "maintenance"}
	TestResults    = []string{"pass", "fail"}
)

// LabTests lists the supported lab tests with unit and standard.
// LabTest is a test method and the material it applies to.
type LabTest struct {
	Name     string `json:"name"`
	Unit     string `json:"unit"`
	Standard string `json:"standard"`
	Material string `json:"material"` // "plastic" (lots, batches) or "asphalt" (mix, deliveries, sections)
}

var LabTests = map[string]LabTest{
	"mfi":                {"Melt flow index", "g/10min", "ISO 1133-1", "plastic"},
	"moisture":           {"Moisture", "%", "ISO 15512", "plastic"},
	"ash":                {"Ash content", "%", "ISO 3451-1", "plastic"},
	"dsc_melting":        {"Melting peak (DSC)", "°C", "ISO 11357-3", "plastic"},
	"itsr":               {"Water sensitivity (ITSR)", "%", "EN 12697-12", "asphalt"},
	"wheel_tracking":     {"Wheel tracking slope (WTS)", "mm/10³ cycles", "EN 12697-22", "asphalt"},
	"air_voids":          {"Air voids", "%", "EN 12697-8", "asphalt"},
	"marshall_stability": {"Marshall stability", "kN", "EN 12697-34", "asphalt"},
	"binder_content":     {"Binder content", "%", "EN 12697-1", "asphalt"},
	"density":            {"Bulk density", "t/m³", "EN 12697-6", "asphalt"},
}

// MaterialOf is the material class of a record type ("" for none).
func MaterialOf(t RecordType) string {
	switch t {
	case TypeRejectLot, TypeRecycledLot, TypeAdditiveBatch:
		return "plastic"
	case TypeAsphaltLot, TypeDelivery, TypeRoadSection:
		return "asphalt"
	}
	return ""
}

// LabTestCodes returns the test codes in a stable order.
func LabTestCodes() []string {
	out := make([]string, 0, len(LabTests))
	for k := range LabTests {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Fields per record type. Common envelope fields (ref, site, occurred_at, mass_kg,
// inputs) are not repeated here.
var Fields = map[RecordType][]Field{
	TypeRejectLot: {
		enum("source", "Source", true, RejectSources, "Where the rejected film came from"),
		num("ldpe_share_pct", "Film (LDPE) share", "%", true, 0, 100, "Share of polyethylene film in the lot", "78"),
		num("moisture_pct", "Moisture", "%", false, 0, 60, "", "9.5"),
		num("contamination_pct", "Contamination", "%", false, 0, 60, "Paper, organics, other polymers", "12"),
		enum("avoided_route", "Would otherwise go to", true, DisposalRoutes, "The disposal route this lot avoids"),
		num("bales", "Bales", "", false, 0, 500, "", "24"),
	},
	TypeRecycledLot: {
		enum("form", "Form", true, RecycledForms, ""),
		text("process", "Process steps", false, "Comma-separated", "washing,shredding,agglomeration"),
		num("mfi_g10min", "Melt flow index", "g/10min", false, 0, 100, "190 °C / 2.16 kg", "1.4"),
		num("moisture_pct", "Moisture", "%", false, 0, 20, "", "0.6"),
		num("ash_pct", "Ash", "%", false, 0, 40, "", "2.8"),
		num("energy_kwh", "Electricity used", "kWh", false, 0, 1e7, "", "5200"),
	},
	TypeAdditiveBatch: {
		text("product", "Product", true, "Commercial name of the additive", "RP-Mod 70"),
		enum("form", "Form", true, AdditiveForms, ""),
		num("recycled_content_pct", "Recycled content", "%", true, 0, 100, "Share of recycled plastic by mass", "92"),
		num("mfi_g10min", "Melt flow index", "g/10min", false, 0, 100, "", "2.1"),
		num("melting_point_c", "Melting point", "°C", false, 60, 200, "DSC peak", "112"),
		num("energy_kwh", "Electricity used", "kWh", false, 0, 1e7, "", "3100"),
	},
	TypeAsphaltLot: {
		text("mix", "Mix designation", true, "EN 13108 designation (or the national equivalent)", "AC 16 surf 50/70 S"),
		enum("process", "Process", true, MixProcesses, "Hot or warm mix"),
		num("binder_pct", "Binder content", "% of mix", true, 2, 9, "", "4.6"),
		num("additive_kg", "Additive dosed", "kg", true, 0, 1e6, "Recycled-plastic additive added in the mixer", "320"),
		num("dosage_pct_mix", "Additive dosage", "% of mix", true, 0, 20, "Additive mass over total mix mass (dry process)", "0.5"),
		num("mixing_temp_c", "Mixing temperature", "°C", true, 90, 200, "", "165"),
		num("gas_m3", "Natural gas used", "m³", false, 0, 1e7, "", "2100"),
	},
	TypeDelivery: {
		text("ticket", "Ticket number", true, "", "T-4471-0193"),
		{Name: "section_id", Label: "Road section", Kind: FRef, Required: true, Help: "Road section the load was laid on", Example: "RSC-…"},
		text("vehicle", "Vehicle", false, "Licence plate", "AB-123-C"),
		num("distance_km", "Haul distance", "km", false, 0, 400, "", "38"),
		num("departure_temp_c", "Temperature at departure", "°C", false, 60, 210, "", "162"),
		num("arrival_temp_c", "Temperature on site", "°C", false, 50, 210, "", "151"),
		num("lat_from", "Laid from (latitude)", "", false, -90, 90, "Where the paver started laying this load (WGS84)", "52.3712"),
		num("lon_from", "Laid from (longitude)", "", false, -180, 180, "Where the paver started laying this load (WGS84)", "4.8971"),
		num("lat_to", "Laid to (latitude)", "", false, -90, 90, "Where the paver finished laying this load (WGS84)", "52.3718"),
		num("lon_to", "Laid to (longitude)", "", false, -180, 180, "Where the paver finished laying this load (WGS84)", "4.8990"),
	},
	TypeRoadSection: {
		text("road", "Road", true, "Road number", "A1"),
		num("km_from", "Start", "km", true, 0, 2000, "", "412.3"),
		num("km_to", "End", "km", true, 0, 2000, "", "415.1"),
		text("carriageway", "Carriageway", false, "", "Right"),
		enum("layer", "Layer", true, Layers, ""),
		num("thickness_cm", "Thickness", "cm", true, 1, 40, "", "5"),
		num("area_m2", "Area", "m²", true, 1, 1e7, "", "29400"),
		enum("works", "Works", true, WorksKinds, ""),
		text("mix", "Mix", true, "", "AC 16 surf 50/70 S"),
		{Name: "owner_org_id", Label: "Road owner", Kind: FOrg, Required: true, Example: "org_…"},
		{Name: "completed_at", Label: "Completed", Kind: FTime, Required: false, Example: "2025-06-14T18:00:00Z"},
		num("lat", "Latitude", "", false, -90, 90, "Mid-point of the section (WGS84)", "52.37"),
		num("lon", "Longitude", "", false, -180, 180, "Mid-point of the section (WGS84)", "4.90"),
		num("lat_from", "Start latitude", "", false, -90, 90, "Start of the section, at km Start (WGS84)", "52.36"),
		num("lon_from", "Start longitude", "", false, -180, 180, "Start of the section, at km Start (WGS84)", "4.88"),
		num("lat_to", "End latitude", "", false, -90, 90, "End of the section, at km End (WGS84)", "52.38"),
		num("lon_to", "End longitude", "", false, -180, 180, "End of the section, at km End (WGS84)", "4.92"),
		text("region", "Region", false, "Region, state or province", "North Brabant"),
	},
	TypeShipment: {
		{Name: "to_site_id", Label: "Receiver", Kind: FSite, Required: true, Help: "One of your own or partner sites", Example: "site_…"},
		{Name: "to_org_id", Label: "Receiving company", Kind: FOrg, Legacy: true, Help: "Shipments recorded before receivers were private sites", Example: "org_…"},
		text("carrier", "Carrier", false, "", "Transportes del Levante"),
		text("vehicle", "Vehicle", false, "Licence plate", "AB-123-C"),
		num("distance_km", "Distance", "km", true, 0, 3000, "", "184"),
	},
	TypeLabTest: {
		{Name: "subject_id", Label: "Tested record", Kind: FRef, Required: true, Example: "RPL-…"},
		{Name: "test", Label: "Test", Kind: FEnum, Required: true, Enum: LabTestCodes(), Example: "mfi"},
		num("value", "Result", "", true, -1e6, 1e6, "In the test's unit", "1.4"),
		enum("result", "Verdict", true, TestResults, ""),
		text("lab", "Laboratory", false, "", "Central materials lab"),
	},
	TypeRelease: {
		{Name: "subject_id", Label: "Lot", Kind: FRef, Required: true, Help: "The lot or batch being released", Example: "RPL-…"},
		enum("decision", "Decision", true, []string{Released, Quarantined}, "Released lots may be shipped; quarantined lots are held"),
		text("note", "Note", false, "Reason, certificate number", "Within spec; CoA 2026-114"),
	},
	TypeAcceptance: {
		{Name: "subject_id", Label: "Accepted record", Kind: FRef, Required: true, Example: "ASL-…"},
		text("finding_code", "Finding", true, "", "C5"),
		text("reason", "Reason", true, "Why the record is accepted", "Weighbridge recalibrated; supplier ticket attached"),
	},
}

// FieldError is one invalid field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

// Validate checks the envelope and the type-specific body. It normalises body
// values in place (numbers to float64, times to RFC 3339 UTC) so the hash is
// stable whatever the input format (JSON or CSV).
func Validate(r *Record) []FieldError {
	var errs []FieldError
	add := func(field, msg string, a ...any) { errs = append(errs, FieldError{field, fmt.Sprintf(msg, a...)}) }
	if !r.Type.Valid() {
		add("type", "unknown record type %q", r.Type)
		return errs
	}
	if strings.TrimSpace(r.Ref) == "" {
		add("ref", "your reference is required")
	} else if len(r.Ref) > 120 {
		add("ref", "at most 120 characters")
	}
	if r.OccurredAt.IsZero() {
		add("occurred_at", "date and time are required")
	} else if r.OccurredAt.After(time.Now().Add(24 * time.Hour)) {
		add("occurred_at", "is in the future")
	}
	if math.IsNaN(r.MassKg) || r.MassKg < 0 || r.MassKg > 5e7 {
		add("mass_kg", "must be between 0 and 50,000,000 kg")
	}
	if !slices.Contains(Provenances, r.Provenance) {
		add("provenance", "must be one of %s", strings.Join(Provenances, ", "))
	}
	void := validateVoid(r, add)
	for i, in := range r.Inputs {
		if in.ID == "" {
			add(fmt.Sprintf("inputs[%d].id", i), "is required")
		}
		if in.MassKg <= 0 && !void {
			add(fmt.Sprintf("inputs[%d].mass_kg", i), "must be positive")
		}
	}
	if r.Body == nil {
		r.Body = map[string]any{}
	}
	known := map[string]bool{FieldVoid: void, FieldVoidReason: void}
	for _, fd := range Fields[r.Type] {
		known[fd.Name] = true
		v, present := r.Body[fd.Name]
		if !present || v == nil || v == "" {
			if fd.Required {
				add(fd.Name, "%s is required", strings.ToLower(fd.Label))
			}
			delete(r.Body, fd.Name)
			continue
		}
		nv, msg := coerce(fd, v)
		if msg != "" {
			add(fd.Name, "%s", msg)
			continue
		}
		r.Body[fd.Name] = nv
	}
	for k := range r.Body {
		if !known[k] {
			add(k, "unknown field for %s", Types[r.Type].Name)
		}
	}
	return errs
}

func coerce(fd Field, v any) (any, string) {
	switch fd.Kind {
	case FNumber:
		var x float64
		switch t := v.(type) {
		case float64:
			x = t
		case int:
			x = float64(t)
		case int64:
			x = float64(t)
		case string:
			p, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(t), ",", "."), 64)
			if err != nil {
				return nil, "must be a number"
			}
			x = p
		default:
			return nil, "must be a number"
		}
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, "must be a finite number"
		}
		if fd.Min != nil && x < *fd.Min || fd.Max != nil && x > *fd.Max {
			return nil, fmt.Sprintf("must be between %g and %g %s", *fd.Min, *fd.Max, fd.Unit)
		}
		return x, ""
	case FEnum:
		s, ok := v.(string)
		if !ok || !slices.Contains(fd.Enum, s) {
			return nil, "must be one of " + strings.Join(fd.Enum, ", ")
		}
		return s, ""
	case FTime:
		s, ok := v.(string)
		if !ok {
			return nil, "must be an RFC 3339 date-time"
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, "must be an RFC 3339 date-time, e.g. 2025-06-14T18:00:00Z"
		}
		return t.UTC().Truncate(time.Second).Format(time.RFC3339), ""
	default:
		s, ok := v.(string)
		if !ok {
			return nil, "must be text"
		}
		s = strings.TrimSpace(s)
		if len(s) > 500 {
			return nil, "at most 500 characters"
		}
		return s, ""
	}
}

// Num reads a number from a validated body (0 when absent).
func Num(body map[string]any, k string) float64 {
	x, _ := body[k].(float64)
	return x
}

// Str reads a string from a validated body ("" when absent).
func Str(body map[string]any, k string) string {
	s, _ := body[k].(string)
	return s
}
