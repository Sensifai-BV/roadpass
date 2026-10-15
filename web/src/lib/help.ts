// Plain-language explanations shown on hover. One place, so a concept is
// always explained the same way wherever it appears.

export const HELP = {
  // supply chain
  passport: "A digital passport follows one lot, batch or road section: where its material came from, who handled it, its tests, its carbon figures, and the proof that nothing was changed.",
  inTransit: "Shipments sent to a company that it has not yet confirmed at its weighbridge.",
  awaitingRelease: "Lots made in the last 90 days that quality has neither released for dispatch nor quarantined.",
  quarantined: "Lots quality has held back: they may not be shipped until released.",
  blocked: "Records that failed a blocking data check (for example a mass that does not add up). They are not sealed until corrected or accepted with a reason.",
  warnings: "Records with a warning from a data check: worth a look, but they do not stop the material.",
  sealedShare: "Share of records whose fingerprint is sealed on the ledger. The rest are waiting for the next seal, blocked, or from companies not yet verified.",
  journey: "What each step of the chain produced in the period: sorted reject film, recycled plastic, additive, asphalt, deliveries to site and finished road sections.",
  recycledLaid: "Recycled plastic contained in the asphalt delivered to road sites in the period, from the delivery tickets.",
  co2Avoided: "Emissions avoided because reject plastic replaced virgin polymer and did not go to landfill or incineration, net of the recycling chain's own footprint.",
  asphaltWithAdditive: "Asphalt produced with the recycled-plastic additive in the period.",
  roadSurfaced: "Length of road sections finished with the additive asphalt in the period.",
  chainFootprint: "Emissions of the recycling chain itself: sorting, recycling, compounding and transport.",
  dataHealth: "Open data-check findings, the lab pass rate and how much of the data is sealed on the ledger.",
  // trust
  signed: "Every record is signed twice: with the company's key and with the personal key of the member who entered it. Anyone can check the signatures.",
  sealed: "Records are sealed in batches: the root of a Merkle tree over their fingerprints is written on a permissioned blockchain ledger.",
  fingerprint: "SHA-256 of the record's content in a canonical form. Change one character and the fingerprint no longer matches.",
  personalKey: "Your own Ed25519 key, held in the vault. It signs what you enter and approve, so each action is provably yours.",
  // compliance
  registerScore: "Met counts fully and partly met counts half, over the requirements that apply. Not-applicable ones are left out.",
  limit: "A bound on a measured value, from a permit, mix design or standard. Values outside it are exceedances.",
  basis: "Check every single value, or the daily or monthly average, as the permit or standard says.",
  exceedance: "A value, or a daily or monthly mean, outside a limit.",
  completeness: "Share of the plant's operating time without data gaps. A gap is a silence longer than three usual intervals; silences over six hours are stops, not missing data.",
  calibration: "Instrument calibrations, checks and replacements. Factory production control needs them on record.",
  attestation: "A statement signed with a responsible member's personal key. It is never changed afterwards; a newer one supersedes it.",
  autoCheck: "Checked automatically from your organisation's signed records over the last 12 months. A statement cannot override it, only mark it not applicable.",
  reportSignOff: "A responsible person signs the report's fingerprint with their personal key. If the month's data change later, the report shows that it needs signing again.",
  // work
  duties: "Duties decide which records a member may sign: intake, production, quality testing, quality release and dispatch.",
  intakeDecision: "At intake the receiver accepts the load, accepts it with a note (for example a weight difference), or rejects it. The decision is signed.",
} as const;

/** What each main section is for, shown when hovering the sidebar. */
export const NAV_HELP: Record<string, string> = {
  "/get-started": "A short guide to set up your company: details, sites, team, data and alerts. Each step says why it matters.",
  "/": "Your own dashboard: figures, charts and the journey of the material. Customise it to your work.",
  "/work": "What waits for your duties: incoming loads to review, lots to produce, test, release and ship.",
  "/status": "Where material stands right now: inbound loads, lots waiting for release, held or blocked records. Oversight bodies see every company.",
  "/calendar": "Your chain's records by day, week or month, and the work planned ahead: production runs, shipments, deliveries and road sections. Overdue plans are highlighted.",
  "/chain": "How material flows from step to step, and who supplies whom.",
  "/passports": "Search and open the digital passport of any lot, batch or road section you can see.",
  "/roads": "Finished road sections with the additive, on the map.",
  "/quality": "Automatic data checks on every record, and the findings to resolve.",
  "/alerts": "Rules that watch your data and notify people; what is firing now.",
  "/compliance": "Limits and exceedances, the rules that apply where you operate with their evidence, and the signed monthly report.",
  "/carbon": "Emissions avoided and the chain's footprint, ready for life-cycle assessment tools.",
  "/plants": "Process data from extruders and asphalt mixers.",
  "/ledger": "The seals on the blockchain ledger, and how to verify any record yourself.",
  "/data": "Bring data in: forms, spreadsheets (CSV), the REST API and NGSI-LD, with API keys.",
  "/settings": "Your profile, preferences and notifications; for admins, the organisation, team and billing.",
  "/glossary": "The words used in ROADPASS, in plain language.",
  "/platform/organisations": "Customer organisations: verify, suspend, change plans, invite their admins.",
  "/platform/users": "Every account on the platform.",
  "/platform/billing": "Plans, subscriptions and invoices.",
  "/platform/system": "Health of the services behind the platform.",
  "/platform/audit": "Every change made across the platform.",
  "/platform/privacy": "Former members of every organisation: data export and erasure.",
};

/** What each kind of organisation does, in one line. */
export const ACTIVITY: Record<string, string> = {
  sorting_plant: "Sorts collected packaging waste and passes on the plastic film it rejects (LDPE).",
  recycler: "Washes and melts reject film into recycled plastic (pellets or agglomerate).",
  additive_producer: "Compounds recycled plastic into an additive for asphalt.",
  asphalt_plant: "Mixes asphalt with the additive and delivers it to road works.",
  contractor: "Lays the asphalt and records each finished road section.",
  road_owner: "Owns or manages the roads (ministry, region, city) and accepts finished sections.",
  lab: "Independent laboratory that tests samples for the other companies.",
  auditor: "Checks passports and evidence across the whole chain. Reads everything, enters nothing.",
  reporter: "Follows the chain's figures for a programme, funder or regulator. Read-only.",
};
