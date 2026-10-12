// Shapes returned by the Go API (internal/*). Keep in step with the Go structs.

export type OrgKind =
  | "sorting_plant" | "recycler" | "additive_producer" | "asphalt_plant"
  | "contractor" | "road_owner" | "lab" | "auditor" | "reporter" | "operator";

export type RecordType =
  | "reject_lot" | "recycled_lot" | "additive_batch" | "asphalt_lot"
  | "delivery" | "road_section" | "shipment" | "lab_test" | "acceptance" | "release";

export type Duty = "receive" | "produce" | "test" | "release" | "ship";

export type Status = "ok" | "flagged" | "blocked" | "accepted";

export interface Org {
  id: string; name: string; kind: OrgKind; kinds: OrgKind[]; taxId?: string; city: string; province: string; region: string;
  country: string; lat: number; lon: number; description?: string; website?: string; provenance: string;
  status: "pending" | "verified" | "suspended"; verifiedAt?: string; createdAt: string;
  legalName?: string; address?: string; contactEmail?: string; phone?: string; reportFooter?: string; hasLogo?: boolean;
  /** The public demo workspace: rebuilt daily, cannot be changed or suspended by anyone. */
  demo?: boolean;
  /** Seals and blocks are listed on the public /explorer page. Off by default. */
  publicLedger?: boolean;
}
/** One entry of an organisation's private address book: its own site (no partner) or a partner company's site. */
export interface Site {
  id: string; orgId: string; name: string; kind: SiteKind; kindOther?: string; partner?: string; address?: string;
  city: string; province: string; region: string; country?: string;
  lat: number; lon: number; capacityTonnesPerYear: number; equipment?: string; archivedAt?: string;
}
export type SiteKind = "sorting_plant" | "recycler" | "additive_producer" | "asphalt_plant" | "contractor" | "road_owner" | "lab" | "warehouse" | "other";
export interface User {
  id: string; orgId: string; email: string; name: string; role: "admin" | "editor" | "viewer"; duties: Duty[]; disabledAt?: string; steps?: OrgKind[];
  demo: boolean; lastLoginAt?: string; createdAt: string; timezone: string;
}
export interface Me { user: User; org: Org; demoMode: boolean; operator: boolean; oversight: boolean; duties: Duty[] }

export interface Signature { keyId: string; orgId: string; value: string; signedAt: string }
export interface Receipt {
  receivedAt: string; massKg: number; decision?: "accepted" | "accepted_with_note" | "rejected"; note?: string; receivedBy?: string; signature: Signature;
}
export interface Input { id: string; massKg: number }
export interface RecordT {
  seq: number; id: string; version: number; prevHash?: string; type: RecordType; orgId: string; siteId?: string;
  counterpartyOrgId?: string; ref: string; occurredAt: string; massKg: number; inputs?: Input[];
  data: Record<string, unknown>; provenance: string; contentHash: string; status: Status;
  signature?: Signature; receipt?: Receipt; sealId?: string; sealed: boolean; leafIndex?: number; createdAt: string;
}

export interface Field {
  name: string; label: string; kind: "number" | "text" | "enum" | "time" | "ref" | "org" | "site"; unit?: string;
  required: boolean; min?: number; max?: number; enum?: string[]; help?: string; example: string; legacy?: boolean;
}
export interface TypeInfo {
  type: RecordType; stage: number; name: string; plural: string; technical: string; summary: string;
  prefix: string; createdBy: OrgKind[]; passport: boolean;
}
export interface Rule { code: string; name: string; severity: "blocker" | "warning"; explain: string }
export interface Term { word: string; technical: string; meaning: string }
export interface Factor { key: string; name: string; value: number; unit: string; year: number; geo: string; source: string }
export interface Scenario { key: string; name: string; about: string; substituteVirgin: boolean; gridKey: string }
/** A named role in a company; access and duties are what the server checks. */
export interface JobRole { key: string; name: string; explain: string; access: "admin" | "editor" | "viewer"; duties: Duty[] | null }

export interface Meta {
  version: string; types: TypeInfo[]; fields: Record<RecordType, Field[]>;
  labTests: Record<string, { name: string; unit: string; standard: string; material: "plastic" | "asphalt" }>; orgKinds: OrgKind[];
  rules: Rule[]; glossary: Term[]; factors: Record<string, Factor>; regionalFactors: Record<string, Record<string, Factor>>; scenarios: Scenario[];
  provenances: string[]; siteKinds: SiteKind[]; demoMode: boolean; simulated: boolean; ledger: boolean; channel: string;
  duties: Duty[]; dutyInfo: Record<Duty, { Name: string; Explain: string }>;
  jobRoles: Partial<Record<OrgKind, JobRole[]>>;
  map?: { tiles: string; attribution: string };
  produces: Partial<Record<OrgKind, RecordType>>; consumes: Partial<Record<OrgKind, RecordType>>;
}

