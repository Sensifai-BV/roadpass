import { test, expect, request as pwRequest, type APIRequestContext, type Page } from "@playwright/test";

// A production-style walkthrough on an EMPTY installation (DEMO_MODE=0) with ONE real organisation taken
// from docs/research/REAL_ROAD_PUBLIC_REFERENCE_DATA.json: Capital Paving (Columbia, Missouri), the contractor of
// the MoDOT Route 740 / Stadium Boulevard recycled-polyethylene trial (final report cmr 23-009, 2021).
//
// Published facts used as they are: the three test sections 25PE / 50PE / 50PEL, their final-layout asphalt
// quantities (1,370.8 / 835.9 / 1,370.8 US short tons), dosage (0.25 / 0.5 / 0.5 % of mix), 4.7 % AC, the
// predominantly-LLDPE post-consumer pellet supplier (Avangard) and the road. The public sources give NO
// sorting lots, recycling runs, additive batches, truck tickets or signed acceptance, and the platform's
// mandatory fields need them. Everything that is not published (lot ids, loads, dates, thickness, mixing
// temperature, pellet recycled content) is a placeholder, so every record is labelled Simulated and says so
// in its note. Nothing here is a statement by Capital Paving, Avangard or MoDOT.
//
// Needs a stack with demo mode off. Each run needs its own RUN (org and user e-mails derive from it).
test.describe.configure({ mode: "serial" });

const gaps: string[] = [];
const gap = (ok: boolean, what: string) => { if (!ok) gaps.push(what); };

const H = { "X-Requested-With": "roadpass" };
const PW = "correct-horse-battery-staple";
const BASE = process.env.BASE_URL ?? "http://localhost:8080";
const RUN = process.env.RUN ?? String(Date.now()).slice(-6);
const PM_EMAIL = process.env.PM_EMAIL ?? "manager@platform.local";
const PM_PASSWORD = process.env.PM_PASSWORD ?? PW;
const ORG_NAME = `Capital Paving (Route 740 reference) ${RUN}`;
const DOMAIN = "capitalpaving.example";
const SHORT_TON = 907.18474; // kg; the report's "tons" are US short tons
const PROV = "Simulated";

type Actor = { api: APIRequestContext; email: string; name: string };
const A: Record<string, Actor> = {};

async function login(email: string, password = PW): Promise<APIRequestContext> {
  const api = await pwRequest.newContext({ baseURL: BASE, extraHTTPHeaders: H });
  const r = await api.post("/api/v1/auth/login", { data: { email, password } });
  expect(r.ok(), `login ${email}: ${await r.text()}`).toBeTruthy();
  return api;
}
async function join(link: string, name: string): Promise<APIRequestContext> {
  const api = await pwRequest.newContext({ baseURL: BASE, extraHTTPHeaders: H });
  const r = await api.post("/api/v1/auth/invitations/accept", { data: { token: link.split("/join/")[1], name, password: PW } });
  expect(r.ok(), `join ${name}: ${await r.text()}`).toBeTruthy();
  return api;
}

const T0 = Date.now() - 20 * 86400_000;
const at = (hours: number) => new Date(T0 + hours * 3600_000).toISOString().replace(/\.\d+Z$/, "Z");

interface Rec { id?: string; type: string; ref: string; siteId?: string; occurredAt: string; massKg: number; inputs?: { id: string; massKg: number }[]; data: Record<string, unknown>; provenance?: string; note?: string }
interface Res { id: string; ref: string; status: string; findings?: { code: string; message: string; severity?: string }[] }

async function submit(who: Actor, rows: Rec[], opts: { expect?: "clean" | "any" } = {}): Promise<Res[]> {
  const out: Res[] = [];
  for (let i = 0; i < rows.length; i += 200) {
    const chunk = rows.slice(i, i + 200).map((r) => ({ provenance: PROV, ...r }));
    const r = await who.api.post("/api/v1/records", { data: { records: chunk } });
    const body = await r.json();
    expect(r.status(), `${who.name} submit: ${JSON.stringify(body).slice(0, 800)}`).toBeLessThan(300);
    const results: Res[] = body.results ?? body.records ?? [];
    expect(results.length, JSON.stringify(body).slice(0, 600)).toBe(chunk.length);
    out.push(...results);
  }
  if ((opts.expect ?? "clean") === "clean") {
    for (const x of out) expect(["ok", "flagged"], `${x.ref}: ${JSON.stringify(x.findings)}`).toContain(x.status);
  }
  return out;
}
const get = async (who: Actor, path: string) => (await who.api.get(path)).json();
const qIds = (w: { queues: Record<string, { items: { record: { id: string } }[] }> }, q: string) => w.queues[q].items.map((i) => i.record.id);

