import { test, expect } from "@playwright/test";
import { signIn, now } from "./helpers";

const H = { "X-Requested-With": "roadpass" };

// The platform manager creates Circuloos (an asphalt plant that also lays
// roads); its admin joins, is guided through setup, and assigns people to the
// sections they work in.
test("a new organisation's admin is guided through setup and assigns people to sections", async ({ page, browser }) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  await signIn(page, "org_sensifai");
  const created = await page.request.post("/api/v1/platform/orgs", { headers: H, data: {
    name: `Circuloos E2E ${stamp}`, kind: "asphalt_plant", kinds: ["contractor"], taxId: "", country: "NL", region: "Noord-Holland", city: "Amsterdam",
    website: "", planId: "business", trial: true, verified: false, adminEmail: `admin+${stamp}@circuloos.example`,
  } });
  expect(created.status()).toBe(201);
  const { link, org } = await created.json();

  // The admin joins and lands in the guide.
  const ctx = await browser.newContext();
  const admin = await ctx.newPage();
  await admin.goto(new URL(link).pathname);
  await admin.getByLabel("Your name").fill("Ana Admin");
  await admin.getByLabel("Choose a password").fill("correct-horse-battery-staple");
  await admin.getByRole("button", { name: "Join" }).click();
  // A brand-new organisation's admin lands in the guide on their first visit.
  await expect(admin.getByRole("heading", { level: 1, name: `Set up Circuloos E2E ${stamp}` })).toBeVisible();
  await expect(admin).toHaveURL(/\/get-started/);
  const setup = await (await admin.request.get("/api/v1/org/setup")).json();
  expect(setup.steps.find((s: { key: string }) => s.key === "profile").done).toBe(true); // country and city came from the platform
  expect(setup.steps.find((s: { key: string }) => s.key === "sites").done).toBe(false);

  // Sites: placed by pasting coordinates.
  const steps = admin.getByRole("navigation", { name: "Setup steps" });
  await steps.getByRole("button", { name: /Sites/ }).click();
  await admin.getByLabel("Name").fill("Asphalt plant Westpoort");
  // The type defaults to the organisation's main activity; its other activities come next.
  await expect(admin.getByLabel("Site type")).toHaveValue("asphalt_plant");
  await admin.getByLabel("Site type").selectOption("contractor");
  await admin.getByLabel("Coordinates (latitude, longitude)").fill("52.4105, 4.8231");
  await admin.getByRole("button", { name: "Add site" }).click();
  await expect(admin.getByText("Asphalt plant Westpoort")).toBeVisible();
  await expect(admin.getByText(/^Road contractor · /)).toBeVisible();

  // Team: roles of both activities, and a section per person.
  await steps.getByRole("button", { name: /Team and roles/ }).click();
  await expect(admin.getByText("Mixing operator").first()).toBeVisible();
  await expect(admin.getByText("Site manager").first()).toBeVisible();
  const mixer = await admin.request.post("/api/v1/org/invitations", { headers: H, data: { email: `mixer+${stamp}@circuloos.example`, role: "editor", duties: ["produce"], steps: ["asphalt_plant"] } });
  expect(mixer.status()).toBe(201);
  expect((await admin.request.post("/api/v1/org/invitations", { headers: H, data: { email: `x+${stamp}@circuloos.example`, role: "editor", duties: ["produce"], steps: ["recycler"] } })).status()).toBe(422);

  // The mixer joins: may sign asphalt lots, not road sections (another section).
  const mctx = await browser.newContext();
  const m = await mctx.newPage();
  await m.goto(new URL((await mixer.json()).link).pathname);
  await m.getByLabel("Your name").fill("Max Mixer");
  await m.getByLabel("Choose a password").fill("correct-horse-battery-staple");
  await m.getByRole("button", { name: "Join" }).click();
  await expect(m.getByText("Awaiting verification.")).toBeVisible();
  const sites = (await (await m.request.get("/api/v1/org")).json()).sites;
  const road = await m.request.post("/api/v1/records", { headers: H, data: { records: [{ type: "road_section", ref: `RS-${stamp}`, occurredAt: now(), massKg: 1000,
    data: { road: "A10", km_from: 1, km_to: 2, layer: "surface", thickness_cm: 4, area_m2: 7000, works: "resurfacing", mix: "AC 11", owner_org_id: org.id, lat: 52.4, lon: 4.8 } }] } });
  expect(road.status()).toBe(422);
  expect(JSON.stringify(await road.json())).toContain("another section");
  const lot = await m.request.post("/api/v1/records", { headers: H, data: { records: [{ type: "asphalt_lot", ref: `AL-${stamp}`, siteId: sites[0].id, occurredAt: now(), massKg: 30000,
    data: { mix: "AC 16 surf", dosage_pct: 0.5, recycled_kg: 150, temp_c: 165 } }] } });
  expect([201, 422]).toContain(lot.status()); // the section check passes; field checks may still ask for more
  if (lot.status() === 422) expect(JSON.stringify(await lot.json())).not.toContain("another section");

  // The guide's progress follows what was really done; it can be put away.
  const after = await (await admin.request.get("/api/v1/org/setup")).json();
  expect(after.steps.find((s: { key: string }) => s.key === "sites").done).toBe(true);
  expect(after.steps.find((s: { key: string }) => s.key === "team").done).toBe(true);
  expect((await admin.request.put("/api/v1/org/setup", { headers: H, data: { dismissed: true } })).status()).toBe(204);
  await mctx.close();
  await ctx.close();
});
