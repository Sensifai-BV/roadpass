// Package domain holds the ROADPASS vocabulary: organisation kinds, record types,
// the supply-chain stages they belong to, and the rules a record must satisfy
// before it is hashed, signed and stored.
//
// Names are business words (docs/ARCHITECTURE.md §1). The technical term from the
// proposal lives only in the glossary.
package domain

import (
	"slices"
	"time"
)

// OrgKind is the role an organisation plays in the chain.
type OrgKind string

const (
	KindSortingPlant     OrgKind = "sorting_plant"
	KindRecycler         OrgKind = "recycler"
	KindAdditiveProducer OrgKind = "additive_producer"
	KindAsphaltPlant     OrgKind = "asphalt_plant"
	KindContractor       OrgKind = "contractor"
	KindRoadOwner        OrgKind = "road_owner"
	KindLab              OrgKind = "lab"
	KindAuditor          OrgKind = "auditor"
	KindReporter         OrgKind = "reporter" // read-only view of the whole chain (programme office, regulator)
	KindOperator         OrgKind = "operator"
)

// OrgKinds in chain order.
var OrgKinds = []OrgKind{KindSortingPlant, KindRecycler, KindAdditiveProducer, KindAsphaltPlant,
	KindContractor, KindRoadOwner, KindLab, KindAuditor, KindReporter, KindOperator}

// Valid reports whether k is a known kind.
func (k OrgKind) Valid() bool { return slices.Contains(OrgKinds, k) }

// SeesEverything is true for kinds whose job is oversight.
func (k OrgKind) SeesEverything() bool {
	return k == KindAuditor || k == KindOperator || k == KindReporter
}

// ReadOnly is true for kinds that never submit records.
func (k OrgKind) ReadOnly() bool { return k == KindReporter }

// RecordType is the kind of evidence a record carries.
type RecordType string

const (
	TypeRejectLot     RecordType = "reject_lot"
	TypeRecycledLot   RecordType = "recycled_lot"
	TypeAdditiveBatch RecordType = "additive_batch"
	TypeAsphaltLot    RecordType = "asphalt_lot"
	TypeDelivery      RecordType = "delivery"
	TypeRoadSection   RecordType = "road_section"
	TypeShipment      RecordType = "shipment"
	TypeLabTest       RecordType = "lab_test"
	TypeAcceptance    RecordType = "acceptance" // a signed decision to accept a flagged record
	TypeRelease       RecordType = "release"    // quality release (or quarantine) of a lot
)

// RecordTypes in chain order.
var RecordTypes = []RecordType{TypeRejectLot, TypeRecycledLot, TypeAdditiveBatch, TypeAsphaltLot,
	TypeDelivery, TypeRoadSection, TypeShipment, TypeLabTest, TypeAcceptance, TypeRelease}

// Valid reports whether t is a known record type.
func (t RecordType) Valid() bool { return slices.Contains(RecordTypes, t) }

// Stage is a step of the material journey (1..6); 0 for cross-cutting records.
type Stage int

// TypeInfo describes one record type for the API, the CSV templates and the UI.
type TypeInfo struct {
	Type      RecordType `json:"type"`
	Stage     Stage      `json:"stage"`
	Name      string     `json:"name"`      // plain name, singular
	Plural    string     `json:"plural"`    // plain name, plural
	Technical string     `json:"technical"` // the proposal's term
	Summary   string     `json:"summary"`
	Prefix    string     `json:"prefix"` // human id prefix
	CreatedBy []OrgKind  `json:"createdBy"`
	// Passport: the record is a traceable unit that has its own passport page.
	Passport bool `json:"passport"`
}

