// Package checks runs the deterministic data checks C1–C8 (docs/ARCHITECTURE.md §6).
//
// The rules see the world through Env, which both the database (live ingest) and
// the simulation (seeding) implement, so seeded findings are produced by exactly the
// code that judges partner data.
package checks

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/signing"
)

// Severity of a finding.
const (
	Blocker = "blocker"
	Warning = "warning"
)

// Rule describes one check for the UI and docs.
type Rule struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Explain  string `json:"explain"`
}

// Rules is the catalogue.
var Rules = []Rule{
	{"C1", "Missing information", Warning, "Evidence a buyer or auditor expects is not filled in."},
	{"C2", "Duplicate", Blocker, "The same record was submitted twice, possibly under a different reference."},
	{"C3", "Dates out of order", Blocker, "Something happened before the material it needs existed or arrived."},
	{"C4", "Broken chain of custody", Blocker, "An input was used by a company it was never shipped to."},
	{"C5", "Mass doesn't add up", Blocker, "More material came out, or was used, than went in."},
	{"C6", "Value out of range", Warning, "A measurement is outside the specification for its material or mix."},
	{"C7", "Signature problem", Blocker, "The record is not signed, or the signature does not match the company's registered key."},
	{"C8", "Dosage out of spec", Warning, "Additive dosage is outside the mix design window."},
	{"C9", "Shipped before release", Warning, "A lot left the plant without quality's release, or while quarantined."},
}

// RuleByCode returns a rule.
func RuleByCode(code string) Rule {
	for _, r := range Rules {
		if r.Code == code {
			return r
		}
	}
	return Rule{Code: code, Name: code, Severity: Warning}
}

// Finding is one problem found on one record.
type Finding struct {
	RecordID string         `json:"recordId"`
	Code     string         `json:"code"`
	Severity string         `json:"severity"`
	Message  string         `json:"message"`
	Fix      string         `json:"fix"`
	Detail   map[string]any `json:"detail,omitempty"`
}

// Env is what the rules may ask about the rest of the chain.
type Env interface {
	// Record returns the latest version of a record, or ok=false.
	Record(ctx context.Context, id string) (*domain.Record, bool, error)
	// ReceivedShipment reports whether a shipment carrying lotID was sent to orgID,
	// and when it was received: zero if not yet, -1 if every such shipment was
	// rejected at intake.
	ReceivedShipment(ctx context.Context, lotID, orgID string) (found bool, receivedAt int64, err error)
	// ConsumedKg is the mass already taken from parentID by records other than exceptID.
	ConsumedKg(ctx context.Context, parentID, exceptID string) (float64, error)
	// DuplicateOf returns another latest record with the same duplicate fingerprint
	// (domain.Record.DupHash).
	DuplicateOf(ctx context.Context, dupHash, exceptID string) (string, bool, error)
	// Release returns the latest quality decision on a lot (released | quarantined)
	// and when it was made.
	Release(ctx context.Context, lotID string) (decision string, at time.Time, found bool, err error)
	// PublicKey returns an organisation's registered key by key id.
	PublicKey(ctx context.Context, keyID string) ([]byte, bool, error)
	// LaidKg is the mass of the deliveries onto a road section that count as laid
	// (latest versions, not void, not rejected at the paver).
	LaidKg(ctx context.Context, sectionID string) (float64, error)
}

// Tolerance on mass balances: weighbridges are calibrated to about ±0.5 %, and
// moisture changes between weighings; 2 % avoids flagging normal practice.
const Tolerance = 0.02

// Run checks one record.
func Run(ctx context.Context, env Env, r *domain.Record) ([]Finding, error) {
	var out []Finding
	add := func(code, msg, fix string, detail map[string]any) {
		out = append(out, Finding{RecordID: r.ID, Code: code, Severity: RuleByCode(code).Severity, Message: msg, Fix: fix, Detail: detail})
	}
	if r.Void() {
		// A void takes nothing and claims nothing: only its signature is judged.
		if err := signature(ctx, env, r, add); err != nil {
			return nil, err
		}
		return mergeByCode(out), nil
	}
	missing(r, add)
	if err := duplicate(ctx, env, r, add); err != nil {
		return nil, err
	}
	inputs, err := loadInputs(ctx, env, r)
	if err != nil {
		return nil, err
	}
	order(r, inputs, add)
	if err := custody(ctx, env, r, inputs, add); err != nil {
		return nil, err
	}
	if err := mass(ctx, env, r, inputs, add); err != nil {
		return nil, err
	}
	ranges(r, inputs, add)
	if err := signature(ctx, env, r, add); err != nil {
		return nil, err
	}
	dosage(r, add)
	if err := released(ctx, env, r, inputs, add); err != nil {
		return nil, err
	}
	return mergeByCode(out), nil
}

