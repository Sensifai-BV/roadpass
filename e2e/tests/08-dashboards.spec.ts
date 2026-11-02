import { test, expect } from "@playwright/test";
import { signIn, withDuty } from "./helpers";

const H = { "X-Requested-With": "roadpass" };

test("each member customises their own Overview; admins share a layout with a role", async ({ page, browser }) => {
  await signIn(page, "org_nuevafilm");
  await page.request.delete("/api/v1/me/layout", { headers: H });
  const start = await (await page.request.get("/api/v1/me/layout")).json();
  const version = start.source === "personal" ? start.version : 0;
  const widgets = [
    { id: "k1", type: "kpi", x: 0, y: 0, w: 3, h: 2, title: "Received", query: { source: "records", types: ["shipment"], measure: "mass_kg", agg: "sum", compare: true } },
    { id: "j1", type: "journey", x: 0, y: 2, w: 12, h: 3, title: "Our journey", options: { journeyStyle: "list", journeyMeasure: "mass", animate: false } },
    { id: "b1", type: "bars", x: 3, y: 0, w: 5, h: 4, title: "Top senders", query: { source: "records", types: ["shipment"], measure: "mass_kg", agg: "sum", groupBy: "org_id", limit: 5 }, options: { orient: "vertical" } },
    { id: "d1", type: "donut", x: 8, y: 0, w: 4, h: 4, title: "By type", query: { source: "records", measure: "mass_kg", agg: "sum", groupBy: "type" }, options: { donutStyle: "pie" } },
    { id: "a1", type: "activity", x: 0, y: 5, w: 6, h: 4, title: "Latest" },
  ];
  const saved = await page.request.put("/api/v1/me/layout", { headers: H, data: { widgets, version } });
  expect(saved.status()).toBe(200);
  const mine = await saved.json();
  expect(mine.source).toBe("personal");

  // The Overview renders the saved layout, built-ins included, without errors.
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/");
  for (const t of ["Received", "Our journey", "Top senders", "By type", "Latest"]) await expect(page.getByRole("region", { name: t })).toBeVisible();
  await expect(page.getByRole("region", { name: "Our journey" }).getByText("Recycle")).toBeVisible();
  await expect(page.getByRole("link", { name: "Dashboards" })).toHaveCount(0);
  expect(errors).toEqual([]);

  // A save from a stale window is refused rather than overwriting.
  expect((await page.request.put("/api/v1/me/layout", { headers: H, data: { widgets, version: mine.version - 1 } })).status()).toBe(409);
  // A query outside the catalogue is rejected, not stored.
  const bad = { ...widgets[0], query: { source: "records", measure: "mass_kg; DROP TABLE records", agg: "sum" } };
  expect((await page.request.put("/api/v1/me/layout", { headers: H, data: { widgets: [bad], version: mine.version } })).status()).toBe(422);

  // The admin shares a layout with intake; intake starts from it, production does not.
  const shared = await page.request.post("/api/v1/me/layout/publish", { headers: H, data: { name: `Intake E2E ${Date.now()}`, audience: ["duty:receive"], widgets: widgets.slice(0, 2) } });
  expect(shared.ok()).toBe(true);
  const intake = await browser.newPage();
  await signIn(intake, "org_nuevafilm", withDuty("receive"));
  await intake.request.delete("/api/v1/me/layout", { headers: H });
  expect((await (await intake.request.get("/api/v1/me/layout")).json()).source).toBe("team");
  const prod = await browser.newPage();
  await signIn(prod, "org_nuevafilm", withDuty("produce"));
  await prod.request.delete("/api/v1/me/layout", { headers: H });
  expect((await (await prod.request.get("/api/v1/me/layout")).json()).source).toBe("default");
  expect((await prod.request.post("/api/v1/me/layout/publish", { headers: H, data: { audience: ["all"], widgets } })).status()).toBe(403);

  await page.request.delete("/api/v1/me/layout", { headers: H });
});