// Types is the catalogue, keyed by type.
var Types = map[RecordType]TypeInfo{
	TypeRejectLot: {TypeRejectLot, 1, "Plastic reject lot", "Plastic reject lots", "Plasmix / LDPE reject",
		"Bales of film rejected by a packaging sorting plant that would otherwise be landfilled or burned.",
		"PRL", []OrgKind{KindSortingPlant}, true},
	TypeRecycledLot: {TypeRecycledLot, 2, "Recycled plastic lot", "Recycled plastic lots", "rLDPE agglomerate",
		"Reject film washed, shredded and agglomerated by a recycler.",
		"RPL", []OrgKind{KindRecycler}, true},
	TypeAdditiveBatch: {TypeAdditiveBatch, 3, "Additive batch", "Additive batches", "Polymer modifier (dry process)",
		"Pellets made from recycled plastic, dosed into asphalt to stiffen it.",
		"ADB", []OrgKind{KindAdditiveProducer}, true},
	TypeAsphaltLot: {TypeAsphaltLot, 4, "Asphalt lot", "Asphalt lots", "MIX (HMA/WMA)",
		"One production lot of one asphalt mix at one plant.",
		"ASL", []OrgKind{KindAsphaltPlant}, true},
	TypeDelivery: {TypeDelivery, 5, "Delivery", "Deliveries", "Delivery ticket",
		"One truckload of asphalt laid on a road section.",
		"DLV", []OrgKind{KindAsphaltPlant, KindContractor}, false},
	TypeRoadSection: {TypeRoadSection, 6, "Road section", "Road sections", "Road / test section",
		"A finished stretch of road and the asphalt laid on it.",
		"RSC", []OrgKind{KindContractor, KindRoadOwner}, true},
	TypeShipment: {TypeShipment, 0, "Shipment", "Shipments", "TR (transfer event)",
		"Material handed from one company to another; signed by both.",
		"SHP", []OrgKind{KindSortingPlant, KindRecycler, KindAdditiveProducer}, false},
	TypeLabTest: {TypeLabTest, 0, "Lab test", "Lab tests", "QA evidence",
		"A quality measurement on a lot, batch or section.",
		"LAB", []OrgKind{KindSortingPlant, KindRecycler, KindAdditiveProducer, KindAsphaltPlant, KindContractor, KindLab}, false},
	TypeAcceptance: {TypeAcceptance, 0, "Acceptance", "Acceptances", "accepted_override",
		"A signed decision to accept a record despite a data-check finding.",
		"ACC", []OrgKind{KindOperator, KindAuditor, KindRoadOwner}, false},
	TypeRelease: {TypeRelease, 0, "Quality release", "Quality releases", "QA release / quarantine",
		"Quality's signed decision that a lot may be shipped, or must be held.",
		// Only recycled lots and additive batches wait for a release (check C9, the release queue).
		"REL", []OrgKind{KindRecycler, KindAdditiveProducer}, false},
}

// Provenance labels. Simulated data must never be relabelled Operational.
const (
	ProvOperational     = "Operational"
	ProvSimulated       = "Simulated"
	ProvPublicReference = "PublicReference"
)

// Provenances accepted on ingest.
var Provenances = []string{ProvOperational, ProvSimulated, ProvPublicReference}

// Status of a record after the data checks.
type Status string

const (
	StatusOK       Status = "ok"       // no open finding
	StatusFlagged  Status = "flagged"  // open warning(s)
	StatusBlocked  Status = "blocked"  // open blocker: not sealed
	StatusAccepted Status = "accepted" // blocker accepted with a signed reason
)