// mergeByCode keeps one finding per rule per record (the store's key); repeated
// hits are listed in the detail and summarised in the message.
func mergeByCode(fs []Finding) []Finding {
	var out []Finding
	idx := map[string]int{}
	for _, f := range fs {
		i, seen := idx[f.Code]
		if !seen {
			idx[f.Code] = len(out)
			out = append(out, f)
			continue
		}
		m := &out[i]
		if m.Detail == nil {
			m.Detail = map[string]any{}
		}
		more, _ := m.Detail["more"].([]string)
		m.Detail["more"] = append(more, f.Message)
		m.Message = fmt.Sprintf("%s (and %d more)", strings.SplitN(out[i].Message, " (and ", 2)[0], len(more)+1)
	}
	return out
}

// StatusFor derives the record status from its findings.
func StatusFor(fs []Finding) domain.Status {
	st := domain.StatusOK
	for _, f := range fs {
		if f.Severity == Blocker {
			return domain.StatusBlocked
		}
		st = domain.StatusFlagged
	}
	return st
}

type addFn func(code, msg, fix string, detail map[string]any)

// recommended lists the optional fields a complete passport should still carry.
var recommended = map[domain.RecordType][]string{
	domain.TypeRejectLot:     {"moisture_pct", "contamination_pct"},
	domain.TypeRecycledLot:   {"mfi_g10min", "moisture_pct", "energy_kwh"},
	domain.TypeAdditiveBatch: {"mfi_g10min", "melting_point_c"},
	domain.TypeAsphaltLot:    {"gas_m3"},
	domain.TypeDelivery:      {"arrival_temp_c", "distance_km"},
}

func missing(r *domain.Record, add addFn) {
	var miss []string
	for _, k := range recommended[r.Type] {
		if _, ok := r.Body[k]; !ok {
			miss = append(miss, label(r.Type, k))
		}
	}
	// Decisions and tests are about a record, not made at a site.
	if r.SiteID == "" && r.Type != domain.TypeAcceptance && r.Type != domain.TypeLabTest && r.Type != domain.TypeRelease && r.Type != domain.TypeRoadSection {
		miss = append(miss, "site")
	}
	if len(miss) > 0 {
		add("C1", "Missing "+strings.Join(miss, ", "),
			"Add the missing values with a correction (new version); buyers and auditors rely on them.",
			map[string]any{"fields": miss})
	}
}

func label(t domain.RecordType, field string) string {
	for _, f := range domain.Fields[t] {
		if f.Name == field {
			return strings.ToLower(f.Label)
		}
	}
	return field
}

func duplicate(ctx context.Context, env Env, r *domain.Record, add addFn) error {
	dh, err := r.DupHash()
	if err != nil {
		return err
	}
	other, ok, err := env.DuplicateOf(ctx, dh, r.ID)
	if err != nil || !ok {
		return err
	}
	add("C2", fmt.Sprintf("Identical to %s", other),
		"If this is the same lot, withdraw one of them. If they are different lots, give each its own reference and weighing.",
		map[string]any{"duplicateOf": other})
	return nil
}

func loadInputs(ctx context.Context, env Env, r *domain.Record) ([]*domain.Record, error) {
	out := make([]*domain.Record, len(r.Inputs))
	for i, in := range r.Inputs {
		p, ok, err := env.Record(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			out[i] = p
		}
	}
	return out, nil
}