export interface Assignment { recordId: string; duty: Duty; userId: string; assignedBy: string; assignedAt: string; note?: string }
export interface WorkItem { record: RecordT; remainingKg: number; decision?: string; since: string; assignment?: Assignment }
export interface Queue { total: number; items: WorkItem[] }
export interface Work {
  duties: Duty[]; windowDays: number;
  queues: { receive: Queue; process: Queue; release: Queue; ship: Queue };
}
export interface StageStatus {
  /** One row per company and activity: the org kind its records belong to. */
  orgId: string; activity?: OrgKind; inTransit: number; inTransitKg: number; awaitingRelease: number; quarantined: number;
  blocked: number; flagged: number; records: number; sealed: number; lastActivity?: string;
}

export interface TypeTotal { type: RecordType; count: number; massKg: number }
export interface Impact {
  asphaltKg: number; recycledKg: number; rejectKg: number; avoidedKg: number; avoidedVirginKg: number;
  chainFootprintKg: number; footprintKg: number; deliveries: number;
}
export interface Overview {
  range: { From: string; To: string }; types: TypeTotal[]; impact: Impact; previous: Impact;
  roads: { sections: number; lengthKm: number; areaM2: number }; labPassRate: number; labTests: number;
}
export interface Point { t: string; series: string; v: number }
export interface Series { grain: "day" | "week" | "month"; points: Point[] }
export interface FlowLink { stage: number; from: string; to: string; tonnes: number }
export interface FunnelStep { type: RecordType; tonnes: number; count: number }
export interface SiteVolume { siteId: string; orgId: string; type: RecordType; massKg: number; count: number }
export interface SectionPoint {
  id: string; road: string; region: string; lat: number; lon: number; lengthKm: number; massKg: number;
  recycledKg: number; avoidedKg: number; layer: string; at: string;
}
export interface Breakdown { key: string; value: number; count: number }
export interface TestStats { test: string; count: number; passRate: number; p10: number; p50: number; p90: number }
export interface PlantDosing {
  siteId: string; orgId: string; lots: number; cycles: number; missedDoses: number; measuredPct: number;
  asphaltTonnes: number; additiveTonnes: number;
}
export interface LineHealth {
  siteId: string; orgId: string; line: string; hours: number; samples: number; stopped: number; suspect: number; outputTonnes: number;
}
export interface Suspect { ts: string; siteId: string; line: string; lotId: string; meltC: number; pressureBar: number; reason: string }
export interface LineHour {
  t: string; siteId: string; line: string; samples: number; stopped: number; suspect: number; meltC: number;
  pressureBar: number; throughputKgH: number; loadPct: number;
}
export interface LotCycle { ts: string; cycle: number; batchKg: number; additiveKg: number; binderKg: number; tempC: number }

export interface Finding {
  id: number; recordId: string; recordType?: RecordType; code: string; severity: "blocker" | "warning";
  message: string; fix: string; detail?: Record<string, unknown>; status: "open" | "resolved" | "accepted";
  orgId: string; createdAt: string; closedAt?: string; closedBy?: string; closeReason?: string;
}

export interface Seal {
  id: string; orgId?: string; merkleRoot: string; leafCount: number; firstSeq: number; lastSeq: number; provenance: string;
  status: "pending" | "sealed" | "failed"; txId?: string; blockNumber?: number; error?: string; createdAt: string; sealedAt?: string;
  mine?: number;
}
/** The ledger share dedicated to one organisation; orgId "" = shared seals made before seals were dedicated. */
export interface OrgBlocks {
  orgId: string; seals: number; records: number; blocks: number[]; pendingSeals: number; firstSealedAt?: string; lastSealedAt?: string;
}
/** records/sealed count every version (corrections included); current/currentSealed only the latest of each record. */
export interface SealStats { records: number; sealed: number; pending: number; blocked: number; seals: number; lastSeal?: Seal; current: number; currentSealed: number }
export interface ChainInfo { channel: string; height: number; currentBlockHash: string; previousBlockHash: string }
export interface LedgerSummary {
  enabled: boolean; online: boolean; channel: string; chain?: ChainInfo; platform: SealStats; mine?: SealStats; error?: string;
}
export interface LedgerTx {
  txId: string; timestamp: string; type: string; creatorMsp: string; chaincode?: string; function?: string;
  subject?: string; valid: boolean; validation: string;
}
export interface Block { number: number; dataHash: string; previousHash: string; txs: LedgerTx[]; sizeBytes: number }
export interface SealOnChain { sealId: string; merkleRoot: string; leafCount: number; submitterMsp: string; txId: string; sealedAt: string }
export interface SealDetail {
  seal: Seal; onChain?: SealOnChain; block?: Block; mine: number; ledgerError?: string;
  records: { id: string; version: number; type: RecordType; orgId: string; leafIndex: number; contentHash: string }[];
}
export interface Step { hash: string; left: boolean }
export interface Verification {
  hash: string; found: boolean; recordId?: string; version?: number; type?: RecordType; orgName?: string;
  occurredAt?: string; latest: boolean; sealId?: string; sealStatus?: string; merkleRoot?: string; txId?: string;
  blockNumber?: number; sealedAt?: string; leafIndex: number; path?: Step[]; computedRoot?: string; included: boolean;
  ledgerRoot?: string; anchored: boolean; ledgerError?: string;
}

