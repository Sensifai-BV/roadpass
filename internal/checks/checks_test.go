package checks

import (
	"context"
	"testing"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/signing"
)

type fakeEnv struct {
	recs     map[string]*domain.Record
	shipped  map[string]int64 // lot|org -> received unix (0 = in transit)
	consumed map[string]float64
	keys     map[string][]byte
	laid     map[string]float64
}

func (f *fakeEnv) Record(_ context.Context, id string) (*domain.Record, bool, error) {
	r, ok := f.recs[id]
	return r, ok, nil
}
func (f *fakeEnv) ReceivedShipment(_ context.Context, lot, org string) (bool, int64, error) {
	t, ok := f.shipped[lot+"|"+org]
	return ok, t, nil
}
func (f *fakeEnv) ConsumedKg(_ context.Context, p, _ string) (float64, error) {
	return f.consumed[p], nil
}
func (f *fakeEnv) LaidKg(_ context.Context, s string) (float64, error) {
	return f.laid[s], nil
}
func (f *fakeEnv) DuplicateOf(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}
func (f *fakeEnv) Release(context.Context, string) (string, time.Time, bool, error) {
	return "released", t0, true, nil
}
func (f *fakeEnv) PublicKey(_ context.Context, id string) ([]byte, bool, error) {
	k, ok := f.keys[id]
	return k, ok, nil
}

var t0 = time.Date(2025, 5, 1, 8, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*fakeEnv, *signing.Key) {
	key, _ := signing.NewKey("org_rec:k1", "org_rec", nil)
	env := &fakeEnv{
		recs: map[string]*domain.Record{
			"PRL-1": {ID: "PRL-1", Type: domain.TypeRejectLot, OrgID: "org_sort", OccurredAt: t0, MassKg: 20000},
		},
		shipped:  map[string]int64{"PRL-1|org_rec": t0.Add(24 * time.Hour).Unix()},
		consumed: map[string]float64{},
		keys:     map[string][]byte{"org_rec:k1": key.Public},
	}
	return env, key
}

func recycled(t *testing.T, key *signing.Key, mutate func(r *domain.Record)) *domain.Record {
	r := &domain.Record{ID: "RPL-1", Version: 1, Type: domain.TypeRecycledLot, OrgID: "org_rec", SiteID: "s1", Ref: "X",
		OccurredAt: t0.Add(48 * time.Hour), MassKg: 15000, Inputs: []domain.Input{{ID: "PRL-1", MassKg: 18000}},
		Body:       map[string]any{"form": "agglomerate", "mfi_g10min": 1.2, "moisture_pct": 0.5, "energy_kwh": 4000.0},
		Provenance: domain.ProvSimulated}
	if mutate != nil {
		mutate(r)
	}
	r.ContentHash, _ = r.ComputeHash()
	sig, _ := key.Sign(r.ContentHash)
	r.Signature = &domain.Signature{KeyID: key.ID, OrgID: "org_rec", Value: sig, SignedAt: r.OccurredAt}
	return r
}

func codes(fs []Finding) map[string]bool {
	m := map[string]bool{}
	for _, f := range fs {
		m[f.Code] = true
	}
	return m
}

