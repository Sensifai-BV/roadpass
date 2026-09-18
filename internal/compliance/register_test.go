package compliance

import (
	"strings"
	"testing"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/store"
)

func good() store.Evidence {
	return store.Evidence{Records: 1000, Signed: 1000, PersonallySigned: 1000, Sealed: 1000, Operational: 1000,
		Passports: 200, PassportsWithInputs: 200, Linkable: 200, Linked: 200, Recycled: 100, RecycledLinked: 100, Lots: 200, LotsTested: 200, LotsReleased: 200, LabTests: 300, LabPassed: 290,
		ShipmentsIn: 100, Receipted: 100, Reviewed: 100, Deliveries: 50, Members: 5, Admins: 1, MembersWithKey: 5,
		OrgVerified: true, OrgKeyOnLedger: true, Limits: 3, Calibrations: 4, AuditEntries: 900}
}

var eu = []*Jurisdiction{&euRegister}

func item(t *testing.T, reg Register, id string) Item {
	t.Helper()
	for _, it := range reg.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("%s not in the register", id)
	return Item{}
}

func TestRegisterAutomatic(t *testing.T) {
	reg := Evaluate(eu, domain.KindRecycler, good(), nil)
	for _, it := range reg.Items {
		if !it.Attest && it.Status != Met {
			t.Errorf("%s = %s (%s), want met", it.ID, it.Status, it.Detail)
		}
		if it.Attest && it.Status != Open {
			t.Errorf("%s = %s, want open (needs a statement)", it.ID, it.Status)
		}
	}
	// Asphalt-only requirements do not apply to a recycler.
	for _, it := range reg.Items {
		if it.Regulation == "cpr" {
			t.Errorf("%s should not apply to a recycler", it.ID)
		}
	}

	e := good()
	e.LotsTested = 150 // 75 %
	e.StaleBlockers, e.OpenBlockers = 2, 3
	reg = Evaluate(eu, domain.KindRecycler, e, nil)
	if s := item(t, reg, "wfd.quality").Status; s != Gap {
		t.Errorf("75%% of lots tested = %s, want gap", s)
	}
	if s := item(t, reg, "wfd.findings").Status; s != Gap {
		t.Errorf("stale blockers = %s, want gap", s)
	}
}

func TestRegisterSimulatedIsCapped(t *testing.T) {
	e := good()
	e.Operational = 0
	reg := Evaluate(eu, domain.KindRecycler, e, nil)
	if !reg.Simulated {
		t.Fatal("register should know the data are simulated")
	}
	it := item(t, reg, "wfd.quality")
	if it.Status != Partial || !strings.Contains(it.Detail, "simulated") {
		t.Errorf("simulated evidence = %s (%s), want partial with a note", it.Status, it.Detail)
	}
	if s := item(t, reg, "gdpr.minimisation").Status; s != Met {
		t.Errorf("design-level GDPR check = %s, want met even on simulated data", s)
	}
}

func TestRegisterAttestations(t *testing.T) {
	att := []store.Attestation{
		{RequirementID: "wfd.permit", Status: "met", Note: "Permit AAI/2024/17 valid to 2031"},
		{RequirementID: "taxonomy.ce", Status: "not_applicable", Note: "Not reporting under the taxonomy"},
		// A statement cannot lift an automatic check; only mark it not applicable.
		{RequirementID: "wfd.quality", Status: "met", Note: "trust me"},
	}
	e := good()
	e.LotsTested = 10
	reg := Evaluate(eu, domain.KindRecycler, e, att)
	if s := item(t, reg, "wfd.permit").Status; s != Met {
		t.Errorf("attested permit = %s, want met", s)
	}
	if s := item(t, reg, "taxonomy.ce").Status; s != NotApplicable {
		t.Errorf("not applicable = %s", s)
	}
	if s := item(t, reg, "wfd.quality").Status; s != Gap {
		t.Errorf("attestation lifted an automatic gap to %s", s)
	}
	// Not-applicable items leave the score's denominator.
	full := Evaluate(eu, domain.KindRecycler, good(), []store.Attestation{
		{RequirementID: "wfd.permit", Status: "met"}, {RequirementID: "espr.retention", Status: "met"},
		{RequirementID: "taxonomy.ce", Status: "not_applicable"}, {RequirementID: "gdpr.dpa", Status: "met"},
	})
	if full.Score != 100 {
		t.Errorf("score = %.1f, want 100", full.Score)
	}
}

func TestOversightOnlyGDPR(t *testing.T) {
	for _, it := range Evaluate(eu, domain.KindReporter, good(), nil).Items {
		if it.Regulation != "gdpr" {
			t.Errorf("%s applies to a reporter", it.ID)
		}
	}
}

