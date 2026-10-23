import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, ChevronRight, CircleAlert, OctagonX, ShieldCheck } from "lucide-react";
import clsx from "clsx";
import { api } from "../../lib/api";
import { useAnalytics, useDirectory, useMeta } from "../../lib/hooks";
import type { FlowLink, LedgerSummary, Overview as OverviewT, RecordT, SectionPoint, SiteVolume } from "../../lib/types";
import { KIND, STAGES, TYPE } from "../../lib/vocab";
import { ago, mass, num, pct } from "../../lib/format";
import { useCountUp } from "../../lib/countUp";
import type { WidgetOptions } from "../../lib/dashboards";
import { Legend, Skel, StatusChip, TypeBadge } from "../ui";
import { Gauge } from "../charts/Gauge";
import { RegionMap, type MapPoint } from "../charts/RegionMap";
import type { MapLine } from "../charts/PointMap";

// Built-in widgets: views with their own data, placed and sized like any other.

type Stage = (typeof STAGES)[number];

/** "1 section", "2 sections": the count with its unit in the right number. */
function count(n: number, one: string, many: string) {
  const r = Math.round(n);
  return `${num(r)} ${r === 1 ? one : many}`;
}

function stageValue(s: Stage, o: OverviewT | undefined, measure: "mass" | "records") {
  const t = o?.types?.find((x) => x.type === s.type);
  if (s.type === "road_section") return { v: o?.roads.sections ?? 0, label: (x: number) => count(x, "section", "sections"), sub: `${num(o?.roads.lengthKm ?? 0, 1)} km` };
  if (measure === "records" || s.type === "delivery") return { v: t?.count ?? 0, label: (x: number) => (s.type === "delivery" ? count(x, "load", "loads") : count(x, "record", "records")), sub: mass(t?.massKg ?? 0) };
  return { v: t?.massKg ?? 0, label: (x: number) => mass(x), sub: count(t?.count ?? 0, TYPE[s.type].name.toLowerCase(), TYPE[s.type].plural.toLowerCase()) };
}

