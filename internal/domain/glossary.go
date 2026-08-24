package domain

// Term is one glossary entry: the word the product uses, and the technical term
// the proposal and industry documents use for the same thing.
type Term struct {
	Word      string `json:"word"`
	Technical string `json:"technical"`
	Meaning   string `json:"meaning"`
}

// Glossary is served at /api/v1/glossary and shown in the UI help panel.
var Glossary = []Term{
	{"Plastic reject lot", "Plasmix, LDPE reject", "Mixed plastic film that a packaging sorting plant could not sell for recycling. Usually landfilled or burned."},
	{"Film (LDPE)", "Low-density polyethylene", "The soft plastic of bags and wrapping film. It melts at about 110 °C, which makes it useful in hot asphalt."},
	{"Recycled plastic lot", "rLDPE agglomerate / regranulate", "Reject film after washing, shredding and densifying into crumbs or pellets."},
	{"Additive batch", "Polymer modifier, dry process", "Pellets made from recycled plastic, added straight into the asphalt mixer to make the road stiffer and longer-lasting."},
	{"Asphalt lot", "MIX, HMA / WMA", "One production run of one asphalt recipe at one plant. Hot mix asphalt is made at 150–180 °C; warm mix at 110–140 °C."},
	{"Delivery", "Delivery ticket", "One truckload of asphalt, from the plant to the paver."},
	{"Road section", "Test section", "A stretch of road between two kilometre points, and the layer that was laid on it."},
	{"Shipment", "TR, transfer event", "Material handed from one company to another. The sender signs when it leaves and the receiver signs when it arrives."},
	{"Passport", "Digital Material / Product Passport (DMP/DPP)", "Everything known about a lot or a road section: where its plastic came from, who handled it, how it was tested, and its carbon footprint."},
	{"Signature", "Ed25519 digital signature", "Proof that a specific company created or accepted a record. It can be checked against the key that company registered on the blockchain."},
	{"Sealed", "Hash anchored on the blockchain ledger", "The record's fingerprint is included in a seal on the blockchain. Changing the record afterwards would be detected."},
	{"Seal", "Merkle root anchor", "One blockchain transaction that commits to the fingerprints of up to 4,096 records at once."},
	{"Fingerprint", "SHA-256 content hash (RFC 8785)", "A 64-character code computed from the record's content. Any change to the record changes it."},
	{"Data checks", "Deterministic assurance rules", "Automatic checks for missing data, duplicates, dates out of order, broken custody, mass that doesn't add up and values out of spec."},
	{"Carbon footprint", "LCA (life-cycle assessment)", "Greenhouse-gas emissions in kg CO₂-equivalent, and the emissions avoided by not landfilling or burning the plastic."},
	{"Recycled content", "Recycled-plastic mass share", "Kilograms of recycled plastic per tonne of asphalt."},
	{"Mass balance", "Mass-balance check", "What leaves a company cannot exceed what came in, minus normal process losses."},
	{"Chain of custody", "CoC", "The unbroken sequence of signed handovers from the sorting plant to the road."},
	{"GRETA export", "GRETA-ready LCA dataset", "The CSV format the GRETA life-cycle assessment tool reads."},
}
