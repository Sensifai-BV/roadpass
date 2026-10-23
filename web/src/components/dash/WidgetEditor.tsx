import { cloneElement, useEffect, useId, useMemo, useState, type ReactElement } from "react";
import { Activity, BarChart3, Gauge as GaugeIcon, Hash, HeartPulse, ListOrdered, Map as MapIcon, MapPinned, PieChart, Plus, Route, Table2, Trash2, Type } from "lucide-react";
import clsx from "clsx";
import { TYPE } from "../../lib/vocab";
import type { RecordType } from "../../lib/types";
import { AGGS, BUILTIN, RANGES, SERIES, WIDGETS, useCatalog, type Filter, type Query, type Widget, type WidgetType } from "../../lib/dashboards";
import { Drawer } from "../ui";
import { WidgetBody } from "./WidgetView";

const ICON: Record<WidgetType, typeof Hash> = { kpi: Hash, timeseries: Activity, bars: BarChart3, donut: PieChart, gauge: GaugeIcon, table: Table2, map: MapIcon, text: Type, journey: Route, sites_map: MapPinned, data_health: HeartPulse, activity: ListOrdered };
const OPS: { value: Filter["op"]; label: string; types: string[] }[] = [
  { value: "eq", label: "is", types: ["string", "bool", "number"] }, { value: "neq", label: "is not", types: ["string", "bool", "number"] },
  { value: "in", label: "is one of", types: ["string", "number"] }, { value: "gt", label: ">", types: ["number"] }, { value: "gte", label: "≥", types: ["number"] },
  { value: "lt", label: "<", types: ["number"] }, { value: "lte", label: "≤", types: ["number"] },
];
const UNIT: Record<string, string> = { kg: "mass", kgco2e: "CO₂e", pct: "%", c: "°C", bar: "bar", km: "km", m2: "area", tkm: "t·km", kg_h: "kg/h", rpm: "rpm", count: "count" };

/** A labelled control, associated by id so assistive tech reads the label. */
function Row({ label, hint, children }: { label: string; hint?: string; children: ReactElement<{ id?: string }> }) {
  const id = useId();
  return <div><label htmlFor={id} className="label">{label}{hint && <span className="font-normal text-muted"> · {hint}</span>}</label>{cloneElement(children, { id })}</div>;
}

/**
 * Configures one widget: what it shows (source, measure, aggregation, grouping,
 * filters) and how (chart style, thresholds), with a live preview.
 */
