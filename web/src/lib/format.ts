// Number and date formatting. Masses are stored in kg and shown in the unit a
// person would say out loud (kg, t, kt); CO₂e likewise.

const nf = (max: number, min = 0) => new Intl.NumberFormat("en-GB", { maximumFractionDigits: max, minimumFractionDigits: min });
const n0 = nf(0);
const n1 = nf(1);
const n2 = nf(2);

export function num(x: number, digits = 0): string {
  if (!Number.isFinite(x)) return "—";
  return digits === 0 ? n0.format(x) : digits === 1 ? n1.format(x) : n2.format(x);
}

export function compact(x: number): string {
  if (!Number.isFinite(x)) return "—";
  const a = Math.abs(x);
  if (a >= 1e9) return `${n1.format(x / 1e9)}B`;
  if (a >= 1e6) return `${n1.format(x / 1e6)}M`;
  if (a >= 1e4) return `${n0.format(x / 1e3)}k`;
  if (a >= 1e3) return `${n1.format(x / 1e3)}k`;
  return n0.format(x);
}

/** Mass from kilograms: "840 kg", "12.4 t", "3.6 kt", "1.2 Mt". */
export function mass(kg: number): string {
  if (!Number.isFinite(kg)) return "—";
  const a = Math.abs(kg);
  if (a >= 1e9) return `${n2.format(kg / 1e9)} Mt`;
  if (a >= 1e7) return `${n0.format(kg / 1e6)} kt`;
  if (a >= 1e6) return `${n1.format(kg / 1e6)} kt`;
  if (a >= 1e4) return `${n0.format(kg / 1e3)} t`;
  if (a >= 1e3) return `${n1.format(kg / 1e3)} t`;
  return `${n0.format(kg)} kg`;
}

/** Mass to the kilogram ("11,850 kg"), for work items people weigh and act on. */
export function exactMass(kg: number): string {
  return Number.isFinite(kg) ? `${n0.format(kg)} kg` : "—";
}

/** Split mass into value and unit for hero numbers. */
export function massParts(kg: number): [string, string] {
  const s = mass(kg);
  const i = s.lastIndexOf(" ");
  return [s.slice(0, i), s.slice(i + 1)];
}

/** CO₂-equivalent from kg. */
export function co2(kg: number): string {
  return `${mass(kg)} CO₂e`;
}

export function pct(x: number, digits = 1): string {
  if (!Number.isFinite(x)) return "—";
  return `${digits === 0 ? n0.format(x) : digits === 1 ? n1.format(x) : n2.format(x)} %`;
}

export function delta(cur: number, prev: number): number | null {
  if (!prev || !Number.isFinite(prev)) return null;
  return ((cur - prev) / Math.abs(prev)) * 100;
}

// Event times (records, receipts, seals) are shown in the user's time zone (set
// from their settings; the browser's when unset). Chart buckets are UTC days and
// months, so their labels are formatted in UTC whatever the zone.
const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
let zone = browserZone;
const make = () => ({
  date: new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric", timeZone: zone }),
  dateTime: new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit", timeZone: zone }),
  short: new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", timeZone: zone }),
});
let f = make();
const monthFmt = new Intl.DateTimeFormat("en-GB", { month: "short", year: "2-digit", timeZone: "UTC" });

/** Sets the zone event times are shown in ("" = the browser's). */
export function setTimeZone(tz: string) {
  const next = tz || browserZone;
  if (next === zone) return;
  try {
    new Intl.DateTimeFormat("en-GB", { timeZone: next });
    zone = next;
    f = make();
  } catch {
    /* unknown zone: keep the current one */
  }
}
export const timeZone = () => zone;
export const browserTimeZone = () => browserZone;

export const date = (s?: string | Date) => (s ? f.date.format(new Date(s)) : "—");
export const dateTime = (s?: string | Date) => (s ? f.dateTime.format(new Date(s)) : "—");
export const month = (s: string | Date) => monthFmt.format(new Date(s));
export const shortDate = (s: string | Date) => f.short.format(new Date(s));

export function ago(s?: string): string {
  if (!s) return "—";
  const d = (Date.now() - new Date(s).getTime()) / 1000;
  if (d < 60) return "just now";
  if (d < 3600) return `${Math.floor(d / 60)} min ago`;
  if (d < 86400) return `${Math.floor(d / 3600)} h ago`;
  if (d < 86400 * 30) return `${Math.floor(d / 86400)} d ago`;
  return date(s);
}

export function shortHash(h?: string, n = 10): string {
  if (!h) return "—";
  return h.length > n * 2 ? `${h.slice(0, n)}…${h.slice(-4)}` : h;
}

export function titleCase(s: string): string {
  return s.replace(/_/g, " ").replace(/^\w/, (c) => c.toUpperCase());
}
