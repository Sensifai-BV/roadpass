package domain

import "slices"

// Duty is a job inside an organisation's step of the chain. A company's people
// (and systems, through API keys) each hold some duties; a record can only be
// submitted by someone holding the duty it belongs to. At a recycler, intake
// receives shipments, production records recycled lots, quality tests and
// releases them, and dispatch ships them on.
type Duty string

const (
	DutyReceive Duty = "receive" // confirm incoming shipments (signed receipt)
	DutyProduce Duty = "produce" // record lots, batches, asphalt lots, road works
	DutyTest    Duty = "test"    // record lab tests
	DutyRelease Duty = "release" // release or quarantine a lot; accept findings
	DutyShip    Duty = "ship"    // record shipments and deliveries
)

// Duties in workflow order.
var Duties = []Duty{DutyReceive, DutyProduce, DutyTest, DutyRelease, DutyShip}

// DutyInfo describes a duty for the UI.
var DutyInfo = map[Duty]struct{ Name, Explain string }{
	DutyReceive: {"Intake", "Confirms incoming shipments at the weighbridge; the receipt is signed."},
	DutyProduce: {"Production", "Records what the plant makes from received material."},
	DutyTest:    {"Quality testing", "Records lab results on lots and batches."},
	DutyRelease: {"Quality release", "Releases lots for dispatch, or quarantines them."},
	DutyShip:    {"Dispatch", "Ships released lots to the next company and records deliveries."},
}

// Valid reports whether d is a known duty.
func (d Duty) Valid() bool { return slices.Contains(Duties, d) }

// DutyFor is the duty a record type needs.
func DutyFor(t RecordType) Duty {
	switch t {
	case TypeShipment, TypeDelivery:
		return DutyShip
	case TypeLabTest:
		return DutyTest
	case TypeRelease, TypeAcceptance:
		return DutyRelease
	}
	return DutyProduce
}

// HasDuty reports whether a duty list grants d (an empty list grants nothing).
func HasDuty(have []string, d Duty) bool { return slices.Contains(have, string(d)) }

// AllDuties as strings (admins and default keys).
func AllDuties() []string {
	out := make([]string, len(Duties))
	for i, d := range Duties {
		out[i] = string(d)
	}
	return out
}

// Produces is the passport record type an organisation kind makes.
var Produces = map[OrgKind]RecordType{
	KindSortingPlant: TypeRejectLot, KindRecycler: TypeRecycledLot, KindAdditiveProducer: TypeAdditiveBatch,
	KindAsphaltPlant: TypeAsphaltLot, KindContractor: TypeRoadSection,
}

// Consumes is the record type an organisation kind takes in (nil: none).
var Consumes = map[OrgKind]RecordType{
	KindRecycler: TypeRejectLot, KindAdditiveProducer: TypeRecycledLot, KindAsphaltPlant: TypeAdditiveBatch,
}

// Release decisions.
const (
	Released    = "released"
	Quarantined = "quarantined"
)