export function WidgetEditor({ widget, isNew, onClose, onSave }: { widget: Widget | null; isNew?: boolean; onClose: () => void; onSave: (w: Widget) => void }) {
  const cat = useCatalog().data;
  const [w, setW] = useState<Widget | null>(widget);
  useEffect(() => setW(widget), [widget]);
  const src = cat?.sources.find((s) => s.key === w?.query?.source);
  const measures = src?.fields.filter((f) => f.kind === "measure") ?? [];
  const dims = src?.fields.filter((f) => f.kind === "dimension") ?? [];
  const measure = measures.find((m) => m.key === w?.query?.measure);
  const needsGroup = w ? WIDGETS[w.type].needsGroup : false;
  const q = w?.query;

  const setQ = (patch: Partial<Query>) => w && setW({ ...w, query: { ...(w.query ?? { source: "records", measure: "mass_kg", agg: "sum" }), ...patch } });
  const setType = (t: WidgetType) => {
    if (!w) return;
    const next: Widget = { ...w, type: t, w: Math.max(w.w, WIDGETS[t].w > 6 ? 6 : 1), h: Math.max(w.h, t === "kpi" ? 2 : 3) };
    if (t === "text") { next.query = undefined; next.options = { ...w.options, text: w.options?.text ?? "" }; }
    else if (BUILTIN.includes(t)) { next.query = undefined; next.range = undefined; if (t === "journey") next.options = { journeyStyle: "pipeline", journeyMeasure: "mass", animate: true, ...w.options }; }
    else if (!next.query) next.query = { source: "records", measure: "mass_kg", agg: "sum" };
    if (next.query) {
      if (WIDGETS[t].needsGroup && !next.query.groupBy) next.query = { ...next.query, groupBy: t === "map" ? "region" : "org_id" };
      if (t === "map") next.query = { ...next.query, groupBy: "region" };
      if (t === "timeseries" && !next.query.interval) next.query = { ...next.query, interval: "auto" };
      if (t !== "timeseries") next.query = { ...next.query, interval: undefined };
      if (t === "kpi" || t === "gauge") next.query = { ...next.query, groupBy: undefined };
    }
    setW(next);
  };
  const preview = useMemo(() => w, [w]);
  if (!w) return null;

  return (
    <Drawer open={!!widget} onOpenChange={(o) => !o && onClose()} title={isNew ? "New widget" : `Edit “${widget?.title ?? ""}”`} sub="Choose what to show and how. The preview uses the page's time range." width={880}>
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); onSave(w); }}>
          <Row label="Title"><input className="input" value={w.title} maxLength={120} onChange={(e) => setW({ ...w, title: e.target.value })} required /></Row>
          <Row label="Explanation" hint="shown when hovering the ⓘ; leave empty to describe the data automatically">
            <input className="input" value={w.options?.help ?? ""} maxLength={400} onChange={(e) => setW({ ...w, options: { ...w.options, help: e.target.value || undefined } })} />
          </Row>
          <div>
            <span className="label">Widget</span>
            <div className="grid grid-cols-4 gap-1.5" role="radiogroup" aria-label="Widget type">
              {(Object.keys(WIDGETS) as WidgetType[]).map((t) => {
                const I = ICON[t];
                return <button key={t} type="button" role="radio" aria-checked={w.type === t} aria-label={WIDGETS[t].name} title={WIDGETS[t].explain} onClick={() => setType(t)}
                  className={clsx("flex flex-col items-center gap-1 rounded-xl border px-2 py-2 text-[11.5px] font-medium", w.type === t ? "border-accent bg-accent-soft" : "border-line hover:bg-surface-2")}>
                  <I size={16} aria-hidden />{WIDGETS[t].name}</button>;
              })}
            </div>
          </div>

          {w.type === "text" ? (
            <Row label="Text" hint="“# ” starts a heading, “- ” a bullet">
              <textarea className="input min-h-[160px] py-2" maxLength={3000} value={w.options?.text ?? ""} onChange={(e) => setW({ ...w, options: { ...w.options, text: e.target.value } })} />
            </Row>
          ) : w.type === "journey" ? (
            <div className="grid grid-cols-2 gap-3">
              <Row label="Style">
                <select className="input" value={w.options?.journeyStyle ?? "pipeline"} onChange={(e) => setW({ ...w, options: { ...w.options, journeyStyle: e.target.value as "pipeline" } })}>
                  <option value="pipeline">Pipeline cards</option><option value="list">List with bars</option><option value="figures">Figures only</option>
                </select>
              </Row>
              <Row label="Measure">
                <select className="input" value={w.options?.journeyMeasure ?? "mass"} onChange={(e) => setW({ ...w, options: { ...w.options, journeyMeasure: e.target.value as "mass" } })}>
                  <option value="mass">Mass</option><option value="records">Number of records</option>
                </select>
              </Row>
              <label className="col-span-2 flex items-center gap-2 text-[13px]"><input type="checkbox" checked={w.options?.animate ?? true} onChange={(e) => setW({ ...w, options: { ...w.options, animate: e.target.checked } })} /> Count the figures up when the period changes</label>
            </div>
          ) : BUILTIN.includes(w.type) ? (
            <p className="rounded-xl bg-surface-2 px-3 py-2.5 text-[13px] text-text-2">{WIDGETS[w.type].explain} It follows the page's time range; rename, resize and move it like any widget.</p>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-3">
                <Row label="Data from">
                  <select className="input" value={q?.source} onChange={(e) => {
                    const s = cat?.sources.find((x) => x.key === e.target.value);
                    const m = s?.fields.find((f) => f.kind === "measure" && f.unit !== "count") ?? s?.fields.find((f) => f.kind === "measure");
                    setQ({ source: e.target.value, measure: m?.key ?? "", types: undefined, filters: [], groupBy: needsGroup ? s?.fields.find((f) => f.kind === "dimension")?.key : undefined });
                  }}>{cat?.sources.map((s) => <option key={s.key} value={s.key}>{s.label}</option>)}</select>
                </Row>
                <Row label="Time window">
                  <select className="input" value={w.range ?? ""} onChange={(e) => setW({ ...w, range: e.target.value as Widget["range"] })}>
                    {RANGES.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
                  </select>
                </Row>
              </div>
              {src?.types && (
                <div>
                  <span className="label">Record types <span className="font-normal text-muted">· none selected = all</span></span>
                  <div className="flex flex-wrap gap-1">
                    {src.types.map((t) => {
                      const on = q?.types?.includes(t);
                      return <button key={t} type="button" aria-pressed={!!on} onClick={() => setQ({ types: on ? q!.types!.filter((x) => x !== t) : [...(q?.types ?? []), t] })}
                        className={clsx("chip", on ? "chip-accent" : "opacity-70 hover:opacity-100")}>{TYPE[t as RecordType]?.plural ?? t}</button>;
                    })}
                  </div>
                </div>
              )}
              <div className="grid grid-cols-2 gap-3">
                <Row label="Measure" hint={measure?.unit ? UNIT[measure.unit] ?? measure.unit : undefined}>
                  <select className="input" value={q?.measure} onChange={(e) => setQ({ measure: e.target.value })}>
                    {measures.map((m) => <option key={m.key} value={m.key}>{m.label}</option>)}
                  </select>
                </Row>
                <Row label="Calculate">
                  <select className="input" value={measure?.unit === "count" && measure.key === measures[0]?.key ? "count" : q?.agg} disabled={measure?.unit === "count" && measure.key === measures[0]?.key}
                    onChange={(e) => setQ({ agg: e.target.value as Query["agg"] })}>
                    {AGGS.map((a) => <option key={a.value} value={a.value}>{a.label}</option>)}
                  </select>
                </Row>
              </div>
              {measure?.help && <p className="-mt-2 text-[12px] text-muted">{measure.help}</p>}
              {(needsGroup || w.type === "timeseries") && (
                <div className="grid grid-cols-2 gap-3">
                  <Row label={w.type === "timeseries" ? "One line per" : "Group by"}>
                    <select className="input" value={q?.groupBy ?? ""} disabled={w.type === "map"} onChange={(e) => setQ({ groupBy: e.target.value || undefined })}>
                      {w.type === "timeseries" && <option value="">Nothing (one line)</option>}
                      {dims.map((d) => <option key={d.key} value={d.key}>{d.label}</option>)}
                    </select>
                  </Row>
                  {w.type === "timeseries" ? (
                    <Row label="Every">
                      <select className="input" value={q?.interval ?? "auto"} onChange={(e) => setQ({ interval: e.target.value as Query["interval"] })}>
                        <option value="auto">Automatic</option><option value="hour">Hour</option><option value="day">Day</option><option value="week">Week</option><option value="month">Month</option>
                      </select>
                    </Row>
                  ) : (
                    <Row label="Show the top">
                      <select className="input" value={q?.limit ?? 10} onChange={(e) => setQ({ limit: Number(e.target.value) })}>
                        {[5, 8, 10, 15, 25, 50].map((n) => <option key={n} value={n}>{n}</option>)}
                      </select>
                    </Row>
                  )}
                </div>
              )}
              <Filters filters={q?.filters ?? []} fields={src?.fields ?? []} onChange={(filters) => setQ({ filters })} />

              <details className="rounded-xl border border-line px-3 py-2" open={w.type === "gauge" || w.type === "timeseries"}>
                <summary className="cursor-pointer text-[13px] font-semibold">Display</summary>
                <div className="mt-3 grid grid-cols-2 gap-3">
                  {w.type === "timeseries" && (
                    <Row label="Style">
                      <select className="input" value={w.options?.style ?? "line"} onChange={(e) => setW({ ...w, options: { ...w.options, style: e.target.value as "line" } })}>
                        <option value="line">Lines</option><option value="area">Areas (stacked)</option><option value="bars">Bars (stacked)</option>
                      </select>
                    </Row>
                  )}
                  {w.type === "bars" && (
                    <Row label="Chart">
                      <select className="input" value={w.options?.orient ?? "horizontal"} onChange={(e) => setW({ ...w, options: { ...w.options, orient: e.target.value as "horizontal" } })}>
                        <option value="horizontal">Horizontal bars</option><option value="vertical">Columns</option>
                      </select>
                    </Row>
                  )}
                  {w.type === "donut" && (
                    <Row label="Chart">
                      <select className="input" value={w.options?.donutStyle ?? "donut"} onChange={(e) => setW({ ...w, options: { ...w.options, donutStyle: e.target.value as "donut" } })}>
                        <option value="donut">Donut</option><option value="pie">Pie</option>
                      </select>
                    </Row>
                  )}
                  {(w.type === "kpi") && (
                    <label className="col-span-2 flex items-center gap-2 text-[13px]"><input type="checkbox" checked={!!q?.compare} onChange={(e) => setQ({ compare: e.target.checked })} /> Compare with the previous period</label>
                  )}
                  {(w.type === "kpi" || w.type === "gauge") && (
                    <label className="col-span-2 flex items-center gap-2 text-[13px]"><input type="checkbox" checked={!!w.options?.invert} onChange={(e) => setW({ ...w, options: { ...w.options, invert: e.target.checked } })} /> Lower is better</label>
                  )}
                  {w.type === "gauge" && (["min", "max", "warn", "critical"] as const).map((k) => (
                    <Row key={k} label={{ min: "Scale from", max: "Scale to", warn: "Warning at", critical: "Critical at" }[k]}>
                      <input className="input num" type="number" step="any" value={w.options?.[k] ?? ""} onChange={(e) => setW({ ...w, options: { ...w.options, [k]: e.target.value === "" ? undefined : Number(e.target.value) } })} />
                    </Row>
                  ))}
                  {(w.type === "bars" || (w.type === "timeseries" && !q?.groupBy)) && (
                    <div className="col-span-2"><span className="label">Colour</span>
                      <div className="flex gap-1.5">{SERIES.map((c) => <button key={c} type="button" aria-label={`Colour ${c}`} aria-pressed={(w.options?.color ?? SERIES[0]) === c} onClick={() => setW({ ...w, options: { ...w.options, color: c } })}
                        className={clsx("size-6 rounded-full ring-offset-2 ring-offset-[var(--bg-elevated)]", (w.options?.color ?? SERIES[0]) === c && "ring-2 ring-text")} style={{ background: c }} />)}</div></div>
                  )}
                  <Row label="Decimals"><select className="input" value={w.options?.decimals ?? ""} onChange={(e) => setW({ ...w, options: { ...w.options, decimals: e.target.value === "" ? undefined : Number(e.target.value) } })}>
                    <option value="">Automatic</option><option value="0">0</option><option value="1">1</option><option value="2">2</option></select></Row>
                </div>
              </details>
            </>
          )}
          <div className="sticky bottom-0 flex justify-end gap-2 bg-[var(--bg-elevated)] pt-2">
            <button type="button" className="btn" onClick={onClose}>Cancel</button>
            <button className="btn btn-primary">Apply</button>
          </div>
        </form>
        <div className="lg:sticky lg:top-0 lg:self-start">
          <span className="label">Preview</span>
          <div className="card rounded-2xl p-4">
            <div className="mb-2 text-[13.5px] font-semibold">{w.title || WIDGETS[w.type].name}</div>
            <div style={{ height: w.type === "kpi" ? 90 : 260 }}>{preview && <WidgetBody w={preview} height={w.type === "kpi" ? 90 : 260} width={400} />}</div>
          </div>
        </div>
      </div>
    </Drawer>
  );
}