export interface OrgRef { id: string; name: string; kind: OrgKind; kinds?: OrgKind[]; city: string; region: string; status: string; logo?: boolean }
export interface PNode {
  id: string; type: RecordType; orgId: string; siteId?: string; ref?: string; massKg: number; occurredAt: string; status: Status;
  signed: boolean; sealed: boolean; share: number; data?: Record<string, unknown>;
}
export interface Stage { stage: number; type: RecordType; name: string; records: number; massKg: number; orgs: string[]; first: string; last: string }
export interface Origin { orgId: string; region: string; kg: number; lots: number; route: string }
export interface TimelineEvent {
  at: string; kind: string; record?: string; orgId?: string; title: string; detail?: string; toSiteId?: string; ok?: boolean;
}
export interface Carbon {
  scenario: string; massKg: number; recycledKg: number; rejectKg: number; avoidedDisposalKg: number;
  avoidedVirginKg: number; avoidedKg: number; footprintKg: number; transportTkm: number; recycledKgPerTonne: number;
  netBenefitKg: number; chainFootprintKg: number;
}
export interface SigView {
  keyId: string; orgId: string; algorithm: string; value: string; publicKey: string; onLedger: boolean;
  ledgerTxId?: string; signedAt: string; valid: boolean; receivedAt?: string; massKg?: number;
  decision?: "accepted" | "accepted_with_note" | "rejected"; note?: string; noteHidden?: boolean;
  holderId?: string; holderName?: string;
}
export interface Trust {
  hashDoc?: Record<string, unknown>; contentHash: string; algorithm: string; signature?: SigView; receipt?: SigView;
  actorSignature?: SigView; receiptActor?: SigView;
  seal?: Seal; leafIndex: number; path?: Step[];
  versions: { version: number; contentHash: string; prevHash?: string; occurredAt: string; sealId?: string }[];
}
export interface Passport {
  access: "full" | "chain" | "public"; record?: RecordT; root: PNode; org: OrgRef; orgs: Record<string, OrgRef>;
  carbon: Carbon; scenarios: Carbon[]; stages: Stage[]; origins: Origin[]; nodes?: PNode[];
  links?: { parent: string; child: string; kind: string; massKg: number }[]; downstream?: PNode[];
  timeline?: TimelineEvent[]; tests?: PNode[]; findings?: Finding[]; trust?: Trust; truncated: boolean;
  simulated: boolean; builtAt: string;
  /** Road sections: asphalt of the accepted deliveries laid on it (what the chain traces). */
  tracedKg?: number;
}

export interface Ingest {
  id: string; orgId: string; channel: string; recordType?: string; status: string; total: number; accepted: number;
  rejected: number; errors?: { row: number; ref: string; errors: { field: string; message: string }[] }[];
  createdBy?: string; createdAt: string; finishedAt?: string;
}
export interface ApiKey {
  id: string; orgId: string; name: string; prefix: string; scopes: string[]; duties: Duty[]; createdBy?: string;
  createdAt: string; lastUsedAt?: string; revokedAt?: string;
}
export interface Invitation { id: string; orgId: string; email: string; role: string; duties: Duty[]; steps?: OrgKind[]; createdAt: string; expiresAt: string }
export interface SigningKeyView {
  id: string; orgId: string; algorithm: string; publicKey: string; status: string; onLedger: boolean;
  ledgerTxId?: string; custody: "vault" | "platform" | "customer"; userId?: string; createdAt: string;
}
export interface AuditEntry { id: number; at: string; orgId?: string; actor: string; action: string; subject?: string; detail?: Record<string, unknown> }

export interface ExplorerSummary {
  online: boolean; channel: string; chain?: ChainInfo; orgs: number; seals: number; records: number; blocks: number;
}
export interface ExplorerOrg {
  id: string; name: string; kind: OrgKind; country: string; demo?: boolean; hasLogo?: boolean; seals: number; records: number;
  blocks: number[]; lastSealedAt?: string;
}
/** A transaction as the public sees it: a private organisation's has hashes only, a public one's has its details. */
export interface ExplorerTx {
  txId: string; timestamp: string; type: string; function?: string; creatorMsp: string; valid: boolean; validation: string; public: boolean;
  merkleRoot?: string; sealId?: string; orgId?: string; orgName?: string; records?: number; provenance?: string; keyId?: string;
}
export interface ExplorerBlock { number: number; dataHash: string; previousHash: string; sizeBytes: number; txs: ExplorerTx[] }
export interface PublicRecord {
  leafIndex: number; id: string; version: number; type: RecordType; ref: string; occurredAt: string; massKg: number; provenance: string; contentHash: string;
}
export interface ExplorerSeal {
  seal: Seal; orgName: string; orgKind: OrgKind; onChain?: SealOnChain; ledgerError?: string; records: PublicRecord[];
}
export interface ExplorerOrgDetail {
  detail: {
    org: ExplorerOrg; kinds: OrgKind[]; city: string; website?: string; description?: string;
    types: { type: RecordType; count: number; massKg: number; sealed: number }[];
    sites: { name: string; kind: string; city: string; region: string; country: string }[];
    roadSections: PublicRecord[];
  };
  seals: Seal[];
}
