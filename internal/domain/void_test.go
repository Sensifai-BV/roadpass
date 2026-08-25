package domain

import (
	"testing"
	"time"
)

func voidDelivery() *Record {
	return &Record{ID: "DLV-26-000001", Type: TypeDelivery, Ref: "T-1", OccurredAt: time.Now().Add(-time.Hour), Provenance: ProvOperational,
		Inputs: []Input{{ID: "ASL-26-000001"}},
		Body:   map[string]any{"ticket": "T-1", "section_id": "RSC-26-000001", FieldVoid: true, FieldVoidReason: "Not laid"}}
}

func TestValidateVoid(t *testing.T) {
	r := voidDelivery()
	if errs := Validate(r); len(errs) != 0 {
		t.Fatalf("void delivery rejected: %v", errs)
	}
	if !r.Void() || r.VoidReason() != "Not laid" || r.CountsMass() {
		t.Fatalf("void = %v, reason %q, counts %v", r.Void(), r.VoidReason(), r.CountsMass())
	}
	for name, mutate := range map[string]func(*Record){
		"mass":      func(r *Record) { r.MassKg = 20000 },
		"input":     func(r *Record) { r.Inputs[0].MassKg = 20000 },
		"no reason": func(r *Record) { delete(r.Body, FieldVoidReason) },
		"not true":  func(r *Record) { r.Body[FieldVoid] = "yes" },
		"new":       func(r *Record) { r.ID = "" },
	} {
		r := voidDelivery()
		mutate(r)
		if errs := Validate(r); len(errs) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	// Without the flag, zero-mass inputs stay invalid.
	r = voidDelivery()
	delete(r.Body, FieldVoid)
	delete(r.Body, FieldVoidReason)
	if errs := Validate(r); len(errs) == 0 {
		t.Fatal("zero-mass input accepted on a live record")
	}
}

func TestCountsMass(t *testing.T) {
	d := &Record{Type: TypeDelivery, MassKg: 20000}
	if !d.CountsMass() {
		t.Fatal("open delivery should count")
	}
	d.Receipt = &Receipt{Decision: AcceptedWithNote}
	if !d.CountsMass() {
		t.Fatal("accepted delivery should count")
	}
	d.Receipt.Decision = Rejected
	if d.CountsMass() {
		t.Fatal("rejected delivery should not count")
	}
	s := &Record{Type: TypeShipment, MassKg: 20000, Receipt: &Receipt{Decision: Rejected}}
	if !s.CountsMass() {
		t.Fatal("a rejected shipment still carried its mass (the lot exists)")
	}
}
