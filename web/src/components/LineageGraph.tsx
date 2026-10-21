import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import clsx from "clsx";
import type { PNode, RecordType } from "../lib/types";
import { TYPE } from "../lib/vocab";
import { mass, shortDate } from "../lib/format";
import { useSize } from "./charts/TimeChart";

const ORDER: RecordType[] = ["reject_lot", "recycled_lot", "additive_batch", "asphalt_lot", "delivery", "road_section"];
const PER_COLUMN = 14;
// Wide enough for a full passport ID ("RSC-26-000001") next to its mass; narrower boxes scroll sideways.
const MIN_COL = 196;
const COL_GAP = 22;
const NODE_H = 30;
const GAP = 6;

/**
 * The material lineage as columns (one per step), largest contributors first.
 * Links are drawn between visible nodes; each column folds its tail into "+N".
 */
export function LineageGraph({ nodes, links, rootId, orgName }: {
  nodes: PNode[]; links: { parent: string; child: string; kind: string; massKg: number }[]; rootId: string; orgName: (id: string) => string;
}) {
  const [ref, width] = useSize<HTMLDivElement>();
  const [hover, setHover] = useState<string | null>(null);
  const nav = useNavigate();

  const layout = useMemo(() => {
    const cols = ORDER.map((t) => nodes.filter((n) => n.type === t).sort((a, b) => b.share * b.massKg - a.share * a.massKg)).filter((c) => c.length);
    const colW = cols.length ? Math.max(MIN_COL, (width - 24) / cols.length) : 0;
    const pos = new Map<string, { x: number; y: number }>();
    const hidden: number[] = [];
    let height = 0;
    cols.forEach((c, ci) => {
      const shown = c.slice(0, PER_COLUMN);
      hidden.push(c.length - shown.length);
      shown.forEach((n, i) => pos.set(n.id, { x: ci * colW, y: i * (NODE_H + GAP) }));
      height = Math.max(height, (shown.length + (c.length > PER_COLUMN ? 1 : 0)) * (NODE_H + GAP));
    });
    return { cols, colW, pos, hidden, height: Math.max(height, NODE_H) + 28 };
  }, [nodes, width]);

  const lit = useMemo(() => {
    if (!hover) return null;
    const set = new Set([hover]);
    // walk both directions from the hovered node
    let grew = true;
    while (grew) {
      grew = false;
      for (const l of links) {
        if (set.has(l.child) && !set.has(l.parent) && layout.pos.has(l.parent)) { set.add(l.parent); grew = true; }
      }
    }
    for (const l of links) if (l.parent === hover) set.add(l.child);
    return set;
  }, [hover, links, layout.pos]);

  const nodeW = layout.colW - COL_GAP;
  return (
    <div ref={ref} className="relative overflow-x-auto">
      {width > 0 && (
        <div style={{ height: layout.height, position: "relative", minWidth: layout.cols.length * MIN_COL }}>
          <svg className="absolute inset-0" width={layout.cols.length * layout.colW} height={layout.height} aria-hidden>
            {links.map((l, i) => {
              const a = layout.pos.get(l.parent);
              const b = layout.pos.get(l.child);
              if (!a || !b) return null;
              const x1 = a.x + nodeW, y1 = a.y + NODE_H / 2 + 22, x2 = b.x, y2 = b.y + NODE_H / 2 + 22;
              const on = !lit || (lit.has(l.parent) && lit.has(l.child));
              const mx = (x1 + x2) / 2;
              return (
                <path key={i} d={`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`} fill="none" stroke="var(--line-strong)"
                  strokeWidth={on && lit ? 1.6 : 1} opacity={on ? 1 : 0.25} style={{ transition: "opacity .15s" }} />
              );
            })}
          </svg>
          {layout.cols.map((c, ci) => (
            <div key={ci} className="absolute top-0" style={{ left: ci * layout.colW, width: nodeW }}>
              <div className="mb-1.5 flex items-center gap-1.5 text-[11px] font-semibold text-muted">
                <span className="size-2 rounded-[2px]" style={{ background: TYPE[c[0].type].color }} />
                {TYPE[c[0].type].plural} · {c.length}
              </div>
              {c.slice(0, PER_COLUMN).map((n) => {
                const on = !lit || lit.has(n.id);
                const isRoot = n.id === rootId;
                // Deliveries and shipments have no passport of their own: they open the passport of the
                // lot they carried, which is where the load came from.
                const target = TYPE[n.type].passport ? n.id : links.find((l) => l.child === n.id && l.kind !== "tested")?.parent;
                const opens = !!target && !isRoot;
                return (
                  <button
                    key={n.id}
                    type="button"
                    onPointerEnter={() => setHover(n.id)}
                    onPointerLeave={() => setHover(null)}
                    onFocus={() => setHover(n.id)}
                    onBlur={() => setHover(null)}
                    onClick={() => opens && nav(`/passports/${target}`)}
                    aria-disabled={!opens}
                    aria-current={isRoot ? "page" : undefined}
                    title={`${TYPE[n.type].name} ${n.id} · ${orgName(n.orgId)} · ${shortDate(n.occurredAt)} · contributed ${mass(n.share * n.massKg)}${isRoot ? " · this passport" : !opens ? "" : TYPE[n.type].passport ? "" : ` · opens the passport of ${target}, the lot it carried`}`}
                    className={clsx("mb-[6px] flex w-full items-center gap-1.5 overflow-hidden rounded-[9px] border bg-surface px-2 text-left transition", opens && "cursor-pointer hover:bg-surface-2")}
                    style={{
                      height: NODE_H, opacity: on ? 1 : 0.35,
                      borderColor: isRoot ? TYPE[n.type].color : "var(--line)",
                      boxShadow: isRoot ? `0 0 0 3px color-mix(in srgb, ${TYPE[n.type].color} 20%, transparent)` : undefined,
                    }}
                  >
                    <span className="h-4 w-[3px] shrink-0 rounded-full" style={{ background: TYPE[n.type].color }} />
                    <span className="mono shrink-0 whitespace-nowrap text-[11px] font-semibold">{n.id}</span>
                    <span className="num ml-auto min-w-0 truncate text-[10.5px] text-muted">{mass(n.share * n.massKg)}</span>
                  </button>
                );
              })}
              {layout.hidden[ci] > 0 && <div className="pl-1 text-[11px] text-muted">+{layout.hidden[ci]} more</div>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
