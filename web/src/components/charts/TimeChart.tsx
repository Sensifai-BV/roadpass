import { useMemo, useRef, useState, useLayoutEffect, useEffect, type ReactNode } from "react";
import { scaleLinear, scaleTime, scaleBand } from "d3-scale";
import { area, line, curveMonotoneX, stack, stackOrderNone, stackOffsetNone } from "d3-shape";
import { max } from "d3-array";
import { Legend } from "../ui";

export function useSize<T extends HTMLElement>(): [React.RefObject<T | null>, number] {
  const ref = useRef<T>(null);
  const [w, setW] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setW(el.clientWidth);
    const ro = new ResizeObserver(([e]) => setW(Math.round(e.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w];
}

export interface SeriesDef {
  key: string;
  label: string;
  color: string;
}

export interface Row {
  t: Date;
  [k: string]: number | Date;
}

/** Pivots API points (t, series, v) into rows keyed by series. */
export function pivot(points: { t: string; series: string; v: number }[], keys: string[]): Row[] {
  const by = new Map<string, Row>();
  for (const p of points) {
    let r = by.get(p.t);
    if (!r) {
      r = { t: new Date(p.t) } as Row;
      for (const k of keys) r[k] = 0;
      by.set(p.t, r);
    }
    r[p.series] = p.v;
  }
  return [...by.values()].sort((a, b) => +a.t - +b.t);
}

const fmtTick = (d: Date, spanDays: number) =>
  spanDays > 400
    ? d.toLocaleDateString("en-GB", { month: "short", year: "2-digit", timeZone: "UTC" })
    : spanDays > 60
      ? d.toLocaleDateString("en-GB", { month: "short", timeZone: "UTC" })
      : d.toLocaleDateString("en-GB", { day: "numeric", month: "short", timeZone: "UTC" });

/**
 * Time series as lines, areas or stacked bars, with a crosshair and a tooltip
 * listing every series at the hovered bucket. One y-axis, always.
 */
export function TimeChart({ rows, series, kind = "line", height = 240, yFormat, tipFormat, legend = true, empty }: {
  rows: Row[]; series: SeriesDef[]; kind?: "line" | "area" | "bars"; height?: number;
  yFormat: (v: number) => string; tipFormat?: (v: number) => string; legend?: boolean; empty?: ReactNode;
}) {
  const [ref, width] = useSize<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const m = { t: 10, r: 8, b: 26, l: 52 };
  const W = Math.max(0, width - m.l - m.r);
  const H = height - m.t - m.b;
  const keys = series.map((s) => s.key);

  const stacked = useMemo(() => {
    if (kind === "line") return null;
    return stack<Row>().keys(keys).order(stackOrderNone).offset(stackOffsetNone)(rows);
  }, [rows, kind, keys.join(",")]); // eslint-disable-line react-hooks/exhaustive-deps

  const yMax = useMemo(() => {
    if (stacked) return max(stacked[stacked.length - 1] ?? [], (d) => d[1]) ?? 0;
    return max(rows, (r) => max(keys, (k) => r[k] as number)) ?? 0;
  }, [rows, stacked, keys.join(",")]); // eslint-disable-line react-hooks/exhaustive-deps

  const x = useMemo(() => {
    const t0 = rows[0]?.t ?? new Date();
    const t1 = rows[rows.length - 1]?.t ?? new Date();
    return scaleTime().domain([t0, t1]).range([0, W]);
  }, [rows, W]);
  const band = useMemo(() => scaleBand<number>().domain(rows.map((_, i) => i)).range([0, W]).paddingInner(rows.length > 60 ? 0.15 : 0.28), [rows, W]);
  const y = useMemo(() => scaleLinear().domain([0, yMax * 1.08 || 1]).nice(4).range([H, 0]), [yMax, H]);
  const spanDays = rows.length > 1 ? (+rows[rows.length - 1].t - +rows[0].t) / 864e5 : 1;
  const xpos = (i: number) => (kind === "bars" ? (band(i) ?? 0) + band.bandwidth() / 2 : x(rows[i].t));

  const onMove = (e: React.PointerEvent<SVGRectElement>) => {
    const r = (e.currentTarget as SVGRectElement).getBoundingClientRect();
    const px = e.clientX - r.left;
    let best = 0;
    let bd = Infinity;
    for (let i = 0; i < rows.length; i++) {
      const d = Math.abs(xpos(i) - px);
      if (d < bd) {
        bd = d;
        best = i;
      }
    }
    setHover(best);
  };
  useEffect(() => setHover(null), [rows]);

  if (!rows.length) {
    return <div className="grid h-[200px] place-items-center text-[13px] text-muted">{empty ?? "No data in this period."}</div>;
  }

  const ticksX = kind === "bars"
    ? rows.map((r, i) => ({ i, t: r.t })).filter((_, i, a) => i % Math.max(1, Math.ceil(a.length / Math.max(2, Math.floor(W / 80)))) === 0)
    : x.ticks(Math.max(2, Math.floor(W / 90))).map((t) => ({ i: -1, t }));
  const hv = hover !== null ? rows[hover] : null;
  const tip = tipFormat ?? yFormat;
  const total = hv ? keys.reduce((s, k) => s + (hv[k] as number), 0) : 0;

  return (
    <div>
      {legend && series.length > 1 && (
        <div className="mb-3">
          <Legend items={series.map((s) => ({ label: s.label, color: s.color }))} />
        </div>
      )}
      <div ref={ref} className="relative" style={{ height }}>
        {width > 0 && (
          <svg className="viz" width={width} height={height} role="img" aria-label={`Chart of ${series.map((s) => s.label).join(", ")}`}>
            <g transform={`translate(${m.l},${m.t})`}>
              <g className="grid">
                {y.ticks(4).map((v) => (
                  <g key={v} transform={`translate(0,${y(v)})`}>
                    <line x2={W} />
                    <text x={-10} dy="0.32em" textAnchor="end">{yFormat(v)}</text>
                  </g>
                ))}
              </g>
              {ticksX.map(({ i, t }) => (
                <text key={+t} x={i >= 0 ? xpos(i) : x(t)} y={H + 18} textAnchor="middle">{fmtTick(t, spanDays)}</text>
              ))}
              {kind === "line" &&
                series.map((s) => (
                  <path key={s.key} fill="none" stroke={s.color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round"
                    d={line<Row>().x((r) => x(r.t)).y((r) => y(r[s.key] as number)).curve(curveMonotoneX)(rows) ?? ""} />
                ))}
              {kind === "area" && stacked &&
                stacked.map((layer, li) => (
                  <g key={layer.key}>
                    <path fill={series[li].color} fillOpacity={0.16}
                      d={area<[number, number] & { data: Row }>().x((d) => x(d.data.t)).y0((d) => y(d[0])).y1((d) => y(d[1])).curve(curveMonotoneX)(layer as never) ?? ""} />
                    <path fill="none" stroke={series[li].color} strokeWidth={2}
                      d={line<[number, number] & { data: Row }>().x((d) => x(d.data.t)).y((d) => y(d[1])).curve(curveMonotoneX)(layer as never) ?? ""} />
                  </g>
                ))}
              {kind === "bars" && stacked &&
                rows.map((_, i) => {
                  const bx = band(i) ?? 0;
                  const bw = band.bandwidth();
                  const segs = stacked.map((layer, li) => ({ li, y0: layer[i][0], y1: layer[i][1] })).filter((s) => s.y1 > s.y0);
                  // One series: quiet tints, with the current (last) and hovered bucket in full colour.
                  const single = series.length === 1;
                  const strong = hover === null ? i === rows.length - 1 : hover === i;
                  const tint = (c: string) => (single && !strong ? `color-mix(in srgb, ${c} 32%, var(--surface-3))` : c);
                  return (
                    <g key={i} opacity={single || hover === null || hover === i ? 1 : 0.55}>
                      {segs.map((s, si) => {
                        const top = y(s.y1);
                        const h = Math.max(0, y(s.y0) - top - (si > 0 ? 2 : 0)); // 2px surface gap between segments
                        const last = si === segs.length - 1;
                        const r = Math.min(4, bw / 2, h);
                        return last ? (
                          <path key={s.li} fill={tint(series[s.li].color)} style={{ transition: "fill .15s" }}
                            d={`M${bx},${top + h} V${top + r} Q${bx},${top} ${bx + r},${top} H${bx + bw - r} Q${bx + bw},${top} ${bx + bw},${top + r} V${top + h} Z`} />
                        ) : (
                          <rect key={s.li} x={bx} y={top} width={bw} height={h} fill={tint(series[s.li].color)} />
                        );
                      })}
                    </g>
                  );
                })}
              <line className="axis-line" x1={0} x2={W} y1={H} y2={H} />
              {hv && kind !== "bars" && (
                <g>
                  <line x1={xpos(hover!)} x2={xpos(hover!)} y1={0} y2={H} stroke="var(--line-strong)" strokeDasharray="3 3" />
                  {series.map((s, li) => {
                    const v = stacked ? stacked[li][hover!][1] : (hv[s.key] as number);
                    return <circle key={s.key} cx={xpos(hover!)} cy={y(v)} r={4} fill={s.color} stroke="var(--surface)" strokeWidth={2} />;
                  })}
                </g>
              )}
              <rect width={W} height={H} fill="transparent" onPointerMove={onMove} onPointerLeave={() => setHover(null)} />
            </g>
          </svg>
        )}
        {hv && (
          <div className="tooltip" style={{ left: Math.min(Math.max(0, m.l + xpos(hover!) + 12), width - 190), top: 4 }}>
            <div className="mb-1 font-semibold">{hv.t.toLocaleDateString("en-GB", { day: spanDays > 400 ? undefined : "numeric", month: "long", year: "numeric", timeZone: "UTC" })}</div>
            {series.map((s) => (
              <div key={s.key} className="flex items-center justify-between gap-4">
                <span className="inline-flex items-center gap-1.5 text-text-2">
                  <span className="inline-block size-2 rounded-[2px]" style={{ background: s.color }} />{s.label}
                </span>
                <span className="num font-semibold">{tip(hv[s.key] as number)}</span>
              </div>
            ))}
            {kind !== "line" && series.length > 1 && (
              <div className="mt-1 flex justify-between gap-4 border-t border-line pt-1 text-text-2">
                <span>Total</span>
                <span className="num font-semibold text-text">{tip(total)}</span>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

export function Sparkline({ values, color = "var(--accent)", height = 34 }: { values: number[]; color?: string; height?: number }) {
  const [ref, width] = useSize<HTMLDivElement>();
  const y = scaleLinear().domain([0, max(values) || 1]).range([height - 3, 3]);
  const xs = scaleLinear().domain([0, Math.max(1, values.length - 1)]).range([2, Math.max(2, width - 2)]);
  const d = line<number>().x((_, i) => xs(i)).y((v) => y(v)).curve(curveMonotoneX)(values) ?? "";
  const a = area<number>().x((_, i) => xs(i)).y0(height).y1((v) => y(v)).curve(curveMonotoneX)(values) ?? "";
  return (
    <div ref={ref} style={{ height }} aria-hidden>
      {width > 0 && (
        <svg width={width} height={height}>
          <path d={a} fill={color} opacity={0.12} />
          <path d={d} fill="none" stroke={color} strokeWidth={2} strokeLinecap="round" />
        </svg>
      )}
    </div>
  );
}
