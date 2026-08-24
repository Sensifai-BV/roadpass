package domain

import "testing"

func TestRoleTitle(t *testing.T) {
	cases := []struct {
		k      OrgKind
		access string
		duties []string
		want   string
	}{
		{KindRecycler, "admin", AllDuties(), "Administrator"},
		{KindRecycler, "editor", []string{"receive"}, "Intake officer"},
		{KindRecycler, "editor", []string{"produce"}, "Recycling operator"},
		{KindAsphaltPlant, "editor", []string{"produce"}, "Mixing operator"},
		{KindRecycler, "editor", []string{"release", "test"}, "Quality manager"},
		{KindRecycler, "editor", []string{"ship", "receive"}, "Intake & Dispatch"},
		{KindRecycler, "viewer", nil, "Reporter"},
		{KindReporter, "viewer", nil, "Reporter"},
		{KindOperator, "admin", AllDuties(), "Platform manager"},
	}
	for _, c := range cases {
		if got := RoleTitle(c.k, c.access, c.duties); got != c.want {
			t.Errorf("RoleTitle(%s, %s, %v) = %q, want %q", c.k, c.access, c.duties, got, c.want)
		}
	}
	for _, k := range OrgKinds {
		seen := map[string]bool{}
		for _, r := range JobRoles(k) {
			if seen[r.Key] {
				t.Errorf("%s: duplicate role key %s", k, r.Key)
			}
			seen[r.Key] = true
			for _, d := range r.Duties {
				if !Duty(d).Valid() {
					t.Errorf("%s/%s: unknown duty %s", k, r.Key, d)
				}
			}
		}
	}
}

func TestJobRolesFor(t *testing.T) {
	roles := JobRolesFor([]OrgKind{KindAsphaltPlant, KindContractor})
	keys := map[string]bool{}
	for _, r := range roles {
		if keys[r.Key] {
			t.Errorf("duplicate key %s", r.Key)
		}
		keys[r.Key] = true
	}
	// The plant's mixing operator and the contractor's paving lead grant the same
	// duty: one role, named after the primary activity.
	if got := RoleTitleFor([]OrgKind{KindAsphaltPlant, KindContractor}, "editor", []string{"produce"}); got != "Mixing operator" {
		t.Errorf("produce = %q", got)
	}
	// The contractor adds its site manager.
	if got := RoleTitleFor([]OrgKind{KindAsphaltPlant, KindContractor}, "editor", []string{"test", "produce"}); got != "Site manager" {
		t.Errorf("produce+test = %q", got)
	}
}

func TestInSteps(t *testing.T) {
	steps := []string{string(KindAsphaltPlant)}
	if !InSteps(TypeAsphaltLot, steps) || !InSteps(TypeDelivery, steps) || !InSteps(TypeLabTest, steps) {
		t.Error("an asphalt member may sign asphalt lots, deliveries and lab tests")
	}
	if InSteps(TypeRoadSection, steps) {
		t.Error("an asphalt member may not sign road sections (the laying section)")
	}
	if !InSteps(TypeRoadSection, nil) {
		t.Error("no sections = every section")
	}
}
