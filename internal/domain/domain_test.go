package domain

import "testing"

func TestOrgIsCountsEveryActivity(t *testing.T) {
	o := Org{Kind: KindRecycler, Kinds: []OrgKind{KindRecycler, KindAsphaltPlant}}
	if !o.Is(KindRecycler) || !o.Is(KindAsphaltPlant) || o.Is(KindContractor) {
		t.Fatal("Is must see the main kind and the added activities, and nothing else")
	}
}
