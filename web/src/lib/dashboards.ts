// Dashboards an admin designs for their team: types mirroring the API, value
// formatting by unit, widget defaults and starter templates.
import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import { compact, mass, num } from "./format";
import { HELP } from "./help";
import type { Duty, OrgKind } from "./types";

export type WidgetType = "kpi" | "timeseries" | "bars" | "donut" | "gauge" | "table" | "map" | "text" | "journey" | "sites_map" | "data_health" | "activity";

/** Widgets that bring their own data instead of a query. */
export const BUILTIN: WidgetType[] = ["text", "journey", "sites_map", "data_health", "activity"];
export type Agg = "sum" | "avg" | "min" | "max" | "median" | "p90" | "count";
export type Interval = "" | "auto" | "hour" | "day" | "week" | "month";
export type WidgetRange = "" | "7d" | "30d" | "90d" | "12m" | "all";

export interface Filter { field: string; op: "eq" | "neq" | "in" | "gt" | "gte" | "lt" | "lte"; value: string | number | boolean | (string | number)[] }

export interface Query {
  source: string; types?: string[]; measure: string; agg: Agg; groupBy?: string; interval?: Interval;
  filters?: Filter[]; limit?: number; compare?: boolean;
}

export interface WidgetOptions {
  style?: "line" | "area" | "bars";        // timeseries
  color?: string;                            // series colour token (s1…s8) for single-series widgets
  min?: number; max?: number;                // gauge scale
  warn?: number; critical?: number;          // gauge thresholds
  invert?: boolean;                          // lower is better (deltas, thresholds)
  text?: string;                             // text widget
  decimals?: number;
  orient?: "horizontal" | "vertical";        // bars: horizontal ranking or vertical columns
  donutStyle?: "donut" | "pie";              // share
  journeyStyle?: "pipeline" | "list" | "figures";
  journeyMeasure?: "mass" | "records";
  animate?: boolean;
  help?: string;                             // the widget's explanation, shown on hover
}

export interface Widget {
  id: string; type: WidgetType; x: number; y: number; w: number; h: number; title: string;
  query?: Query; range?: WidgetRange; options?: WidgetOptions;
}

export interface Dashboard {
  id: string; orgId: string; name: string; description: string; audience: string[]; position: number;
  widgets: Widget[]; version: number; createdBy: string; updatedBy: string; createdAt: string; updatedAt: string;
}

export interface CatalogField { key: string; label: string; kind: "measure" | "dimension"; type: "number" | "string" | "bool"; unit?: string; help?: string }
export interface CatalogSource { key: string; label: string; help: string; fields: CatalogField[]; types?: string[] }
export interface Catalog { sources: CatalogSource[]; widgetTypes: WidgetType[] }

export interface ResultRow { t?: string; group?: string; v: number; n: number }
export interface Result {
  unit: string; interval?: string; rows: ResultRow[]; previous?: number; groups?: string[];
  field: CatalogField; groupBy?: CatalogField; tookMs: number; at: string;
}

export const COLS = 12;
export const ROW_H = 72;
export const GAP = 16;

export const WIDGETS: Record<WidgetType, { name: string; explain: string; w: number; h: number; needsGroup?: boolean; needsTime?: boolean }> = {
  kpi: { name: "Value", explain: "One number, with the change from the previous period.", w: 3, h: 2 },
  timeseries: { name: "Time series", explain: "A measure over time as lines, areas or bars; optionally one series per group.", w: 6, h: 4, needsTime: true },
  bars: { name: "Ranking", explain: "Top groups by a measure, as horizontal bars.", w: 4, h: 4, needsGroup: true },
  donut: { name: "Share", explain: "How a total splits across groups.", w: 4, h: 4, needsGroup: true },
  gauge: { name: "Gauge", explain: "One value against a scale, with warning and critical thresholds.", w: 3, h: 3 },
  table: { name: "Table", explain: "Groups with their value and record count.", w: 6, h: 4, needsGroup: true },
  map: { name: "Map", explain: "A measure by region on the map.", w: 6, h: 5, needsGroup: true },
  text: { name: "Text", explain: "A heading or note for your team.", w: 4, h: 2 },
  journey: { name: "The journey", explain: "The chain's six steps for the period, as a pipeline, a list or plain figures.", w: 12, h: 3 },
  sites_map: { name: "Where it happens", explain: "Plants sized by tonnes handled, and finished road sections.", w: 7, h: 5 },
  data_health: { name: "Data health", explain: "Open findings, lab pass rate and how much is sealed on the ledger.", w: 5, h: 5 },
  activity: { name: "Latest activity", explain: "The newest records you can see.", w: 5, h: 5 },
};