// Org is an organisation in the chain.
type Org struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        OrgKind   `json:"kind"`  // primary role in the chain
	Kinds       []OrgKind `json:"kinds"` // every role the organisation plays (includes Kind)
	TaxID       string    `json:"taxId,omitempty"`
	City        string    `json:"city"`
	Province    string    `json:"province"`
	Region      string    `json:"region"`
	Country     string    `json:"country"`
	Lat         float64   `json:"lat"`
	Lon         float64   `json:"lon"`
	Description string    `json:"description,omitempty"`
	Website     string    `json:"website,omitempty"`
	// Branding for reports and passports (set by the organisation's admins).
	LegalName    string     `json:"legalName,omitempty"`
	Address      string     `json:"address,omitempty"`
	ContactEmail string     `json:"contactEmail,omitempty"`
	Phone        string     `json:"phone,omitempty"`
	ReportFooter string     `json:"reportFooter,omitempty"`
	HasLogo      bool       `json:"hasLogo,omitempty"`
	Provenance   string     `json:"provenance"`
	Status       string     `json:"status"` // pending | verified | suspended
	VerifiedAt   *time.Time `json:"verifiedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	// Demo marks the public demo workspace: rebuilt daily, writes limited.
	Demo bool `json:"demo,omitempty"`
	// PublicLedger shows the organisation's seals and blocks on the public explorer.
	PublicLedger bool `json:"publicLedger"`
}

// Is reports whether the organisation plays the given role in the chain: its
// main kind or any other activity it has added.
func (o Org) Is(k OrgKind) bool { return o.Kind == k || slices.Contains(o.Kinds, k) }

// Organisation statuses.
const (
	OrgPending   = "pending"
	OrgVerified  = "verified"
	OrgSuspended = "suspended"
)

// Site is one entry of an organisation's private supply-chain address book:
// one of its own plants or depots (Partner empty) or a site of a partner
// company it works with (Partner = that company's name). Sites are never shared
// between customers.
type Site struct {
	ID         string     `json:"id"`
	OrgID      string     `json:"orgId"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`                // one of SiteKinds
	KindOther  string     `json:"kindOther,omitempty"` // free text when Kind is "other"
	Partner    string     `json:"partner,omitempty"`   // partner company; empty = our own site
	Address    string     `json:"address,omitempty"`
	City       string     `json:"city"`
	Province   string     `json:"province"`
	Region     string     `json:"region"`
	Country    string     `json:"country,omitempty"`
	Lat        float64    `json:"lat"`
	Lon        float64    `json:"lon"`
	CapacityT  float64    `json:"capacityTonnesPerYear"`
	Equipment  string     `json:"equipment,omitempty"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
}

// Own reports whether the site belongs to the organisation itself.
func (s Site) Own() bool { return s.Partner == "" }

// Site types: the chain steps plus depots and a free-text "other".
const (
	SiteWarehouse = "warehouse"
	SiteOther     = "other"
)

// SiteKinds lists the site types in chain order.
var SiteKinds = []string{string(KindSortingPlant), string(KindRecycler), string(KindAdditiveProducer), string(KindAsphaltPlant),
	string(KindContractor), string(KindRoadOwner), string(KindLab), SiteWarehouse, SiteOther}

// Input is one consumed or carried item with the mass taken from it.
type Input struct {
	ID     string  `json:"id"`
	MassKg float64 `json:"massKg"`
}

// Record is one piece of signed evidence. Body holds the type-specific fields.
type Record struct {
	Seq          int64          `json:"seq"`
	ID           string         `json:"id"`
	Version      int            `json:"version"`
	PrevHash     string         `json:"prevHash,omitempty"`
	Type         RecordType     `json:"type"`
	OrgID        string         `json:"orgId"`
	SiteID       string         `json:"siteId,omitempty"`
	Counterparty string         `json:"counterpartyOrgId,omitempty"`
	Ref          string         `json:"ref"`
	OccurredAt   time.Time      `json:"occurredAt"`
	MassKg       float64        `json:"massKg"`
	Inputs       []Input        `json:"inputs,omitempty"`
	Body         map[string]any `json:"data"`
	Provenance   string         `json:"provenance"`
	Note         string         `json:"note,omitempty"` // the signer's remark (signed)
	ContentHash  string         `json:"contentHash"`
	Status       Status         `json:"status"`
	Signature    *Signature     `json:"signature,omitempty"`
	Receipt      *Receipt       `json:"receipt,omitempty"`
	// ActorSignature is the submitting member's personal signature over the same
	// message as Signature; absent for API keys and simulated data.
	ActorSignature *Signature `json:"actorSignature,omitempty"`
	SealID         string     `json:"sealId,omitempty"`
	Sealed         bool       `json:"sealed"` // the seal is committed on the ledger
	LeafIndex      int        `json:"leafIndex,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	CreatedBy      string     `json:"createdBy,omitempty"` // user or API key that submitted it
}

// Signature is one organisation's Ed25519 signature over a record hash.