// allowedInput lists which record types a type may consume or carry.
var allowedInput = map[domain.RecordType][]domain.RecordType{
	domain.TypeRecycledLot:   {domain.TypeRejectLot},
	domain.TypeAdditiveBatch: {domain.TypeRecycledLot},
	domain.TypeAsphaltLot:    {domain.TypeAdditiveBatch},
	domain.TypeDelivery:      {domain.TypeAsphaltLot},
	domain.TypeShipment:      {domain.TypeRejectLot, domain.TypeRecycledLot, domain.TypeAdditiveBatch},
}

func order(r *domain.Record, inputs []*domain.Record, add addFn) {
	for i, p := range inputs {
		if p == nil {
			continue
		}
		if r.OccurredAt.Before(p.OccurredAt) {
			add("C3", fmt.Sprintf("Recorded %s, before its input %s existed (%s)",
				r.OccurredAt.Format("2 Jan 2006 15:04"), r.Inputs[i].ID, p.OccurredAt.Format("2 Jan 2006 15:04")),
				"Check the date and time on both records; correct the wrong one.",
				map[string]any{"input": r.Inputs[i].ID})
			return
		}
	}
	if r.Type == domain.TypeShipment && r.Receipt != nil && r.Receipt.ReceivedAt.Before(r.OccurredAt) {
		add("C3", "Received before it was sent",
			"Correct the departure or arrival time (the receiver's weighbridge ticket is usually right).", nil)
	}
}

func custody(ctx context.Context, env Env, r *domain.Record, inputs []*domain.Record, add addFn) error {
	allowed := allowedInput[r.Type]
	for i, p := range inputs {
		id := r.Inputs[i].ID
		if p == nil {
			add("C4", fmt.Sprintf("Input %s is unknown", id),
				"Ask the supplier to register the lot, or correct the reference.", map[string]any{"input": id})
			continue
		}
		if !containsType(allowed, p.Type) {
			add("C4", fmt.Sprintf("%s cannot be an input of a %s", domain.Types[p.Type].Name, strings.ToLower(domain.Types[r.Type].Name)),
				"Reference the right kind of lot.", map[string]any{"input": id})
			continue
		}
		own := p.OrgID == r.OrgID
		if own && r.Type == domain.TypeShipment {
			continue // the holder ships its own lot
		}
		if r.Type == domain.TypeShipment {
			add("C4", fmt.Sprintf("Ships %s, which belongs to another company", id),
				"Only the holder of a lot can ship it.", map[string]any{"input": id})
			continue
		}
		found, recv, err := env.ReceivedShipment(ctx, id, r.OrgID)
		if err != nil {
			return err
		}
		if own && !found {
			continue // made and used inside the company, never moved between its sites
		}
		// Own material that was shipped to another of the company's sites passes the
		// same intake gate as material from outside: received, and not rejected.
		switch {
		case !found:
			add("C4", fmt.Sprintf("Uses %s, which was never shipped to this company", id),
				"Ask the supplier to record the shipment, or correct the input reference.", map[string]any{"input": id})
		case recv < 0:
			add("C4", fmt.Sprintf("Uses %s, which was rejected at intake", id),
				"Rejected material does not enter your stock; return it or have the supplier correct the shipment.", map[string]any{"input": id})
		case recv == 0:
			add("C4", fmt.Sprintf("Uses %s before its shipment was received", id),
				"Sign the shipment receipt first.", map[string]any{"input": id})
		case recv > r.OccurredAt.Unix():
			add("C3", fmt.Sprintf("Uses %s before it arrived", id),
				"Check the production time against the shipment receipt.", map[string]any{"input": id})
		}
	}
	return nil
}

func containsType(ts []domain.RecordType, t domain.RecordType) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