export const AGGS: { value: Agg; label: string }[] = [
  { value: "sum", label: "Total" }, { value: "avg", label: "Average" }, { value: "median", label: "Median" },
  { value: "p90", label: "90th percentile" }, { value: "min", label: "Lowest" }, { value: "max", label: "Highest" }, { value: "count", label: "Count of records" },
];

export const RANGES: { value: WidgetRange; label: string }[] = [
  { value: "", label: "Page time range" }, { value: "7d", label: "Last 7 days" }, { value: "30d", label: "Last 30 days" },
  { value: "90d", label: "Last 90 days" }, { value: "12m", label: "Last 12 months" }, { value: "all", label: "All time" },
];

/** Series colours in fixed order (validated categorical palette). */
export const SERIES = ["var(--s1)", "var(--s2)", "var(--s3)", "var(--s4)", "var(--s5)", "var(--s6)", "var(--s7)", "var(--s8)"];

/** Formats a value in its catalogue unit. */
export function fmtUnit(v: number, unit: string, decimals?: number): string {
  if (!Number.isFinite(v)) return "—";
  const d = (x: number, def: number) => num(x, decimals ?? def);
  switch (unit) {
    case "kg": return mass(v);
    case "kgco2e": return `${mass(v)} CO₂e`;
    case "pct": return `${d(v, 1)} %`;
    case "c": return `${d(v, 1)} °C`;
    case "bar": return `${d(v, 1)} bar`;
    case "km": return `${d(v, 1)} km`;
    case "m2": return Math.abs(v) >= 1e4 ? `${d(v / 1e4, 1)} ha` : `${d(v, 0)} m²`;
    case "tkm": return `${compact(v)} tkm`;
    case "kg_h": return `${d(v, 0)} kg/h`;
    case "rpm": return `${d(v, 0)} rpm`;
    case "count": return Math.abs(v) >= 1e4 ? compact(v) : num(v, 0);
  }
  return Math.abs(v) >= 1e4 ? compact(v) : d(v, 2);
}

/** Splits a formatted value into number and unit for hero figures. */
export function splitUnit(s: string): [string, string] {
  const m = s.match(/^([−\-]?[\d.,]+[kMB]?)\s*(.*)$/);
  return m ? [m[1], m[2]] : [s, ""];
}

/** Resolves a widget's own range, or the page's when it has none. */
export function resolveRange(r: WidgetRange | undefined, page: { from: string; to: string }): { from: string; to: string } {
  if (!r) return page;
  const to = new Date();
  to.setUTCHours(0, 0, 0, 0);
  to.setUTCDate(to.getUTCDate() + 1);
  const from = new Date(to);
  if (r === "all") return { from: "2000-01-01", to: to.toISOString().slice(0, 10) };
  if (r === "12m") from.setUTCMonth(from.getUTCMonth() - 12);
  else from.setUTCDate(from.getUTCDate() - (r === "7d" ? 7 : r === "30d" ? 30 : 90));
  return { from: from.toISOString().slice(0, 10), to: to.toISOString().slice(0, 10) };
}

export function useCatalog() {
  return useQuery({ queryKey: ["explore-catalog"], queryFn: () => api.get<Catalog>("/explore/catalog"), staleTime: 3_600_000 });
}

/** Audience tokens as people read them. */
export function audienceLabel(a: string, duty: Record<Duty, { name: string }>): string {
  if (a === "all") return "Everyone";
  if (a.startsWith("role:")) return { admin: "Admins", editor: "Editors", viewer: "Viewers" }[a.slice(5)] ?? a;
  if (a.startsWith("duty:")) return duty[a.slice(5) as Duty]?.name ?? a;
  return a;
}

