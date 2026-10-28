import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarClock, CalendarPlus, ChevronLeft, ChevronRight, CircleAlert, CircleCheck, Link2 } from "lucide-react";
import clsx from "clsx";
import { api, qs } from "../lib/api";
import { useDirectory, useMe, useMeta } from "../lib/hooks";
import type { Me, Meta, OrgKind, RecordT, RecordType, Status } from "../lib/types";
import { DUTY, KIND, STAGES, TYPE, dutyFor, siteKindName, siteLabel } from "../lib/vocab";
import { dateTime, mass, timeZone } from "../lib/format";
import {
  addDays, addMonths, civilLabel, civilOf, fromInput, hm, key, minuteOfDay, sameDay, startOfWeek, toInput, today, zoned, type Civil,
} from "../lib/zoned";
import { Card, Drawer, Empty, ErrorBox, KV, Legend, Modal, PageHeader, RecordLink, Segmented, Skel } from "../components/ui";

// ---------------------------------------------------------------- data

interface CalEvent {
  kind: RecordType | "receipt"; recordId: string; recordType: RecordType; ref: string; at: string; orgId: string;
  counterpartyOrgId?: string; ownerOrgId?: string; siteId?: string; massKg: number; status: Status; decision?: string; root?: string;
}
type PlanStatus = "planned" | "done" | "cancelled";
interface WorkPlan {
  id: string; orgId: string; recordType: RecordType; title: string; siteId?: string; counterpartyOrgId?: string; toSiteId?: string; ownerOrgId?: string;
  startsAt: string; endsAt?: string; massKg: number; note: string; status: PlanStatus; recordId?: string; doneAt?: string; doneBy?: string;
  createdBy: string; createdAt: string; updatedAt: string; overdue: boolean;
}
interface CalendarData { events: CalEvent[]; plans: WorkPlan[]; truncated: boolean }

type View = "day" | "week" | "month";
type Show = "all" | "records" | "plans";

const PLANNABLE: RecordType[] = ["reject_lot", "recycled_lot", "additive_batch", "asphalt_lot", "shipment", "delivery", "road_section"];
const LOT_TYPES: RecordType[] = ["reject_lot", "recycled_lot", "additive_batch", "asphalt_lot"];
const STAGE_TYPES: RecordType[] = [...LOT_TYPES, "delivery", "road_section"];

/** What a plan of type t is called. */
function planKind(t: RecordType): string {
  if (LOT_TYPES.includes(t)) return `Production run · ${TYPE[t].name.toLowerCase()}`;
  if (t === "road_section") return "Road-section paving";
  return TYPE[t].name;
}

/** The record types the signed-in member may plan (mirrors the server's mayPlan). */
function plannableFor(me: Me | undefined, meta: Meta | undefined): RecordType[] {
  if (!me || !meta || me.org.kind === "reporter" || me.org.status === "suspended") return [];
  const kinds = new Set<OrgKind>([me.org.kind, ...(me.org.kinds ?? [])]);
  return PLANNABLE.filter((t) => {
    const info = meta.types.find((x) => x.type === t);
    const made = kinds.has("operator") || !!info?.createdBy.some((k) => kinds.has(k));
    const steps = me.user.role === "admin" ? [] : me.user.steps ?? [];
    const inSteps = !steps.length || !!info?.createdBy.some((k) => steps.includes(k));
    return made && me.duties.includes(dutyFor(t)) && inSteps;
  });
}

// ---------------------------------------------------------------- layout model

interface Item {
  id: string; start: Date; end: Date; allDay: boolean; color: string; title: string; sub: string;
  plan?: WorkPlan; event?: CalEvent;
}

const HOUR = 44; // px per hour in the day and week grids
const POINT_MIN = 30; // a record is drawn as a 30-minute block

function rangeOf(view: View, anchor: Civil): Civil[] {
  if (view === "day") return [anchor];
  if (view === "week") return Array.from({ length: 7 }, (_, i) => addDays(startOfWeek(anchor), i));
  const first = { ...anchor, d: 1 };
  const last = addDays(addMonths(first, 1), -1);
  const start = startOfWeek(first);
  const n = Math.round((Date.UTC(last.y, last.m - 1, last.d) - Date.UTC(start.y, start.m - 1, start.d)) / 864e5) + 1;
  return Array.from({ length: Math.ceil(n / 7) * 7 }, (_, i) => addDays(start, i));
}

/** Days (keys) an item covers. */
function daysOf(it: Item): string[] {
  const out: string[] = [];
  let c = civilOf(it.start);
  const last = civilOf(new Date(Math.max(it.start.getTime(), it.end.getTime() - 1)));
  for (let i = 0; i < 40; i++) {
    out.push(key(c));
    if (sameDay(c, last)) break;
    c = addDays(c, 1);
  }
  return out;
}

