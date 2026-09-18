package compliance

import (
	"slices"

	"github.com/sensifai/roadpass/internal/domain"
)

// euEEA lists the EU member states and the EEA countries that apply EU product,
// waste and data-protection law (ISO 3166-1 alpha-2).
var euEEA = []string{"AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR", "DE", "GR", "HU", "IE", "IT", "LV", "LT", "LU",
	"MT", "NL", "PL", "PT", "RO", "SK", "SI", "ES", "SE", "IS", "LI", "NO"}

var euRegister = Jurisdiction{
	Code: "EU", Name: "European Union", Countries: euEEA,
	Scope: "EU law for EU member states and the EEA. National transpositions (permits, end-of-waste orders) are signed statements.",
	Regulations: []Regulation{
		{Key: "espr", Name: "Digital Product Passport", Act: "Ecodesign for Sustainable Products Regulation (EU) 2024/1781", URL: "https://eur-lex.europa.eu/eli/reg/2024/1781/oj"},
		{Key: "wfd", Name: "End-of-waste", Act: "Waste Framework Directive 2008/98/EC, art. 6", URL: "https://eur-lex.europa.eu/eli/dir/2008/98/oj"},
		{Key: "ppwr", Name: "Recycled content", Act: "Packaging and Packaging Waste Regulation (EU) 2025/40", URL: "https://eur-lex.europa.eu/eli/reg/2025/40/oj"},
		{Key: "cpr", Name: "Construction products", Act: "Construction Products Regulation (EU) 2024/3110 · EN 13108", URL: "https://eur-lex.europa.eu/eli/reg/2024/3110/oj"},
		{Key: "csrd", Name: "Sustainability reporting", Act: "CSRD (EU) 2022/2464 · ESRS E1 and E5", URL: "https://eur-lex.europa.eu/eli/dir/2022/2464/oj"},
		{Key: "taxonomy", Name: "EU Taxonomy", Act: "Regulation (EU) 2020/852 · Circular economy delegated act", URL: "https://eur-lex.europa.eu/eli/reg/2020/852/oj"},
		{Key: "gdpr", Name: "Data protection", Act: "General Data Protection Regulation (EU) 2016/679", URL: "https://eur-lex.europa.eu/eli/reg/2016/679/oj", Privacy: true},
	},
	Requirements: []Requirement{
		// ESPR — Digital Product Passport
		{ID: "espr.identifier", Regulation: "espr", Ref: "Art. 9–10", Title: "Unique identifier for every passport",
			Explain: "Each lot, batch and road section carries a unique, persistent identifier linked to its passport.",
			Kinds:   append(slices.Clone(plants), domain.KindContractor), auto: checkIdentifier},
		{ID: "espr.integrity", Regulation: "espr", Ref: "Art. 10(1)", Title: "Passport data are authentic and tamper-evident",
			Explain: "Records are signed by the company and the person who made them, and sealed on the ledger.", auto: checkIntegrity},
		{ID: "espr.traceability", Regulation: "espr", Ref: "Art. 9(2)", Title: "Passports trace back to their inputs",
			Explain: "Each passport names the lots it was made from, so the chain can be followed back to the sorted waste.",
			Kinds:   []domain.OrgKind{domain.KindRecycler, domain.KindAdditiveProducer, domain.KindAsphaltPlant, domain.KindContractor}, auto: checkTraceability},
		{ID: "espr.access", Regulation: "espr", Ref: "Art. 10(1)(e)", Title: "Access rights by role",
			Explain: "Customers, authorities and the public see what they are entitled to; every member has a named role.", auto: checkAccess},
		{ID: "espr.retention", Regulation: "espr", Ref: "Art. 10(1)(g)", Title: "Passport kept for the product's lifetime",
			Explain: "Passports must stay available for the expected life of the road. State your retention policy and back-up arrangement.", Attest: true},

		// Waste Framework Directive — end-of-waste
		{ID: "wfd.input", Regulation: "wfd", Ref: "Art. 6(1)(a)", Title: "Acceptance of incoming waste is checked and recorded",
			Explain: "Every incoming load is weighed, reviewed and signed at intake: accepted, accepted with a note, or rejected.", Kinds: receivers, auto: checkIntake},
		{ID: "wfd.quality", Regulation: "wfd", Ref: "Art. 6(1)(c)", Title: "Output tested against technical requirements",
			Explain: "Each lot has at least one lab test before it leaves the plant.", Kinds: recyclers, auto: checkTested},
		{ID: "wfd.conformity", Regulation: "wfd", Ref: "Art. 6(1)(d)", Title: "Statement of conformity for each lot",
			Explain: "A quality release (or quarantine) is signed for each lot before dispatch.", Kinds: recyclers, auto: checkReleased},
		{ID: "wfd.findings", Regulation: "wfd", Ref: "Art. 6(2)", Title: "Non-conformities are followed up",
			Explain: "Blocking data findings are resolved or accepted with a reason within 30 days.", Kinds: plants, auto: checkFindings},
		{ID: "wfd.permit", Regulation: "wfd", Ref: "Art. 23", Title: "Waste treatment permit is valid",
			Explain: "Your plant's permit to treat this waste stream is valid and covers the volumes you handle.", Kinds: recyclers, Attest: true},

		// PPWR — recycled content
		{ID: "ppwr.origin", Regulation: "ppwr", Ref: "Art. 7", Title: "Origin of recycled plastic is documented",
			Explain: "Recycled material can be traced to post-consumer waste input, so its recycled content can be calculated and verified.",
			Kinds:   []domain.OrgKind{domain.KindRecycler, domain.KindAdditiveProducer}, auto: checkOrigin},

		// CPR / EN 13108 — asphalt
		{ID: "cpr.fpc", Regulation: "cpr", Ref: "EN 13108-21", Title: "Factory production control",
			Explain: "Asphalt lots are tested and instruments calibrated, as factory production control requires.", Kinds: asphalt, auto: checkFPC},
		{ID: "cpr.limits", Regulation: "cpr", Ref: "EN 13108-1", Title: "Mix within declared limits",
			Explain: "Limits are set for the mix (additive dosage, temperature) and checked every month.", Kinds: asphalt, auto: checkLimits},
		{ID: "cpr.dop", Regulation: "cpr", Ref: "Art. 15", Title: "Declaration of performance for each mix",
			Explain: "Each asphalt mix with the additive has a declaration of performance and CE marking.", Kinds: asphalt, Attest: true},

		// CSRD — ESRS E1 and E5
		{ID: "csrd.e5", Regulation: "csrd", Ref: "ESRS E5-4, E5-5", Title: "Resource inflows and outflows with recycled share",
			Explain: "Masses in and out, with the recycled share, come from signed records rather than estimates.", auto: checkMasses},
		{ID: "csrd.e1", Regulation: "csrd", Ref: "ESRS E1-6", Title: "Emissions avoided are calculated from deliveries",
			Explain: "Avoided CO₂e is computed per delivery from the recycled content and documented factors.", Kinds: roads, auto: checkDeliveries},

		// EU Taxonomy
		{ID: "taxonomy.ce", Regulation: "taxonomy", Ref: "Art. 13 · CE 2.x", Title: "Substantial contribution to the circular economy",
			Explain: "Document how the activity meets the technical screening criteria and does no significant harm.", Attest: true},

		// GDPR
		{ID: "gdpr.minimisation", Regulation: "gdpr", Ref: "Art. 5(1)(c)", Title: "Personal data kept to a minimum",
			Explain: "Passports and the ledger carry pseudonymous ids only; names and e-mails stay in the account database.", auto: checkMinimisation},
		{ID: "gdpr.rights", Regulation: "gdpr", Ref: "Art. 15, 17, 20", Title: "Access, portability and erasure on request",
			Explain: "A member's data can be exported, and a former member's personal data erased.", auto: checkRights},
		{ID: "gdpr.security", Regulation: "gdpr", Ref: "Art. 32", Title: "Security of processing",
			Explain: "Each member signs with a personal key held in the vault; the company key is registered on the ledger.", auto: checkSecurity},
		{ID: "gdpr.records", Regulation: "gdpr", Ref: "Art. 30", Title: "Record of processing activities",
			Explain: "Every change made through ROADPASS is in the audit log.", auto: checkAuditLog},
		{ID: "gdpr.dpa", Regulation: "gdpr", Ref: "Art. 28", Title: "Data processing agreement with the platform operator",
			Explain: "A data processing agreement with the ROADPASS operator is signed and filed.", Attest: true},
	},
}