export const newId = () => Math.random().toString(36).slice(2, 10);

/** A fresh widget of a type, with sensible data defaults. */
export function defaultWidget(type: WidgetType, y: number): Widget {
  const base = { id: newId(), type, x: 0, y, w: WIDGETS[type].w, h: WIDGETS[type].h, title: WIDGETS[type].name };
  switch (type) {
    case "text": return { ...base, title: "Note", options: { text: "" } };
    case "journey": return { ...base, title: "The journey, this period", options: { journeyStyle: "pipeline", journeyMeasure: "mass", animate: true } };
    case "sites_map": case "data_health": case "activity": return base;
    case "timeseries": return { ...base, title: "Mass over time", query: { source: "records", measure: "mass_kg", agg: "sum", interval: "auto" }, options: { style: "area" } };
    case "gauge": return { ...base, title: "Test pass rate", query: { source: "records", types: ["lab_test"], measure: "test_pass_rate", agg: "avg" }, options: { min: 0, max: 100, warn: 90, critical: 80 } };
    case "kpi": return { ...base, title: "Mass", query: { source: "records", measure: "mass_kg", agg: "sum", compare: true } };
    case "map": return { ...base, title: "By region", query: { source: "records", measure: "mass_kg", agg: "sum", groupBy: "region" } };
    default: return { ...base, title: "Mass by organisation", query: { source: "records", measure: "mass_kg", agg: "sum", groupBy: "org_id", limit: 8 } };
  }
}

type Tpl = { key: string; name: string; explain: string; audience: string[]; widgets: Omit<Widget, "id">[] };
const q = (x: Query): Query => x;

