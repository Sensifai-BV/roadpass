package ingest

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/keyring"
	"github.com/sensifai/roadpass/internal/signing"
	"github.com/sensifai/roadpass/internal/store"
)

// TestVoidRejectedDelivery runs the paver rejection and the plant's void against
// Postgres. It needs an empty, disposable database (the schema is dropped):
//
//	ROADPASS_TEST_DATABASE_URL=postgres://… go test ./internal/ingest
func TestVoidRejectedDelivery(t *testing.T) {
	url := os.Getenv("ROADPASS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ROADPASS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	local, err := signing.NewVault("0000000000000000000000000000000000000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{St: st, Keys: &keyring.Keyring{St: st, Local: local}}
	now := time.Now().UTC().Truncate(time.Second)
	org := func(id string, k domain.OrgKind) domain.Org {
		o := domain.Org{ID: id, Name: id, Kind: k, Kinds: []domain.OrgKind{k}, Country: "NL", Provenance: domain.ProvOperational,
			Status: domain.OrgVerified, VerifiedAt: &now}
		if err := st.InsertOrg(ctx, o); err != nil {
			t.Fatal(err)
		}
		if err := svc.Keys.EnsureOrgKey(ctx, id); err != nil {
			t.Fatal(err)
		}
		return o
	}
	plant, con := org("org_plant", domain.KindAsphaltPlant), org("org_con", domain.KindContractor)
	owner := org("org_owner", domain.KindRoadOwner)
	all := domain.AllDuties()
	submit := func(o domain.Org, in Input) Result {
		t.Helper()
		res, rowErrs, err := svc.Process(ctx, o, Actor{ID: "test"}, all, "", []Input{in})
		if err != nil || len(rowErrs) > 0 {
			t.Fatalf("submit %s: %v %v", in.Type, err, rowErrs)
		}
		return res[0]
	}
	sec := submit(con, Input{Type: "road_section", Ref: "N-1 km 1", OccurredAt: now.Add(-5 * time.Hour), MassKg: 0, Data: map[string]any{
		"road": "N-1", "km_from": 1.0, "km_to": 2.0, "layer": domain.Layers[0], "thickness_cm": 5.0, "area_m2": 7000.0,
		"works": domain.WorksKinds[0], "mix": "AC 16 surf", "owner_org_id": owner.ID}})
	lot := submit(plant, Input{Type: "asphalt_lot", Ref: "L-1", OccurredAt: now.Add(-4 * time.Hour), MassKg: 48000, Data: map[string]any{
		"mix": "AC 16 surf", "process": domain.MixProcesses[0], "binder_pct": 4.6, "additive_kg": 240.0, "dosage_pct_mix": 0.5, "mixing_temp_c": 165.0}})
	dlv := submit(plant, Input{Type: "delivery", Ref: "T-1", OccurredAt: now.Add(-3 * time.Hour), MassKg: 24000,
		Inputs: []domain.Input{{ID: lot.ID, MassKg: 24000}}, Data: map[string]any{"ticket": "T-1", "section_id": sec.ID}})

	// Voiding before the contractor decided is allowed; voiding someone else's record is not.
	if _, err := svc.VoidInput(ctx, con, dlv.ID, VoidIn{Reason: "x"}); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("contractor voided the plant's delivery: %v", err)
	}
	if _, err := svc.VoidInput(ctx, plant, lot.ID, VoidIn{Reason: "x"}); !errors.Is(err, ErrReview) {
		t.Fatalf("voided a lot a delivery uses: %v", err)
	}

	// The contractor rejects the load at the paver: C4 blocker on the delivery.
	if _, err := svc.Receipt(ctx, con, Actor{ID: "test"}, all, dlv.ID, ReceiptIn{ReceivedAt: now.Add(-2 * time.Hour),
		Decision: domain.Rejected, Note: "118 °C at the paver"}); err != nil {
		t.Fatal(err)
	}
	if fs, _, _ := st.Findings(ctx, store.SystemViewer, store.FindingFilter{RecordID: dlv.ID, Status: "open", Code: "C4"}); len(fs) != 1 {
		t.Fatalf("open C4 after rejection: %+v", fs)
	}
	var queued int
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM reproject WHERE id=$1`, dlv.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("receipt not queued for re-projection: %d %v", queued, err)
	}

	// The plant voids it: a signed version 2 with no mass, sealed like any version.
	if _, err := svc.VoidInput(ctx, plant, dlv.ID, VoidIn{Reason: " "}); !errors.Is(err, ErrReview) {
		t.Fatalf("void without a reason: %v", err)
	}
	in, err := svc.VoidInput(ctx, plant, dlv.ID, VoidIn{Reason: "Returned to the plant, not laid"})
	if err != nil {
		t.Fatal(err)
	}
	v2 := submit(plant, in)
	if v2.Version != 2 || v2.Status != domain.StatusOK || len(v2.Findings) != 0 {
		t.Fatalf("void version: %+v", v2)
	}
	rec, err := st.RecordAny(ctx, dlv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Void() || rec.MassKg != 0 || rec.Signature == nil || rec.PrevHash == "" || rec.VoidReason() != "Returned to the plant, not laid" {
		t.Fatalf("void record: void=%v mass=%v sig=%v prev=%q", rec.Void(), rec.MassKg, rec.Signature != nil, rec.PrevHash)
	}
	if vs, _ := st.Versions(ctx, store.SystemViewer, dlv.ID); len(vs) != 2 || vs[0].MassKg != 24000 || vs[0].Receipt == nil {
		t.Fatalf("old version not kept: %+v", vs)
	}
	if fs, _, _ := st.Findings(ctx, store.SystemViewer, store.FindingFilter{RecordID: dlv.ID, Status: "open"}); len(fs) != 0 {
		t.Fatalf("findings still open after the void: %+v", fs)
	}

	// Lineage, work queues: zero mass, the lot's asphalt is free again, nothing to receive.
	var linkKg float64
	var links int
	if err := st.DB.QueryRow(ctx, `SELECT count(*), COALESCE(sum(mass_kg),0) FROM links WHERE child_id=$1 OR parent_id=$1`, dlv.ID).Scan(&links, &linkKg); err != nil {
		t.Fatal(err)
	}
	if links != 2 || linkKg != 0 {
		t.Fatalf("links of the void delivery: %d edges, %v kg", links, linkKg)
	}
	env := &dbEnv{q: st.DB}
	if used, _ := env.ConsumedKg(ctx, lot.ID, ""); used != 0 {
		t.Fatalf("lot still shows %v kg delivered", used)
	}
	w, err := st.WorkFor(ctx, con, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range w.Receive.Items {
		if it.Record.ID == dlv.ID {
			t.Fatal("void delivery waits for the contractor's receipt")
		}
	}
	if _, err := svc.Receipt(ctx, con, Actor{ID: "test"}, all, dlv.ID, ReceiptIn{ReceivedAt: now, MassKg: 24000}); !errors.Is(err, ErrReview) {
		t.Fatalf("receipt on a void delivery: %v", err)
	}
	if _, err := svc.VoidInput(ctx, plant, dlv.ID, VoidIn{Reason: "again"}); !errors.Is(err, ErrReview) {
		t.Fatalf("voided twice: %v", err)
	}
}