/** The chain's six steps for the period, in one of three styles. */
export function JourneyWidget({ options, width }: { options?: WidgetOptions; width: number }) {
  const ov = useAnalytics<OverviewT>("overview");
  const o = ov.data;
  const style = options?.journeyStyle ?? "pipeline";
  const measure = options?.journeyMeasure ?? "mass";
  const animate = options?.animate ?? true;
  if (ov.isLoading) return <Skel h={110} />;
  if (style === "list") {
    const vals = STAGES.map((s) => stageValue(s, o, measure).v);
    const maxV = Math.max(1, ...vals.filter((_, i) => STAGES[i].type !== "road_section" && STAGES[i].type !== "delivery"));
    return (
      <ul className="flex flex-col gap-0.5">
        {STAGES.map((s, i) => {
          const sv = stageValue(s, o, measure);
          const share = s.type === "road_section" || s.type === "delivery" ? 1 : sv.v / maxV;
          return (
            <li key={s.n}>
              <Link to={`/passports?type=${s.type === "delivery" ? "road_section" : s.type}`} className="group grid grid-cols-[22px_1fr_auto] items-center gap-x-3 gap-y-0.5 rounded-lg px-1 py-0.5 hover:bg-surface-2">
                <span className="grid size-[22px] place-items-center rounded-md" style={{ background: `color-mix(in srgb, ${s.color} 16%, transparent)`, color: s.color }}><s.icon size={12} aria-hidden /></span>
                <span className="text-[13px] font-medium">{s.title}</span>
                <span className="num text-[13px] font-semibold">{sv.label(sv.v)}</span>
                <span />
                <span className="col-span-2 h-[5px] overflow-hidden rounded-full bg-surface-3">
                  <span className={clsx("block h-full rounded-full", animate && "jt-bar")} style={{ width: `${Math.max(2, share * 100)}%`, background: s.color, animationDelay: `${i * 0.12}s` }} />
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
    );
  }
  if (style === "figures") {
    return (
      <div className="grid h-full grid-cols-3 gap-x-4 gap-y-3 xl:grid-cols-6">
        {STAGES.map((s) => { const sv = stageValue(s, o, measure); return (
          <div key={s.n} className="min-w-0">
            <div className="flex items-center gap-1.5 text-[12px] font-medium text-text-2"><span className="size-2 rounded-full" style={{ background: s.color }} />{s.title}</div>
            <div className="num mt-1 truncate text-[20px] font-semibold tracking-tight">{sv.label(sv.v)}</div>
          </div>); })}
      </div>
    );
  }
  // One row, like the chain itself; narrow widgets scroll sideways.
  return (
    <div className="-mx-1 overflow-x-auto px-1 pb-1 scroll-thin">
      <ol key={`${o?.range.From}${o?.range.To}`} className="grid gap-3" style={{ gridTemplateColumns: "repeat(6, minmax(132px, 1fr))", minWidth: width < 6 * 132 ? 6 * 144 : undefined }}>
        {STAGES.map((s, i) => <PipelineStep key={s.n} s={s} o={o} measure={measure} animate={animate} last={i === STAGES.length - 1} />)}
      </ol>
    </div>
  );
}

function PipelineStep({ s, o, measure, animate, last }: { s: Stage; o?: OverviewT; measure: "mass" | "records"; animate: boolean; last: boolean }) {
  const sv = stageValue(s, o, measure);
  const shown = useCountUp(sv.v);
  const v = animate ? shown : sv.v;
  return (
    <li className="relative">
      <Link to={`/passports?type=${s.type === "delivery" ? "road_section" : s.type}`} className="group relative flex h-full flex-col rounded-2xl bg-surface-2 p-3.5 transition hover:bg-surface-3">
        <div className="flex items-center justify-between">
          <span className="grid size-8 place-items-center rounded-[10px]" style={{ background: `color-mix(in srgb, ${s.color} 16%, transparent)`, color: s.color }}><s.icon size={16} strokeWidth={2.2} aria-hidden /></span>
          <span className="text-[11px] font-semibold text-muted">{s.n}</span>
        </div>
        <div className="mt-2.5 truncate text-[13px] font-semibold">{s.title}</div>
        <div className="truncate text-[11.5px] text-muted">{s.who}</div>
        <div className="num mt-2 text-[20px] font-semibold tracking-tight" aria-label={sv.label(sv.v)}>{sv.label(v)}</div>
        <div className="mt-0.5 truncate text-[11.5px] text-muted">{sv.sub}</div>
      </Link>
      {!last && <ChevronRight size={14} className="absolute -right-[13px] top-1/2 z-10 -translate-y-1/2 text-muted" aria-hidden />}
    </li>
  );
}

/**
 * The chain on a real map: every company's sites at their own coordinates (any
 * country), sized by tonnes handled, the material flows between companies, and
 * finished road sections.
 */
export function SitesMapWidget({ height }: { height: number }) {
  const map = useAnalytics<{ sites: SiteVolume[]; sections: SectionPoint[] }>("map");
  const flows = useAnalytics<FlowLink[]>("flows");
  const dir = useDirectory();
  const nav = useNavigate();
  const { points, lines } = useMemo(() => {
    if (!map.data || !dir.ready) return { points: [] as MapPoint[], lines: [] as MapLine[] };
    const siteMass = new Map<string, number>();
    for (const v of map.data.sites) siteMass.set(v.siteId, (siteMass.get(v.siteId) ?? 0) + v.massKg);
    const maxM = Math.max(1, ...siteMass.values());
    const located = dir.sites.filter((s) => Number.isFinite(s.lat) && Number.isFinite(s.lon) && !(s.lat === 0 && s.lon === 0));
    const sites: MapPoint[] = located.flatMap((s) => {
      const org = dir.org(s.orgId);
      if (!org || !KIND[org.kind]) return [];
      const m = siteMass.get(s.id) ?? 0;
      return [{ id: s.id, lat: s.lat, lon: s.lon, r: 4 + 10 * Math.sqrt(m / maxM), color: KIND[org.kind].color, label: org.name,
        sub: `${s.name} · ${s.city}${m ? ` · ${mass(m)} handled` : ""}` }];
    });
    // A flow is drawn from the sending company's busiest site to the receiver's.
    const anchor = new Map<string, [number, number]>();
    for (const s of [...located].sort((a, b) => (siteMass.get(a.id) ?? 0) - (siteMass.get(b.id) ?? 0))) anchor.set(s.orgId, [s.lat, s.lon]);
    const maxT = Math.max(1e-9, ...(flows.data ?? []).map((f) => f.tonnes));
    const lines: MapLine[] = (flows.data ?? []).flatMap((f) => {
      const a = anchor.get(f.from), b = anchor.get(f.to);
      if (!a || !b) return [];
      return [{ id: `${f.from}>${f.to}`, from: a, to: b, weight: 1.5 + 4 * (f.tonnes / maxT), color: STAGES[f.stage - 1]?.color ?? "var(--muted)",
        label: `${dir.orgName(f.from)} → ${dir.orgName(f.to)} · ${num(f.tonnes, 1)} t` }];
    });
    const secs: MapPoint[] = map.data.sections.slice(0, 1500).map((s) => ({
      id: s.id, lat: s.lat, lon: s.lon, r: 6, color: "var(--s6)", label: `${s.road} · ${s.id}`,
      sub: `${num(s.lengthKm, 1)} km · ${mass(s.recycledKg)} recycled plastic · click to open the passport`, onClick: () => nav(`/passports/${s.id}`),
    }));
    // Sections last so they sit on top of the plant circles and can be clicked.
    return { points: [...sites.sort((a, b) => b.r - a.r), ...secs], lines };
  }, [map.data, flows.data, dir, nav]);
  if (map.isLoading || !dir.ready) return <Skel h={Math.max(80, height - 40)} />;
  return (
    <div className="flex h-full flex-col">
      <div className="min-h-0 flex-1"><RegionMap points={points} lines={lines} height={Math.max(120, height - 44)} /></div>
      <div className="mt-2"><Legend items={[...STAGES.slice(0, 4).map((s) => ({ label: s.who, color: s.color })), { label: "Road sections", color: "var(--s6)" }]} /></div>
    </div>
  );
}

/** Open findings, lab pass rate and how much is sealed. */
export function DataHealthWidget() {
  const ov = useAnalytics<OverviewT>("overview");
  const counts = useQuery({ queryKey: ["findings-summary"], queryFn: () => api.get<Record<string, Record<string, number>>>("/findings/summary") });
  const ledger = useQuery({ queryKey: ["ledger-summary"], queryFn: () => api.get<LedgerSummary>("/ledger/summary") });
  const rules = useMeta().data?.rules ?? [];
  const open = { blocker: 0, warning: 0 };
  for (const [code, st] of Object.entries(counts.data ?? {})) {
    const sev = rules.find((r) => r.code === code)?.severity ?? "warning";
    open[sev] += st.open ?? 0;
  }
  const s = ledger.data?.mine ?? ledger.data?.platform;
  const sealedShare = s && s.records ? (s.sealed / s.records) * 100 : 0;
  const o = ov.data;
  return (
    <div className="flex h-full flex-col">
      <ul className="flex flex-col gap-3 text-[13px]">
        <li className="flex items-center justify-between"><span className="inline-flex items-center gap-2"><OctagonX size={15} className="text-critical" aria-hidden />Open blockers</span><span className="num font-semibold">{num(open.blocker)}</span></li>
        <li className="flex items-center justify-between"><span className="inline-flex items-center gap-2"><CircleAlert size={15} className="text-warning" aria-hidden />Open warnings</span><span className="num font-semibold">{num(open.warning)}</span></li>
        <li className="flex items-center justify-between"><span className="inline-flex items-center gap-2"><ShieldCheck size={15} className="text-good" aria-hidden />Lab tests passed</span><span className="num font-semibold">{o?.labTests ? pct((o.labPassRate ?? 0) * 100) : "—"}</span></li>
      </ul>
      <div className="mt-auto border-t border-line pt-3">
        <Gauge value={sealedShare} label="sealed on the ledger" color="var(--accent)" size={160}
          sub={ledger.data?.online ? `Channel ${ledger.data.channel} · block ${num(ledger.data.chain?.height ?? 0)}` : "Ledger offline: records are queued for sealing."} />
      </div>
    </div>
  );
}

/** The newest records the viewer can see. */
export function ActivityWidget({ height }: { height: number }) {
  const q = useQuery({ queryKey: ["activity"], queryFn: () => api.get<RecordT[]>("/activity"), refetchInterval: 60_000 });
  const dir = useDirectory();
  const n = Math.max(3, Math.floor((height - 10) / 44));
  return (
    <ul className="-mx-2 flex flex-col">
      {(q.data ?? []).slice(0, n).map((r) => (
        <li key={r.id}>
          <Link to={TYPE[r.type].passport ? `/passports/${r.id}` : `/passports/${r.inputs?.[0]?.id ?? r.id}`} className="flex items-center gap-3 rounded-xl px-2 py-1.5 hover:bg-surface-2">
            <TypeBadge type={r.type} />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[13px] font-medium">{TYPE[r.type].name} <span className="mono text-muted">{r.id}</span></span>
              <span className="block truncate text-[11.5px] text-muted">{dir.orgName(r.orgId)} · {ago(r.occurredAt)}</span>
            </span>
            {r.status !== "ok" ? <StatusChip status={r.status} withIcon={false} /> : <ArrowRight size={13} className="text-muted" />}
          </Link>
        </li>
      ))}
      {q.isLoading && [0, 1, 2, 3].map((i) => <Skel key={i} h={40} className="my-1" />)}
    </ul>
  );
}