/** Starting points an admin can adapt. */
export const TEMPLATES: Tpl[] = [
  {
    key: "intake", name: "Intake desk", explain: "What arrives, from whom, and how much is still waiting.", audience: ["duty:receive"],
    widgets: [
      { type: "kpi", x: 0, y: 0, w: 3, h: 2, title: "Shipments received", query: q({ source: "records", types: ["shipment"], measure: "records", agg: "count", compare: true }) },
      { type: "kpi", x: 3, y: 0, w: 3, h: 2, title: "Mass received", query: q({ source: "records", types: ["shipment"], measure: "mass_kg", agg: "sum", compare: true }) },
      { type: "kpi", x: 6, y: 0, w: 3, h: 2, title: "Average distance", query: q({ source: "records", types: ["shipment"], measure: "distance_km", agg: "avg", compare: true }), options: { invert: true } },
      { type: "text", x: 9, y: 0, w: 3, h: 2, title: "Checklist", options: { text: "Weigh every load.\nSign the receipt with the weighbridge mass.\nReject loads with visible contamination." } },
      { type: "timeseries", x: 0, y: 2, w: 8, h: 4, title: "Incoming mass by sender kind", query: q({ source: "records", types: ["shipment"], measure: "mass_kg", agg: "sum", interval: "auto", groupBy: "org_kind", limit: 5 }), options: { style: "bars" } },
      { type: "bars", x: 8, y: 2, w: 4, h: 4, title: "Top senders", query: q({ source: "records", types: ["shipment"], measure: "mass_kg", agg: "sum", groupBy: "org_id", limit: 8 }) },
    ],
  },
  {
    key: "production", name: "Plant production", explain: "Output, dosing and process temperatures of your plants.", audience: ["duty:produce"],
    widgets: [
      { type: "kpi", x: 0, y: 0, w: 3, h: 2, title: "Produced", query: q({ source: "records", types: ["recycled_lot", "additive_batch", "asphalt_lot"], measure: "mass_kg", agg: "sum", compare: true }) },
      { type: "kpi", x: 3, y: 0, w: 3, h: 2, title: "Recycled plastic contained", query: q({ source: "records", types: ["asphalt_lot"], measure: "recycled_kg", agg: "sum", compare: true }) },
      { type: "gauge", x: 6, y: 0, w: 3, h: 3, title: "Average dosage", query: q({ source: "records", types: ["asphalt_lot"], measure: "dosage", agg: "avg" }), options: { min: 0, max: 2, warn: 1.4, critical: 1.5 } },
      { type: "kpi", x: 9, y: 0, w: 3, h: 2, title: "Missed dosing cycles", range: "30d", query: q({ source: "mixer_cycles", measure: "missed", agg: "sum", compare: true }), options: { invert: true } },
      { type: "timeseries", x: 0, y: 2, w: 6, h: 4, title: "Mix temperature", range: "30d", query: q({ source: "mixer_cycles", measure: "temp_c", agg: "avg", interval: "day", groupBy: "site_id", limit: 4 }), options: { style: "line" } },
      { type: "donut", x: 6, y: 3, w: 6, h: 4, title: "Output by mix", query: q({ source: "records", types: ["asphalt_lot"], measure: "mass_kg", agg: "sum", groupBy: "mix", limit: 6 }) },
    ],
  },
  {
    key: "quality", name: "Quality lab", explain: "Pass rates and results by test.", audience: ["duty:test", "duty:release"],
    widgets: [
      { type: "gauge", x: 0, y: 0, w: 3, h: 3, title: "Pass rate", query: q({ source: "records", types: ["lab_test"], measure: "test_pass_rate", agg: "avg" }), options: { min: 0, max: 100, warn: 95, critical: 90 } },
      { type: "kpi", x: 3, y: 0, w: 3, h: 2, title: "Tests", query: q({ source: "records", types: ["lab_test"], measure: "records", agg: "count", compare: true }) },
      { type: "table", x: 6, y: 0, w: 6, h: 4, title: "Pass rate by test", query: q({ source: "records", types: ["lab_test"], measure: "test_pass_rate", agg: "avg", groupBy: "test" }) },
      { type: "timeseries", x: 0, y: 3, w: 6, h: 4, title: "Pass rate over time", query: q({ source: "records", types: ["lab_test"], measure: "test_pass_rate", agg: "avg", interval: "auto" }), options: { style: "line" } },
    ],
  },
  {
    key: "impact", name: "Management impact", explain: "Plastic diverted, CO₂e avoided and where it went.", audience: ["role:viewer", "role:admin"],
    widgets: [
      { type: "kpi", x: 0, y: 0, w: 3, h: 2, title: "Recycled plastic laid", options: { help: HELP.recycledLaid }, query: q({ source: "records", types: ["delivery"], measure: "recycled_kg", agg: "sum", compare: true }) },
      { type: "kpi", x: 3, y: 0, w: 3, h: 2, title: "CO₂e avoided", options: { help: HELP.co2Avoided }, query: q({ source: "records", types: ["delivery"], measure: "avoided_kg", agg: "sum", compare: true }) },
      { type: "kpi", x: 6, y: 0, w: 3, h: 2, title: "Road surfaced", options: { help: HELP.roadSurfaced }, query: q({ source: "records", types: ["road_section"], measure: "length_km", agg: "sum", compare: true }) },
      { type: "kpi", x: 9, y: 0, w: 3, h: 2, title: "Chain footprint", query: q({ source: "records", types: ["delivery"], measure: "chain_kg", agg: "sum", compare: true }), options: { invert: true } },
      { type: "timeseries", x: 0, y: 2, w: 7, h: 4, title: "Recycled plastic reaching roads", query: q({ source: "records", types: ["delivery"], measure: "recycled_kg", agg: "sum", interval: "auto" }), options: { style: "area" } },
      { type: "map", x: 7, y: 2, w: 5, h: 5, title: "Road surfaced by region", query: q({ source: "records", types: ["road_section"], measure: "length_km", agg: "sum", groupBy: "region" }) },
    ],
  },
];

