import {
  Recycle, Factory, FlaskConical, Building2, Truck, Route, Landmark, Microscope, ShieldCheck, Boxes,
  Package, Layers, TestTube2, BadgeCheck, Stamp, Telescope, PackageOpen, Cog, Send, Warehouse, MapPin, type LucideIcon,
} from "lucide-react";
import type { OrgKind, RecordType, Site, SiteKind, Status } from "./types";

/** The six steps of the material journey, each with its fixed series colour. */
export interface StageDef {
  n: number;
  kind: OrgKind;
  type: RecordType;
  title: string; // the step, as a verb phrase
  who: string; // who does it
  color: string; // CSS variable
  icon: LucideIcon;
}

export const STAGES: StageDef[] = [
  { n: 1, kind: "sorting_plant", type: "reject_lot", title: "Sort", who: "Sorting plants", color: "var(--s1)", icon: Boxes },
  { n: 2, kind: "recycler", type: "recycled_lot", title: "Recycle", who: "Recyclers", color: "var(--s2)", icon: Recycle },
  { n: 3, kind: "additive_producer", type: "additive_batch", title: "Make additive", who: "Additive producers", color: "var(--s3)", icon: FlaskConical },
  { n: 4, kind: "asphalt_plant", type: "asphalt_lot", title: "Mix asphalt", who: "Asphalt plants", color: "var(--s4)", icon: Factory },
  { n: 5, kind: "contractor", type: "delivery", title: "Lay", who: "Road contractors", color: "var(--s5)", icon: Truck },
  { n: 6, kind: "road_owner", type: "road_section", title: "Own the road", who: "Road owners", color: "var(--s6)", icon: Route },
];

export const KIND: Record<OrgKind, { name: string; plural: string; color: string; icon: LucideIcon }> = {
  sorting_plant: { name: "Sorting plant", plural: "Sorting plants", color: "var(--s1)", icon: Boxes },
  recycler: { name: "Recycler", plural: "Recyclers", color: "var(--s2)", icon: Recycle },
  additive_producer: { name: "Additive producer", plural: "Additive producers", color: "var(--s3)", icon: FlaskConical },
  asphalt_plant: { name: "Asphalt plant", plural: "Asphalt plants", color: "var(--s4)", icon: Factory },
  contractor: { name: "Road contractor", plural: "Road contractors", color: "var(--s5)", icon: Truck },
  road_owner: { name: "Road owner", plural: "Road owners", color: "var(--s6)", icon: Landmark },
  lab: { name: "Laboratory", plural: "Laboratories", color: "var(--s7)", icon: Microscope },
  auditor: { name: "Auditor", plural: "Auditors", color: "var(--muted)", icon: ShieldCheck },
  reporter: { name: "Reporter", plural: "Reporters", color: "var(--muted)", icon: Telescope },
  operator: { name: "Platform operator", plural: "Platform operator", color: "var(--muted)", icon: Building2 },
};

/** Site types of the address book: the chain steps plus depots and a free-text "other". */
export const SITE_KIND: Record<SiteKind, { name: string; color: string; icon: LucideIcon }> = {
  sorting_plant: { name: KIND.sorting_plant.name, color: KIND.sorting_plant.color, icon: KIND.sorting_plant.icon },
  recycler: { name: KIND.recycler.name, color: KIND.recycler.color, icon: KIND.recycler.icon },
  additive_producer: { name: KIND.additive_producer.name, color: KIND.additive_producer.color, icon: KIND.additive_producer.icon },
  asphalt_plant: { name: KIND.asphalt_plant.name, color: KIND.asphalt_plant.color, icon: KIND.asphalt_plant.icon },
  contractor: { name: KIND.contractor.name, color: KIND.contractor.color, icon: KIND.contractor.icon },
  road_owner: { name: KIND.road_owner.name, color: KIND.road_owner.color, icon: KIND.road_owner.icon },
  lab: { name: KIND.lab.name, color: KIND.lab.color, icon: KIND.lab.icon },
  warehouse: { name: "Warehouse / depot", color: "var(--muted)", icon: Warehouse },
  other: { name: "Other", color: "var(--muted)", icon: MapPin },
};

/** The site's type as shown to people ("Other" shows what the user typed). */
export const siteKindName = (s: Pick<Site, "kind" | "kindOther">) =>
  s.kind === "other" ? s.kindOther || "Other" : SITE_KIND[s.kind]?.name ?? s.kind;

/** One line naming a site: "Name · Partner, City". */
export const siteLabel = (s: Site) => [s.name, [s.partner, s.city].filter(Boolean).join(", ")].filter(Boolean).join(" · ");

