// Layout of dashboard widgets on a 12-column grid. Pure functions, so the drag
// and resize behaviour can be tested without a browser.

export interface Box { id: string; x: number; y: number; w: number; h: number }

export const collides = (a: Box, b: Box) =>
  a.id !== b.id && a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;

/**
 * Places `moved` exactly where it is and lets every other box fall up as far as
 * it can without overlapping (gravity), pushing down whatever `moved` now
 * covers. With no `moved`, simply compacts the layout.
 */
export function settle<T extends Box>(boxes: T[], moved?: Box): T[] {
  const fixed = moved ? boxes.find((b) => b.id === moved.id) : undefined;
  const placed: Box[] = fixed ? [{ ...fixed, ...moved! }] : [];
  const others = boxes.filter((b) => b.id !== moved?.id).sort((a, b) => a.y - b.y || a.x - b.x);
  const out = new Map<string, T>();
  if (fixed) out.set(fixed.id, { ...fixed, ...moved! } as T);
  for (const b of others) {
    const c = { ...b, y: 0 };
    while (placed.some((p) => collides(c, p))) {
      // jump below the lowest box it collides with
      c.y = Math.max(...placed.filter((p) => collides(c, p)).map((p) => p.y + p.h));
    }
    placed.push(c);
    out.set(c.id, c);
  }
  return boxes.map((b) => out.get(b.id)!);
}

/**
 * Moving a box onto one of the same size swaps the two (the natural result of
 * rearranging tiles); anything else settles by gravity.
 */
export function arrange<T extends Box>(boxes: T[], moved: Box, origin: Box): T[] {
  const hit = boxes.filter((b) => collides(moved, b));
  if (hit.length === 1 && hit[0].w === moved.w && hit[0].h === moved.h && (moved.x !== origin.x || moved.y !== origin.y)) {
    const other = hit[0];
    const swapped = boxes.map((b) => (b.id === moved.id ? { ...b, x: moved.x, y: moved.y, w: moved.w, h: moved.h }
      : b.id === other.id ? { ...b, x: origin.x, y: origin.y } : b));
    if (!swapped.some((a) => swapped.some((c) => collides(a, c)))) return settle(swapped);
  }
  return settle(settle(boxes, moved));
}

/** Clamps a box into the grid. */
export function clampBox<T extends Box>(b: T, cols: number, minW = 1, minH = 1, maxH = 24): T {
  const w = Math.max(minW, Math.min(cols, b.w));
  const h = Math.max(minH, Math.min(maxH, b.h));
  return { ...b, w, h, x: Math.max(0, Math.min(cols - w, b.x)), y: Math.max(0, b.y) };
}

/** The first free spot for a new box of size w×h: top-left, scanning rows. */
export function freeSpot(boxes: Box[], w: number, h: number, cols: number): { x: number; y: number } {
  for (let y = 0; ; y++) {
    for (let x = 0; x + w <= cols; x++) {
      const c = { id: "\u0000", x, y, w, h };
      if (!boxes.some((b) => collides(c, b))) return { x, y };
    }
  }
}

/** Grid height in rows. */
export const rows = (boxes: Box[]) => boxes.reduce((m, b) => Math.max(m, b.y + b.h), 0);
