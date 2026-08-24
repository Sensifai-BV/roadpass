package domain

import (
	"slices"
	"strings"
)

// JobRole is a named role in a company: what people call the job (intake
// officer, recycling operator, quality manager…) and the access and duties it
// grants. Access and duties are what the server checks; the role is how admins
// pick them and how everyone reads them.
type JobRole struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Explain string   `json:"explain"`
	Access  string   `json:"access"` // admin | editor | viewer
	Duties  []string `json:"duties"`
}

var plants = []OrgKind{KindRecycler, KindAdditiveProducer, KindAsphaltPlant}

// operatorName is the production role at each step of the chain.
var operatorName = map[OrgKind]string{
	KindSortingPlant: "Sorting operator", KindRecycler: "Recycling operator", KindAdditiveProducer: "Compounding operator",
	KindAsphaltPlant: "Mixing operator", KindContractor: "Paving crew lead",
}

// JobRoles lists the roles an organisation of kind k can hand out, admin first.
func JobRoles(k OrgKind) []JobRole {
	admin := JobRole{"admin", "Administrator", "Manages the team, roles, keys and settings; can do every job.", "admin", AllDuties()}
	if k == KindOperator {
		return []JobRole{
			{"admin", "Platform manager", "Manages organisations, users, plans and billing for the whole platform.", "admin", AllDuties()},
			{"support", "Platform support", "Reads every organisation's data to help them; changes nothing.", "viewer", nil},
		}
	}
	if k == KindReporter {
		return []JobRole{{"reporter", "Reporter", "Follows the whole chain and its figures for a programme or regulator; read-only.", "viewer", nil}}
	}
	if k == KindAuditor {
		return []JobRole{admin, {"auditor", "Auditor", "Checks passports, evidence and seals across the chain; read-only.", "viewer", nil}}
	}
	out := []JobRole{admin}
	in := func(ks ...OrgKind) bool { return slices.Contains(ks, k) }
	if in(append(plants, KindContractor)...) {
		out = append(out, JobRole{"intake", "Intake officer", "Checks and signs incoming loads at the weighbridge: accept, accept with a note, or reject.", "editor", []string{"receive"}})
	}
	if name, ok := operatorName[k]; ok {
		out = append(out, JobRole{"operator", name, "Records what the site makes and issues its passports for the next step.", "editor", []string{"produce"}})
	}
	if k == KindSortingPlant {
		out = append(out, JobRole{"supervisor", "Sorting supervisor", "Records sorted lots and their quality tests.", "editor", []string{"produce", "test"}})
	}
	if k == KindContractor {
		out = append(out, JobRole{"site", "Site manager", "Records road works and the tests on site.", "editor", []string{"produce", "test"}})
	}
	if in(append(plants, KindSortingPlant, KindContractor, KindLab)...) {
		out = append(out, JobRole{"lab", "Lab technician", "Records lab results on lots and batches.", "editor", []string{"test"}})
	}
	if in(KindRecycler, KindAdditiveProducer) {
		out = append(out, JobRole{"quality", "Quality manager", "Tests lots and releases them for dispatch, or quarantines them.", "editor", []string{"test", "release"}})
	}
	if k == KindRoadOwner {
		// The road owner is the independent party for its roads: it accepts
		// data-check findings on sections laid on them (a signed acceptance).
		out = append(out, JobRole{"assets", "Road asset manager", "Reviews the sections laid on your roads and accepts data-check findings as the independent party.", "editor", []string{"release"}})
	}
	if in(append(plants, KindSortingPlant)...) {
		out = append(out, JobRole{"dispatch", "Dispatcher", "Ships released lots to the next company.", "editor", []string{"ship"}})
	}
	return append(out, JobRole{"reporter", "Reporter", "Follows and reports on the supply chain, the passports and the state of the company's sections; downloads reports. Enters nothing.", "viewer", nil})
}

// JobRolesFor lists the roles of an organisation with several activities (an
// asphalt plant that also paves roads): the primary kind's roles first, then
// any role the others add. Two roles granting the same access and duties are
// one role, named after the primary activity.
func JobRolesFor(kinds []OrgKind) []JobRole {
	var out []JobRole
	seen := map[string]bool{}
	for _, k := range kinds {
		for _, r := range JobRoles(k) {
			ds := slices.Clone(r.Duties)
			slices.Sort(ds)
			id := r.Access + ":" + strings.Join(ds, ",")
			if seen[id] {
				continue
			}
			seen[id] = true
			if slices.ContainsFunc(out, func(x JobRole) bool { return x.Key == r.Key }) {
				r.Key += "_" + string(k)
			}
			out = append(out, r)
		}
	}
	return out
}

// RoleKeyFor is the key of the named job role whose access and duties match a
// member exactly ("" when an admin combined duties freely).
func RoleKeyFor(kinds []OrgKind, access string, duties []string) string {
	for _, r := range JobRolesFor(kinds) {
		if r.Access == access && (access != "editor" || sameSet(r.Duties, duties)) {
			return r.Key
		}
	}
	return ""
}

// RoleTitle names what a member does: the matching job role, or the duties
// they hold when an admin combined them freely.
func RoleTitle(k OrgKind, access string, duties []string) string {
	return RoleTitleFor([]OrgKind{k}, access, duties)
}

// RoleTitleFor is RoleTitle for an organisation with several activities.
func RoleTitleFor(kinds []OrgKind, access string, duties []string) string {
	for _, r := range JobRolesFor(kinds) {
		if r.Access == access && (access != "editor" || sameSet(r.Duties, duties)) {
			return r.Name
		}
	}
	if access == "editor" && len(duties) > 0 {
		names := make([]string, 0, len(duties))
		for _, d := range Duties {
			if slices.Contains(duties, string(d)) {
				names = append(names, DutyInfo[d].Name)
			}
		}
		return strings.Join(names, " & ")
	}
	if access == "admin" {
		return "Administrator"
	}
	return "Reporter"
}

// RoleTitleIn is RoleTitleFor for a member assigned to some sections of the
// chain (steps): an editor's role is named after those sections, so a member of
// a sorting-and-mixing company who only mixes is a Mixing operator, not the
// Sorting operator the combined catalogue names the same duties after.
func RoleTitleIn(kinds []OrgKind, steps []string, access string, duties []string) string {
	if access == "editor" && len(steps) > 0 {
		ks := make([]OrgKind, len(steps))
		for i, st := range steps {
			ks[i] = OrgKind(st)
		}
		for _, r := range JobRolesFor(ks) {
			if r.Access == access && sameSet(r.Duties, duties) {
				return r.Name
			}
		}
	}
	return RoleTitleFor(kinds, access, duties)
}

// InSteps reports whether a member assigned to steps may sign records of type
// t: the record must belong to one of their sections. No steps = every section.
func InSteps(t RecordType, steps []string) bool {
	if len(steps) == 0 {
		return true
	}
	for _, k := range Types[t].CreatedBy {
		if slices.Contains(steps, string(k)) {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}

// AllJobRoles is the catalogue for every organisation kind.
func AllJobRoles() map[OrgKind][]JobRole {
	out := make(map[OrgKind][]JobRole, len(OrgKinds))
	for _, k := range OrgKinds {
		out[k] = JobRoles(k)
	}
	return out
}
