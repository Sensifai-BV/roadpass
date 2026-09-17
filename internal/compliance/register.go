// Package compliance is the regulatory register, limit reporting and the
// monthly compliance report.
//
// The register is per jurisdiction and data-driven: a Jurisdiction is a set of
// regulations and requirements that applies to organisations in some countries
// (and optionally only some of their regions, for a state's own rules). An
// organisation gets every jurisdiction that covers it — a Texas plant gets the
// US federal set and the Texas set. A country no jurisdiction covers gets an
// empty register (limits and the monthly report still apply). To add one,
// write a jur_<code>.go file with its Jurisdiction and append it to
// Jurisdictions; requirement ids carry the jurisdiction's prefix so signed
// statements never cross borders.
//
// Each requirement is judged either automatically from the organisation's own
// data (Evidence) or by a member's signed statement (attestation), because
// some duties — a declaration of performance, a permit — live outside
// ROADPASS. Automatic judgements never trust simulated data: with no
// operational records they are capped at "partial".
package compliance

import (
	"slices"
	"strings"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/store"
)

// Statuses.
const (
	Met           = "met"
	Partial       = "partial"
	Gap           = "gap"            // automatic check failed
	Open          = "open"           // needs a signed statement
	NotApplicable = "not_applicable" // attested as not applying
)

// Attested statuses a member may sign.
var AttestStatuses = []string{"met", "partial", "not_met", "not_applicable"}

// Requirement is one obligation in the register.
type Requirement struct {
	ID         string           `json:"id"`         // "<jurisdiction prefix>.<name>", unique across jurisdictions
	Regulation string           `json:"regulation"` // key of a Regulation in the same jurisdiction
	Ref        string           `json:"ref"`        // article or clause
	Title      string           `json:"title"`
	Explain    string           `json:"explain"`
	Kinds      []domain.OrgKind `json:"kinds,omitempty"` // empty: every chain organisation
	Attest     bool             `json:"attest"`          // judged by a signed statement only
	auto       func(e store.Evidence) (string, string)
	privacy    bool // set from the regulation: applies to oversight bodies, judged by design
}

// Regulation is a law the register draws from.
type Regulation struct {
	Key, Name, Act, URL string
	// Privacy marks data-protection law: the only kind that applies to
	// oversight bodies, and judged by ROADPASS's design rather than records.
	Privacy bool `json:"Privacy,omitempty"`
}

// Jurisdiction is one set of rules and where it applies.
type Jurisdiction struct {
	Code      string   `json:"code"`  // "EU", "GB", "US", "US-TX"
	Name      string   `json:"name"`  // shown on the page
	Scope     string   `json:"scope"` // what the set covers and leaves out
	Countries []string `json:"-"`     // ISO 3166-1 alpha-2
	// Regions limits the set to subdivisions of Countries (ISO 3166-2 code or
	// the name organisations record); empty means the whole country.
	Regions      []string      `json:"-"`
	Regulations  []Regulation  `json:"-"`
	Requirements []Requirement `json:"-"`
}

// Covers reports whether the jurisdiction applies in a country and region.
func (j *Jurisdiction) Covers(country, region string) bool {
	if !slices.Contains(j.Countries, country) {
		return false
	}
	return len(j.Regions) == 0 || slices.ContainsFunc(j.Regions, func(r string) bool { return strings.EqualFold(r, strings.TrimSpace(region)) })
}

// Jurisdictions are the registers ROADPASS carries, broadest first.
var Jurisdictions = []*Jurisdiction{&euRegister, &gbRegister, &usRegister, &usTexasRegister}

func init() {
	seen := map[string]bool{}
	for _, j := range Jurisdictions {
		priv := map[string]bool{}
		for _, rg := range j.Regulations {
			priv[rg.Key] = rg.Privacy
		}
		for i := range j.Requirements {
			r := &j.Requirements[i]
			if _, ok := priv[r.Regulation]; !ok || seen[r.ID] || (r.auto == nil) != r.Attest {
				panic("compliance: bad requirement " + r.ID + " in " + j.Code)
			}
			seen[r.ID] = true
			r.privacy = priv[r.Regulation]
		}
	}
}

// For returns the jurisdictions that cover an organisation's country and region.
func For(country, region string) []*Jurisdiction {
	var out []*Jurisdiction
	for _, j := range Jurisdictions {
		if j.Covers(country, region) {
			out = append(out, j)
		}
	}
	return out
}

// Available lists every register ROADPASS carries.
func Available() []JurisdictionRef {
	out := make([]JurisdictionRef, len(Jurisdictions))
	for i, j := range Jurisdictions {
		out[i] = JurisdictionRef{j.Code, j.Name, j.Scope}
	}
	return out
}

