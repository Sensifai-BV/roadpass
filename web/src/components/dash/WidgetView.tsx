import { useMemo } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import { api } from "../../lib/api";
import { useDirectory, useMeta, useRange } from "../../lib/hooks";
import { KIND, TYPE } from "../../lib/vocab";
import { delta, titleCase } from "../../lib/format";
import { fmtUnit, resolveRange, SERIES, splitUnit, type Query, type Result, type Widget } from "../../lib/dashboards";
import type { OrgKind, RecordType } from "../../lib/types";
import { Delta, Skel } from "../ui";
import { TimeChart, type Row } from "../charts/TimeChart";
import { BarList } from "../charts/BarList";
import { Gauge } from "../charts/Gauge";
import { RegionMap } from "../charts/RegionMap";
import { ActivityWidget, DataHealthWidget, JourneyWidget, SitesMapWidget } from "./Builtins";

const SEQ = ["var(--seq-1)", "var(--seq-2)", "var(--seq-3)", "var(--seq-4)", "var(--seq-5)", "var(--seq-6)"];

/** Runs a widget's query over its own window, or the page's. */
export function useWidgetData(q: Query | undefined, range: Widget["range"]) {
  const page = useRange();
  const r = resolveRange(range, page);
  return useQuery({
    queryKey: ["explore", JSON.stringify(q), r.from, r.to],
    queryFn: ({ signal }) => api.send<Result>("POST", "/explore/query", { query: q, from: `${r.from}T00:00:00Z`, to: `${r.to}T00:00:00Z` }, signal),
    enabled: !!q,
    staleTime: 30_000,
    placeholderData: keepPreviousData,
    retry: false,
  });
}

/** Turns grouped keys (organisation ids, record types…) into names. */
export function useGroupLabel(field?: string) {
  const dir = useDirectory();
  const meta = useMeta().data;
  return (k: string) => {
    if (!k) return "—";
    switch (field) {
      case "org_id": case "counterparty": return dir.orgName(k);
      case "site_id": return dir.site(k)?.name ?? k;
      case "type": return TYPE[k as RecordType]?.name ?? k;
      case "org_kind": return KIND[k as OrgKind]?.name ?? k;
      case "test_pass": return k === "1" ? "Passed" : "Failed";
      case "route": return k.charAt(0).toUpperCase() + k.slice(1);
      case "test": return meta?.labTests[k]?.name ?? titleCase(k);
    }
    // Other dimensions hold stored codes ("surface", "agricultural_film"): show them as words.
    return /^[a-z0-9_]+$/.test(k) ? titleCase(k) : k;
  };
}

/** The body of one widget: data fetched and drawn by type. */
export function WidgetBody({ w, height, width = 600 }: { w: Widget; height: number; width?: number }) {
  switch (w.type) {
    case "text": return <TextBody text={w.options?.text ?? ""} />;
    case "journey": return <JourneyWidget options={w.options} width={width} />;
    case "sites_map": return <SitesMapWidget height={height} />;
    case "data_health": return <DataHealthWidget />;
    case "activity": return <ActivityWidget height={height} />;
  }
  return <DataBody w={w} height={height} />;
}

function TextBody({ text }: { text: string }) {
  // Plain text with "# " headings and "- " bullets; never HTML.
  const lines = text.split("\n");
  return (
    <div className="flex flex-col gap-1 text-[13px] leading-relaxed text-text-2">
      {lines.map((l, i) => l.startsWith("# ") ? <div key={i} className="text-[15px] font-semibold text-text">{l.slice(2)}</div>
        : l.startsWith("- ") ? <div key={i} className="flex gap-2"><span className="text-muted">•</span>{l.slice(2)}</div>
        : l.trim() === "" ? <div key={i} className="h-1" /> : <div key={i}>{l}</div>)}
      {!text && <span className="text-muted">Empty note. Edit the widget to write something.</span>}
    </div>
  );
}