func mass(ctx context.Context, env Env, r *domain.Record, inputs []*domain.Record, add addFn) error {
	var in float64
	for _, x := range r.Inputs {
		in += x.MassKg
	}
	switch r.Type {
	case domain.TypeRecycledLot:
		if in > 0 && r.MassKg > in*(1+Tolerance) {
			add("C5", fmt.Sprintf("Output %s is more than the %s of reject film that went in", kg(r.MassKg), kg(in)),
				"Check the output weighing or add the missing input lots.", map[string]any{"inKg": in, "outKg": r.MassKg})
		}
	case domain.TypeAdditiveBatch:
		rc := domain.Num(r.Body, "recycled_content_pct") / 100
		need := r.MassKg * rc
		switch {
		case in > 0 && need > in*(1+Tolerance):
			add("C5", fmt.Sprintf("Claims %s of recycled plastic but only %s went in", kg(need), kg(in)),
				"Lower the recycled-content claim or add the missing input lots.", map[string]any{"inKg": in, "claimedKg": need})
		case rc > 0 && in > need*(1+Tolerance)+1:
			// The opposite disagreement: more recycled plastic went in than the
			// declared content accounts for. The passport counts only the declared
			// share, so either the inputs or the declaration is wrong.
			add("C5", fmt.Sprintf("Used %s of recycled plastic but declares %s %% recycled content (%s)", kg(in), strconv.FormatFloat(rc*100, 'f', -1, 64), kg(need)),
				"Correct the input quantities to what went into this batch, or the recycled-content declaration.", map[string]any{"inKg": in, "claimedKg": need})
		}
	case domain.TypeAsphaltLot:
		if a := domain.Num(r.Body, "additive_kg"); math.Abs(a-in) > math.Max(a, in)*Tolerance+1 {
			add("C5", fmt.Sprintf("Additive dosed (%s) differs from the additive batches used (%s)", kg(a), kg(in)),
				"Make the dosed quantity match the batches drawn from the silo.", map[string]any{"dosedKg": a, "inputsKg": in})
		}
	case domain.TypeShipment:
		if r.Receipt != nil && math.Abs(r.Receipt.MassKg-r.MassKg) > r.MassKg*Tolerance {
			rm := r.Receipt.MassKg
			add("C5", fmt.Sprintf("Sent %s, received %s", kg(r.MassKg), kg(rm)),
				"Compare both weighbridge tickets; record the difference as a loss if it is real.", map[string]any{"sentKg": r.MassKg, "receivedKg": rm})
		}
	}
	if r.Type == domain.TypeRoadSection && domain.Str(r.Body, "completed_at") != "" {
		laid, err := env.LaidKg(ctx, r.ID)
		if err != nil {
			return err
		}
		if f, bad := AsLaid(r, laid); bad {
			add(f.Code, f.Message, f.Fix, f.Detail)
		}
	}
	// No parent may give more than it holds.
	if r.Type == domain.TypeShipment {
		return nil
	}
	for i, p := range inputs {
		if p == nil {
			continue
		}
		used, err := env.ConsumedKg(ctx, p.ID, r.ID)
		if err != nil {
			return err
		}
		if total := used + r.Inputs[i].MassKg; total > p.MassKg*(1+Tolerance) {
			add("C5", fmt.Sprintf("%s has %s but %s has been used from it", p.ID, kg(p.MassKg), kg(total)),
				"One of the records using this lot has the wrong quantity.", map[string]any{"input": p.ID, "availableKg": p.MassKg, "usedKg": total})
		}
	}
	return nil
}

// AsLaid judges a completed road section's as-laid mass against laidKg, the
// deliveries onto it that count (a load rejected at the paver or voided does not):
// claiming more asphalt than was accepted means a load is missing or a rejected
// load was counted. It is run on the section's own versions and again when a
// delivery onto it is rejected.
func AsLaid(r *domain.Record, laidKg float64) (Finding, bool) {
	if r.MassKg <= 0 || r.MassKg <= laidKg*(1+Tolerance) {
		return Finding{}, false
	}
	return Finding{RecordID: r.ID, Code: "C5", Severity: RuleByCode("C5").Severity,
		Message: fmt.Sprintf("As-laid mass %s is more than the %s of accepted deliveries onto the section", kg(r.MassKg), kg(laidKg)),
		Fix:     "Record the missing deliveries, or correct the as-laid mass (loads rejected at the paver are not laid).",
		Detail:  map[string]any{"asLaidKg": r.MassKg, "acceptedDeliveriesKg": laidKg}}, true
}

func kg(x float64) string {
	if x >= 10000 {
		return fmt.Sprintf("%.1f t", x/1000)
	}
	return fmt.Sprintf("%.0f kg", x)
}

