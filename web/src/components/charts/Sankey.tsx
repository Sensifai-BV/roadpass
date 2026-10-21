import { useId, useMemo, useState } from "react";
import { sankey, sankeyLinkHorizontal, type SankeyNode, type SankeyLink } from "d3-sankey";
import { useSize } from "./TimeChart";
import type { FlowLink, OrgRef } from "../../lib/types";
import { STAGES } from "../../lib/vocab";
import { mass } from "../../lib/format";

interface N { id: string; org: string | null; name: string; col: number; color: string; real: number }
interface L { source: string; target: string; value: number; real: number; stage: number }

const MAX_PER_COLUMN = 9;
const HEAD = 44; // room for the column headers
const PAD_X = 12;
const NODE_W = 12;

/**
 * Organisation-to-organisation flows of plastic through the chain.
 *
 * - Columns are the chain's steps, each with a header; a node's colour is its step.
 * - A flow fades from its sender's colour to its receiver's.
 * - Widths are tonnes of plastic. Plastic shrinks along the chain (114 t of film
 *   becomes 1 t in a road), so a flow below 2 % of the largest is drawn at that
 *   minimum width to stay visible; labels and tooltips always show the true tonnes.
 * - Hover a company or a flow to trace it; click a company to open its passports of that step.
 */
