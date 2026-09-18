package compliance

import "github.com/sensifai/roadpass/internal/domain"

// gbRegister is United Kingdom law. Waste law is devolved: the waste
// references are the England and Wales instruments, and organisations in
// Scotland or Northern Ireland sign the permit statement against their own
// regime. Northern Ireland applies EU product rules under the Windsor Framework.
var gbRegister = Jurisdiction{
	Code: "GB", Name: "United Kingdom", Countries: []string{"GB"},
	Scope: "UK law, with England and Wales waste instruments (Scotland and Northern Ireland sign permits against their own regimes; " +
		"Northern Ireland applies EU product rules under the Windsor Framework).",
	Regulations: []Regulation{
		{Key: "uk_duty", Name: "Waste duty of care", Act: "Environmental Protection Act 1990, s. 34 · Waste (England and Wales) Regulations 2011, reg. 35",
			URL: "https://www.legislation.gov.uk/ukpga/1990/43/section/34"},
		{Key: "uk_permit", Name: "Environmental permit", Act: "Environmental Permitting (England and Wales) Regulations 2016",
			URL: "https://www.legislation.gov.uk/uksi/2016/1154/contents"},
		{Key: "uk_eow", Name: "End-of-waste", Act: "Environment Agency guidance: legal definition of waste and end-of-waste",
			URL: "https://www.gov.uk/government/publications/legal-definition-of-waste-guidance"},
		{Key: "uk_cpr", Name: "Construction products", Act: "Regulation (EU) No 305/2011 as retained in GB law (UK CPR) · BS EN 13108",
			URL: "https://www.legislation.gov.uk/eur/2011/305/contents"},
		{Key: "uk_shw", Name: "Highway works", Act: "Specification for Highway Works, Series 900 (Manual of Contract Documents for Highway Works, Vol. 1)",
			URL: "https://www.standardsforhighways.co.uk/"},
		{Key: "uk_secr", Name: "Energy and carbon reporting", Act: "Companies (Directors' Report) and LLP (Energy and Carbon Report) Regulations 2018 (SECR)",
			URL: "https://www.legislation.gov.uk/uksi/2018/1155/contents"},
		{Key: "uk_gdpr", Name: "Data protection", Act: "UK GDPR · Data Protection Act 2018", URL: "https://www.legislation.gov.uk/ukpga/2018/12/contents", Privacy: true},
	},
	Requirements: []Requirement{
		// Duty of care
		{ID: "gb.duty.intake", Regulation: "uk_duty", Ref: "EPA 1990 s. 34(1)", Title: "Incoming waste is checked against its description",
			Explain: "Every load received is weighed, compared with its transfer note and signed at intake: accepted, accepted with a note, or rejected.",
			Kinds:   receivers, auto: checkIntake},
		{ID: "gb.duty.transfer", Regulation: "uk_duty", Ref: "WR 2011 reg. 35", Title: "Transfer notes for waste you pass on",
			Explain: "Each transfer of waste carries a written description and transfer note, kept for at least two years (duty of care code of practice).",
			Kinds:   []domain.OrgKind{domain.KindSortingPlant, domain.KindRecycler}, Attest: true},
		{ID: "gb.duty.traceability", Regulation: "uk_duty", Ref: "EPA 1990 s. 34(1)(c)", Title: "Each lot traces to the waste it came from",
			Explain: "Each passport names the loads it was made from, so the waste can be followed from producer to product.",
			Kinds:   []domain.OrgKind{domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant, domain.KindContractor}, auto: checkTraceability},

		// Permit
		{ID: "gb.permit.valid", Regulation: "uk_permit", Ref: "reg. 12", Title: "Permit or registered exemption covers the plastic you treat",
			Explain: "Your environmental permit (or registered waste exemption, or the Scottish / Northern Irish equivalent) covers this waste stream and volume.",
			Kinds:   recyclers, Attest: true},

		// End-of-waste
		{ID: "gb.eow.quality", Regulation: "uk_eow", Ref: "End-of-waste test", Title: "Recovered plastic meets a recognised specification",
			Explain: "Each lot has at least one lab test before it leaves the plant, so it can be shown to meet the specification its buyer uses.",
			Kinds:   recyclers, auto: checkTested},
		{ID: "gb.eow.release", Regulation: "uk_eow", Ref: "End-of-waste test", Title: "Lots released only when they conform",
			Explain: "A quality release (or quarantine) is signed for each lot before dispatch.", Kinds: recyclers, auto: checkReleased},
		{ID: "gb.eow.findings", Regulation: "uk_eow", Ref: "End-of-waste test", Title: "Non-conformities are followed up",
			Explain: "Blocking data findings are resolved or accepted with a reason within 30 days.", Kinds: plants, auto: checkFindings},

		// UK CPR
		{ID: "gb.cpr.fpc", Regulation: "uk_cpr", Ref: "BS EN 13108-21", Title: "Factory production control",
			Explain: "Asphalt lots are tested and instruments calibrated, as factory production control requires.", Kinds: asphalt, auto: checkFPC},
		{ID: "gb.cpr.limits", Regulation: "uk_cpr", Ref: "BS EN 13108-1", Title: "Mix within declared limits",
			Explain: "Limits are set for the mix (additive dosage, temperature) and checked every month.", Kinds: asphalt, auto: checkLimits},
		{ID: "gb.cpr.dop", Regulation: "uk_cpr", Ref: "Art. 4, 8", Title: "Declaration of performance and conformity marking",
			Explain: "Each asphalt mix with the additive has a declaration of performance and UKCA marking (or CE marking where GB still accepts it).",
			Kinds:   asphalt, Attest: true},

		// Highway works
		{ID: "gb.shw.series900", Regulation: "uk_shw", Ref: "Series 900", Title: "Mix accepted by the overseeing organisation",
			Explain: "Asphalt with a recycled-plastic additive complies with Series 900 or a departure the overseeing organisation has approved.",
			Kinds:   []domain.OrgKind{domain.KindAsphaltPlant, domain.KindContractor, domain.KindRoadOwner}, Attest: true},

		// SECR
		{ID: "gb.secr.masses", Regulation: "uk_secr", Ref: "Sch. 7 Pt 7A", Title: "Figures behind the energy and carbon report are signed",
			Explain: "Masses, energy and haulage come from signed records rather than estimates.", auto: checkMasses},
		{ID: "gb.secr.report", Regulation: "uk_secr", Ref: "Sch. 7 Pt 7A", Title: "Energy use and emissions in the directors' report",
			Explain: "Large companies report UK energy use, emissions and an intensity ratio each year; state whether that applies and where it is filed.", Attest: true},

		// UK GDPR
		{ID: "gb.gdpr.minimisation", Regulation: "uk_gdpr", Ref: "Art. 5(1)(c)", Title: "Personal data kept to a minimum",
			Explain: "Passports and the ledger carry pseudonymous ids only; names and e-mails stay in the account database.", auto: checkMinimisation},
		{ID: "gb.gdpr.rights", Regulation: "uk_gdpr", Ref: "Art. 15, 17, 20", Title: "Access, portability and erasure on request",
			Explain: "A member's data can be exported, and a former member's personal data erased.", auto: checkRights},
		{ID: "gb.gdpr.security", Regulation: "uk_gdpr", Ref: "Art. 32", Title: "Security of processing",
			Explain: "Each member signs with a personal key held in the vault; the company key is registered on the ledger.", auto: checkSecurity},
		{ID: "gb.gdpr.records", Regulation: "uk_gdpr", Ref: "Art. 30", Title: "Record of processing activities",
			Explain: "Every change made through ROADPASS is in the audit log.", auto: checkAuditLog},
		{ID: "gb.gdpr.dpa", Regulation: "uk_gdpr", Ref: "Art. 28", Title: "Data processing agreement with the platform operator",
			Explain: "A data processing agreement with the ROADPASS operator is signed and filed.", Attest: true},
	},
}
