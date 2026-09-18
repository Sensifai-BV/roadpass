package compliance

import "github.com/sensifai/roadpass/internal/domain"

// usRegister is US federal law. Non-hazardous solid waste and recycling are
// regulated by the states (RCRA Subtitle D), so waste-handling duties sit in
// the state sets below; there is no general federal data-protection law.
var usRegister = Jurisdiction{
	Code: "US", Name: "United States (federal)", Countries: []string{"US"},
	Scope: "Federal rules only. Recycling of non-hazardous waste is state law (see the state set, where ROADPASS carries one); " +
		"there is no general federal data-protection law, so none is scored.",
	Regulations: []Regulation{
		{Key: "us_ftc", Name: "Recycled-content claims", Act: "FTC Guides for the Use of Environmental Marketing Claims, 16 CFR 260.13",
			URL: "https://www.ecfr.gov/current/title-16/chapter-I/subchapter-B/part-260/section-260.13"},
		{Key: "us_qa", Name: "Construction quality assurance", Act: "23 CFR 637 Subpart B — quality assurance procedures for federal-aid highway construction",
			URL: "https://www.ecfr.gov/current/title-23/chapter-I/subchapter-G/part-637/subpart-B"},
	},
	Requirements: []Requirement{
		// FTC Green Guides
		{ID: "us.ftc.origin", Regulation: "us_ftc", Ref: "§ 260.13(b)", Title: "Recycled content is recovered or diverted from the waste stream",
			Explain: "A recycled-content claim needs proof that the material came out of the waste stream; each lot traces to the waste it was made from.",
			Kinds:   []domain.OrgKind{domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant}, auto: checkOrigin},
		{ID: "us.ftc.claims", Regulation: "us_ftc", Ref: "§ 260.13(a), (c)", Title: "Recycled-content claims are qualified",
			Explain: "Claims on products and documents state the recycled share (and pre- or post-consumer where claimed) and do not overstate it.",
			Kinds:   []domain.OrgKind{domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant}, Attest: true},

		// 23 CFR 637
		{ID: "us.qa.testing", Regulation: "us_qa", Ref: "§ 637.207", Title: "Quality-control testing of each lot",
			Explain: "Asphalt lots are tested and instruments calibrated, so quality-control results can be used in the agency's acceptance decision.",
			Kinds:   asphalt, auto: checkFPC},
		{ID: "us.qa.limits", Regulation: "us_qa", Ref: "§ 637.205", Title: "Mix within specification limits",
			Explain: "Limits from the agency's specification (additive dosage, temperature) are set and checked every month.", Kinds: asphalt, auto: checkLimits},
		{ID: "us.qa.labs", Regulation: "us_qa", Ref: "§ 637.209", Title: "Testing by qualified laboratories and personnel",
			Explain: "Tests used in acceptance come from qualified laboratories and certified technicians (AASHTO accreditation or an FHWA-approved equivalent where required).",
			Kinds:   []domain.OrgKind{domain.KindAsphaltPlant, domain.KindLab}, Attest: true},
		{ID: "us.qa.findings", Regulation: "us_qa", Ref: "§ 637.207(a)", Title: "Non-conformities are followed up",
			Explain: "Blocking data findings are resolved or accepted with a reason within 30 days.", Kinds: plants, auto: checkFindings},
	},
}

// usTexasRegister is Texas state law on top of the federal set.
var usTexasRegister = Jurisdiction{
	Code: "US-TX", Name: "Texas", Countries: []string{"US"}, Regions: []string{"US-TX", "TX", "Texas"},
	Scope: "Texas Commission on Environmental Quality rules for recycling facilities and the TxDOT specification for asphalt.",
	Regulations: []Regulation{
		{Key: "tx_328", Name: "Recycling facilities", Act: "30 TAC Chapter 328, Subchapter A (§§ 328.3–328.5)",
			URL: "https://www.law.cornell.edu/regulations/texas/30-Tex-Admin-Code-SS-328-5"},
		{Key: "tx_dot", Name: "TxDOT specifications", Act: "TxDOT Standard Specifications for Construction and Maintenance of Highways, Streets, and Bridges (2024)",
			URL: "https://www.txdot.gov/business/resources/txdot-specifications.html"},
	},
	Requirements: []Requirement{
		{ID: "tx.328.notice", Regulation: "tx_328", Ref: "§ 328.5", Title: "Notice to the TCEQ, or permit or exemption",
			Explain: "The facility has filed its notice of intent with the TCEQ (or holds a permit or qualifies for an exemption) for the material it recycles.",
			Kinds:   recyclers, Attest: true},
		{ID: "tx.328.records", Regulation: "tx_328", Ref: "§ 328.5", Title: "Incoming recyclable material is recorded",
			Explain: "Every incoming load is weighed, reviewed and signed at intake, so the facility's records of material received are complete.",
			Kinds:   []domain.OrgKind{domain.KindSortingPlant, domain.KindRecycler, domain.KindAdditiveProducer}, auto: checkIntake},
		{ID: "tx.328.storage", Regulation: "tx_328", Ref: "§ 328.4 · TCEQ-20049", Title: "Stored material is recycled or moved on in time",
			Explain: "At least half of each material on hand at the start of a six-month period is recycled or transferred by its end.",
			Kinds:   recyclers, Attest: true},
		{ID: "tx.dot.mix", Regulation: "tx_dot", Ref: "Item 341", Title: "Mix with the additive is allowed for the project",
			Explain: "Hot-mix asphalt with a recycled-plastic additive is covered by the project's specification or a special provision approved by the engineer.",
			Kinds:   []domain.OrgKind{domain.KindAsphaltPlant, domain.KindContractor, domain.KindRoadOwner}, Attest: true},
	},
}