async function uiLogin(page: Page, email: string) {
  await page.goto("/login");
  await page.getByLabel("Work e-mail").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(PW);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

const own: Record<string, string> = {};
const ids: Record<string, string> = {};
let org: { id: string };

// ---------------------------------------------------------------- 1. the organisation and its administrator
test("the platform manager creates the one real organisation", async () => {
  const pm = await login(PM_EMAIL, PM_PASSWORD);
  const created = await pm.post("/api/v1/platform/orgs", { data: {
    name: ORG_NAME, kind: "asphalt_plant", kinds: ["sorting_plant", "recycler", "additive_producer", "contractor", "road_owner"],
    taxId: "", country: "US", region: "Missouri", city: "Columbia", website: "",
    planId: "business", trial: true, verified: false, adminEmail: `admin.${RUN}@${DOMAIN}`,
  } });
  expect(created.status(), await created.text()).toBe(201);
  const c = await created.json();
  org = c.org;
  expect(c.org.status).toBe("pending");
  A.pm = { api: pm, email: PM_EMAIL, name: "Platform Manager" };
  A.admin = { api: await join(c.link, "Administrator"), email: `admin.${RUN}@${DOMAIN}`, name: "Administrator" };
});

test("the administrator sets up the sites in the setup guide", async ({ page }) => {
  test.setTimeout(180_000);
  await uiLogin(page, A.admin.email);
  await page.goto("/get-started");
  await expect(page.getByRole("heading", { level: 1, name: new RegExp(`Set up ${ORG_NAME.replace(/[()]/g, "\\$&")}`) })).toBeVisible();
  await page.getByRole("navigation", { name: "Setup steps" }).getByRole("button", { name: /Sites/ }).click();
  // Coordinates are approximate town centres around Columbia, Missouri (placeholders, not surveyed plants).
  const sites: [string, string, string][] = [
    ["Sorting yard Columbia", "sorting_plant", "38.95, -92.33"],
    ["Pellet recycling Columbia", "recycler", "38.96, -92.30"],
    ["Additive handling Columbia", "additive_producer", "38.97, -92.28"],
    ["Asphalt plant Columbia", "asphalt_plant", "38.93, -92.25"],
    ["Paving crew base Columbia", "contractor", "38.94, -92.32"],
    ["Route 740 asset office", "road_owner", "38.9386, -92.329"],
  ];
  for (const [name, kind, coords] of sites) {
    await page.getByLabel("Name").fill(name);
    await page.getByLabel("Site type").selectOption(kind);
    await page.getByLabel("Coordinates (latitude, longitude)").fill(coords);
    await page.getByRole("button", { name: "Add site" }).click();
    await expect(page.getByText(name).first()).toBeVisible();
  }
  const o = await (await A.admin.api.get("/api/v1/org")).json();
  for (const s of o.sites) own[s.kind] = s.id;
  expect(Object.keys(own).sort()).toEqual(["additive_producer", "asphalt_plant", "contractor", "recycler", "road_owner", "sorting_plant"]);
});

const TEAM: { key: string; name: string; role: string; duties: string[]; steps: string[] }[] = [
  { key: "intake", name: "Intake Officer", role: "editor", duties: ["receive"], steps: [] },
  { key: "sorter", name: "Sorting Operator", role: "editor", duties: ["produce"], steps: ["sorting_plant"] },
  { key: "recycler", name: "Recycling Operator", role: "editor", duties: ["produce"], steps: ["recycler"] },
  { key: "compounder", name: "Additive Operator", role: "editor", duties: ["produce"], steps: ["additive_producer"] },
  { key: "mixer", name: "Plant Operator", role: "editor", duties: ["produce"], steps: ["asphalt_plant"] },
  { key: "site", name: "Site Manager", role: "editor", duties: ["produce", "test"], steps: ["contractor", "road_owner"] },
  { key: "lab", name: "Lab Technician", role: "editor", duties: ["test"], steps: [] },
  { key: "quality", name: "Quality Manager", role: "editor", duties: ["test", "release"], steps: [] },
  { key: "dispatch", name: "Dispatcher", role: "editor", duties: ["ship"], steps: [] },
  { key: "reporter", name: "Reporter", role: "viewer", duties: [], steps: [] },
];

test("the administrator builds the team: one named job role per person", async ({ page }) => {
  for (const m of TEAM) {
    const email = `${m.key}.${RUN}@${DOMAIN}`;
    const inv = await A.admin.api.post("/api/v1/org/invitations", { data: { email, role: m.role, duties: m.duties, steps: m.steps } });
    expect(inv.status(), `${m.key}: ${await inv.text()}`).toBe(201);
    A[m.key] = { api: await join((await inv.json()).link, m.name), email, name: m.name };
  }
  await uiLogin(page, A.admin.email);
  await page.goto("/settings");
  await page.getByRole("button", { name: "Team & roles" }).or(page.getByRole("link", { name: "Team & roles" })).first().click();
  for (const role of ["Intake officer", "Sorting operator", "Quality manager", "Dispatcher", "Lab technician", "Reporter"]) {
    await expect(page.getByText(role).first(), role).toBeVisible();
  }
  await page.screenshot({ path: test.info().outputPath("team.png"), fullPage: true });
});

test("the platform manager verifies the organisation", async () => {
  const r = await A.pm.api.post(`/api/v1/platform/orgs/${org.id}/status`, { data: { status: "verified" } });
  expect(r.status(), await r.text()).toBeLessThan(300);
  expect((await (await A.admin.api.get("/api/v1/org")).json()).org.status).toBe("verified");
});

// ---------------------------------------------------------------- 2. the published numbers
const kgOf = (tons: number) => Math.round(tons * SHORT_TON);
interface Section { key: "25PE" | "50PE" | "50PEL"; tons: number; dosage: number; lat: [number, number, number, number] }
const SECTIONS: Section[] = [
  { key: "25PE", tons: 1370.8, dosage: 0.25, lat: [38.9446, -92.3330, 38.9395, -92.3329] },
  { key: "50PE", tons: 835.9, dosage: 0.5, lat: [38.9395, -92.3329, 38.9362, -92.3326] },
  { key: "50PEL", tons: 1370.8, dosage: 0.5, lat: [38.9362, -92.3326, 38.9310, -92.3324] },
];
const THICK_CM = 4;       // placeholder: the lift thickness is not in the extracted facts
const WIDTH_M = 7;        // placeholder lane pair
const mass = (s: Section) => kgOf(s.tons);
const addKg = (s: Section) => Math.round((mass(s) * s.dosage) / 100);
const area = (s: Section) => Math.round(mass(s) / 2450 / (THICK_CM / 100));
const lenKm = (s: Section) => area(s) / WIDTH_M / 1000;
const NOTE = "Published: section quantity, dosage and AC content (MoDOT cmr 23-009, Route 740). Placeholders: lot/ticket ids, dates, loads, thickness, temperature, pellet recycled content.";

test("the plan from the published quantities", async () => {
  const total = SECTIONS.reduce((a, s) => a + addKg(s), 0);
  console.log("sections kg:", SECTIONS.map((s) => `${s.key} ${mass(s)} kg, additive ${addKg(s)} kg`).join(" | "), `pellets total ${total} kg`);
  expect(total).toBeLessThan(14000);
});

// ---------------------------------------------------------------- 3. material journey through the work queues
const REJECT_KG = { A: 15000, B: 14000 };

test("sorting operator records two reject lots (stage 1)", async () => {
  const res = await submit(A.sorter, (["A", "B"] as const).map((k, i) => ({
    type: "reject_lot", ref: `COL-R740-${k}`, siteId: own.sorting_plant, occurredAt: at(1 + 2 * i), massKg: REJECT_KG[k], note: NOTE,
    data: { source: "municipal_packaging", ldpe_share_pct: 78, moisture_pct: 10, contamination_pct: 10, avoided_route: "landfill", bales: Math.round(REJECT_KG[k] / 810) },
  })));
  res.forEach((r, i) => (ids[`PRL-${"AB"[i]}`] = r.id));
});

test("dispatcher ships both lots to the recycler; intake signs the receipt", async () => {
  const w = await get(A.dispatch, "/api/v1/work");
  expect(qIds(w, "ship")).toEqual(expect.arrayContaining([ids["PRL-A"], ids["PRL-B"]]));
  const [s] = await submit(A.dispatch, [{ type: "shipment", ref: "COL-SHP-1", siteId: own.sorting_plant, occurredAt: at(6), massKg: 29000, note: NOTE,
    inputs: [{ id: ids["PRL-A"], massKg: 15000 }, { id: ids["PRL-B"], massKg: 14000 }],
    data: { to_site_id: own.recycler, carrier: "Own fleet", vehicle: "placeholder", distance_km: 6 } }]);
  ids["SHP-1"] = s.id;
  const todo = qIds(await get(A.intake, "/api/v1/work"), "receive");
  expect(todo).toContain(s.id);
  const r = await A.intake.api.post(`/api/v1/shipments/${s.id}/receipt`, { data: { receivedAt: at(8), massKg: 28980, decision: "accepted" } });
  expect(r.status(), await r.text()).toBeLessThan(300);
});

test("recycling operator makes the pellets (stage 2); lab tests; quality releases", async () => {
  const process = qIds(await get(A.recycler, "/api/v1/work"), "process");
  expect(process).toEqual(expect.arrayContaining([ids["PRL-A"], ids["PRL-B"]]));
  const [r1] = await submit(A.recycler, [{ type: "recycled_lot", ref: "COL-R740-PEL1", siteId: own.recycler, occurredAt: at(12), massKg: 19000, note: NOTE,
    inputs: [{ id: ids["PRL-A"], massKg: 15000 }, { id: ids["PRL-B"], massKg: 14000 }],
    data: { form: "regranulate", process: "washing,shredding,melt filtration,pelletising", mfi_g10min: 1.2, moisture_pct: 0.2, ash_pct: 1.5, energy_kwh: 6200 } }]);
  ids["RPL-1"] = r1.id;
  await submit(A.lab, [
    { type: "lab_test", ref: "LAB-PEL1-MFI", siteId: own.recycler, occurredAt: at(14), massKg: 0, data: { subject_id: r1.id, test: "mfi", value: 1.2, result: "pass", lab: "In-house" } },
  ], { expect: "any" });
  expect(qIds(await get(A.quality, "/api/v1/work"), "release")).toContain(r1.id);
  await submit(A.quality, [{ type: "release", ref: "REL-PEL1", siteId: own.recycler, occurredAt: at(16), massKg: 0, data: { subject_id: r1.id, decision: "released", note: "MFI 1.2 within film grade" } }], { expect: "any" });
});

test("dispatcher ships the pellets to additive handling; intake signs", async () => {
  const [s] = await submit(A.dispatch, [{ type: "shipment", ref: "COL-SHP-2", siteId: own.recycler, occurredAt: at(18), massKg: 14000, note: NOTE,
    inputs: [{ id: ids["RPL-1"], massKg: 14000 }], data: { to_site_id: own.additive_producer, carrier: "Own fleet", vehicle: "placeholder", distance_km: 3 } }]);
  const r = await A.intake.api.post(`/api/v1/shipments/${s.id}/receipt`, { data: { receivedAt: at(19), massKg: 13990, decision: "accepted" } });
  expect(r.status(), await r.text()).toBeLessThan(300);
});

test("additive operator records two additive batches from the pellets (stage 3); release; ship to the asphalt plant", async () => {
  const lots = await submit(A.compounder, [
    { type: "additive_batch", ref: "COL-R740-AB1", siteId: own.additive_producer, occurredAt: at(22), massKg: 7000, note: NOTE,
      inputs: [{ id: ids["RPL-1"], massKg: 7000 }],
      data: { product: "Post-consumer LLDPE pellets (supplier: Avangard Innovative per MoDOT report)", form: "pellets", recycled_content_pct: 100, mfi_g10min: 1.2, melting_point_c: 122, energy_kwh: 0 } },
    { type: "additive_batch", ref: "COL-R740-AB2", siteId: own.additive_producer, occurredAt: at(23), massKg: 7000, note: NOTE,
      inputs: [{ id: ids["RPL-1"], massKg: 7000 }],
      data: { product: "Post-consumer LLDPE pellets (supplier: Avangard Innovative per MoDOT report)", form: "pellets", recycled_content_pct: 100, mfi_g10min: 1.2, melting_point_c: 122, energy_kwh: 0 } },
  ]);
  ids["ADB-1"] = lots[0].id; ids["ADB-2"] = lots[1].id;
  await submit(A.lab, lots.map((l, i) => ({ type: "lab_test", ref: `LAB-AB${i + 1}-DSC`, siteId: own.additive_producer, occurredAt: at(24), massKg: 0,
    data: { subject_id: l.id, test: "dsc_melting", value: 122, result: "pass", lab: "In-house" } })) as Rec[], { expect: "any" });
  await submit(A.quality, lots.map((l, i) => ({ type: "release", ref: `REL-AB${i + 1}`, siteId: own.additive_producer, occurredAt: at(25), massKg: 0,
    data: { subject_id: l.id, decision: "released", note: "Melting peak 122 °C" } })) as Rec[], { expect: "any" });
  const [s] = await submit(A.dispatch, [{ type: "shipment", ref: "COL-SHP-3", siteId: own.additive_producer, occurredAt: at(26), massKg: 14000, note: NOTE,
    inputs: [{ id: lots[0].id, massKg: 7000 }, { id: lots[1].id, massKg: 7000 }], data: { to_site_id: own.asphalt_plant, carrier: "Own fleet", vehicle: "placeholder", distance_km: 8 } }]);
  const r = await A.intake.api.post(`/api/v1/shipments/${s.id}/receipt`, { data: { receivedAt: at(28), massKg: 13995, decision: "accepted" } });
  expect(r.status(), await r.text()).toBeLessThan(300);
});

// Stage 4: one asphalt lot per published test section; additive drawn from the batches in order.
test("plant operator produces the three asphalt lots at the published dosages (stage 4)", async () => {
  const pool = [{ id: ids["ADB-1"], left: 7000 }, { id: ids["ADB-2"], left: 7000 }];
  const rows: Rec[] = SECTIONS.map((s, i) => {
    let need = addKg(s);
    const inputs: { id: string; massKg: number }[] = [];
    for (const b of pool) { const take = Math.min(b.left, need); if (take > 0) { inputs.push({ id: b.id, massKg: take }); b.left -= take; need -= take; } }
    return { type: "asphalt_lot", ref: `COL-R740-${s.key}`, siteId: own.asphalt_plant, occurredAt: at(40 + 24 * i), massKg: mass(s), inputs, note: NOTE,
      data: { mix: `Route 740 surface mix ${s.key}`, process: "hot", binder_pct: 4.7, additive_kg: addKg(s), dosage_pct_mix: s.dosage, mixing_temp_c: 150 } };
  });
  const res = await submit(A.mixer, rows);
  res.forEach((r, i) => (ids[`ASL-${SECTIONS[i].key}`] = r.id));
  expect(rows.some((r) => (r.inputs?.length ?? 0) > 1), "one lot draws on two additive batches").toBeTruthy();
});

// Stage 5–6: road sections and 25 t placeholder loads.
const secData = (s: Section, extra: Record<string, unknown> = {}) => ({
  road: "Route 740 (Stadium Blvd)", km_from: 0, km_to: +lenKm(s).toFixed(3), carriageway: "Both", layer: "surface", thickness_cm: THICK_CM, area_m2: area(s),
  works: "rehabilitation", mix: `Route 740 surface mix ${s.key}`, owner_org_id: org.id, region: "Missouri",
  lat: (s.lat[0] + s.lat[2]) / 2, lon: (s.lat[1] + s.lat[3]) / 2, lat_from: s.lat[0], lon_from: s.lat[1], lat_to: s.lat[2], lon_to: s.lat[3], ...extra,
});
const delivered: Record<string, { id: string; kg: number; hours: number }[]> = {};
const secStart = (i: number) => 120 + 48 * i;

test("site manager opens the three road sections", async () => {
  const res = await submit(A.site, SECTIONS.map((s, i) => ({
    type: "road_section", ref: `COL-R740-SEC-${s.key}`, siteId: own.contractor, occurredAt: at(secStart(i)), massKg: mass(s), data: secData(s), note: NOTE,
  })));
  SECTIONS.forEach((s, i) => (ids[`RS-${s.key}`] = res[i].id));
});

test("dispatch delivers 25 t truckloads; intake at the paver signs each", async () => {
  test.setTimeout(240_000);
  const rows: Rec[] = [];
  const meta: { sec: string; kg: number; hours: number }[] = [];
  let n = 0;
  SECTIONS.forEach((s, si) => {
    const lot = ids[`ASL-${s.key}`];
    let need = mass(s), i = 0;
    delivered[s.key] = [];
    while (need > 0) {
      const kg = Math.min(25000, need);
      need -= kg;
      const f0 = 1 - (need + kg) / mass(s), f1 = 1 - need / mass(s);
      const hours = secStart(si) + 1 + i * 0.3;
      rows.push({ type: "delivery", ref: `COL-TKT-${String(++n).padStart(4, "0")}`, siteId: own.asphalt_plant, occurredAt: at(hours), massKg: kg, inputs: [{ id: lot, massKg: kg }], note: NOTE,
        data: { ticket: `PLACEHOLDER-${n}`, section_id: ids[`RS-${s.key}`], vehicle: "placeholder", distance_km: 12, departure_temp_c: 155, arrival_temp_c: 148,
          lat_from: +(s.lat[0] + (s.lat[2] - s.lat[0]) * f0).toFixed(6), lon_from: +(s.lat[1] + (s.lat[3] - s.lat[1]) * f0).toFixed(6),
          lat_to: +(s.lat[0] + (s.lat[2] - s.lat[0]) * f1).toFixed(6), lon_to: +(s.lat[1] + (s.lat[3] - s.lat[1]) * f1).toFixed(6) } });
      meta.push({ sec: s.key, kg, hours });
      i++;
    }
  });
  const res = await submit(A.dispatch, rows, { expect: "any" });
  res.forEach((r, k) => delivered[meta[k].sec].push({ id: r.id, kg: meta[k].kg, hours: meta[k].hours }));
  const blocked = res.filter((r) => r.status === "blocked");
  gap(blocked.length === 0, `${blocked.length} deliveries blocked: ${JSON.stringify(blocked[0]?.findings)}`);
  console.log(`deliveries: ${res.length}`);
  for (const d of Object.values(delivered).flat()) {
    const r = await A.intake.api.post(`/api/v1/shipments/${d.id}/receipt`, { data: { receivedAt: at(d.hours + 0.4), massKg: d.kg, decision: "accepted" } });
    expect(r.status(), await r.text()).toBeLessThan(300);
  }
});

test("site manager completes each section with the laid mass (stage 6)", async () => {
  const res = await submit(A.site, SECTIONS.map((s, i) => ({
    id: ids[`RS-${s.key}`], type: "road_section", ref: `COL-R740-SEC-${s.key}`, siteId: own.contractor, occurredAt: at(secStart(i) + 20),
    massKg: delivered[s.key].reduce((a, d) => a + d.kg, 0), data: secData(s, { completed_at: at(secStart(i) + 19) }), note: NOTE,
  })), { expect: "any" });
  for (const r of res) gap(r.status !== "blocked", `${r.ref} blocked: ${JSON.stringify(r.findings)}`);
  res.forEach((r) => console.log(r.ref, r.status, JSON.stringify(r.findings?.map((f) => f.code))));
});

// ---------------------------------------------------------------- 4. sealing and passports
test("the worker seals everything on the ledger", async () => {
  test.setTimeout(240_000);
  const probe = [ids["PRL-A"], ids["RPL-1"], ids["ADB-1"], ids["ASL-50PEL"], ids["RS-25PE"], ids["RS-50PEL"]];
  await expect.poll(async () => {
    let done = 0;
    for (const id of probe) { const r = await get(A.admin, `/api/v1/records/${id}`); if ((r.record ?? r).sealed) done++; }
    return done;
  }, { timeout: 200_000, intervals: [3000] }).toBe(probe.length);
});

test("the road passports trace back to the reject lots and carry the published mass", async () => {
  for (const s of SECTIONS) {
    const p = await get(A.admin, `/api/v1/passports/${ids[`RS-${s.key}`]}`);
    expect(p.stages.length, `${s.key} stages`).toBeGreaterThanOrEqual(5);
    const nodes = (p.nodes ?? []).map((n: { id: string }) => n.id);
    expect(nodes, `${s.key} traces to its asphalt lot`).toContain(ids[`ASL-${s.key}`]);
    expect(nodes, `${s.key} traces to a reject lot`).toEqual(expect.arrayContaining([ids["PRL-A"]]));
    gap(Math.abs(p.root.massKg - mass(s)) < 1, `${s.key} root mass ${p.root.massKg}, expected ${mass(s)}`);
    gap(p.trust?.signature?.valid === true, `${s.key} signature not valid`);
    console.log(s.key, "stages:", p.stages.map((x: { name: string; records: number }) => `${x.name}:${x.records}`).join("|"), "carbon:", JSON.stringify(p.carbon).slice(0, 160), "traced:", p.tracedKg);
  }
});

// ---------------------------------------------------------------- 5. the dashboard as each role sees it
const PAGES = ["/", "/work", "/chain", "/status", "/calendar", "/passports", "/roads", "/quality", "/carbon", "/plants", "/ledger", "/data", "/compliance", "/alerts", "/settings", "/glossary"];

async function sweep(browser: import("@playwright/test").Browser, who: Actor, paths: string[], shots = false) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 960 } });
  await ctx.addCookies((await who.api.storageState()).cookies);
  const page = await ctx.newPage();
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(`${who.name}: page error ${e.message}`));
  page.on("response", (r) => { if (r.url().includes("/api/") && r.status() >= 500) errors.push(`${who.name}: ${r.status()} ${r.url()}`); });
  for (const path of paths) {
    await page.goto(path);
    await page.waitForLoadState("domcontentloaded");
    await page.waitForTimeout(1200);
    const body = (await page.locator("body").innerText()).slice(0, 4000);
    if (/Something went wrong|Unexpected (Application )?Error|Cannot read propert/i.test(body)) errors.push(`${who.name}: error screen on ${path}`);
    if (shots) await page.screenshot({ path: test.info().outputPath(`${who.name.split(" ")[0]}${path.replace(/\W+/g, "_")}.png`), fullPage: true });
  }
  await ctx.close();
  return errors;
}