/** Side-by-side lanes for overlapping blocks in one day column. */
function lanes(items: { it: Item; top: number; bottom: number }[]) {
  const sorted = [...items].sort((a, b) => a.top - b.top || b.bottom - a.bottom);
  const out: { it: Item; top: number; bottom: number; lane: number; of: number }[] = [];
  let cluster: typeof out = [];
  let clusterEnd = -1;
  let laneEnds: number[] = [];
  const flush = () => {
    const of = laneEnds.length || 1;
    for (const c of cluster) c.of = of;
    out.push(...cluster);
    cluster = [];
    laneEnds = [];
  };
  for (const x of sorted) {
    if (x.top >= clusterEnd && cluster.length) flush();
    let lane = laneEnds.findIndex((e) => e <= x.top);
    if (lane < 0) { lane = laneEnds.length; laneEnds.push(x.bottom); } else laneEnds[lane] = x.bottom;
    cluster.push({ ...x, lane, of: 1 });
    clusterEnd = Math.max(clusterEnd, x.bottom);
  }
  flush();
  return out;
}

// ---------------------------------------------------------------- page

export default function CalendarPage() {
  const me = useMe().data;
  const meta = useMeta().data;
  const dir = useDirectory();
  const nav = useNavigate();
  const [sp, setSp] = useSearchParams();
  const view = (["day", "week", "month"].includes(sp.get("view") ?? "") ? sp.get("view") : "week") as View;
  const anchor: Civil = useMemo(() => {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(sp.get("date") ?? "");
    return m ? { y: +m[1], m: +m[2], d: +m[3] } : today();
  }, [sp]);
  const go = (v: View, c: Civil) => setSp((p) => { const n = new URLSearchParams(p); n.set("view", v); n.set("date", key(c)); return n; }, { replace: true });
  const [show, setShow] = useState<Show>("all");
  const [open, setOpen] = useState<string | null>(null); // plan id in the drawer
  const [form, setForm] = useState<{ plan?: WorkPlan; start?: Date } | null>(null);

  const days = useMemo(() => rangeOf(view, anchor), [view, anchor]);
  const from = zoned(days[0]);
  const to = zoned(addDays(days[days.length - 1], 1));
  const q = useQuery({
    queryKey: ["calendar", from.toISOString(), to.toISOString()],
    queryFn: ({ signal }) => api.get<CalendarData>(`/calendar${qs({ from: from.toISOString(), to: to.toISOString() })}`, signal),
    placeholderData: keepPreviousData,
  });
  const attention = useQuery({ queryKey: ["plans", "planned"], queryFn: () => api.get<WorkPlan[]>("/plans?status=planned") });
  const canPlan = plannableFor(me, meta);

  const colorOf = (t: RecordType, orgId: string) =>
    STAGE_TYPES.includes(t) ? TYPE[t].color : KIND[dir.org(orgId)?.kind ?? "operator"]?.color ?? "var(--muted)";

  const items: Item[] = useMemo(() => {
    const out: Item[] = [];
    if (!q.data) return out;
    if (show !== "plans") {
      for (const e of q.data.events) {
        const start = new Date(e.at);
        const receipt = e.kind === "receipt";
        const who = receipt ? e.counterpartyOrgId ?? e.orgId : e.orgId;
        const other = receipt ? e.orgId : e.counterpartyOrgId ?? e.ownerOrgId;
        out.push({
          id: `${e.kind}:${e.recordId}`, start, end: new Date(start.getTime() + POINT_MIN * 60e3), allDay: false,
          color: colorOf(e.recordType, who), event: e,
          title: receipt ? "Shipment received" : TYPE[e.recordType]?.name ?? e.recordType,
          sub: [e.recordId, e.massKg ? mass(e.massKg) : "", receipt ? `from ${dir.orgName(other)}` : other ? `${e.recordType === "road_section" ? "owner" : "to"} ${dir.orgName(other)}` : dir.orgName(e.orgId)]
            .filter(Boolean).join(" · "),
        });
      }
    }
    if (show !== "records") {
      for (const p of q.data.plans) {
        const start = new Date(p.startsAt);
        const end = p.endsAt ? new Date(p.endsAt) : new Date(start.getTime() + 60 * 60e3);
        out.push({
          id: `plan:${p.id}`, start, end, allDay: end.getTime() - start.getTime() >= 12 * 3600e3 || !sameDay(civilOf(start), civilOf(end.getTime() - 1)),
          color: colorOf(p.recordType, p.orgId), plan: p,
          title: p.title || planKind(p.recordType),
          sub: [`Planned by ${dir.orgName(p.orgId)}`, p.massKg ? mass(p.massKg) : "", p.toSiteId && dir.site(p.toSiteId) ? `to ${dir.site(p.toSiteId)!.name}` : p.counterpartyOrgId ? `to ${dir.orgName(p.counterpartyOrgId)}` : "",
            p.ownerOrgId ? `owner ${dir.orgName(p.ownerOrgId)}` : ""].filter(Boolean).join(" · "),
        });
      }
    }
    return out;
  }, [q.data, show, dir]);

  const byDay = useMemo(() => {
    const m = new Map<string, Item[]>();
    for (const it of items) for (const k of daysOf(it)) m.set(k, [...(m.get(k) ?? []), it]);
    for (const v of m.values()) v.sort((a, b) => a.start.getTime() - b.start.getTime());
    return m;
  }, [items]);

  const onItem = (it: Item) => {
    if (it.plan) setOpen(it.plan.id);
    else if (it.event) nav(`/passports/${it.event.root || it.event.recordId}`);
  };
  const step = (n: number) => go(view, view === "month" ? addMonths({ ...anchor, d: 1 }, n) : addDays(anchor, n * (view === "week" ? 7 : 1)));
  const title = view === "month" ? civilLabel.monthYear(anchor) : view === "day" ? civilLabel.dayLong(anchor)
    : `${civilLabel.dayMonth(days[0])} – ${civilLabel.dayMonth(days[6])} ${days[6].y}`;
  const overdue = (attention.data ?? []).filter((p) => p.overdue);
  const upcoming = (attention.data ?? []).filter((p) => !p.overdue).slice(0, 6);
  const planOpen = q.data?.plans.find((p) => p.id === open) ?? attention.data?.find((p) => p.id === open);

  return (
    <div>
      <PageHeader title="Calendar"
        sub={<>Records of your chain at the time they happened, and the work planned ahead. Times in {timeZone().replace(/_/g, " ")}.</>}
        actions={canPlan.length ? <button className="btn btn-primary" onClick={() => setForm({})}><CalendarPlus size={15} aria-hidden /> Plan work</button> : undefined} />

      <Card pad={false}>
        <div className="flex flex-wrap items-center gap-2 border-b border-line px-4 py-3">
          <div className="flex items-center gap-1">
            <button className="btn btn-sm" onClick={() => go(view, today())}>Today</button>
            <button className="btn btn-ghost btn-icon" aria-label="Previous" onClick={() => step(-1)}><ChevronLeft size={16} /></button>
            <button className="btn btn-ghost btn-icon" aria-label="Next" onClick={() => step(1)}><ChevronRight size={16} /></button>
          </div>
          <h2 className="mr-auto text-[15px] font-semibold tracking-tight" aria-live="polite">{title}</h2>
          <Segmented label="Show" value={show} onChange={setShow} options={[{ value: "all", label: "All" }, { value: "records", label: "Records" }, { value: "plans", label: "Plans" }]} />
          <Segmented label="View" value={view} onChange={(v) => go(v, anchor)} options={[{ value: "day", label: "Day" }, { value: "week", label: "Week" }, { value: "month", label: "Month" }]} />
        </div>
        {q.error ? <div className="p-4"><ErrorBox error={q.error} /></div> : null}
        {q.data?.truncated && <p className="px-4 pt-3 text-[12px] text-muted">Showing the first 3000 records of this span; switch to the week or day view to see all.</p>}
        {!q.data ? <div className="p-4"><Skel h={480} /></div>
          : view === "month"
            ? <MonthGrid days={days} anchor={anchor} byDay={byDay} onItem={onItem} onDay={(c) => go("day", c)} />
            : <TimeGrid days={days} byDay={byDay} onItem={onItem} onSlot={canPlan.length ? (start) => setForm({ start }) : undefined} onDay={(c) => go("day", c)} />}
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t border-line px-4 py-3">
          <Legend items={[...STAGES.map((s) => ({ label: s.title, color: s.color })), { label: "Lab", color: "var(--s7)" }]} />
          <span className="flex items-center gap-1.5 text-[11.5px] text-muted"><span className="inline-block h-3 w-5 rounded border border-dashed border-text-2" /> Planned</span>
          <span className="flex items-center gap-1.5 text-[11.5px] text-muted"><span className="inline-block h-3 w-5 rounded ring-2 ring-[var(--critical)]" /> Overdue</span>
        </div>
      </Card>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Card title="Overdue plans" sub="Planned work whose time has passed without being marked done.">
          {attention.isLoading ? <Skel h={80} /> : overdue.length ? <PlanList plans={overdue} onOpen={setOpen} /> :
            <Empty icon={<CircleCheck size={20} />} title="Nothing overdue">Every plan is on time, done or cancelled.</Empty>}
        </Card>
        <Card title="Coming up" sub="The next open plans you are involved in.">
          {attention.isLoading ? <Skel h={80} /> : upcoming.length ? <PlanList plans={upcoming} onOpen={setOpen} /> :
            <Empty icon={<CalendarClock size={20} />} title="No open plans">{canPlan.length ? "Plan a production run, shipment, delivery or road section with Plan work." : "Plans that involve your company appear here."}</Empty>}
        </Card>
      </div>

      {planOpen && <PlanDrawer plan={planOpen} me={me} canPlan={canPlan} onClose={() => setOpen(null)} onEdit={() => { setForm({ plan: planOpen }); setOpen(null); }} />}
      {form && me && <PlanForm me={me} types={canPlan} plan={form.plan} start={form.start} onClose={() => setForm(null)} onSaved={(p) => { setForm(null); setOpen(p.id); go(view, civilOf(new Date(p.startsAt))); }} />}
    </div>
  );
}