function DataBody({ w, height }: { w: Widget; height: number }) {
  const q = useWidgetData(w.query, w.range);
  const label = useGroupLabel(w.query?.groupBy);
  const res = q.data;
  const fmt = (v: number) => fmtUnit(v, res?.unit ?? "", w.options?.decimals);
  const sorted = useMemo(() => [...(res?.rows ?? [])], [res]);

  if (q.isLoading) return <Skel h={Math.max(40, height - 8)} />;
  if (q.error) return <div className="flex items-start gap-2 text-[12.5px] text-critical"><AlertTriangle size={14} className="mt-0.5 shrink-0" />{(q.error as Error).message}</div>;
  if (!res || !res.rows.length) return <div className="grid h-full place-items-center text-[12.5px] text-muted">No data in this period.</div>;

  switch (w.type) {
    case "kpi": {
      const v = res.rows.reduce((s, r) => s + r.v, 0);
      // An average (a pass rate, a mean temperature) over no records is unknown, not 0.
      if (w.query?.agg === "avg" && res.rows.reduce((s, r) => s + (r.n ?? 0), 0) === 0) {
        return <div className="flex h-full flex-col justify-end"><span className="hero-num num text-[30px] font-semibold tracking-tight text-muted">—</span>
          <span className="mt-1 text-[12px] text-muted">No records in this period</span></div>;
      }
      const [n, u] = splitUnit(fmt(v));
      const d = res.previous !== undefined ? delta(v, res.previous) : null;
      return (
        <div className="flex h-full flex-col justify-end">
          <div className="flex items-baseline gap-1.5"><span className="hero-num num text-[30px] font-semibold tracking-tight">{n}</span><span className="text-[14px] text-text-2">{u}</span></div>
          <div className="mt-1 flex items-center gap-2 text-[12px] text-muted">
            {w.query?.compare && <Delta value={d} invert={w.options?.invert} />}
            {/* No delta (nothing in the previous period): say so rather than leave "vs" dangling. */}
            <span>{!w.query?.compare ? `${res.rows[0]?.n ?? 0} records` : d === null || !Number.isFinite(d) ? (res.previous === undefined ? "no comparison available" : "no data in the previous period") : "vs previous period"}</span>
          </div>
        </div>
      );
    }
    case "timeseries": {
      const groups = res.groups?.length ? res.groups : ["value"];
      const by = new Map<string, Row>();
      for (const r of res.rows) {
        const key = r.t!;
        const row = by.get(key) ?? ({ t: new Date(key) } as Row);
        for (const g of groups) if (row[g] === undefined) row[g] = 0;
        row[r.group || "value"] = r.v;
        by.set(key, row);
      }
      const rows = [...by.values()].sort((a, b) => +a.t - +b.t);
      const series = groups.map((g, i) => ({ key: g, label: g === "value" ? res.field.label : label(g), color: groups.length === 1 && w.options?.color ? w.options.color : SERIES[i % SERIES.length] }));
      return <TimeChart rows={rows} series={series} kind={w.options?.style ?? "line"} height={height} legend={groups.length > 1} yFormat={(v) => (v === 0 ? "0" : fmtUnit(v, res.unit, 0))} tipFormat={fmt} />;
    }
    case "bars":
      if (w.options?.orient === "vertical") return <Columns rows={sorted.map((r) => ({ label: label(r.group ?? ""), v: r.v }))} fmt={fmt} color={w.options?.color ?? SERIES[0]} height={height} />;
      return (
        <div className="h-full overflow-y-auto pr-1 scroll-thin">
          <BarList items={sorted.map((r) => ({ key: r.group ?? "", label: label(r.group ?? ""), value: r.v, sub: `${r.n} records` }))} format={fmt} color={w.options?.color ?? SERIES[0]} />
        </div>
      );
    case "donut":
      return <Donut rows={sorted.map((r) => ({ label: label(r.group ?? ""), v: r.v }))} fmt={fmt} size={Math.min(height, 220)} pie={w.options?.donutStyle === "pie"} />;
    case "gauge": {
      const v = res.rows.reduce((s, r) => s + r.v, 0);
      const min = w.options?.min ?? 0, max = w.options?.max ?? 100;
      const pct = ((v - min) / Math.max(1e-9, max - min)) * 100;
      const { warn, critical, invert } = w.options ?? {};
      const bad = (t?: number) => t !== undefined && (invert ? v >= t : v <= t) ;
      const color = bad(critical) ? "var(--critical)" : bad(warn) ? "var(--warning)" : "var(--good)";
      return <div className="grid h-full place-items-center"><Gauge value={pct} display={fmt(v)} label={res.field.label} color={color} size={Math.min(220, height * 1.6)}
        sub={warn !== undefined || critical !== undefined ? `warning ${warn !== undefined ? fmt(warn) : "—"} · critical ${critical !== undefined ? fmt(critical) : "—"}` : undefined} /></div>;
    }
    case "table":
      return (
        <div className="h-full overflow-auto scroll-thin">
          <table className="table text-[12.5px]">
            <thead><tr><th>{res.groupBy?.label ?? "Group"}</th><th className="text-right">{res.field.label}</th><th className="text-right">Records</th></tr></thead>
            <tbody>{sorted.map((r) => <tr key={r.group}><td>{label(r.group ?? "")}</td><td className="num text-right">{fmt(r.v)}</td><td className="num text-right text-text-2">{r.n}</td></tr>)}</tbody>
          </table>
        </div>
      );
    case "map": {
      const by = new Map(sorted.map((r) => [r.group ?? "", r.v]));
      const maxV = Math.max(1e-9, ...by.values());
      return <RegionMap points={[]} height={height} fill={(name) => { const v = by.get(name); return v === undefined ? undefined : SEQ[Math.min(SEQ.length - 1, Math.floor((v / maxV) * SEQ.length))]; }} />;
    }
  }
  return null;
}