// Spec windows (warning only). Sources: docs/research/SPAIN_DATA_SOURCES.md.
type window struct {
	lo, hi float64
	what   string
}

var specs = map[domain.RecordType]map[string]window{
	domain.TypeRecycledLot: {
		"mfi_g10min":   {0.2, 4, "melt flow index for film-grade recycled PE"},
		"moisture_pct": {0, 3, "moisture for extrusion"},
		"ash_pct":      {0, 8, "ash (inorganic contamination)"},
	},
	domain.TypeAdditiveBatch: {
		"melting_point_c":      {100, 135, "melting peak for PE-based modifiers"},
		"recycled_content_pct": {50, 100, "recycled content declared for the product"},
	},
	domain.TypeDelivery: {
		"arrival_temp_c": {130, 175, "temperature at the paver (PG-3: min. 130 °C)"},
	},
}

var testSpecs = map[string]window{
	"itsr":           {80, 110, "water sensitivity (PG-3 Art. 542: min. 80 %, surface 85 %)"},
	"air_voids":      {3, 8, "air voids (PG-3 Table 542.12)"},
	"wheel_tracking": {0, 0.15, "wheel tracking slope (PG-3 Table 542.13)"},
	"moisture":       {0, 3, "moisture"},
	"ash":            {0, 8, "ash"},
	"mfi":            {0.2, 4, "melt flow index"},
	"binder_content": {3.5, 6.5, "binder content"},
}

func ranges(r *domain.Record, inputs []*domain.Record, add addFn) {
	check := func(field string, v float64, w window) {
		if v < w.lo || v > w.hi {
			add("C6", fmt.Sprintf("%s is %g, outside %g–%g (%s)", label(r.Type, field), round(v), w.lo, w.hi, w.what),
				"Re-test, or record why this lot is still fit for use.", map[string]any{"field": field, "value": v, "min": w.lo, "max": w.hi})
		}
	}
	for field, w := range specs[r.Type] {
		if v, ok := r.Body[field].(float64); ok {
			if r.Type == domain.TypeDelivery && field == "arrival_temp_c" && warmInput(inputs) {
				// PG-3 Art. 542: the 130 °C minimum at the paver does not apply to warm mix.
				w = window{95, 150, "temperature at the paver for warm mix"}
			}
			check(field, v, w)
		}
	}
	if r.Type == domain.TypeAsphaltLot {
		// PG-3 Art. 542: HMA ≤165 °C at the mixer (batch plants); WMA ≤140 °C.
		lo, hi := 135.0, 170.0
		if domain.Str(r.Body, "process") == "warm" {
			lo, hi = 100, 140
		}
		if v := domain.Num(r.Body, "mixing_temp_c"); v < lo || v > hi {
			check("mixing_temp_c", v, window{lo, hi, domain.Str(r.Body, "process") + " mix temperature"})
		}
	}
	if r.Type == domain.TypeRoadSection {
		// Compacted asphalt weighs about 2.4 t/m³ (2.3–2.5 for dense mixes), so a
		// section's mass should be close to area × thickness × 2.4. A third off
		// usually means a typo in area, thickness or mass, or loads missing.
		area, thick := domain.Num(r.Body, "area_m2"), domain.Num(r.Body, "thickness_cm")
		if area > 0 && thick > 0 && r.MassKg > 0 {
			want := area * thick / 100 * 2400
			if ratio := r.MassKg / want; ratio < 0.67 || ratio > 1.5 {
				what := "planned"
				if domain.Str(r.Body, "completed_at") != "" {
					what = "as-laid"
				}
				add("C6", fmt.Sprintf("The %s mass (%.0f t) does not fit %g m² × %g cm (about %.0f t at 2.4 t/m³)", what, r.MassKg/1000, area, thick, want/1000),
					"Check area, thickness and mass, and that every delivery is recorded.", map[string]any{"massKg": r.MassKg, "expectedKg": math.Round(want)})
			}
		}
	}
	if r.Type == domain.TypeLabTest {
		test := domain.Str(r.Body, "test")
		if w, ok := testSpecs[test]; ok {
			if v := domain.Num(r.Body, "value"); v < w.lo || v > w.hi {
				add("C6", fmt.Sprintf("%s result %g is outside %g–%g", domain.LabTests[test].Name, round(v), w.lo, w.hi),
					"Re-test, or quarantine the tested lot.", map[string]any{"test": test, "value": v})
			}
		}
		if domain.Str(r.Body, "result") == "fail" {
			add("C6", domain.LabTests[test].Name+" failed", "Quarantine the tested lot and record the decision.", map[string]any{"test": test})
		}
	}
}