export function ChainSankey({ links, orgName, height = 540, onSelect }: {
  links: FlowLink[]; orgName: (id: string) => string; height?: number; onSelect?: (orgId: string, col: number) => void;
}) {
  const [ref, width] = useSize<HTMLDivElement>();
  const uid = useId().replace(/[^a-zA-Z0-9]/g, "");
  const [hoverNode, setHoverNode] = useState<string | null>(null);
  const [hoverLink, setHoverLink] = useState<number | null>(null);
  const [tip, setTip] = useState<{ x: number; y: number; title: string; lines: string[] } | null>(null);

  const graph = useMemo(() => {
    // A node is an organisation at one column: stage s runs from column s-1 to
    // column s. An organisation that does several steps itself shows once per
    // column, so its flows are not self-loops.
    const key = (org: string, col: number) => `${col}|${org}`;
    const volume = new Map<string, number>();
    for (const l of links) {
      for (const k of [key(l.from, l.stage - 1), key(l.to, l.stage)]) volume.set(k, (volume.get(k) ?? 0) + l.tonnes);
    }
    // Fold the long tail of each column into "Other …" so the chart stays legible.
    const keep = new Set<string>();
    for (let c = 0; c <= 5; c++) {
      [...volume.keys()].filter((k) => k.startsWith(`${c}|`))
        .sort((a, b) => (volume.get(b) ?? 0) - (volume.get(a) ?? 0)).slice(0, MAX_PER_COLUMN).forEach((k) => keep.add(k));
    }
    const nodeId = (org: string, col: number) => (keep.has(key(org, col)) ? key(org, col) : `${col}|other`);
    const nodes = new Map<string, N>();
    const agg = new Map<string, L>();
    for (const l of links) {
      const s = nodeId(l.from, l.stage - 1);
      const t = nodeId(l.to, l.stage);
      for (const id of [s, t]) {
        if (!nodes.has(id)) {
          const c = Number(id.slice(0, id.indexOf("|")));
          const org = id.slice(id.indexOf("|") + 1);
          nodes.set(id, {
            id, col: c, org: org === "other" ? null : org, real: 0,
            name: org === "other" ? `Other ${STAGES[c]?.who.toLowerCase() ?? ""}` : orgName(org),
            color: STAGES[c]?.color ?? "var(--muted)",
          });
        }
      }
      const k = `${s}>${t}`;
      const e = agg.get(k);
      if (e) { e.real += l.tonnes; }
      else agg.set(k, { source: s, target: t, value: 0, real: l.tonnes, stage: l.stage });
    }
    const out = [...agg.values()].filter((l) => l.real > 0);
    // A node's true volume is the larger of what flows in and what flows out.
    const inn = new Map<string, number>(), outv = new Map<string, number>();
    for (const l of out) {
      outv.set(l.source, (outv.get(l.source) ?? 0) + l.real);
      inn.set(l.target, (inn.get(l.target) ?? 0) + l.real);
    }
    for (const n of nodes.values()) n.real = Math.max(inn.get(n.id) ?? 0, outv.get(n.id) ?? 0);
    const floor = Math.max(...out.map((l) => l.real), 0) * 0.02;
    for (const l of out) l.value = Math.max(l.real, floor);
    return { nodes: [...nodes.values()], links: out };
  }, [links, orgName]);

  const cols = useMemo(() => [...new Set(graph.nodes.map((n) => n.col))].sort((a, b) => a - b), [graph.nodes]);

  const layout = useMemo(() => {
    if (width < 240 || !graph.links.length) return null;
    // d3 counts columns from the longest path, which is shorter than the chain
    // when a step has no shipment (done in-house), so clamp the column for d3,
    // then put every node back at its own column.
    const rank = (n: N) => cols.indexOf(n.col);
    const gen = sankey<N, L>()
      .nodeId((d) => d.id)
      .nodeAlign((d, n) => Math.min(rank(d as N), n - 1))
      .nodeWidth(NODE_W)
      .nodePadding(14)
      .nodeSort((a, b) => (b.value ?? 0) - (a.value ?? 0))
      .extent([[PAD_X, HEAD], [width - PAD_X, height - 10]]);
    try {
      const g = gen({ nodes: graph.nodes.map((d) => ({ ...d })), links: graph.links.map((d) => ({ ...d })) });
      const span = width - 2 * PAD_X - NODE_W;
      const step = cols.length > 1 ? span / (cols.length - 1) : 0;
      for (const n of g.nodes) {
        n.x0 = PAD_X + rank(n) * step;
        n.x1 = n.x0 + NODE_W;
      }
      return gen.update(g);
    } catch {
      return null;
    }
  }, [graph, cols, width, height]);

  const nodeKey = (n: SankeyNode<N, L>) => n.org ?? n.id;
  const lit = (l: SankeyLink<N, L>, i: number) => {
    if (hoverLink !== null) return hoverLink === i;
    if (!hoverNode) return true;
    const s = l.source as SankeyNode<N, L>, t = l.target as SankeyNode<N, L>;
    return `${s.col}|${nodeKey(s)}` === hoverNode || `${t.col}|${nodeKey(t)}` === hoverNode;
  };
  const place = (e: React.PointerEvent, title: string, lines: string[]) => {
    const box = (ref.current as HTMLDivElement).getBoundingClientRect();
    setTip({ x: Math.min(e.clientX - box.left + 14, width - 230), y: Math.max(e.clientY - box.top - 8, 0), title, lines });
  };

  return (
    <div ref={ref} style={{ height }} className="relative">
      {layout && (
        <svg className="viz" width={width} height={height} role="img" aria-label="Flows of plastic between organisations, by step">
          <defs>
            {layout.links.map((l, i) => {
              const s = l.source as SankeyNode<N, L>, t = l.target as SankeyNode<N, L>;
              return (
                <linearGradient key={i} id={`${uid}g${i}`} gradientUnits="userSpaceOnUse" x1={s.x1} x2={t.x0}>
                  <stop offset="0" style={{ stopColor: s.color }} />
                  <stop offset="1" style={{ stopColor: t.color }} />
                </linearGradient>
              );
            })}
          </defs>

          {/* column headers */}
          {cols.map((c, i) => {
            const st = STAGES[c];
            const x = PAD_X + i * (cols.length > 1 ? (width - 2 * PAD_X - NODE_W) / (cols.length - 1) : 0) + NODE_W / 2;
            const anchor = i === 0 ? "start" : i === cols.length - 1 ? "end" : "middle";
            const tx = i === 0 ? PAD_X : i === cols.length - 1 ? width - PAD_X : x;
            return (
              <g key={c} transform={`translate(${tx},18)`}>
                <text textAnchor={anchor} style={{ fill: "var(--text)", fontSize: 12, fontWeight: 650, letterSpacing: "0.01em" }}>
                  <tspan style={{ fill: st?.color }}>● </tspan>{st?.title ?? ""}
                </text>
                <text textAnchor={anchor} y={14} style={{ fill: "var(--muted)", fontSize: 10.5 }}>{st?.who ?? ""}</text>
              </g>
            );
          })}

          <g fill="none">
            {layout.links.map((l, i) => {
              const s = l.source as SankeyNode<N, L>, t = l.target as SankeyNode<N, L>;
              const on = lit(l, i);
              return (
                <path key={i} d={sankeyLinkHorizontal()(l) ?? ""} stroke={`url(#${uid}g${i})`}
                  strokeOpacity={on ? (hoverLink === i ? 0.7 : 0.42) : 0.07} strokeWidth={Math.max(1.5, l.width ?? 1)}
                  style={{ transition: "stroke-opacity .18s", cursor: "default" }}
                  onPointerEnter={(e) => { setHoverLink(i); place(e, `${s.name} → ${t.name}`, [mass(l.real * 1000), `${pctOf(l.real, s)} of ${s.name}’s output`]); }}
                  onPointerMove={(e) => place(e, `${s.name} → ${t.name}`, [mass(l.real * 1000), `${pctOf(l.real, s)} of ${s.name}’s output`])}
                  onPointerLeave={() => { setHoverLink(null); setTip(null); }} />
              );
            })}
          </g>

          {layout.nodes.map((n) => {
            const x0 = n.x0 ?? 0, x1 = n.x1 ?? 0, y0 = n.y0 ?? 0, y1 = n.y1 ?? 0;
            const h = y1 - y0;
            const id = `${n.col}|${nodeKey(n)}`;
            const last = n.col === cols[cols.length - 1];
            const left = last && cols.length > 1; // the last column labels to its left
            const two = h >= 30;
            const on = hoverNode === id;
            return (
              <g key={n.id} onPointerEnter={(e) => { setHoverNode(id); place(e, n.name, [`${mass(n.real * 1000)} of plastic`, n.org && onSelect ? "Click to open its passports" : ""].filter(Boolean)); }}
                onPointerMove={(e) => place(e, n.name, [`${mass(n.real * 1000)} of plastic`, n.org && onSelect ? "Click to open its passports" : ""].filter(Boolean))}
                onPointerLeave={() => { setHoverNode(null); setTip(null); }}
                onClick={() => onSelect && n.org && onSelect(n.org, n.col)} style={{ cursor: onSelect && n.org ? "pointer" : "default" }}>
                <rect x={x0} y={y0} width={x1 - x0} height={Math.max(2, h)} rx={4} fill={n.color} opacity={on ? 1 : 0.92} />
                {h >= 13 && (
                  <text x={left ? x0 - 8 : x1 + 8} y={(y0 + y1) / 2} dy={two ? "-0.15em" : "0.35em"} textAnchor={left ? "end" : "start"}
                    style={{ ...halo, fill: "var(--text)", fontSize: 12, fontWeight: on ? 700 : 600 }}>
                    {clip(short(n.name), 26)}
                    {!two && <tspan style={{ fill: "var(--text-2)", fontWeight: 500 }}>{`  ${mass(n.real * 1000)}`}</tspan>}
                  </text>
                )}
                {two && (
                  <text x={left ? x0 - 8 : x1 + 8} y={(y0 + y1) / 2} dy="1.15em" textAnchor={left ? "end" : "start"}
                    style={{ ...halo, fill: "var(--text-2)", fontSize: 11 }}>{mass(n.real * 1000)}</text>
                )}
                <rect x={x0 - 6} y={y0 - 3} width={x1 - x0 + 12} height={Math.max(10, h + 6)} fill="transparent" />
              </g>
            );
          })}
        </svg>
      )}
      {tip && (
        <div className="tooltip max-w-[240px]" style={{ left: tip.x, top: tip.y }}>
          <div className="font-semibold">{tip.title}</div>
          {tip.lines.map((x, i) => <div key={i} className={i === 0 ? "num text-[13px] font-semibold" : "text-muted"}>{x}</div>)}
        </div>
      )}
      {!layout && width > 0 && <div className="grid h-full place-items-center text-[13px] text-muted">No flows in this period.</div>}
    </div>
  );
}

// A label sits on top of flows, so it gets a halo in the card colour to stay readable.
const halo = { paintOrder: "stroke", stroke: "var(--surface)", strokeWidth: 3.5, strokeLinejoin: "round" } as const;

function pctOf(v: number, s: SankeyNode<N, L>): string {
  const out = (s.sourceLinks ?? []).reduce((a, l) => a + ((l as unknown as L).real ?? 0), 0);
  return out > 0 ? `${Math.round((100 * v) / out)} %` : "";
}

function clip(s: string, n: number): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s;
}

function short(name: string): string {
  return name.replace(/ (S\.A\.|S\.L\.)$/, "").replace(/ \(demo authority\)$/, "").replace(/ \(platform operator\)$/, "");
}

export type { OrgRef };