function Filters({ filters, fields, onChange }: { filters: Filter[]; fields: { key: string; label: string; type: string }[]; onChange: (f: Filter[]) => void }) {
  const set = (i: number, f: Partial<Filter>) => onChange(filters.map((x, j) => (j === i ? { ...x, ...f } : x)));
  return (
    <div>
      <span className="label">Only where</span>
      <div className="flex flex-col gap-1.5">
        {filters.map((f, i) => {
          const fd = fields.find((x) => x.key === f.field);
          const ops = OPS.filter((o) => o.types.includes(fd?.type ?? "string"));
          return (
            <div key={i} className="grid grid-cols-[1fr_92px_1fr_auto] gap-1.5">
              <select className="input h-8 text-[12.5px]" value={f.field} aria-label="Field" onChange={(e) => set(i, { field: e.target.value })}>{fields.map((x) => <option key={x.key} value={x.key}>{x.label}</option>)}</select>
              <select className="input h-8 text-[12.5px]" value={f.op} aria-label="Condition" onChange={(e) => set(i, { op: e.target.value as Filter["op"] })}>{ops.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</select>
              <input className="input h-8 text-[12.5px]" aria-label="Value" placeholder={f.op === "in" ? "a, b, c" : fd?.type === "bool" ? "true / false" : ""}
                value={Array.isArray(f.value) ? f.value.join(", ") : String(f.value ?? "")}
                onChange={(e) => {
                  const raw = e.target.value;
                  const conv = (v: string) => (fd?.type === "number" ? Number(v) : fd?.type === "bool" ? v.trim() === "true" : v.trim());
                  set(i, { value: f.op === "in" ? raw.split(",").map((x) => x.trim()).filter(Boolean).map((x) => conv(x) as string | number) : (conv(raw) as string | number | boolean) });
                }} />
              <button type="button" className="btn btn-ghost btn-icon h-8" aria-label="Remove condition" onClick={() => onChange(filters.filter((_, j) => j !== i))}><Trash2 size={13} /></button>
            </div>
          );
        })}
        {filters.length < 10 && fields.length > 0 && (
          <button type="button" className="btn btn-sm w-fit" onClick={() => onChange([...filters, { field: fields.find((f) => f.type === "string")?.key ?? fields[0].key, op: "eq", value: "" }])}><Plus size={13} /> Add a condition</button>
        )}
      </div>
    </div>
  );
}