export const TYPE: Record<RecordType, { name: string; plural: string; color: string; icon: LucideIcon; passport: boolean }> = {
  reject_lot: { name: "Plastic reject lot", plural: "Plastic reject lots", color: "var(--s1)", icon: Boxes, passport: true },
  recycled_lot: { name: "Recycled plastic lot", plural: "Recycled plastic lots", color: "var(--s2)", icon: Recycle, passport: true },
  additive_batch: { name: "Additive batch", plural: "Additive batches", color: "var(--s3)", icon: FlaskConical, passport: true },
  asphalt_lot: { name: "Asphalt lot", plural: "Asphalt lots", color: "var(--s4)", icon: Layers, passport: true },
  delivery: { name: "Delivery", plural: "Deliveries", color: "var(--s5)", icon: Truck, passport: false },
  road_section: { name: "Road section", plural: "Road sections", color: "var(--s6)", icon: Route, passport: true },
  shipment: { name: "Shipment", plural: "Shipments", color: "var(--muted)", icon: Package, passport: false },
  lab_test: { name: "Lab test", plural: "Lab tests", color: "var(--s7)", icon: TestTube2, passport: false },
  acceptance: { name: "Acceptance", plural: "Acceptances", color: "var(--muted)", icon: BadgeCheck, passport: false },
  release: { name: "Quality release", plural: "Quality releases", color: "var(--s3)", icon: Stamp, passport: false },
};

/** The duty a record type needs (mirrors domain.DutyFor). */
export function dutyFor(t: RecordType): import("./types").Duty {
  if (t === "shipment" || t === "delivery") return "ship";
  if (t === "lab_test") return "test";
  if (t === "release" || t === "acceptance") return "release";
  return "produce";
}

/** Jobs inside a step of the chain. */
export const DUTY: Record<import("./types").Duty, { name: string; short: string; icon: LucideIcon; explain: string }> = {
  receive: { name: "Intake", short: "Receive", icon: PackageOpen, explain: "Confirms incoming shipments; the receipt is signed." },
  produce: { name: "Production", short: "Produce", icon: Cog, explain: "Records what the plant makes from received material." },
  test: { name: "Quality testing", short: "Test", icon: TestTube2, explain: "Records lab results on lots and batches." },
  release: { name: "Quality release", short: "Release", icon: Stamp, explain: "Releases lots for dispatch, or quarantines them." },
  ship: { name: "Dispatch", short: "Ship", icon: Send, explain: "Ships released lots to the next company." },
};

export const STATUS: Record<Status, { label: string; chip: string; explain: string }> = {
  ok: { label: "Checked", chip: "chip-good", explain: "Passed every data check." },
  flagged: { label: "Warning", chip: "chip-warning", explain: "A data check raised a warning; it is still sealed." },
  blocked: { label: "Blocked", chip: "chip-critical", explain: "A blocking data check failed; it is not sealed until resolved." },
  accepted: { label: "Accepted", chip: "chip-accent", explain: "A finding was accepted with a signed reason." },
};

export const LAB_TEST_NAMES: Record<string, string> = {
  mfi: "Melt flow index", moisture: "Moisture", ash: "Ash content", dsc_melting: "Melting peak (DSC)",
  itsr: "Water sensitivity (ITSR)", wheel_tracking: "Wheel tracking", air_voids: "Air voids",
  marshall_stability: "Marshall stability", binder_content: "Binder content", density: "Bulk density",
};

// Short labels for the demo dataset's long region names; other names display as-is.
export const REGION_SHORT: Record<string, string> = {
  "Andalucía": "Andalucía", "Aragón": "Aragón", "Principado de Asturias": "Asturias", "Cantabria": "Cantabria",
  "Castilla y León": "Castilla y León", "Castilla-La Mancha": "Castilla-La Mancha", "Cataluña": "Cataluña",
  "Comunitat Valenciana": "C. Valenciana", "Extremadura": "Extremadura", "Galicia": "Galicia",
  "Comunidad de Madrid": "Madrid", "Región de Murcia": "Murcia", "Comunidad Foral de Navarra": "Navarra",
  "País Vasco": "País Vasco", "La Rioja": "La Rioja",
};

/** The passports a company of this kind mostly makes: the tab its Passports list opens on. */
export function homeType(kind?: string): RecordType {
  return kind === "sorting_plant" ? "reject_lot" : kind === "recycler" ? "recycled_lot" : kind === "additive_producer" ? "additive_batch"
    : kind === "asphalt_plant" ? "asphalt_lot" : kind === "lab" ? "lab_test" : "road_section";
}