// Find returns a requirement by id from the registers that apply in a country and region.
func Find(country, region, id string) (Requirement, bool) {
	for _, j := range For(country, region) {
		if i := slices.IndexFunc(j.Requirements, func(r Requirement) bool { return r.ID == id }); i >= 0 {
			return j.Requirements[i], true
		}
	}
	return Requirement{}, false
}

// Applies reports whether a requirement concerns an organisation kind.
func (r Requirement) Applies(k domain.OrgKind) bool {
	if k == domain.KindOperator || k == domain.KindReporter || k == domain.KindAuditor {
		return r.privacy // oversight bodies only process personal data
	}
	return len(r.Kinds) == 0 || slices.Contains(r.Kinds, k)
}

var rank = map[string]int{Gap: 0, Open: 1, Partial: 2, Met: 3, NotApplicable: 4}

func worst(a, b string) string {
	if rank[a] <= rank[b] {
		return a
	}
	return b
}

var (
	plants    = []domain.OrgKind{domain.KindSortingPlant, domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant}
	recyclers = []domain.OrgKind{domain.KindSortingPlant, domain.KindRecycler, domain.KindAdditiveProducer}
	receivers = []domain.OrgKind{domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant, domain.KindContractor}
	roads     = []domain.OrgKind{domain.KindAsphaltPlant, domain.KindContractor, domain.KindRoadOwner}
	asphalt   = []domain.OrgKind{domain.KindAsphaltPlant}
)

// Item is a requirement's state in an organisation's register.
type Item struct {
	Requirement
	Jurisdiction string             `json:"jurisdiction"`
	Status       string             `json:"status"`
	Auto         string             `json:"auto,omitempty"` // the automatic judgement, when there is one
	Detail       string             `json:"detail"`
	Attestation  *store.Attestation `json:"attestation,omitempty"`
}

// JurisdictionRef names a register that applies, for the page.
type JurisdictionRef struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

// Register is an organisation's register with its score.
type Register struct {
	Jurisdiction  string            `json:"jurisdiction"` // names of the registers that apply, "" when none covers the country yet
	Jurisdictions []JurisdictionRef `json:"jurisdictions"`
	Regulations   []Regulation      `json:"regulations"` // in display order
	Items         []Item            `json:"items"`
	Score         float64           `json:"score"` // 0–100: met = 1, partial = ½, over applicable items
	Counts        map[string]int    `json:"counts"`
	Simulated     bool              `json:"simulated"` // no operational data: automatic checks are capped
}

// ForOrg evaluates every register that applies in the organisation's country and region.
func ForOrg(country, region string, k domain.OrgKind, e store.Evidence, latest []store.Attestation) Register {
	return Evaluate(For(country, region), k, e, latest)
}

// Evaluate builds the register of an organisation of kind k from jurisdictions js.
func Evaluate(js []*Jurisdiction, k domain.OrgKind, e store.Evidence, latest []store.Attestation) Register {
	att := map[string]*store.Attestation{}
	for i := range latest {
		if latest[i].Period == "" {
			att[latest[i].RequirementID] = &latest[i]
		}
	}
	reg := Register{Jurisdictions: []JurisdictionRef{}, Regulations: []Regulation{}, Items: []Item{}, Counts: map[string]int{},
		Simulated: e.Records > 0 && e.Operational == 0}
	var names []string
	var points, total float64
	for _, j := range js {
		names = append(names, j.Name)
		reg.Jurisdictions = append(reg.Jurisdictions, JurisdictionRef{j.Code, j.Name, j.Scope})
		reg.Regulations = append(reg.Regulations, j.Regulations...)
		for _, r := range j.Requirements {
			if !r.Applies(k) {
				continue
			}
			it := Item{Requirement: r, Jurisdiction: j.Code, Attestation: att[r.ID]}
			if r.auto != nil {
				it.Auto, it.Detail = r.auto(e)
				if reg.Simulated && it.Auto == Met && !r.privacy {
					it.Auto = Partial
					it.Detail += " Based on simulated demo data only; operational records are needed to count as met."
				}
				it.Status = it.Auto
			} else {
				it.Status, it.Detail = Open, "Needs a signed statement from a responsible person."
			}
			if a := it.Attestation; a != nil {
				switch {
				case a.Status == "not_applicable":
					it.Status = NotApplicable
				case r.Attest:
					it.Status = map[string]string{"met": Met, "partial": Partial, "not_met": Gap}[a.Status]
					it.Detail = "Signed statement: " + a.Note
				}
			}
			reg.Counts[it.Status]++
			if it.Status != NotApplicable {
				total++
				points += map[string]float64{Met: 1, Partial: 0.5}[it.Status]
			}
			reg.Items = append(reg.Items, it)
		}
	}
	reg.Jurisdiction = strings.Join(names, " · ")
	if total > 0 {
		reg.Score = 100 * points / total
	}
	return reg
}