/** The Overview everyone starts from until they or their admins change it. */
export const DEFAULT_OVERVIEW: Omit<Widget, "id">[] = [
  { type: "kpi", x: 0, y: 0, w: 3, h: 2, title: "Recycled plastic laid", options: { help: HELP.recycledLaid }, query: q({ source: "records", types: ["delivery"], measure: "recycled_kg", agg: "sum", compare: true }) },
  { type: "kpi", x: 3, y: 0, w: 3, h: 2, title: "CO₂e avoided", options: { help: HELP.co2Avoided }, query: q({ source: "records", types: ["delivery"], measure: "avoided_kg", agg: "sum", compare: true }) },
  { type: "kpi", x: 6, y: 0, w: 3, h: 2, title: "Asphalt with additive", options: { help: HELP.asphaltWithAdditive }, query: q({ source: "records", types: ["asphalt_lot"], measure: "mass_kg", agg: "sum", compare: true }) },
  { type: "kpi", x: 9, y: 0, w: 3, h: 2, title: "Road surfaced", options: { help: HELP.roadSurfaced }, query: q({ source: "records", types: ["road_section"], measure: "length_km", agg: "sum", compare: true }) },
  { type: "journey", x: 0, y: 2, w: 12, h: 3, title: "The journey, this period", options: { journeyStyle: "pipeline", journeyMeasure: "mass", animate: true, help: HELP.journey } },
  { type: "timeseries", x: 0, y: 5, w: 7, h: 5, title: "Recycled plastic reaching roads", query: q({ source: "records", types: ["delivery"], measure: "recycled_kg", agg: "sum", interval: "auto" }), options: { style: "bars", color: "var(--s2)" } },
  { type: "sites_map", x: 7, y: 5, w: 5, h: 5, title: "Where it happens" },
  { type: "timeseries", x: 0, y: 10, w: 6, h: 4, title: "Material through the chain", query: q({ source: "records", types: ["reject_lot", "recycled_lot", "additive_batch"], measure: "mass_kg", agg: "sum", interval: "auto", groupBy: "type" }), options: { style: "area" } },
  { type: "data_health", x: 6, y: 10, w: 3, h: 4, title: "Data health", options: { help: HELP.dataHealth } },
  { type: "activity", x: 9, y: 10, w: 3, h: 4, title: "Latest activity" },
];

const PRODUCES: Partial<Record<OrgKind, { type: string; name: string }>> = {
  sorting_plant: { type: "reject_lot", name: "Reject film sorted" }, recycler: { type: "recycled_lot", name: "Recycled plastic produced" },
  additive_producer: { type: "additive_batch", name: "Additive produced" }, asphalt_plant: { type: "asphalt_lot", name: "Asphalt produced" },
};

/**
 * The starting Overview for a member: a plant sees what it made, received and
 * shipped; road owners, contractors and oversight see the impact on roads.
 */
