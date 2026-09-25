package ngsild

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
)

func sample() *domain.Record {
	return &domain.Record{
		ID: "RPL-24-000001", Version: 1, Type: domain.TypeRecycledLot, OrgID: "org_rec1", SiteID: "site_rec1a",
		Ref: "L-1", OccurredAt: time.Date(2025, 3, 4, 10, 0, 0, 0, time.UTC), MassKg: 18250,
		Inputs:     []domain.Input{{ID: "PRL-24-000010", MassKg: 12000}, {ID: "PRL-24-000011", MassKg: 9800}},
		Body:       map[string]any{"form": "agglomerate", "mfi_g10min": 1.4, "moisture_pct": 0.6},
		Provenance: domain.ProvSimulated,
	}
}

func roundTrip(t *testing.T, e Entity) Entity {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var out Entity
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The fingerprint must not depend on the wire format.
func TestNormalizedAndConciseHashLikeTheRecord(t *testing.T) {
	r := sample()
	want, _ := r.ComputeHash()

	norm := roundTrip(t, FromRecord(r, &Location{40.4, -3.7}, &Proof{ContentHash: want}, nil))
	got, err := ToRecord(norm, "org_rec1")
	if err != nil {
		t.Fatal(err)
	}
	got.Version = 1
	if h, _ := got.ComputeHash(); h != want {
		t.Fatalf("normalized round trip changed the hash\n got %s\nwant %s", h, want)
	}

	concise := roundTrip(t, Entity{
		"id": "urn:ngsi-ld:RecycledPlasticLot:RPL-24-000001", "type": "RecycledPlasticLot",
		"reference": "L-1", "dateObserved": "2025-03-04T10:00:00Z",
		"mass":     map[string]any{"value": 18.25, "unitCode": "TNE"},
		"issuedBy": map[string]any{"object": "urn:ngsi-ld:Organization:org_rec1"},
		"atSite":   map[string]any{"object": "urn:ngsi-ld:Site:site_rec1a"},
		"hasInput": []any{
			map[string]any{"object": "urn:ngsi-ld:PlasticRejectLot:PRL-24-000010", "mass": 12000.0},
			map[string]any{"object": "urn:ngsi-ld:PlasticRejectLot:PRL-24-000011", "mass": 9800.0},
		},
		"provenance": "Simulated", "form": "agglomerate", "mfiG10min": 1.4, "moisturePct": 0.6,
	})
	got2, err := ToRecord(concise, "org_rec1")
	if err != nil {
		t.Fatal(err)
	}
	got2.Version = 1
	if errs := domain.Validate(got2); len(errs) > 0 {
		t.Fatalf("concise entity invalid: %v", errs)
	}
	if h, _ := got2.ComputeHash(); h != want {
		t.Fatalf("concise form changed the hash\n got %s\nwant %s", h, want)
	}
}

func TestRejects(t *testing.T) {
	cases := map[string]Entity{
		"unknown type":     {"type": "Car"},
		"foreign issuer":   {"type": "RecycledPlasticLot", "issuedBy": map[string]any{"object": "urn:ngsi-ld:Organization:other"}},
		"unknown attr":     {"type": "RecycledPlasticLot", "colour": "red"},
		"bad unit":         {"type": "RecycledPlasticLot", "mass": map[string]any{"value": 1.0, "unitCode": "LBR"}},
		"id of other type": {"type": "RecycledPlasticLot", "id": "urn:ngsi-ld:AsphaltLot:x"},
		"input without mass": {"type": "RecycledPlasticLot",
			"hasInput": map[string]any{"type": "Relationship", "object": "urn:ngsi-ld:PlasticRejectLot:x"}},
	}
	for name, e := range cases {
		if _, err := ToRecord(e, "org_rec1"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestContextCheck(t *testing.T) {
	self := "https://roadpass.example" + ContextPath
	if CheckContext([]any{self, CoreContext}, self) != nil {
		t.Fatal("own + core context refused")
	}
	if err := CheckContext("https://example.org/other.jsonld", self); err == nil {
		t.Fatal("foreign context accepted")
	}
}

func TestKeyValues(t *testing.T) {
	kv := KeyValues(roundTrip(t, FromRecord(sample(), nil, nil, nil)))
	if kv["mass"] != 18250.0 || kv["issuedBy"] != "urn:ngsi-ld:Organization:org_rec1" || kv["dateObserved"] != "2025-03-04T10:00:00Z" {
		t.Fatalf("keyValues: %v", kv)
	}
}

func TestSignerNoteIsPartOfTheFingerprint(t *testing.T) {
	r := sample()
	before, _ := r.ComputeHash()
	r.Note = "Bales wet on arrival; dried 24 h before extrusion."
	with, _ := r.ComputeHash()
	if with == before {
		t.Fatal("a note must change the fingerprint (it is signed content)")
	}
	got, err := ToRecord(roundTrip(t, FromRecord(r, nil, nil, nil)), r.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	got.Version = r.Version
	if h, _ := got.ComputeHash(); h != with {
		t.Fatalf("the note was lost on the NGSI-LD round trip")
	}
	if d1, _ := r.DupHash(); func() bool { r.Note = ""; d2, _ := r.DupHash(); return d1 != d2 }() {
		t.Fatal("the duplicate fingerprint must ignore the note")
	}
}
