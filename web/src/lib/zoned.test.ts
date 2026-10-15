import { describe, expect, it } from "vitest";
import { addDays, addMonths, startOfWeek, wall, weekday, zoned } from "./zoned";

describe("zoned", () => {
  it("finds local midnight across DST changes", () => {
    // Amsterdam: CEST (+2) until 25 Oct 2026, then CET (+1).
    expect(zoned({ y: 2026, m: 10, d: 25 }, 0, 0, "Europe/Amsterdam").toISOString()).toBe("2026-10-24T22:00:00.000Z");
    expect(zoned({ y: 2026, m: 10, d: 26 }, 0, 0, "Europe/Amsterdam").toISOString()).toBe("2026-10-25T23:00:00.000Z");
    expect(zoned({ y: 2026, m: 3, d: 1 }, 9, 30, "America/Sao_Paulo").toISOString()).toBe("2026-03-01T12:30:00.000Z");
  });
  it("reads wall-clock parts in a zone", () => {
    expect(wall(Date.UTC(2026, 0, 1, 23, 30), "Asia/Tokyo")).toMatchObject({ y: 2026, m: 1, d: 2, h: 8, min: 30 });
  });
  it("does civil date arithmetic", () => {
    expect(addDays({ y: 2026, m: 12, d: 31 }, 1)).toEqual({ y: 2027, m: 1, d: 1 });
    expect(addMonths({ y: 2026, m: 1, d: 31 }, 1)).toEqual({ y: 2026, m: 2, d: 28 });
    expect(weekday({ y: 2026, m: 10, d: 16 })).toBe(4); // Friday
    expect(startOfWeek({ y: 2026, m: 10, d: 18 })).toEqual({ y: 2026, m: 10, d: 12 });
  });
});