/** A donut for shares: at most six slices, the rest folded into "Other". */
function Donut({ rows, fmt, size, pie }: { rows: { label: string; v: number }[]; fmt: (v: number) => string; size: number; pie?: boolean }) {
  const top = rows.slice(0, 6);
  const rest = rows.slice(6).reduce((s, r) => s + r.v, 0);
  const parts = rest > 0 ? [...top, { label: "Other", v: rest }] : top;
  const total = parts.reduce((s, p) => s + p.v, 0) || 1;
  const sw0 = Math.max(14, size * 0.13);
  // A pie is a donut whose ring reaches the centre.
  const r = pie ? (size / 2 - 8) / 2 : size / 2 - 8, cx = size / 2, cy = size / 2, sw = pie ? size / 2 - 8 : sw0;
  let a0 = -Math.PI / 2;
  const arcs = parts.map((p, i) => {
    const a1 = a0 + (p.v / total) * Math.PI * 2;
    const large = a1 - a0 > Math.PI ? 1 : 0;
    const gap = parts.length > 1 ? 0.012 : 0;
    const s = [cx + r * Math.cos(a0 + gap), cy + r * Math.sin(a0 + gap)], e = [cx + r * Math.cos(a1 - gap), cy + r * Math.sin(a1 - gap)];
    const d = parts.length === 1 ? `M ${cx} ${cy - r} A ${r} ${r} 0 1 1 ${cx - 0.01} ${cy - r}` : `M ${s[0]} ${s[1]} A ${r} ${r} 0 ${large} 1 ${e[0]} ${e[1]}`;
    a0 = a1;
    return { d, color: p.label === "Other" ? "var(--muted)" : SERIES[i % SERIES.length], p };
  });
  return (
    <div className="flex h-full items-center gap-4">
      <svg width={size} height={size} role="img" aria-label="Share by group" className="shrink-0">
        {arcs.map((a, i) => <path key={i} d={a.d} fill="none" stroke={a.color} strokeWidth={sw}><title>{`${a.p.label}: ${fmt(a.p.v)}`}</title></path>)}
        {!pie && <text x={cx} y={cy + 5} textAnchor="middle" style={{ fill: "var(--text)", fontSize: 15, fontWeight: 650 }}>{fmt(total)}</text>}
      </svg>
      <ul className="min-w-0 flex-1 text-[12.5px]">
        {arcs.map((a, i) => (
          <li key={i} className="flex items-center gap-2 py-0.5">
            <span className="size-2.5 shrink-0 rounded-[3px]" style={{ background: a.color }} />
            <span className="min-w-0 flex-1 truncate text-text-2">{a.p.label}</span>
            <span className="num font-medium">{Math.round((a.p.v / total) * 100)}%</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Vertical columns for a ranking, labels under each column. */
function Columns({ rows, fmt, color, height }: { rows: { label: string; v: number }[]; fmt: (v: number) => string; color: string; height: number }) {
  const max = Math.max(1e-9, ...rows.map((r) => r.v));
  const plot = Math.max(60, height - 44);
  return (
    <div className="flex h-full items-end gap-2 overflow-x-auto pb-1 scroll-thin" role="img" aria-label="Ranking as columns">
      {rows.map((r, i) => (
        <div key={i} className="group flex min-w-[34px] flex-1 flex-col items-center" title={`${r.label}: ${fmt(r.v)}`}>
          <span className="num mb-1 text-[11px] text-text-2 opacity-0 transition group-hover:opacity-100">{fmt(r.v)}</span>
          <div className="w-full max-w-[44px] rounded-t-[4px]" style={{ height: Math.max(2, (r.v / max) * plot), background: color }} />
          <span className="mt-1.5 w-full truncate text-center text-[11px] text-muted">{r.label}</span>
        </div>
      ))}
    </div>
  );
}