test("the administrator opens every dashboard page without an error", async ({ browser }) => {
  test.setTimeout(240_000);
  const errors = await sweep(browser, A.admin, PAGES, true);
  gap(errors.length === 0, errors.join("; "));
});

test("each role opens its own pages", async ({ browser }) => {
  test.setTimeout(300_000);
  const errors: string[] = [];
  for (const k of ["sorter", "recycler", "compounder", "mixer", "site", "lab", "quality", "dispatch", "intake", "reporter"]) {
    errors.push(...await sweep(browser, A[k], ["/", "/work", "/passports", "/roads"]));
  }
  gap(errors.length === 0, errors.join("; "));
});

test("passport pages show the lineage, tests and trust for every step", async ({ browser }) => {
  test.setTimeout(240_000);
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 960 } });
  await ctx.addCookies((await A.admin.api.storageState()).cookies);
  const page = await ctx.newPage();
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  for (const k of ["PRL-A", "RPL-1", "ADB-1", "ASL-50PE", "RS-25PE", "RS-50PEL"]) {
    await page.goto(`/passports/${ids[k]}`);
    await expect(page.getByText(ids[k]).first(), k).toBeVisible();
    await page.waitForTimeout(1500);
    await page.screenshot({ path: test.info().outputPath(`passport-${k}.png`), fullPage: true });
  }
  await ctx.close();
  gap(errors.length === 0, `passport page errors: ${errors.join("; ")}`);
});

test("roads page lists the three sections with their materials", async ({ page }) => {
  await uiLogin(page, A.admin.email);
  await page.goto("/roads");
  await page.waitForTimeout(2000);
  const body = await page.locator("body").innerText();
  gap(/Route 740/.test(body), "the roads page does not mention Route 740");
  await page.screenshot({ path: test.info().outputPath("roads.png"), fullPage: true });
});

test("walkthrough summary: product gaps found", async () => {
  console.log("GAPS:\n" + (gaps.length ? gaps.map((g) => " - " + g).join("\n") : " none"));
  expect(gaps, gaps.join("\n")).toEqual([]);
});