// ---------------------------------------------------------------- views

function Chip({ it, onClick, compact }: { it: Item; onClick: () => void; compact?: boolean }) {
  const p = it.plan;
  const muted = p && p.status !== "planned";
  return (
    <button type="button" onClick={(e) => { e.stopPropagation(); onClick(); }}
      title={`${it.allDay ? dateTime(it.start) : hm(it.start)} ${it.title}\n${it.sub}${p?.overdue ? "\nOverdue" : ""}`}
      className={clsx("flex w-full min-w-0 items-center gap-1.5 rounded-md px-1.5 text-left text-[11.5px] leading-[18px] transition hover:brightness-95",
        p ? "border border-dashed" : "border-l-[3px]", p?.overdue && "ring-2 ring-[var(--critical)]", muted && "opacity-55", p?.status === "cancelled" && "line-through")}
      style={{ borderColor: it.color, background: `color-mix(in srgb, ${it.color} ${p ? 8 : 16}%, transparent)` }}>
      {!compact && !it.allDay && <span className="num shrink-0 text-muted">{hm(it.start)}</span>}
      {p?.status === "done" && <CircleCheck size={11} className="shrink-0" aria-label="Done" />}
      {p?.overdue && <CircleAlert size={11} className="shrink-0 text-critical" aria-label="Overdue" />}
      <span className="truncate font-medium">{it.title}</span>
    </button>
  );
}