export function defaultOverview(kind: OrgKind, orgId: string, oversight: boolean): Omit<Widget, "id">[] {
  const p = PRODUCES[kind];
  if (kind === "lab" && !oversight) {
    // A laboratory measures other companies' material: its tests, not road impact.
    const mine = [{ field: "org_id", op: "eq" as const, value: orgId }];
    const lab: Omit<Widget, "id">[] = [
      { type: "kpi", x: 0, y: 0, w: 3, h: 2, title: "Tests reported", options: { help: "Lab tests your laboratory signed in the period." }, query: q({ source: "records", types: ["lab_test"], measure: "records", agg: "count", compare: true, filters: mine }) },
      { type: "kpi", x: 3, y: 0, w: 3, h: 2, title: "Pass rate", options: { help: "Share of your tests whose result was within specification." }, query: q({ source: "records", types: ["lab_test"], measure: "test_pass_rate", agg: "avg", compare: true, filters: mine }) },
      { type: "bars", x: 6, y: 0, w: 6, h: 4, title: "Tests by method", query: q({ source: "records", types: ["lab_test"], measure: "records", agg: "count", groupBy: "test", limit: 10, filters: mine }) },
      { type: "timeseries", x: 0, y: 2, w: 6, h: 4, title: "Tests over time", query: q({ source: "records", types: ["lab_test"], measure: "records", agg: "count", interval: "auto", groupBy: "test", limit: 5, filters: mine }), options: { style: "bars" } },
      { ...DEFAULT_OVERVIEW[8], x: 0, y: 6, w: 6, h: 4 }, { ...DEFAULT_OVERVIEW[9], x: 6, y: 6, w: 6, h: 4 },
    ];
    return lab;
  }
  if (!p || oversight) return DEFAULT_OVERVIEW;
  const kpis: Omit<Widget, "id">[] = [
    { type: "kpi", x: 0, y: 0, w: 3, h: 2, title: p.name, options: { help: "What your plants produced in the period, from your signed production records." }, query: q({ source: "records", types: [p.type], measure: "mass_kg", agg: "sum", compare: true }) },
    { type: "kpi", x: 3, y: 0, w: 3, h: 2, title: "Received", options: { help: "Mass of the loads other companies shipped to you in the period." }, query: q({ source: "records", types: ["shipment"], measure: "mass_kg", agg: "sum", compare: true, filters: [{ field: "counterparty", op: "eq", value: orgId }] }) },
    // Asphalt leaves as truckload deliveries to road sections, not as shipments.
    kind === "asphalt_plant"
      ? { type: "kpi", x: 6, y: 0, w: 3, h: 2, title: "Delivered", options: { help: "Asphalt you delivered to road sections in the period, from your delivery tickets." }, query: q({ source: "records", types: ["delivery"], measure: "mass_kg", agg: "sum", compare: true, filters: [{ field: "org_id", op: "eq", value: orgId }] }) }
      : { type: "kpi", x: 6, y: 0, w: 3, h: 2, title: "Shipped on", options: { help: "Mass you shipped to the next step of the chain in the period." }, query: q({ source: "records", types: ["shipment"], measure: "mass_kg", agg: "sum", compare: true, filters: [{ field: "org_id", op: "eq", value: orgId }] }) },
    { type: "kpi", x: 9, y: 0, w: 3, h: 2, title: "Lab tests passed", options: { help: "Share of your lab tests in the period whose result was within specification." }, query: q({ source: "records", types: ["lab_test"], measure: "test_pass_rate", agg: "avg", compare: true }) },
  ];
  return [
    ...kpis,
    DEFAULT_OVERVIEW[4],
    { type: "timeseries", x: 0, y: 5, w: 7, h: 5, title: p.name, query: q({ source: "records", types: [p.type], measure: "mass_kg", agg: "sum", interval: "auto" }), options: { style: "bars", color: "var(--s2)" } },
    ...DEFAULT_OVERVIEW.slice(6),
  ];
}

export const withIds = (ws: Omit<Widget, "id">[]): Widget[] => ws.map((w) => ({ ...w, id: newId() }));

/**
 * Explains a widget in words: its own description when it has one, otherwise
 * what it computes, from the catalogue ("Total of Mass for Shipments, by
 * Organisation, over the page's period, compared with the previous period").
 */
export function describeWidget(w: Widget, cat?: Catalog): string {
  if (w.options?.help) return w.options.help;
  if (!w.query) return WIDGETS[w.type].explain;
  const q = w.query;
  const src = cat?.sources.find((s) => s.key === q.source);
  const field = src?.fields.find((f) => f.key === q.measure);
  const by = q.groupBy ? src?.fields.find((f) => f.key === q.groupBy)?.label : undefined;
  const agg = AGGS.find((a) => a.value === q.agg)?.label ?? q.agg;
  const parts = [
    q.measure === "records" || q.agg === "count" ? `Number of ${(q.types?.length ? q.types.join(", ").replace(/_/g, " ") + " records" : src?.label.toLowerCase() ?? "records")}`
      : `${agg} of ${field?.label.toLowerCase() ?? q.measure}${q.types?.length ? ` for ${q.types.join(", ").replace(/_/g, " ")}` : ""}`,
    by ? `by ${by.toLowerCase()}` : "",
    w.type === "timeseries" ? "over time" : "",
    w.range ? `in the last ${RANGES.find((r) => r.value === w.range)?.label.toLowerCase() ?? w.range}` : "over the period picked at the top",
    q.compare ? "compared with the previous period of the same length" : "",
  ].filter(Boolean).join(", ");
  return `${parts}.${field?.help ? " " + field.help : ""}`;
}
