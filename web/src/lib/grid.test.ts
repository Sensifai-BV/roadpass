import { describe, expect, it } from "vitest";
import { arrange, collides, freeSpot, rows, settle, type Box } from "./grid";

const b = (id: string, x: number, y: number, w: number, h: number): Box => ({ id, x, y, w, h });
const overlaps = (bs: Box[]) => bs.some((a) => bs.some((c) => collides(a, c)));

describe("settle", () => {
  it("compacts gaps upwards", () => {
    const out = settle([b("a", 0, 5, 6, 2), b("b", 6, 9, 6, 3)]);
    expect(out.map((x) => x.y)).toEqual([0, 0]);
  });
  it("pushes down what a moved box now covers, and never overlaps", () => {
    const start = [b("a", 0, 0, 6, 2), b("b", 6, 0, 6, 2), b("c", 0, 2, 12, 3)];
    const out = settle(start, b("c", 0, 0, 12, 3));
    expect(out.find((x) => x.id === "c")).toMatchObject({ x: 0, y: 0 });
    expect(out.find((x) => x.id === "a")!.y).toBe(3);
    expect(out.find((x) => x.id === "b")!.y).toBe(3);
    expect(overlaps(out)).toBe(false);
  });
  it("keeps every box when shuffled hard", () => {
    let bs = Array.from({ length: 30 }, (_, i) => b(`w${i}`, (i * 5) % 10, i, 2 + (i % 3), 1 + (i % 4)));
    for (let k = 0; k < 50; k++) {
      const m = bs[(k * 7) % bs.length];
      bs = settle(bs, { ...m, x: (k * 3) % 9, y: (k * 5) % 12 });
      expect(bs).toHaveLength(30);
      expect(overlaps(bs)).toBe(false);
    }
    expect(rows(settle(bs))).toBeLessThanOrEqual(rows(bs));
  });
});

describe("freeSpot", () => {
  it("finds the first hole that fits", () => {
    expect(freeSpot([b("a", 0, 0, 6, 2)], 6, 2, 12)).toEqual({ x: 6, y: 0 });
    expect(freeSpot([b("a", 0, 0, 12, 2)], 4, 2, 12)).toEqual({ x: 0, y: 2 });
  });
});

describe("arrange", () => {
  it("swaps two tiles of the same size", () => {
    const row = [b("a", 0, 0, 3, 2), b("b", 3, 0, 3, 2), b("c", 6, 0, 3, 2), b("d", 9, 0, 3, 2)];
    const out = arrange(row, { ...row[3], x: 0 }, row[3]);
    expect(out.find((x) => x.id === "d")).toMatchObject({ x: 0, y: 0 });
    expect(out.find((x) => x.id === "a")).toMatchObject({ x: 9, y: 0 });
    expect(rows(out)).toBe(2);
  });
  it("falls back to gravity for different sizes", () => {
    const out = arrange([b("a", 0, 0, 6, 2), b("big", 6, 0, 6, 4)], { id: "big", x: 0, y: 0, w: 6, h: 4 }, b("big", 6, 0, 6, 4));
    expect(out.some((x) => out.some((y) => collides(x, y)))).toBe(false);
  });
});