function MonthGrid({ days, anchor, byDay, onItem, onDay }: {
  days: Civil[]; anchor: Civil; byDay: Map<string, Item[]>; onItem: (it: Item) => void; onDay: (c: Civil) => void;
}) {
  const now = today();
  const MAX = 3;
  return (
    <div role="grid" aria-label="Month">
      <div className="grid grid-cols-7 border-b border-line text-[11px] font-semibold uppercase tracking-wide text-muted" role="row">
        {days.slice(0, 7).map((d) => <div key={key(d)} className="px-2 py-1.5" role="columnheader">{civilLabel.weekdayShort(d)}</div>)}
      </div>
      <div className="grid grid-cols-7">
        {days.map((d) => {
          const list = byDay.get(key(d)) ?? [];
          const other = d.m !== anchor.m;
          return (
            <div key={key(d)} role="gridcell" className={clsx("min-h-[104px] min-w-0 border-b border-r border-line p-1 [&:nth-child(7n)]:border-r-0", other && "bg-surface-2/60")}>
              <button type="button" onClick={() => onDay(d)} aria-label={`Open ${civilLabel.dayLong(d)}`}
                className={clsx("mb-0.5 grid size-6 place-items-center rounded-full text-[12px] font-semibold num hover:bg-surface-3",
                  sameDay(d, now) ? "bg-[var(--text)] text-[var(--bg)] hover:bg-[var(--text)]" : other ? "text-muted" : "")}>{d.d}</button>
              <div className="hidden flex-col gap-0.5 sm:flex">
                {list.slice(0, MAX).map((it) => <Chip key={it.id} it={it} onClick={() => onItem(it)} compact />)}
                {list.length > MAX && <button type="button" className="px-1.5 text-left text-[11px] font-medium text-text-2 hover:underline" onClick={() => onDay(d)}>+{list.length - MAX} more</button>}
              </div>
              {list.length > 0 && (
                <button type="button" className="flex flex-wrap gap-0.5 px-1 sm:hidden" onClick={() => onDay(d)} aria-label={`${list.length} items`}>
                  {list.slice(0, 6).map((it) => <span key={it.id} className="size-1.5 rounded-full" style={{ background: it.color }} />)}
                </button>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function TimeGrid({ days, byDay, onItem, onSlot, onDay }: {
  days: Civil[]; byDay: Map<string, Item[]>; onItem: (it: Item) => void; onSlot?: (start: Date) => void; onDay: (c: Civil) => void;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const now = today();
  const [nowMin, setNowMin] = useState(() => minuteOfDay(Date.now()));
  useEffect(() => {
    const t = window.setInterval(() => setNowMin(minuteOfDay(Date.now())), 60_000);
    return () => window.clearInterval(t);
  }, []);
  // Open at the working day (or the first item of the span), not at midnight.
  const firstMin = Math.min(...days.flatMap((d) => (byDay.get(key(d)) ?? []).filter((i) => !i.allDay && sameDay(civilOf(i.start), d)).map((i) => minuteOfDay(i.start))), 7 * 60);
  useEffect(() => {
    scroller.current?.scrollTo({ top: Math.max(0, (firstMin / 60 - 0.5) * HOUR) });
  }, [days[0].y, days[0].m, days[0].d, days.length]);
  const allDay = days.map((d) => (byDay.get(key(d)) ?? []).filter((i) => i.allDay));
  const cols = `52px repeat(${days.length}, minmax(${days.length > 1 ? 96 : 0}px, 1fr))`;

  return (
    <div className="overflow-x-auto scroll-thin">
      <div style={{ minWidth: days.length > 1 ? 52 + 96 * days.length : undefined }}>
        <div className="grid border-b border-line" style={{ gridTemplateColumns: cols }}>
          <div />
          {days.map((d) => (
            <button key={key(d)} type="button" onClick={() => onDay(d)} className="border-l border-line px-2 py-1.5 text-left hover:bg-surface-2">
              <span className="text-[11px] font-semibold uppercase tracking-wide text-muted">{civilLabel.weekdayShort(d)}</span>{" "}
              <span className={clsx("num inline-grid size-6 place-items-center rounded-full text-[13px] font-semibold", sameDay(d, now) && "bg-[var(--text)] text-[var(--bg)]")}>{d.d}</span>
            </button>
          ))}
        </div>
        {allDay.some((l) => l.length) && (
          <div className="grid border-b border-line" style={{ gridTemplateColumns: cols }}>
            <div className="px-1.5 py-1 text-right text-[10.5px] text-muted">all day</div>
            {allDay.map((l, i) => (
              <div key={i} className="flex min-w-0 flex-col gap-0.5 border-l border-line p-1">
                {l.map((it) => <Chip key={it.id} it={it} onClick={() => onItem(it)} compact />)}
              </div>
            ))}
          </div>
        )}
        <div ref={scroller} className="scroll-thin max-h-[620px] overflow-y-auto">
          <div className="relative grid" style={{ gridTemplateColumns: cols, height: 24 * HOUR }}>
            <div className="relative">
              {Array.from({ length: 24 }, (_, h) => (
                <div key={h} className="num absolute right-1.5 -translate-y-1/2 text-[10.5px] text-muted" style={{ top: h * HOUR }}>{h ? `${String(h).padStart(2, "0")}:00` : ""}</div>
              ))}
            </div>
            {days.map((d) => {
              const blocks = lanes((byDay.get(key(d)) ?? []).filter((i) => !i.allDay).map((it) => {
                const startsToday = sameDay(civilOf(it.start), d);
                const top = startsToday ? minuteOfDay(it.start) : 0;
                const endsToday = sameDay(civilOf(it.end.getTime() - 1), d);
                const bottom = Math.max(top + POINT_MIN, endsToday ? (minuteOfDay(it.end) || 24 * 60) : 24 * 60);
                return { it, top, bottom };
              }));
              return (
                <div key={key(d)} className={clsx("relative border-l border-line", onSlot && "cursor-copy")}
                  onClick={onSlot ? (e) => {
                    const y = e.clientY - e.currentTarget.getBoundingClientRect().top;
                    const min = Math.floor((y / HOUR) * 2) * 30;
                    onSlot(zoned(d, Math.floor(min / 60), min % 60));
                  } : undefined}>
                  {Array.from({ length: 24 }, (_, h) => <div key={h} className="absolute inset-x-0 border-t border-line/70" style={{ top: h * HOUR }} />)}
                  {sameDay(d, now) && <div className="pointer-events-none absolute inset-x-0 z-10 h-0.5 bg-[var(--critical)]" style={{ top: (nowMin / 60) * HOUR }} aria-hidden />}
                  {blocks.map(({ it, top, bottom, lane, of }) => {
                    const p = it.plan;
                    return (
                      <button key={it.id} type="button" onClick={(e) => { e.stopPropagation(); onItem(it); }}
                        title={`${hm(it.start)} ${it.title}\n${it.sub}${p?.overdue ? "\nOverdue" : ""}`}
                        className={clsx("absolute overflow-hidden rounded-md px-1.5 py-0.5 text-left text-[11.5px] leading-tight transition hover:z-20 hover:brightness-95",
                          p ? "border border-dashed" : "border-l-[3px]", p?.overdue && "ring-2 ring-[var(--critical)]", p && p.status !== "planned" && "opacity-55", p?.status === "cancelled" && "line-through")}
                        style={{ top: (top / 60) * HOUR + 1, height: ((bottom - top) / 60) * HOUR - 2, left: `calc(${(lane / of) * 100}% + 2px)`, width: `calc(${100 / of}% - 4px)`,
                          borderColor: it.color, background: `color-mix(in srgb, ${it.color} ${p ? 10 : 18}%, var(--surface))` }}>
                        <div className="flex items-center gap-1 font-medium">
                          {p?.overdue && <CircleAlert size={11} className="shrink-0 text-critical" aria-label="Overdue" />}
                          {p?.status === "done" && <CircleCheck size={11} className="shrink-0" aria-label="Done" />}
                          <span className="truncate">{it.title}</span>
                        </div>
                        <div className="truncate text-[10.5px] text-text-2"><span className="num">{hm(it.start)}</span> · {it.sub}</div>
                      </button>
                    );
                  })}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- plans

function PlanStatusChip({ p }: { p: WorkPlan }) {
  if (p.overdue) return <span className="chip chip-critical"><CircleAlert size={12} aria-hidden /> Overdue</span>;
  if (p.status === "done") return <span className="chip chip-good"><CircleCheck size={12} aria-hidden /> Done</span>;
  if (p.status === "cancelled") return <span className="chip">Cancelled</span>;
  return <span className="chip chip-accent">Planned</span>;
}

function PlanList({ plans, onOpen }: { plans: WorkPlan[]; onOpen: (id: string) => void }) {
  const dir = useDirectory();
  return (
    <ul className="-mx-2 flex flex-col">
      {plans.map((p) => (
        <li key={p.id}>
          <button type="button" onClick={() => onOpen(p.id)} className="flex w-full items-center gap-3 rounded-xl px-2 py-2 text-left hover:bg-surface-2">
            <span className="h-8 w-1 shrink-0 rounded-full" style={{ background: LOT_TYPES.includes(p.recordType) || p.recordType === "delivery" || p.recordType === "road_section" ? TYPE[p.recordType].color : KIND[dir.org(p.orgId)?.kind ?? "operator"].color }} />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[13px] font-medium">{p.title || planKind(p.recordType)}</span>
              <span className="block truncate text-[12px] text-muted">{dateTime(p.startsAt)} · {dir.orgName(p.orgId)}{p.massKg ? ` · ${mass(p.massKg)}` : ""}</span>
            </span>
            <PlanStatusChip p={p} />
          </button>
        </li>
      ))}
    </ul>
  );
}

function PlanDrawer({ plan, me, canPlan, onClose, onEdit }: { plan: WorkPlan; me?: Me; canPlan: RecordType[]; onClose: () => void; onEdit: () => void }) {
  const dir = useDirectory();
  const qc = useQueryClient();
  const mine = me?.org.id === plan.orgId && canPlan.includes(plan.recordType);
  const [linking, setLinking] = useState(false);
  const [pick, setPick] = useState("");
  const cands = useQuery({ queryKey: ["plan-candidates", plan.id], queryFn: () => api.get<RecordT[]>(`/plans/${plan.id}/candidates`), enabled: linking });
  const status = useMutation({
    mutationFn: (b: { status: PlanStatus; recordId?: string }) => api.send<WorkPlan>("POST", `/plans/${plan.id}/status`, b),
    onSuccess: () => { setLinking(false); void qc.invalidateQueries({ queryKey: ["calendar"] }); void qc.invalidateQueries({ queryKey: ["plans"] }); },
  });
  const site = dir.site(plan.siteId);
  const rows: [ReactNode, ReactNode][] = [
    ["What", planKind(plan.recordType)],
    ["When", <>{dateTime(plan.startsAt)}{plan.endsAt ? <> – {dateTime(plan.endsAt)}</> : null}</>],
    ["Planned by", dir.orgName(plan.orgId)],
  ];
  if (site) rows.push(["Site", `${site.name}${site.city ? `, ${site.city}` : ""}`]);
  const toSite = dir.site(plan.toSiteId);
  if (toSite) rows.push(["Receiving site", siteLabel(toSite)]);
  else if (plan.counterpartyOrgId) rows.push([plan.recordType === "shipment" ? "Receiver" : "Counterparty", dir.orgName(plan.counterpartyOrgId)]);
  if (plan.ownerOrgId) rows.push(["Road owner", dir.orgName(plan.ownerOrgId)]);
  rows.push(["Expected mass", plan.massKg ? mass(plan.massKg) : "—"]);
  if (plan.note) rows.push(["Note", <span className="whitespace-pre-wrap">{plan.note}</span>]);
  rows.push(["Status", <PlanStatusChip p={plan} />]);
  if (plan.recordId) rows.push(["Real record", <RecordLink id={plan.recordId} type={plan.recordType} />]);
  if (plan.doneAt) rows.push(["Done", dateTime(plan.doneAt)]);

  return (
    <Drawer open onOpenChange={(o) => !o && onClose()} title={plan.title || planKind(plan.recordType)} sub={<>Plan <span className="mono">{plan.id}</span> · not a signed record</>}>
      <KV items={rows} />
      <p className="mt-4 text-[12px] text-muted">Plans are shared with the companies involved and are never signed or sealed. The record that does the work is the evidence.</p>
      {mine && (
        <div className="mt-5 flex flex-col gap-3 border-t border-line pt-4">
          {plan.status === "planned" && !linking && (
            <div className="flex flex-wrap gap-2">
              <button className="btn btn-primary" onClick={() => setLinking(true)}><CircleCheck size={15} aria-hidden /> Mark done</button>
              <button className="btn" onClick={onEdit}>Edit</button>
              <button className="btn btn-ghost" disabled={status.isPending} onClick={() => status.mutate({ status: "cancelled" })}>Cancel plan</button>
            </div>
          )}
          {plan.status !== "planned" && <div><button className="btn" disabled={status.isPending} onClick={() => status.mutate({ status: "planned" })}>Reopen</button></div>}
          {linking && (
            <div className="flex flex-col gap-2">
              <div className="label mb-0">Link the record that did this work</div>
              {cands.error ? <ErrorBox error={cands.error} /> : cands.isLoading ? <Skel h={60} /> : cands.data?.length ? (
                <div className="flex max-h-72 flex-col gap-1 overflow-y-auto scroll-thin">
                  {cands.data.map((r) => (
                    <label key={r.id} className={clsx("flex cursor-pointer items-center gap-2 rounded-xl px-2.5 py-2 text-[13px] hover:bg-surface-2", pick === r.id && "bg-surface-2")}>
                      <input type="radio" name="rec" checked={pick === r.id} onChange={() => setPick(r.id)} />
                      <span className="mono font-medium">{r.id}</span>
                      <span className="truncate text-muted">{dateTime(r.occurredAt)} · {mass(r.massKg)}{r.ref ? ` · ${r.ref}` : ""}</span>
                    </label>
                  ))}
                </div>
              ) : <p className="text-[12.5px] text-muted">No {TYPE[plan.recordType].plural.toLowerCase()} of your company within two weeks of this plan yet.</p>}
              <div className="flex flex-wrap gap-2">
                <button className="btn btn-primary" disabled={!pick || status.isPending} onClick={() => status.mutate({ status: "done", recordId: pick })}><Link2 size={14} aria-hidden /> Link and mark done</button>
                <button className="btn" disabled={status.isPending} onClick={() => status.mutate({ status: "done" })}>Done without a record</button>
                <button className="btn btn-ghost" onClick={() => setLinking(false)}>Back</button>
              </div>
            </div>
          )}
          {status.error ? <ErrorBox error={status.error} /> : null}
        </div>
      )}
      {!mine && me && me.org.id !== plan.orgId && <p className="mt-4 text-[12.5px] text-text-2">{dir.orgName(plan.orgId)} made this plan; only they can change it.</p>}
    </Drawer>
  );
}

function F({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return <label className="block"><span className="label">{label}{hint && <span className="font-normal text-muted"> · {hint}</span>}</span>{children}</label>;
}

function nextHour(): Date {
  const t = Date.now() + 3600e3;
  return zoned(civilOf(t), Math.floor(minuteOfDay(t) / 60), 0);
}

function PlanForm({ me, types, plan, start, onClose, onSaved }: {
  me: Me; types: RecordType[]; plan?: WorkPlan; start?: Date; onClose: () => void; onSaved: (p: WorkPlan) => void;
}) {
  const dir = useDirectory();
  const qc = useQueryClient();
  const s0 = plan ? new Date(plan.startsAt) : start ?? nextHour();
  const [v, setV] = useState({
    recordType: plan?.recordType ?? types[0],
    title: plan?.title ?? "",
    startsAt: toInput(s0),
    endsAt: plan?.endsAt ? toInput(new Date(plan.endsAt)) : toInput(new Date(s0.getTime() + 2 * 3600e3)),
    siteId: plan?.siteId ?? "",
    counterpartyOrgId: plan?.counterpartyOrgId ?? "",
    toSiteId: plan?.toSiteId ?? "",
    ownerOrgId: plan?.ownerOrgId ?? "",
    tonnes: plan?.massKg ? String(plan.massKg / 1000) : "",
    note: plan?.note ?? "",
  });
  const set = (p: Partial<typeof v>) => setV((x) => ({ ...x, ...p }));
  const book = dir.sites.filter((s) => s.orgId === me.org.id && !s.archivedAt);
  const sites = book.filter((s) => !s.partner);
  const chain = dir.orgs.filter((o) => o.id !== me.org.id && !["operator", "auditor", "reporter"].includes(o.kind)).sort((a, b) => a.name.localeCompare(b.name));
  const owners = dir.orgs.filter((o) => (o.kinds ?? [o.kind]).includes("road_owner") || o.kind === "road_owner").sort((a, b) => a.name.localeCompare(b.name));
  const orgLabel = (o: { name: string; city: string; country: string }) => `${o.name}${o.city ? ` · ${o.city}` : ""}${o.country ? ` (${o.country})` : ""}`;
  const t = v.recordType;
  const needsCp = t === "shipment" || t === "delivery";

  const save = useMutation({
    mutationFn: () => {
      const s = fromInput(v.startsAt);
      const e = v.endsAt ? fromInput(v.endsAt) : null;
      const body = {
        recordType: t, title: v.title, siteId: v.siteId, toSiteId: t === "shipment" ? v.toSiteId : "",
        counterpartyOrgId: t !== "shipment" && (needsCp || LOT_TYPES.includes(t)) ? v.counterpartyOrgId : "",
        ownerOrgId: t === "road_section" ? v.ownerOrgId : "", startsAt: s?.toISOString(), endsAt: e ? e.toISOString() : null,
        massKg: v.tonnes ? Math.round(Number(v.tonnes) * 1000) : 0, note: v.note,
      };
      return plan ? api.send<WorkPlan>("PUT", `/plans/${plan.id}`, body) : api.send<WorkPlan>("POST", "/plans", body);
    },
    onSuccess: (p) => { void qc.invalidateQueries({ queryKey: ["calendar"] }); void qc.invalidateQueries({ queryKey: ["plans"] }); onSaved(p); },
  });

  return (
    <Modal open onOpenChange={(o) => !o && onClose()} title={plan ? "Edit plan" : "Plan work"}
      sub={`Shared with the companies involved; not signed or sealed. Times in ${timeZone().replace(/_/g, " ")}.`} width={560}>
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
        <F label="What">
          <select className="input" value={t} onChange={(e) => set({ recordType: e.target.value as RecordType })}>
            {types.map((x) => <option key={x} value={x}>{planKind(x)}</option>)}
          </select>
          <span className="mt-1 block text-[12px] text-muted">Needs the {DUTY[dutyFor(t)].name.toLowerCase()} duty.</span>
        </F>
        <F label="Title" hint="optional"><input className="input" maxLength={160} value={v.title} onChange={(e) => set({ title: e.target.value })} placeholder={planKind(t)} /></F>
        <div className="grid gap-3 sm:grid-cols-2">
          <F label="Starts"><input className="input" type="datetime-local" required value={v.startsAt} onChange={(e) => set({ startsAt: e.target.value })} /></F>
          <F label="Ends" hint="optional"><input className="input" type="datetime-local" min={v.startsAt} value={v.endsAt} onChange={(e) => set({ endsAt: e.target.value })} /></F>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <F label="Site" hint={sites.length ? "optional" : "none registered"}>
            <select className="input" value={v.siteId} onChange={(e) => set({ siteId: e.target.value })} disabled={!sites.length}>
              <option value="">—</option>
              {sites.map((s) => <option key={s.id} value={s.id}>{s.name}{s.city ? ` · ${s.city}` : ""}</option>)}
            </select>
          </F>
          <F label="Expected mass" hint="tonnes"><input className="input num" type="number" min={0} step="any" value={v.tonnes} onChange={(e) => set({ tonnes: e.target.value })} placeholder="0" /></F>
        </div>
        {t === "shipment" && (
          <F label="Receiving site">
            <select className="input" required value={v.toSiteId} onChange={(e) => set({ toSiteId: e.target.value })}>
              <option value="">—</option>
              <optgroup label="Our sites">{sites.filter((s) => s.id !== v.siteId).map((s) => <option key={s.id} value={s.id}>{siteLabel(s)} · {siteKindName(s)}</option>)}</optgroup>
              <optgroup label="Partner sites">{book.filter((s) => !!s.partner).map((s) => <option key={s.id} value={s.id}>{siteLabel(s)} · {siteKindName(s)}</option>)}</optgroup>
            </select>
            <span className="mt-1 block text-[12px] text-muted">From your own address book (Settings → Organisation). Partner sites do not see your plans.</span>
          </F>
        )}
        {t !== "shipment" && (needsCp || LOT_TYPES.includes(t)) && (
          <F label={t === "delivery" ? "Delivered to" : "For customer"} hint="optional">
            <select className="input" value={v.counterpartyOrgId} onChange={(e) => set({ counterpartyOrgId: e.target.value })}>
              <option value="">—</option>
              {chain.map((o) => <option key={o.id} value={o.id}>{orgLabel(o)} — {KIND[o.kind].name}</option>)}
            </select>
            <span className="mt-1 block text-[12px] text-muted">The company named here sees this plan in its calendar.</span>
          </F>
        )}
        {t === "road_section" && (
          <F label="Road owner">
            <select className="input" required value={v.ownerOrgId} onChange={(e) => set({ ownerOrgId: e.target.value })}>
              <option value="">—</option>
              {owners.map((o) => <option key={o.id} value={o.id}>{orgLabel(o)}</option>)}
            </select>
          </F>
        )}
        <F label="Note" hint="optional"><textarea className="input min-h-[72px] py-2" maxLength={1000} value={v.note} onChange={(e) => set({ note: e.target.value })} /></F>
        {save.error ? <ErrorBox error={save.error} /> : null}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn btn-ghost" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn btn-primary" disabled={save.isPending}>{plan ? "Save changes" : "Add to calendar"}</button>
        </div>
      </form>
    </Modal>
  );
}