func TestCleanRecordHasNoFindings(t *testing.T) {
	env, key := setup(t)
	fs, err := Run(context.Background(), env, recycled(t, key, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("unexpected findings: %+v", fs)
	}
	if StatusFor(fs) != domain.StatusOK {
		t.Fatal("clean record not ok")
	}
}

func TestEachRuleFires(t *testing.T) {
	cases := []struct {
		name   string
		code   string
		mutate func(*fakeEnv, *domain.Record)
	}{
		{"missing evidence", "C1", func(_ *fakeEnv, r *domain.Record) { delete(r.Body, "mfi_g10min") }},
		{"before input", "C3", func(_ *fakeEnv, r *domain.Record) { r.OccurredAt = t0.Add(-time.Hour) }},
		{"never shipped", "C4", func(e *fakeEnv, _ *domain.Record) { delete(e.shipped, "PRL-1|org_rec") }},
		{"unknown input", "C4", func(_ *fakeEnv, r *domain.Record) { r.Inputs[0].ID = "PRL-404" }},
		{"output > input", "C5", func(_ *fakeEnv, r *domain.Record) { r.MassKg = 19000 }},
		{"lot over-used", "C5", func(e *fakeEnv, _ *domain.Record) { e.consumed["PRL-1"] = 5000 }},
		{"spec", "C6", func(_ *fakeEnv, r *domain.Record) { r.Body["moisture_pct"] = 4.0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, key := setup(t)
			var r *domain.Record
			r = recycled(t, key, func(x *domain.Record) { c.mutate(env, x) })
			fs, err := Run(context.Background(), env, r)
			if err != nil {
				t.Fatal(err)
			}
			if !codes(fs)[c.code] {
				t.Fatalf("want %s, got %+v", c.code, fs)
			}
		})
	}
}

func TestSignatureTamperIsBlocker(t *testing.T) {
	env, key := setup(t)
	r := recycled(t, key, nil)
	r.MassKg = 14000 // changed after signing
	r.ContentHash, _ = r.ComputeHash()
	fs, _ := Run(context.Background(), env, r)
	if !codes(fs)["C7"] || StatusFor(fs) != domain.StatusBlocked {
		t.Fatalf("tampered record not blocked: %+v", fs)
	}
}

func TestDosageWindow(t *testing.T) {
	r := &domain.Record{Type: domain.TypeAsphaltLot, Body: map[string]any{"dosage_pct_mix": 2.4, "process": "hot", "mixing_temp_c": 165.0}}
	var got []string
	dosage(r, func(code, _, _ string, _ map[string]any) { got = append(got, code) })
	if len(got) != 1 || got[0] != "C8" {
		t.Fatalf("dosage 14 %% not flagged: %v", got)
	}
}

func TestVoidOnlyJudgesSignature(t *testing.T) {
	env, key := setup(t)
	env.consumed["PRL-1"] = 20000 // the input is used up: a live record would trip C5
	void := func(r *domain.Record) {
		r.Version, r.MassKg, r.Inputs = 2, 0, []domain.Input{{ID: "PRL-1"}}
		r.Body = map[string]any{"form": "agglomerate", domain.FieldVoid: true, domain.FieldVoidReason: "entered twice"}
		r.OccurredAt = t0.Add(-time.Hour) // before its input: C3 on a live record
	}
	fs, err := Run(context.Background(), env, recycled(t, key, void))
	if err != nil || len(fs) != 0 {
		t.Fatalf("void: findings %v, err %v", fs, err)
	}
	r := recycled(t, key, void)
	r.Signature.Value[0] ^= 1
	fs, _ = Run(context.Background(), env, r)
	if !codes(fs)["C7"] || len(fs) != 1 {
		t.Fatalf("tampered void: %v", fs)
	}
}

// A completed section may not claim more asphalt than the deliveries onto it that
// count: a load rejected at the paver was not laid.
func TestAsLaidMassAgainstAcceptedDeliveries(t *testing.T) {
	sec := &domain.Record{ID: "RSC-1", Type: domain.TypeRoadSection, MassKg: 50000,
		Body: map[string]any{"completed_at": "2026-10-16T10:00:00Z"}}
	env := &fakeEnv{laid: map[string]float64{"RSC-1": 50200}}
	fs, err := Run(context.Background(), env, sec)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if f.Code == "C5" {
			t.Fatalf("accepted deliveries cover the section, got %s", f.Message)
		}
	}
	env.laid["RSC-1"] = 25000 // one of two loads rejected
	fs, _ = Run(context.Background(), env, sec)
	var hit *Finding
	for i := range fs {
		if fs[i].Code == "C5" {
			hit = &fs[i]
		}
	}
	if hit == nil || hit.Severity != Blocker || hit.Detail["acceptedDeliveriesKg"] != 25000.0 {
		t.Fatalf("want a C5 blocker against 25 t accepted, got %+v", fs)
	}
	// A planned section (not completed) is not judged yet.
	delete(sec.Body, "completed_at")
	if _, bad := AsLaid(sec, 0); !bad {
		t.Fatal("AsLaid judges the mass it is given")
	}
	fs, _ = Run(context.Background(), env, sec)
	if codes(fs)["C5"] {
		t.Fatal("planned section judged against deliveries")
	}
}

// A company with several sites ships between them: the lot's holder is also its
// user, but the receiving site's intake still has to sign for it, and a load it
// rejected never enters stock.
func TestOwnSiteTransferPassesTheIntakeGate(t *testing.T) {
	cases := []struct {
		name    string
		shipped map[string]int64
		want    string // "" = no C4
	}{
		{"never moved between sites", nil, ""},
		{"received", map[string]int64{"PRL-1|org_rec": t0.Add(24 * time.Hour).Unix()}, ""},
		{"rejected at intake", map[string]int64{"PRL-1|org_rec": -1}, "C4"},
		{"not yet received", map[string]int64{"PRL-1|org_rec": 0}, "C4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, key := setup(t)
			env.recs["PRL-1"].OrgID = "org_rec" // the company made the lot itself
			env.shipped = c.shipped
			fs, err := Run(context.Background(), env, recycled(t, key, nil))
			if err != nil {
				t.Fatal(err)
			}
			if got := codes(fs)["C4"]; got != (c.want == "C4") {
				t.Fatalf("C4 = %v, want %q: %+v", got, c.want, fs)
			}
		})
	}
}

// Declared recycled content and the recycled inputs must agree both ways: inputs
// above the declaration are as wrong as a declaration above the inputs.
func TestAdditiveDeclarationMatchesInputs(t *testing.T) {
	env, _ := setup(t)
	akey, _ := signing.NewKey("org_rec:k1", "org_rec", nil)
	env.keys["org_rec:k1"] = akey.Public
	env.recs["RPL-1"] = &domain.Record{ID: "RPL-1", Type: domain.TypeRecycledLot, OrgID: "org_rec", OccurredAt: t0, MassKg: 20000}
	batch := func(inKg float64) *domain.Record {
		r := &domain.Record{ID: "ADB-1", Version: 1, Type: domain.TypeAdditiveBatch, OrgID: "org_rec", SiteID: "s1", Ref: "B",
			OccurredAt: t0.Add(48 * time.Hour), MassKg: 14000, Inputs: []domain.Input{{ID: "RPL-1", MassKg: inKg}},
			Body:       map[string]any{"product": "P", "form": "pellets", "recycled_content_pct": 92.0, "mfi_g10min": 2.0, "melting_point_c": 111.0, "energy_kwh": 3000.0},
			Provenance: domain.ProvSimulated}
		r.ContentHash, _ = r.ComputeHash()
		sig, _ := akey.Sign(r.ContentHash)
		r.Signature = &domain.Signature{KeyID: akey.ID, OrgID: "org_rec", Value: sig, SignedAt: r.OccurredAt}
		return r
	}
	for _, c := range []struct {
		inKg float64
		c5   bool
	}{{12880, false}, {14000, true}, {11000, true}} {
		fs, err := Run(context.Background(), env, batch(c.inKg))
		if err != nil {
			t.Fatal(err)
		}
		if codes(fs)["C5"] != c.c5 {
			t.Errorf("inputs %v kg for a 92 %% claim on 14 t: C5 = %v, want %v (%+v)", c.inKg, codes(fs)["C5"], c.c5, fs)
		}
	}
}