func TestAttestationMessageCoversEveryField(t *testing.T) {
	a := store.Attestation{OrgID: "org_a", RequirementID: "wfd.permit", Status: "met", Note: "n", EvidenceURL: "https://x", At: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	base := AttestationMessage(a)
	for _, change := range []func(*store.Attestation){
		func(x *store.Attestation) { x.Note = "m" }, func(x *store.Attestation) { x.Status = "partial" },
		func(x *store.Attestation) { x.EvidenceURL = "" }, func(x *store.Attestation) { x.Period = "2026-09" },
		func(x *store.Attestation) { x.At = x.At.Add(time.Second) }, func(x *store.Attestation) { x.OrgID = "org_b" },
	} {
		b := a
		change(&b)
		if AttestationMessage(b) == base {
			t.Errorf("changing a field did not change the signed message")
		}
	}
	if !strings.HasPrefix(base, "roadpass/v1/attestation\n") {
		t.Errorf("message lacks its domain prefix: %q", base)
	}
	if PreviousMonth(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) != "2025-12" {
		t.Error("previous month across a year boundary")
	}
}

func TestJurisdictionsByCountryAndRegion(t *testing.T) {
	codes := func(country, region string) string {
		var out []string
		for _, j := range For(country, region) {
			out = append(out, j.Code)
		}
		return strings.Join(out, ",")
	}
	for _, c := range []struct{ country, region, want string }{
		{"NL", "Gelderland", "EU"}, {"ES", "", "EU"}, {"NO", "", "EU"}, {"GB", "Scotland", "GB"},
		{"US", "Texas", "US,US-TX"}, {"US", "US-TX", "US,US-TX"}, {"US", "California", "US"}, {"BR", "", ""},
	} {
		if got := codes(c.country, c.region); got != c.want {
			t.Errorf("%s/%s: %q, want %q", c.country, c.region, got, c.want)
		}
	}
	reg := ForOrg("US", "Texas", domain.KindAsphaltPlant, good(), nil)
	if reg.Jurisdiction != "United States (federal) · Texas" || len(reg.Items) == 0 {
		t.Fatalf("Texas asphalt plant register: %q, %d items", reg.Jurisdiction, len(reg.Items))
	}
	for _, it := range reg.Items {
		if !strings.HasPrefix(it.ID, "us.") && !strings.HasPrefix(it.ID, "tx.") {
			t.Errorf("%s in a US register", it.ID)
		}
	}
	if item(t, reg, "us.qa.testing").Status != Met || item(t, reg, "tx.dot.mix").Status != Open {
		t.Error("US checks not judged")
	}
	if empty := ForOrg("BR", "", domain.KindRecycler, good(), nil); empty.Jurisdiction != "" || len(empty.Items) != 0 {
		t.Errorf("no register for BR, got %q", empty.Jurisdiction)
	}
	// A statement is only accepted for a requirement of the organisation's own registers.
	if _, ok := Find("US", "Texas", "wfd.permit"); ok {
		t.Error("EU requirement found for a Texas organisation")
	}
	if _, ok := Find("GB", "", "gb.permit.valid"); !ok {
		t.Error("UK requirement not found")
	}
}

func TestEveryRegisterIsSourced(t *testing.T) {
	for _, j := range Jurisdictions {
		if j.Name == "" || j.Scope == "" || len(j.Countries) == 0 || len(j.Requirements) == 0 {
			t.Errorf("%s incomplete", j.Code)
		}
		for _, rg := range j.Regulations {
			if rg.Act == "" || !strings.HasPrefix(rg.URL, "https://") {
				t.Errorf("%s/%s has no source", j.Code, rg.Key)
			}
		}
		for _, r := range j.Requirements {
			if r.Ref == "" || r.Title == "" || r.Explain == "" {
				t.Errorf("%s incomplete", r.ID)
			}
		}
	}
	// Oversight bodies get data protection only, and only where a law covers it.
	if n := len(ForOrg("GB", "", domain.KindReporter, good(), nil).Items); n == 0 {
		t.Error("UK GDPR should apply to a reporter")
	}
	if n := len(ForOrg("US", "Texas", domain.KindReporter, good(), nil).Items); n != 0 {
		t.Errorf("%d US items for a reporter; there is no federal data-protection law in the set", n)
	}
}

// Sorted reject lots are the origin of the chain: they must not count against
// traceability, and a road section counts as linked through its deliveries.
func TestTraceabilityIgnoresChainOrigin(t *testing.T) {
	e := store.Evidence{Passports: 13, Linkable: 9, Linked: 9, Recycled: 4, RecycledLinked: 4}
	if st, _ := checkTraceability(e); st != Met {
		t.Fatalf("traceability = %s, want met", st)
	}
	if st, _ := checkOrigin(e); st != Met {
		t.Fatalf("origin = %s, want met", st)
	}
	e.Linked = 4
	if st, _ := checkTraceability(e); st != Gap {
		t.Fatalf("traceability with 4 of 9 linked = %s, want gap", st)
	}
}
