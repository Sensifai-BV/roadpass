// Calendar arithmetic in the viewer's time zone (format.timeZone()). Days are
// civil dates {y, m, d} (m 1–12); instants are Date. A day starts at local
// midnight in the zone, which DST can move, so every boundary is converted.
import { timeZone } from "./format";

export interface Civil { y: number; m: number; d: number }

const fmts = new Map<string, Intl.DateTimeFormat>();
function fmt(zone: string) {
  let f = fmts.get(zone);
  if (!f) {
    f = new Intl.DateTimeFormat("en-US", { timeZone: zone, hourCycle: "h23", year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "numeric", second: "numeric" });
    fmts.set(zone, f);
  }
  return f;
}

/** Wall-clock parts of an instant in the viewer's zone. */
export function wall(t: Date | number, zone = timeZone()) {
  const p: Record<string, number> = {};
  for (const x of fmt(zone).formatToParts(new Date(t))) if (x.type !== "literal") p[x.type] = Number(x.value);
  return { y: p.year, m: p.month, d: p.day, h: p.hour % 24, min: p.minute, s: p.second };
}

/** The zone's offset from UTC at an instant, in ms. */
function offset(ms: number, zone: string): number {
  const w = wall(ms, zone);
  return Date.UTC(w.y, w.m - 1, w.d, w.h, w.min, w.s) - Math.floor(ms / 1000) * 1000;
}

/** The instant a wall-clock time in the viewer's zone names. */
export function zoned(c: Civil, h = 0, min = 0, zone = timeZone()): Date {
  const guess = Date.UTC(c.y, c.m - 1, c.d, h, min);
  let t = guess - offset(guess, zone);
  t = guess - offset(t, zone); // second pass settles DST edges
  return new Date(t);
}

export const civilOf = (t: Date | number): Civil => { const w = wall(t); return { y: w.y, m: w.m, d: w.d }; };
export const today = () => civilOf(Date.now());
export const key = (c: Civil) => `${c.y}-${String(c.m).padStart(2, "0")}-${String(c.d).padStart(2, "0")}`;
export const sameDay = (a: Civil, b: Civil) => a.y === b.y && a.m === b.m && a.d === b.d;

export function addDays(c: Civil, n: number): Civil {
  const t = new Date(Date.UTC(c.y, c.m - 1, c.d + n));
  return { y: t.getUTCFullYear(), m: t.getUTCMonth() + 1, d: t.getUTCDate() };
}
export function addMonths(c: Civil, n: number): Civil {
  const t = new Date(Date.UTC(c.y, c.m - 1 + n, 1));
  const last = new Date(Date.UTC(t.getUTCFullYear(), t.getUTCMonth() + 1, 0)).getUTCDate();
  return { y: t.getUTCFullYear(), m: t.getUTCMonth() + 1, d: Math.min(c.d, last) };
}
/** Day of week, Monday = 0. */
export const weekday = (c: Civil) => (new Date(Date.UTC(c.y, c.m - 1, c.d)).getUTCDay() + 6) % 7;
export const startOfWeek = (c: Civil) => addDays(c, -weekday(c));

/** Minutes since local midnight of an instant. */
export function minuteOfDay(t: Date | number): number {
  const w = wall(t);
  return w.h * 60 + w.min;
}

/** "YYYY-MM-DDTHH:mm" for a datetime-local input, in the viewer's zone. */
export function toInput(t: Date | number): string {
  const w = wall(t);
  return `${key(w)}T${String(w.h).padStart(2, "0")}:${String(w.min).padStart(2, "0")}`;
}
/** The instant a datetime-local value names in the viewer's zone. */
export function fromInput(v: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(v);
  if (!m) return null;
  return zoned({ y: +m[1], m: +m[2], d: +m[3] }, +m[4], +m[5]);
}

const label = (o: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat("en-GB", { ...o, timeZone: "UTC" });
const L = {
  dayLong: label({ weekday: "long", day: "numeric", month: "long", year: "numeric" }),
  dayShort: label({ weekday: "short", day: "numeric" }),
  monthYear: label({ month: "long", year: "numeric" }),
  dayMonth: label({ day: "numeric", month: "short" }),
  weekdayShort: label({ weekday: "short" }),
};
const asUTC = (c: Civil) => new Date(Date.UTC(c.y, c.m - 1, c.d));
/** Labels for civil dates (no zone conversion needed: they are already local). */
export const civilLabel = {
  dayLong: (c: Civil) => L.dayLong.format(asUTC(c)),
  dayShort: (c: Civil) => L.dayShort.format(asUTC(c)),
  monthYear: (c: Civil) => L.monthYear.format(asUTC(c)),
  dayMonth: (c: Civil) => L.dayMonth.format(asUTC(c)),
  weekdayShort: (c: Civil) => L.weekdayShort.format(asUTC(c)),
};

/** "14:05" in the viewer's zone. */
export function hm(t: Date | number | string): string {
  const w = wall(new Date(t));
  return `${String(w.h).padStart(2, "0")}:${String(w.min).padStart(2, "0")}`;
}
