import { useRef, useState, type ReactNode } from "react";
import { GripVertical, MoveDiagonal2 } from "lucide-react";
import clsx from "clsx";
import { arrange, clampBox, rows, settle, type Box } from "../../lib/grid";
import { COLS, GAP, ROW_H, WIDGETS, type Widget } from "../../lib/dashboards";
import { useSize } from "../charts/TimeChart";

const MIN: Partial<Record<Widget["type"], [number, number]>> = { kpi: [2, 2], gauge: [2, 3], timeseries: [3, 3], map: [3, 4], donut: [3, 3], table: [3, 3], bars: [3, 3] };

interface Drag { id: string; mode: "move" | "resize"; px: number; py: number; ox: number; oy: number; start: Box; preview: Box }

/**
 * Widgets on a 12-column grid. In edit mode a widget moves by its handle and
 * resizes from its corner; others make room and the layout closes gaps. With the
 * keyboard: focus a widget, arrows move it, Shift + arrows resize it. On narrow
 * screens the widgets stack in reading order and editing is off.
 */
export function Grid({ widgets, editing, onChange, render }: {
  widgets: Widget[]; editing: boolean; onChange: (w: Widget[]) => void;
  render: (w: Widget, size: { width: number; height: number }) => ReactNode;
}) {
  const [ref, width] = useSize<HTMLDivElement>();
  const [drag, setDrag] = useState<Drag | null>(null);
  const stacked = width > 0 && width < 720;
  const colW = (width - GAP * (COLS - 1)) / COLS;
  const px = (b: Box) => ({ left: b.x * (colW + GAP), top: b.y * (ROW_H + GAP), width: b.w * colW + (b.w - 1) * GAP, height: b.h * ROW_H + (b.h - 1) * GAP });
  const live = useRef(widgets);
  live.current = widgets;

  const layout = drag ? (drag.mode === "move" ? arrange(widgets, drag.preview, drag.start) : settle(widgets, drag.preview)) : widgets;

  const commit = (moved: Box, origin?: Box) => {
    onChange(origin ? arrange(live.current, moved, origin) : settle(settle(live.current, moved)));
  };

  const onPointerDown = (e: React.PointerEvent, w: Widget, mode: Drag["mode"]) => {
    if (!editing || e.button !== 0) return;
    e.preventDefault();
    (e.currentTarget as Element).setPointerCapture(e.pointerId);
    const b = { id: w.id, x: w.x, y: w.y, w: w.w, h: w.h };
    setDrag({ id: w.id, mode, px: e.clientX, py: e.clientY, ox: 0, oy: 0, start: b, preview: b });
  };
  const onPointerMove = (e: React.PointerEvent) => {
    if (!drag) return;
    const dx = Math.round((e.clientX - drag.px) / (colW + GAP));
    const dy = Math.round((e.clientY - drag.py) / (ROW_H + GAP));
    const w = widgets.find((x) => x.id === drag.id)!;
    const [mw, mh] = MIN[w.type] ?? [1, 1];
    const s = drag.start;
    const next = drag.mode === "move" ? { ...s, x: s.x + dx, y: s.y + dy } : { ...s, w: s.w + dx, h: s.h + dy };
    setDrag({ ...drag, ox: e.clientX - drag.px, oy: e.clientY - drag.py, preview: clampBox(next, COLS, mw, mh) });
  };
  const onPointerUp = () => {
    if (drag) commit(drag.preview, drag.mode === "move" ? drag.start : undefined);
    setDrag(null);
  };

  const onKey = (e: React.KeyboardEvent, w: Widget) => {
    if (!editing) return;
    const step: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };
    const d = step[e.key];
    if (!d) return;
    e.preventDefault();
    const [mw, mh] = MIN[w.type] ?? [1, 1];
    const b = e.shiftKey ? { ...w, w: w.w + d[0], h: w.h + d[1] } : { ...w, x: w.x + d[0], y: w.y + d[1] };
    const origin = { id: w.id, x: w.x, y: w.y, w: w.w, h: w.h };
    commit(clampBox({ id: w.id, x: b.x, y: b.y, w: b.w, h: b.h }, COLS, mw, mh), e.shiftKey ? undefined : origin);
  };

  if (stacked) {
    const ordered = [...widgets].sort((a, b) => a.y - b.y || a.x - b.x);
    return (
      <div ref={ref} className="flex flex-col gap-3">
        {ordered.map((w) => {
          const h = Math.max(2, w.h) * ROW_H * 0.85;
          return <div key={w.id} style={{ height: w.type === "kpi" ? 130 : h }}>{render(w, { width, height: h })}</div>;
        })}
      </div>
    );
  }

  const height = Math.max(1, rows(layout) + (editing ? 3 : 0)) * (ROW_H + GAP);
  return (
    <div ref={ref} className={clsx("relative", editing && "dash-grid-editing")} style={{ height, ["--col" as string]: `${colW + GAP}px`, ["--row" as string]: `${ROW_H + GAP}px` }}
      onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={() => setDrag(null)}>
      {width > 0 && drag && <div className="pointer-events-none absolute rounded-2xl border-2 border-dashed border-accent/60 bg-accent-soft/60 transition-all duration-100" style={px(drag.preview)} />}
      {width > 0 && layout.map((b) => {
        const w = widgets.find((x) => x.id === b.id)!;
        const isDragged = drag?.id === b.id;
        // The dragged widget follows the pointer; the rest glide to their place.
        const pos = isDragged && drag.mode === "move" ? px(drag.start) : isDragged ? px(drag.preview) : px(b);
        const body = { width: pos.width, height: pos.height };
        return (
          <div key={b.id} tabIndex={editing ? 0 : -1} onKeyDown={(e) => onKey(e, w)} aria-label={editing ? `${w.title || WIDGETS[w.type].name}: arrows move, Shift+arrows resize` : undefined}
            className={clsx("absolute outline-none", !isDragged && "transition-[left,top,width,height] duration-200 ease-out", isDragged && "z-20 opacity-90",
              editing && "rounded-2xl focus-visible:ring-2 focus-visible:ring-accent")}
            style={isDragged && drag.mode === "move" ? { ...pos, transform: `translate(${drag.ox}px, ${drag.oy}px)` } : pos}>
            {render(w, body)}
            {editing && (
              <>
                <button type="button" aria-label="Drag to move" className="absolute left-1/2 top-1 z-10 -translate-x-1/2 cursor-grab touch-none rounded-md bg-surface-3/90 px-1.5 py-0.5 text-muted active:cursor-grabbing"
                  onPointerDown={(e) => onPointerDown(e, w, "move")}><GripVertical size={13} className="rotate-90" /></button>
                <button type="button" aria-label="Drag to resize" className="absolute bottom-1 right-1 z-10 cursor-nwse-resize touch-none rounded-md bg-surface-3/90 p-0.5 text-muted"
                  onPointerDown={(e) => onPointerDown(e, w, "resize")}><MoveDiagonal2 size={13} /></button>
              </>
            )}
          </div>
        );
      })}
    </div>
  );
}