func round(x float64) float64 { return math.Round(x*100) / 100 }

func signature(ctx context.Context, env Env, r *domain.Record, add addFn) error {
	if r.Signature == nil {
		add("C7", "Not signed", "Sign the record with the company key (the platform signs API and CSV submissions automatically).", nil)
		return nil
	}
	if r.Signature.OrgID != r.OrgID {
		add("C7", "Signed by another company", "Only the issuing company may sign its records.", map[string]any{"keyId": r.Signature.KeyID})
		return nil
	}
	pub, ok, err := env.PublicKey(ctx, r.Signature.KeyID)
	if err != nil {
		return err
	}
	if !ok || signing.Verify(pub, r.ContentHash, r.Signature.Value) != nil {
		add("C7", "Signature does not verify against the registered key",
			"Re-sign the record; if the key was replaced, register the new key first.", map[string]any{"keyId": r.Signature.KeyID})
	}
	if rc := r.Receipt; rc != nil {
		if rc.Signature.OrgID != r.Counterparty {
			add("C7", "Receipt signed by a company other than the receiver", "Only the receiver may confirm a shipment.", nil)
			return nil
		}
		pub, ok, err := env.PublicKey(ctx, rc.Signature.KeyID)
		if err != nil {
			return err
		}
		if !ok || signing.VerifyReceipt(pub, r.ContentHash, signing.ReceiptFacts{ReceivedAt: rc.ReceivedAt, MassKg: rc.MassKg, Decision: rc.Decision, Note: rc.Note}, rc.Signature.Value) != nil {
			add("C7", "Receipt signature does not verify", "The receiver should confirm the shipment again.", map[string]any{"keyId": rc.Signature.KeyID})
		}
	}
	return nil
}

// released checks that products leave a plant only after quality has released them.
func released(ctx context.Context, env Env, r *domain.Record, inputs []*domain.Record, add addFn) error {
	if r.Type != domain.TypeShipment {
		return nil
	}
	for i, p := range inputs {
		if p == nil || (p.Type != domain.TypeRecycledLot && p.Type != domain.TypeAdditiveBatch) {
			continue
		}
		d, at, ok, err := env.Release(ctx, p.ID)
		if err != nil {
			return err
		}
		switch {
		case !ok || at.After(r.OccurredAt):
			add("C9", fmt.Sprintf("%s was shipped before quality released it", r.Inputs[i].ID),
				"Record the quality release (or explain the urgent dispatch) before the receiver uses the lot.", map[string]any{"input": p.ID})
		case d == domain.Quarantined:
			add("C9", fmt.Sprintf("%s was shipped while quarantined", r.Inputs[i].ID),
				"Recall the shipment, or release the lot with a reason.", map[string]any{"input": p.ID})
		}
	}
	return nil
}

// Dosage window for dry-process polymer modifiers, % of mix mass: POLYMIX (Spain)
// used 1 %, the RILEM interlaboratory study went to 1.5 %; below 0.2 % the additive
// has no measurable effect.
const (
	DosageMin = 0.2
	DosageMax = 1.5
)

func dosage(r *domain.Record, add addFn) {
	if r.Type != domain.TypeAsphaltLot {
		return
	}
	if d := domain.Num(r.Body, "dosage_pct_mix"); d < DosageMin || d > DosageMax {
		add("C8", fmt.Sprintf("Dosage %.2f %% of mix is outside the %g–%g %% design window", d, DosageMin, DosageMax),
			"Check the dosing scale and the mix design; record the lot as a trial if intended.", map[string]any{"dosage": d})
	}
}

func warmInput(inputs []*domain.Record) bool {
	for _, p := range inputs {
		if p != nil && domain.Str(p.Body, "process") == "warm" {
			return true
		}
	}
	return false
}
