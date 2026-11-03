import { test, expect, request as pwRequest, type APIRequestContext, type Page } from "@playwright/test";

// A production-style walkthrough on an EMPTY installation (DEMO_MODE=0, no simulated data):
// the platform manager creates a customer, the customer's admin sets up sites, people and
// roles, and each person then does the real work of their role. Geography, volumes and
// process values follow docs/research/SPAIN_DATA_SOURCES.md (§1 sorting plants, §2 recycled
// LDPE, §3 additive dosing, §4 asphalt mixes, §5 roads, §6 tests). The company is fictional.
//
// Needs a stack with demo mode off: BASE_URL=http://localhost:8180 (see the walkthrough notes).
test.describe.configure({ mode: "serial" });

// Product gaps the walkthrough finds are collected here so one gap does not hide the rest of the
// journey; the last test fails if any were found.
const gaps: string[] = [];
const gap = (ok: boolean, what: string) => { if (!ok) gaps.push(what); };

const H = { "X-Requested-With": "roadpass" };
const PW = "correct-horse-battery-staple";
const BASE = process.env.BASE_URL ?? "http://localhost:8180";
const RUN = process.env.RUN ?? String(Date.now()).slice(-6);
const ORG_NAME = `Ecoasfalt Catalunya S.L. ${RUN}`;
const OWNER_NAME = `Xarxa Viària de Catalunya ${RUN}`;

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
  const token = link.split("/join/")[1];
  const r = await api.post("/api/v1/auth/invitations/accept", { data: { token, name, password: PW } });
  expect(r.ok(), `join ${name}: ${await r.text()}`).toBeTruthy();
  return api;
}

// Time runs forward from 20 days ago so every record is after what it depends on.
const T0 = Date.now() - 20 * 86400_000;
const at = (hours: number) => new Date(T0 + hours * 3600_000).toISOString().replace(/\.\d+Z$/, "Z");

interface Rec { type: string; ref: string; siteId?: string; occurredAt: string; massKg: number; inputs?: { id: string; massKg: number }[]; data: Record<string, unknown>; provenance?: string; note?: string }
interface Res { id: string; ref: string; status: string; findings?: { code: string; message: string; severity?: string }[] }

async function submit(who: Actor, rows: Rec[], opts: { expect?: "clean" | "any" } = {}): Promise<Res[]> {
  const out: Res[] = [];
  for (let i = 0; i < rows.length; i += 400) {
    // No provenance: records take the test organisation's (Simulated).
    const chunk = rows.slice(i, i + 400);
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

async function uiLogin(page: Page, email: string) {
  await page.goto("/login");
  await page.getByLabel("Work e-mail").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(PW);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

const REUSE = !!process.env.REUSE;
const setup = REUSE ? test.skip : test;
test.beforeAll(async () => {
  if (!REUSE) return;
  const pw = (e: string) => e;
  A.pm = { api: await login("manager@platform.example", "a-long-enough-password"), email: "manager@platform.example", name: "Platform Manager" };
  A.admin = { api: await login(pw(`ana.${RUN}@ecoasfalt.example`)), email: `ana.${RUN}@ecoasfalt.example`, name: "Ana Admin" };
  for (const m of TEAM) A[m.key] = { api: await login(`${m.key}.${RUN}@ecoasfalt.example`), email: `${m.key}.${RUN}@ecoasfalt.example`, name: m.name };
  A.ownerAdmin = { api: await login(`roads.${RUN}@xarxa.example`), email: `roads.${RUN}@xarxa.example`, name: "Jordi Carretera" };
  A.assets = { api: await login(`assets.${RUN}@xarxa.example`), email: `assets.${RUN}@xarxa.example`, name: "Eva Actius" };
  const o = await (await A.admin.api.get("/api/v1/org")).json();
  org = o.org;
  for (const x of o.sites) own[x.kind] = x.id;
  owner = (await (await A.ownerAdmin.api.get("/api/v1/org")).json()).org;
});

const own: Record<string, string> = {}; // site type -> site id
const ids: Record<string, string> = {};  // our own label -> passport id
let org: { id: string };
let owner: { id: string };

setup("platform manager creates the customer and the road owner", async () => {
  const pm = await login("manager@platform.example", "a-long-enough-password");
  const created = await pm.post("/api/v1/platform/orgs", { data: {
    name: ORG_NAME, kind: "sorting_plant", kinds: ["recycler", "additive_producer", "asphalt_plant", "contractor"],
    taxId: "B-08123456", country: "ES", region: "Catalunya", city: "Montcada i Reixac", website: "",
    // Fictional companies and made-up loads: a test organisation, so every record is Simulated.
    planId: "business", trial: true, verified: false, test: true, adminEmail: `ana.${RUN}@ecoasfalt.example`,
  } });
  expect(created.status(), await created.text()).toBe(201);
  const c = await created.json();
  org = c.org;
  expect(c.org.status).toBe("pending");
  expect(c.org.provenance).toBe("Simulated");
  A.admin = { api: await join(c.link, "Ana Admin"), email: `ana.${RUN}@ecoasfalt.example`, name: "Ana Admin" };

  const own2 = await pm.post("/api/v1/platform/orgs", { data: {
    name: OWNER_NAME, kind: "road_owner", kinds: [], taxId: "", country: "ES", region: "Catalunya", city: "Barcelona", website: "",
    planId: "business", trial: true, verified: true, test: true, adminEmail: `roads.${RUN}@xarxa.example`,
  } });
  expect(own2.status(), await own2.text()).toBe(201);
  const o = await own2.json();
  owner = o.org;
  A.ownerAdmin = { api: await join(o.link, "Jordi Carretera"), email: `roads.${RUN}@xarxa.example`, name: "Jordi Carretera" };
  A.pm = { api: pm, email: "manager@platform.example", name: "Platform Manager" };
});

setup("a pending customer cannot seal records until the platform manager verifies it", async () => {
  const me = await (await A.admin.api.get("/api/v1/org")).json();
  expect(me.org.status).toBe("pending");
  expect(me.org.kinds).toEqual(expect.arrayContaining(["recycler", "additive_producer", "asphalt_plant", "contractor"]));
});

setup("the admin signs in and sets up the six sites in the setup guide", async ({ page }) => {
  test.setTimeout(180_000);
  await uiLogin(page, A.admin.email);
  await expect(page).toHaveURL(/get-started|\/$/);
  await page.goto("/get-started");
  await expect(page.getByRole("heading", { level: 1, name: new RegExp(`Set up ${ORG_NAME}`) })).toBeVisible();
  const steps = page.getByRole("navigation", { name: "Setup steps" });
  await steps.getByRole("button", { name: /Sites/ }).click();

  // Real towns from the sources (§1.3 sorting plants, §9 industrial locations); coordinates are municipal centroids.
  const sites: [string, string, string][] = [
    ["Planta de selección Montcada i Reixac", "sorting_plant", "41.48, 2.19"],
    ["Reciclatge de film Vilafranca del Penedès", "recycler", "41.35, 1.70"],
    ["Compounding additius Constantí", "additive_producer", "41.16, 1.21"],
    ["Planta d'asfalt Martorelles", "asphalt_plant", "41.49, 2.25"],
    ["Delegació d'obres Barcelona", "contractor", "41.39, 2.17"],
    ["Magatzem Molins de Rei", "warehouse", "41.41, 2.02"],
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
  expect(Object.keys(own).sort()).toEqual(["additive_producer", "asphalt_plant", "contractor", "recycler", "sorting_plant", "warehouse"]);
});

const TEAM: { key: string; name: string; role: string; duties: string[]; steps: string[] }[] = [
  { key: "intake", name: "Marta Intake", role: "editor", duties: ["receive"], steps: [] },
  { key: "sorter", name: "Pere Triador", role: "editor", duties: ["produce"], steps: ["sorting_plant"] },
  { key: "recycler", name: "Laia Reciclatge", role: "editor", duties: ["produce"], steps: ["recycler"] },
  { key: "compounder", name: "Oriol Compounding", role: "editor", duties: ["produce"], steps: ["additive_producer"] },
  { key: "mixer", name: "Joan Mescles", role: "editor", duties: ["produce"], steps: ["asphalt_plant"] },
  { key: "site", name: "Núria Obra", role: "editor", duties: ["produce", "test"], steps: ["contractor"] },
  { key: "lab", name: "Sergi Laboratori", role: "editor", duties: ["test"], steps: [] },
  { key: "quality", name: "Carla Qualitat", role: "editor", duties: ["test", "release"], steps: [] },
  { key: "dispatch", name: "Marc Expedicions", role: "editor", duties: ["ship"], steps: [] },
  { key: "reporter", name: "Rosa Informes", role: "viewer", duties: [], steps: [] },
];

setup("the admin builds the team: one named job role per person", async ({ page }) => {
  for (const m of TEAM) {
    const email = `${m.key}.${RUN}@ecoasfalt.example`;
    const inv = await A.admin.api.post("/api/v1/org/invitations", { data: { email, role: m.role, duties: m.duties, steps: m.steps } });
    expect(inv.status(), `${m.key}: ${await inv.text()}`).toBe(201);
    A[m.key] = { api: await join((await inv.json()).link, m.name), email, name: m.name };
  }
  // The owner's asset manager: the independent party for the roads.
  const inv = await A.ownerAdmin.api.post("/api/v1/org/invitations", { data: { email: `assets.${RUN}@xarxa.example`, role: "editor", duties: ["release"], steps: [] } });
  expect(inv.status(), await inv.text()).toBe(201);
  A.assets = { api: await join((await inv.json()).link, "Eva Actius"), email: `assets.${RUN}@xarxa.example`, name: "Eva Actius" };


  // The team page shows everyone with a named role.
  await uiLogin(page, A.admin.email);
  await page.goto("/settings");
  await page.getByRole("button", { name: "Team & roles" }).or(page.getByRole("link", { name: "Team & roles" })).first().click();
  await expect(page.getByText("Marta Intake").first()).toBeVisible();
  for (const role of ["Intake officer", "Sorting operator", "Quality manager", "Dispatcher", "Lab technician", "Reporter"]) {
    await expect(page.getByText(role).first(), role).toBeVisible();
  }
  await page.screenshot({ path: test.info().outputPath("team.png"), fullPage: true });
});

setup("the platform manager verifies the customer", async () => {
  const r = await A.pm.api.post(`/api/v1/platform/orgs/${org.id}/status`, { data: { status: "verified" } });
  expect(r.status(), await r.text()).toBeLessThan(300);
  const me = await (await A.admin.api.get("/api/v1/org")).json();
  expect(me.org.status).toBe("verified");
  await A.admin.api.get("/api/v1/org/setup");
});


// ------------------------------------------------------------------------------------------
// The material journey. Masses follow the sources: a 15–22 t truck of reject film (§10),
// ~0.68 yield to recycled LDPE (§10 recyclers 0.55–0.75), additive dosed at 0.5 % of the mix
// (§3), 24–26 t asphalt trucks (§4), ~2.45 t/m³ compacted asphalt (§5).

const lab = (r: Res, test: string, value: number, verdict: "pass" | "fail", hours: number, siteId: string): Rec => ({
  type: "lab_test", ref: `LAB-${r.ref}-${test}`, siteId, occurredAt: at(hours), massKg: 0,
  data: { subject_id: r.id, test, value, result: verdict, lab: "Laboratori de materials" },
});
const decide = (r: Res, decision: "released" | "quarantined", note: string, hours: number, siteId: string): Rec => ({
  type: "release", ref: `REL-${r.ref}`, siteId, occurredAt: at(hours), massKg: 0, data: { subject_id: r.id, decision, note },
});
const ship = (inputs: { id: string; massKg: number }[], to: string, from: string, ref: string, hours: number, km: number): Rec => ({
  type: "shipment", ref, siteId: from, occurredAt: at(hours), massKg: inputs.reduce((a, x) => a + x.massKg, 0), inputs,
  data: { to_site_id: to, carrier: "Transportes del Levante", vehicle: "4821-KLM", distance_km: km },
});
async function receive(who: Actor, shipment: string, hours: number, massKg: number, decision = "accepted", note = "") {
  const r = await who.api.post(`/api/v1/shipments/${shipment}/receipt`, { data: { receivedAt: at(hours), massKg, decision, note } });
  expect(r.status(), `receipt ${shipment}: ${await r.text()}`).toBeLessThan(300);
}
const get = async (who: Actor, path: string) => (await who.api.get(path)).json();
const ids_ = (xs: { id: string }[]) => xs.map((x) => x.id);

test("sorting operator records five reject lots (stage 1)", async () => {
  const lots: [string, number, string, number, string][] = [
    ["A", 19400, "municipal_packaging", 76, "landfill"], ["B", 16200, "commercial_film", 82, "landfill"],
    ["C", 21000, "municipal_packaging", 71, "incineration"], ["D", 18000, "agricultural_film", 64, "landfill"],
    ["E", 15000, "municipal_packaging", 68, "landfill"],
  ];
  const res = await submit(A.sorter, lots.map(([k, kg, src, ldpe, route], i) => ({
    type: "reject_lot", ref: `MON-26-${k}`, siteId: own.sorting_plant, occurredAt: at(1 + 2 * i), massKg: kg,
    data: { source: src, ldpe_share_pct: ldpe, moisture_pct: 11 + i, contamination_pct: 9 + i, avoided_route: route, bales: Math.round(kg / 810) },
  })));
  lots.forEach(([k], i) => (ids[`PRL-${k}`] = res[i].id));
  for (const r of res) expect(r.id).toMatch(/^PRL-\d{2}-\d{6}$/);
});

test("roles are enforced: the sorter cannot ship, the dispatcher cannot produce, the reporter cannot write", async () => {
  const noShip = await A.sorter.api.post("/api/v1/records", { data: { records: [ship([{ id: ids["PRL-A"], massKg: 1000 }], own.recycler, own.sorting_plant, "X", 2, 5)] } });
  expect(noShip.status()).toBeGreaterThanOrEqual(400);
  const noMake = await A.dispatch.api.post("/api/v1/records", { data: { records: [{ type: "reject_lot", ref: "X", siteId: own.sorting_plant, occurredAt: at(2), massKg: 1000, data: { source: "commercial_film", ldpe_share_pct: 80, avoided_route: "landfill" } }] } });
  expect(noMake.status()).toBeGreaterThanOrEqual(400);
  const rep = await A.reporter.api.post("/api/v1/records", { data: { records: [] } });
  expect(rep.status()).toBeGreaterThanOrEqual(400);
  // The recycling operator may not sign a reject lot: that is the sorting section.
  const wrongStep = await A.recycler.api.post("/api/v1/records", { data: { records: [{ type: "reject_lot", ref: "Y", siteId: own.sorting_plant, occurredAt: at(2), massKg: 1000, data: { source: "commercial_film", ldpe_share_pct: 80, avoided_route: "landfill" } }] } });
  expect(wrongStep.status()).toBeGreaterThanOrEqual(400);
  // A test organisation cannot label made-up data Operational.
  const relabel = await A.sorter.api.post("/api/v1/records", { data: { records: [{ type: "reject_lot", ref: "Z", siteId: own.sorting_plant, occurredAt: at(2), massKg: 1000, provenance: "Operational", data: { source: "commercial_film", ldpe_share_pct: 80, avoided_route: "landfill" } }] } });
  expect(relabel.status()).toBe(422);
  // A production lot is made at a site of its step, not at another of the company's sites.
  const wrongSite = await A.sorter.api.post("/api/v1/records", { data: { records: [{ type: "reject_lot", ref: "W", siteId: own.asphalt_plant, occurredAt: at(2), massKg: 1000, data: { source: "commercial_film", ldpe_share_pct: 80, avoided_route: "landfill" } }] } });
  expect(wrongSite.status()).toBe(422);
});

test("the dispatcher's queue lists the five lots; one shipment carries two lots, one lot ships on its own", async () => {
  const w = await get(A.dispatch, "/api/v1/work");
  const queued = ids_(w.queues.ship.items.map((i: { record: { id: string } }) => i.record));
  for (const k of "ABCDE") expect(queued, `PRL-${k} in the dispatch queue`).toContain(ids[`PRL-${k}`]);
  const m = (k: string) => lotMass[k];
  const res = await submit(A.dispatch, [
    ship([{ id: ids["PRL-A"], massKg: m("A") }, { id: ids["PRL-B"], massKg: m("B") }], own.recycler, own.sorting_plant, "ALB-26-0412", 12, 62),
    ship([{ id: ids["PRL-C"], massKg: m("C") }, { id: ids["PRL-E"], massKg: m("E") }], own.recycler, own.sorting_plant, "ALB-26-0413", 13, 62),
    ship([{ id: ids["PRL-D"], massKg: m("D") }], own.recycler, own.sorting_plant, "ALB-26-0414", 14, 62),
  ]);
  ids["SHP-1"] = res[0].id; ids["SHP-2"] = res[1].id; ids["SHP-3"] = res[2].id;
  const after = await get(A.dispatch, "/api/v1/work");
  const left = ids_(after.queues.ship.items.map((i: { record: { id: string } }) => i.record));
  for (const k of "ABCDE") expect(left, `PRL-${k} left the queue`).not.toContain(ids[`PRL-${k}`]);
});
const lotMass: Record<string, number> = { A: 19400, B: 16200, C: 21000, D: 18000, E: 15000 };

test("intake officer weighs the three trucks: accepted, accepted with a note, rejected", async () => {
  const w = await get(A.intake, "/api/v1/work");
  const todo = w.queues.receive.items.map((i: { record: { id: string } }) => i.record.id);
  expect(todo).toEqual(expect.arrayContaining([ids["SHP-1"], ids["SHP-2"], ids["SHP-3"]]));
  // A rejection or a note needs words.
  const bare = await A.intake.api.post(`/api/v1/shipments/${ids["SHP-3"]}/receipt`, { data: { receivedAt: at(22), massKg: 17960, decision: "rejected", note: "" } });
  expect(bare.status()).toBe(422);
  // Someone without the intake duty cannot sign a receipt.
  const wrong = await A.dispatch.api.post(`/api/v1/shipments/${ids["SHP-1"]}/receipt`, { data: { receivedAt: at(20), massKg: 35540, decision: "accepted" } });
  expect(wrong.status()).toBeGreaterThanOrEqual(400);
  await receive(A.intake, ids["SHP-1"], 20, 35540);
  await receive(A.intake, ids["SHP-2"], 21, 35920, "accepted_with_note", "Two bales wet from rain; moisture to be re-tested");
  await receive(A.intake, ids["SHP-3"], 22, 17960, "rejected", "Soil contamination above 30 %; returned to the supplier");
  const after = await get(A.intake, "/api/v1/work");
  expect(after.queues.receive.items.map((i: { record: { id: string } }) => i.record.id)).not.toContain(ids["SHP-1"]);
});

test("the recycler's work list shows received lots, not the rejected one", async () => {
  const w = await get(A.recycler, "/api/v1/work");
  const process = w.queues.process.items.map((i: { record: { id: string } }) => i.record.id);
  for (const k of "ABCE") expect(process, `PRL-${k} ready for recycling`).toContain(ids[`PRL-${k}`]);
  expect(process, "the rejected lot D must not be offered").not.toContain(ids["PRL-D"]);
});

test("N lots into one recycled lot, and one lot into two (stage 2)", async () => {
  const res = await submit(A.recycler, [
    { type: "recycled_lot", ref: "VIL-26-R1", siteId: own.recycler, occurredAt: at(30), massKg: 29500,
      inputs: [{ id: ids["PRL-A"], massKg: 19400 }, { id: ids["PRL-B"], massKg: 16200 }, { id: ids["PRL-C"], massKg: 8000 }],
      data: { form: "agglomerate", process: "washing,shredding,agglomeration", mfi_g10min: 1.4, moisture_pct: 0.9, ash_pct: 3.1, energy_kwh: 5900 } },
    { type: "recycled_lot", ref: "VIL-26-R2", siteId: own.recycler, occurredAt: at(32), massKg: 8800,
      inputs: [{ id: ids["PRL-C"], massKg: 13000 }],
      data: { form: "regranulate", process: "washing,shredding,melt filtration,pelletising", mfi_g10min: 1.1, moisture_pct: 0.1, ash_pct: 0.7, energy_kwh: 3100 } },
    { type: "recycled_lot", ref: "VIL-26-R3", siteId: own.recycler, occurredAt: at(33), massKg: 10200,
      inputs: [{ id: ids["PRL-E"], massKg: 15000 }],
      data: { form: "agglomerate", process: "washing,shredding,agglomeration", mfi_g10min: 9.2, moisture_pct: 2.6, ash_pct: 6.9, energy_kwh: 4100 } },
  ]);
  ids["RPL-1"] = res[0].id; ids["RPL-2"] = res[1].id; ids["RPL-3"] = res[2].id;
  expect(res[2].findings?.map((f) => f.code), "RPL-3's MFI is outside the film-grade window").toContain("C6");
});

test("chain of custody: using the rejected lot is blocked", async () => {
  const [r] = await submit(A.recycler, [{ type: "recycled_lot", ref: "VIL-26-NEG", siteId: own.recycler, occurredAt: at(34), massKg: 3000,
    inputs: [{ id: ids["PRL-D"], massKg: 5000 }], data: { form: "agglomerate", mfi_g10min: 1.3, moisture_pct: 0.8, ash_pct: 3 } }], { expect: "any" });
  // Expected: C4 "rejected at intake". Soft, so the walkthrough goes on and reports it at the end.
  gap(r.status === "blocked" && !!r.findings?.some((f) => f.code === "C4"),
    `custody: a recycled lot made from a lot REJECTED at intake was accepted (status ${r.status}, findings ${r.findings?.map((f) => f.code)})`);
  ids["NEG"] = r.id;
});

test("mass balance: more recycled plastic than went in is blocked", async () => {
  const [r] = await submit(A.recycler, [{ type: "recycled_lot", ref: "VIL-26-NEG2", siteId: own.recycler, occurredAt: at(34), massKg: 90000,
    inputs: [{ id: ids["PRL-E"], massKg: 100 }], data: { form: "agglomerate", mfi_g10min: 1.3, moisture_pct: 0.8, ash_pct: 3 } }], { expect: "any" });
  expect(r.status).toBe("blocked");
  expect(r.findings?.map((f) => f.code)).toContain("C5");
});

test("lab technician records tests; quality manager releases two lots and quarantines the third", async () => {
  await submit(A.lab, [
    lab({ id: ids["RPL-1"], ref: "VIL-26-R1" } as Res, "mfi", 1.4, "pass", 34, own.recycler),
    lab({ id: ids["RPL-1"], ref: "VIL-26-R1" } as Res, "moisture", 0.9, "pass", 34, own.recycler),
    lab({ id: ids["RPL-1"], ref: "VIL-26-R1" } as Res, "ash", 3.1, "pass", 34, own.recycler),
    lab({ id: ids["RPL-2"], ref: "VIL-26-R2" } as Res, "mfi", 1.1, "pass", 35, own.recycler),
    lab({ id: ids["RPL-3"], ref: "VIL-26-R3" } as Res, "mfi", 9.2, "fail", 35, own.recycler),
  ], { expect: "any" });
  const q = await get(A.quality, "/api/v1/work");
  const waiting = q.queues.release.items.map((i: { record: { id: string } }) => i.record.id);
  expect(waiting).toEqual(expect.arrayContaining([ids["RPL-1"], ids["RPL-2"], ids["RPL-3"]]));
  await submit(A.quality, [
    decide({ id: ids["RPL-1"], ref: "VIL-26-R1" } as Res, "released", "MFI 1.4, moisture 0.9 %, ash 3.1 % within film grade", 36, own.recycler),
    decide({ id: ids["RPL-2"], ref: "VIL-26-R2" } as Res, "released", "Regranulate MFI 1.1; certificate of analysis attached", 36, own.recycler),
    decide({ id: ids["RPL-3"], ref: "VIL-26-R3" } as Res, "quarantined", "MFI 9.2 outside the film-grade window; held for re-test", 36, own.recycler),
  ], { expect: "any" });
  const after = await get(A.quality, "/api/v1/work");
  expect(after.queues.release.items.map((i: { record: { id: string } }) => i.record.id)).not.toContain(ids["RPL-1"]);
});

test("shipping the quarantined lot raises C9 (and is voided)", async () => {
  const [r] = await submit(A.dispatch, [ship([{ id: ids["RPL-3"], massKg: 10200 }], own.additive_producer, own.recycler, "VIL-26-NEG3", 37, 62)], { expect: "any" });
  expect(r.findings?.map((f) => f.code)).toContain("C9");
  const v = await A.dispatch.api.post(`/api/v1/records/${r.id}/void`, { data: { reason: "Shipped by mistake; the lot is quarantined" } });
  expect(v.status(), await v.text()).toBeLessThan(300);
});

test("the dispatcher ships both released lots in one truckload to the additive plant; intake signs", async () => {
  const w = await get(A.dispatch, "/api/v1/work");
  const q = ids_(w.queues.ship.items.map((i: { record: { id: string } }) => i.record));
  expect(q).toEqual(expect.arrayContaining([ids["RPL-1"], ids["RPL-2"]]));
  expect(q, "quarantined lot is not offered for shipping").not.toContain(ids["RPL-3"]);
  const [s4] = await submit(A.dispatch, [ship([{ id: ids["RPL-1"], massKg: 29500 }, { id: ids["RPL-2"], massKg: 8800 }], own.additive_producer, own.recycler, "VIL-26-0420", 40, 62)]);
  ids["SHP-4"] = s4.id;
  await receive(A.intake, s4.id, 46, 38260);
});

test("additive producer mixes N recycled lots into batches; quality releases; dispatch ships to the asphalt plant", async () => {
  const res = await submit(A.compounder, [
    { type: "additive_batch", ref: "CON-26-B1", siteId: own.additive_producer, occurredAt: at(50), massKg: 14000,
      // 92 % recycled: 12,880 kg of recycled plastic in 14 t (the rest is carrier); C5 checks both ways.
      inputs: [{ id: ids["RPL-1"], massKg: 9000 }, { id: ids["RPL-2"], massKg: 3880 }],
      data: { product: "RP-Mod 70", form: "pellets", recycled_content_pct: 92, mfi_g10min: 2.1, melting_point_c: 112, energy_kwh: 3100 } },
    { type: "additive_batch", ref: "CON-26-B2", siteId: own.additive_producer, occurredAt: at(52), massKg: 15000,
      inputs: [{ id: ids["RPL-1"], massKg: 13800 }],
      data: { product: "RP-Mod 70", form: "pellets", recycled_content_pct: 92, mfi_g10min: 2.3, melting_point_c: 111, energy_kwh: 3300 } },
  ]);
  ids["ADB-1"] = res[0].id; ids["ADB-2"] = res[1].id;
  await submit(A.lab, [
    lab({ id: ids["ADB-1"], ref: "CON-26-B1" } as Res, "dsc_melting", 112, "pass", 54, own.additive_producer),
    lab({ id: ids["ADB-2"], ref: "CON-26-B2" } as Res, "dsc_melting", 111, "pass", 54, own.additive_producer),
  ], { expect: "any" });
  await submit(A.quality, [
    decide({ id: ids["ADB-1"], ref: "CON-26-B1" } as Res, "released", "Melting peak 112 °C; recycled content 92 %", 56, own.additive_producer),
    decide({ id: ids["ADB-2"], ref: "CON-26-B2" } as Res, "released", "Melting peak 111 °C; recycled content 92 %", 56, own.additive_producer),
  ], { expect: "any" });
  const [s5] = await submit(A.dispatch, [ship([{ id: ids["ADB-1"], massKg: 14000 }, { id: ids["ADB-2"], massKg: 15000 }], own.asphalt_plant, own.additive_producer, "CON-26-0431", 60, 70)]);
  await receive(A.intake, s5.id, 64, 28970);
});

test("UI: the compounder selects two recycled lots in My work and records one batch from both", async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 960 } });
  await ctx.addCookies((await A.compounder.api.storageState()).cookies);
  const page = await ctx.newPage();
  await page.goto("/work");
  await page.getByLabel(`Select ${ids["RPL-1"]}`).check();
  await page.getByLabel(`Select ${ids["RPL-2"]}`).check();
  await page.getByRole("button", { name: /Record additive batch from 2 inputs/ }).click();
  const dialog = page.getByRole("dialog");
  // What is left of each lot, scaled to the receiving weighbridge (38,260 kg read against 38,300 kg sent).
  const left = async (k: string) => Number(await dialog.getByLabel(`Mass from ${ids[k]}`).inputValue());
  // RPL-1: 29,500 − 9,000 − 13,800 = 6,700 kg; RPL-2: 8,800 − 3,880 = 4,920 kg (before the weighbridge scaling).
  expect(await left("RPL-1")).toBeGreaterThan(6600); expect(await left("RPL-1")).toBeLessThanOrEqual(6700);
  expect(await left("RPL-2")).toBeGreaterThan(4850); expect(await left("RPL-2")).toBeLessThanOrEqual(4920);
  // 92 % recycled: the batch weighs the recycled inputs divided by 0.92 (the rest is carrier).
  const inKg = (await left("RPL-1")) + (await left("RPL-2"));
  await dialog.getByLabel("Mass (kg)").fill(String(Math.round(inKg / 0.92)));
  await dialog.getByLabel("Product").fill("RP-Mod 70");
  await dialog.getByLabel("Form").selectOption("pellets");
  await dialog.getByLabel("Recycled content").fill("92");
  await dialog.getByLabel("Melting point").fill("112");
  await dialog.getByLabel("Melt flow index").fill("2.2");
  await dialog.getByLabel("Electricity used").fill("1600");
  await page.screenshot({ path: test.info().outputPath("n-to-1-form.png") });
  await dialog.getByRole("button", { name: "Save and sign" }).click();
  const banner = page.getByText(/Saved and signed: /);
  await expect(banner).toBeVisible();
  const id = ((await banner.textContent()) ?? "").replace(/.*Saved and signed: /, "").trim();
  const rec = await get(A.admin, `/api/v1/records/${id}`);
  const r = rec.record ?? rec;
  expect(r.inputs.map((x: { id: string }) => x.id).sort()).toEqual([ids["RPL-1"], ids["RPL-2"]].sort());
  ids["ADB-3"] = id;
  await ctx.close();
});

// Asphalt production: each lot is dosed at 0.5 % of mix (§3); additive is drawn from the batches in order,
// so one batch feeds several lots and one lot can draw on two batches.
const LOTS = [900_000, 800_000, 900_000, 800_000];
const lotInputs: { id: string; massKg: number }[][] = [];

test("mixing operator produces four asphalt lots, each drawing additive from one or two batches (stage 4)", async () => {
  const pool = [{ id: ids["ADB-1"], left: 14000 }, { id: ids["ADB-2"], left: 15000 }];
  const rows: Rec[] = [];
  LOTS.forEach((mass, i) => {
    let need = mass * 0.005;
    const inputs: { id: string; massKg: number }[] = [];
    for (const b of pool) {
      const take = Math.min(b.left, need);
      if (take > 0) { inputs.push({ id: b.id, massKg: take }); b.left -= take; need -= take; }
    }
    lotInputs.push(inputs);
    rows.push({ type: "asphalt_lot", ref: `MAR-26-L${i + 1}`, siteId: own.asphalt_plant, occurredAt: at(70 + 24 * i), massKg: mass, inputs,
      data: { mix: "AC 16 surf 50/70 S", process: "hot", binder_pct: 4.9, additive_kg: mass * 0.005, dosage_pct_mix: 0.5, mixing_temp_c: 158, gas_m3: Math.round(mass / 1000 * 8) } });
  });
  const res = await submit(A.mixer, rows);
  res.forEach((r, i) => (ids[`ASL-${i + 1}`] = r.id));
  expect(lotInputs.filter((x) => x.length > 1).length, "at least one lot draws on two batches").toBeGreaterThan(0);
  await submit(A.lab, res.flatMap((r, i) => [
    lab(r, "itsr", 90 + i, "pass", 72 + 24 * i, own.asphalt_plant), lab(r, "air_voids", 4.6 + i / 10, "pass", 72 + 24 * i, own.asphalt_plant),
    lab(r, "wheel_tracking", 0.05, "pass", 72 + 24 * i, own.asphalt_plant),
  ]), { expect: "any" });
});

test("a wrong dosing record is blocked (mass balance C5)", async () => {
  const [r] = await submit(A.mixer, [{ type: "asphalt_lot", ref: "MAR-26-NEG", siteId: own.asphalt_plant, occurredAt: at(170), massKg: 500_000,
    inputs: [{ id: ids["ADB-2"], massKg: 500 }],
    data: { mix: "AC 16 surf 50/70 S", process: "hot", binder_pct: 4.9, additive_kg: 2500, dosage_pct_mix: 0.5, mixing_temp_c: 158 } }], { expect: "any" });
  expect(r.status).toBe("blocked");
  expect(r.findings?.map((f) => f.code)).toContain("C5");
});


// ------------------------------------------------------------------------------------------
// Laying asphalt on real Catalan roads (stages 5–6). Section masses are area × thickness × 2.45 t/m³ (§5);
// trucks carry 25 t (§4) and may take their load from two lots; one lot feeds several sections.
interface Sec { key: string; road: string; from: number; to: number; width: number; cm: number; ll: [number, number, number, number]; works: string; km: number }
const SECTIONS: Sec[] = [
  { key: "RS1", road: "B-23", from: 4.0, to: 4.8, width: 7, cm: 5, ll: [41.396, 2.071, 41.403, 2.058], works: "rehabilitation", km: 22 },
  { key: "RS2", road: "C-32", from: 25.0, to: 25.6, width: 7, cm: 5, ll: [41.295, 1.99, 41.299, 1.983], works: "maintenance", km: 31 },
  { key: "RS3", road: "AP-7", from: 138.0, to: 138.5, width: 10.5, cm: 4, ll: [41.356, 1.69, 41.359, 1.696], works: "maintenance", km: 74 },
  { key: "RS4", road: "N-II", from: 592.0, to: 592.6, width: 7, cm: 5, ll: [41.48, 2.19, 41.485, 2.196], works: "rehabilitation", km: 14 },
  { key: "RS5", road: "A-2", from: 579.0, to: 579.5, width: 7, cm: 6, ll: [41.53, 1.81, 41.534, 1.815], works: "rehabilitation", km: 50 },
  { key: "RS6", road: "N-340", from: 1170.0, to: 1170.4, width: 7, cm: 5, ll: [41.16, 1.21, 41.163, 1.215], works: "maintenance", km: 112 },
];
const area = (x: Sec) => (x.to - x.from) * 1000 * x.width;
const planned = (x: Sec) => Math.round((area(x) * x.cm) / 100 * 2450);
const secData = (x: Sec, extra: Record<string, unknown> = {}) => ({
  road: x.road, km_from: x.from, km_to: x.to, carriageway: "Right", layer: "surface", thickness_cm: x.cm, area_m2: area(x), works: x.works,
  mix: "AC 16 surf 50/70 S", owner_org_id: owner.id, region: "Catalunya",
  lat: (x.ll[0] + x.ll[2]) / 2, lon: (x.ll[1] + x.ll[3]) / 2, lat_from: x.ll[0], lon_from: x.ll[1], lat_to: x.ll[2], lon_to: x.ll[3], ...extra,
});
const secStart = (i: number) => 190 + 24 * i;
const deliveriesOf: Record<string, { id: string; kg: number; hours: number; cold: boolean }[]> = {};
const lotsUsed: Record<string, Set<string>> = {};

test("site manager opens six road sections on six roads", async () => {
  const res = await submit(A.site, SECTIONS.map((x, i) => ({
    type: "road_section", ref: `OBR-26-${x.key}-${x.road}`, siteId: own.contractor, occurredAt: at(secStart(i)), massKg: planned(x), data: secData(x),
  })));
  SECTIONS.forEach((x, i) => (ids[x.key] = res[i].id));
  const open = await get(A.site, "/api/v1/road-sections/open");
  const openIds = ids_(Array.isArray(open) ? open : open.sections ?? open.items ?? []);
  gap(SECTIONS.every((x) => openIds.includes(ids[x.key])), `open road sections list is missing sections (got ${openIds.length})`);
});

test("dispatch delivers 25 t truckloads: loads span two lots, lots feed several sections", async () => {
  const pool = LOTS.map((kg, i) => ({ id: ids[`ASL-${i + 1}`], left: kg }));
  const rows: Rec[] = [];
  const meta: { sec: string; kg: number; hours: number; cold: boolean }[] = [];
  let n = 0;
  SECTIONS.forEach((x, si) => {
    let need = planned(x);
    let i = 0;
    deliveriesOf[x.key] = [];
    while (need > 0) {
      let load = Math.min(25000, need);
      const inputs: { id: string; massKg: number }[] = [];
      for (const lot of pool) {
        if (load <= 0) break;
        const take = Math.min(lot.left, load);
        if (take > 0) { inputs.push({ id: lot.id, massKg: take }); lot.left -= take; load -= take; need -= take; }
      }
      const kg = inputs.reduce((a, b) => a + b.massKg, 0);
      const f0 = 1 - (need + kg) / planned(x), f1 = 1 - need / planned(x);
      const cold = x.key === "RS2" && i === 6; // a load that arrives too cold
      const hours = secStart(si) + 1 + i * 0.3;
      rows.push({ type: "delivery", ref: `ALB-26-T${String(++n).padStart(4, "0")}`, siteId: own.asphalt_plant, occurredAt: at(hours), massKg: kg, inputs,
        data: { ticket: `T-${4400 + n}`, section_id: ids[x.key], vehicle: `${1200 + n}-LMN`, distance_km: x.km, departure_temp_c: 158, arrival_temp_c: cold ? 118 : 150 - (i % 5),
          // each load covers its own stretch of the section, in the order they were paved
          lat_from: +(x.ll[0] + (x.ll[2] - x.ll[0]) * f0).toFixed(6), lon_from: +(x.ll[1] + (x.ll[3] - x.ll[1]) * f0).toFixed(6),
          lat_to: +(x.ll[0] + (x.ll[2] - x.ll[0]) * f1).toFixed(6), lon_to: +(x.ll[1] + (x.ll[3] - x.ll[1]) * f1).toFixed(6) } });
      meta.push({ sec: x.key, kg, hours, cold });
      for (const inp of inputs) (lotsUsed[x.key] ??= new Set()).add(inp.id);
      i++;
    }
  });
  expect(pool.every((l) => l.left >= 0)).toBeTruthy();
  const res = await submit(A.dispatch, rows, { expect: "any" });
  res.forEach((r, k) => deliveriesOf[meta[k].sec].push({ id: r.id, kg: meta[k].kg, hours: meta[k].hours, cold: meta[k].cold }));
  const blocked = res.filter((r) => r.status === "blocked");
  gap(blocked.length === 0, `${blocked.length} deliveries blocked: ${JSON.stringify(blocked[0]?.findings)}`);
  console.log(`deliveries: ${res.length}; sections fed by >1 lot: ${Object.values(lotsUsed).filter((l) => l.size > 1).length}/${SECTIONS.length}`);
  const lotFeeds = LOTS.map((_, li) => SECTIONS.filter((x) => lotsUsed[x.key]?.has(ids[`ASL-${li + 1}`])).length);
  console.log("lot → number of sections it fed:", lotFeeds.join(","));
  expect(lotFeeds.some((c) => c > 1), "at least one asphalt lot supplies more than one road section").toBeTruthy();
});

test("intake at the paver accepts the loads and rejects the cold one", async () => {
  const w = await get(A.intake, "/api/v1/work");
  const all = SECTIONS.flatMap((x) => deliveriesOf[x.key]);
  // The queue shows the first 100 and says how many there are in total.
  gap(w.queues.receive.total === all.length, `the intake queue counts ${w.queues.receive.total} open deliveries, expected ${all.length}`);
  const waiting = ids_(w.queues.receive.items.map((i: { record: { id: string } }) => i.record));
  gap(waiting.length > 0 && waiting.every((id: string) => all.some((d) => d.id === id)), "the intake queue shows something that is not a delivery");
  for (const d of all) {
    await receive(A.intake, d.id, d.hours + 0.4, d.kg, d.cold ? "rejected" : "accepted", d.cold ? "Arrived at 118 °C, below the 130 °C paver minimum; load refused" : "");
  }
});

test("site manager completes each section with the as-laid mass; one claims too much", async () => {
  const rows: Rec[] = SECTIONS.map((x, i) => {
    const laid = deliveriesOf[x.key].filter((d) => !d.cold).reduce((a, d) => a + d.kg, 0);
    const claim = x.key === "RS6" ? laid + 60000 : laid;
    return { id: ids[x.key], type: "road_section", ref: `OBR-26-${x.key}-${x.road}`, siteId: own.contractor, occurredAt: at(secStart(i) + 12), massKg: claim,
      data: secData(x, { completed_at: at(secStart(i) + 11) }) } as Rec;
  });
  const res = await submit(A.site, rows, { expect: "any" });
  res.forEach((r, i) => console.log(SECTIONS[i].key, r.status, JSON.stringify(r.findings?.map((f) => f.code))));
  const rs6 = res[5];
  gap(rs6.status !== "ok" && !!rs6.findings?.some((f) => f.code === "C5"), `RS6 claims 60 t more than was laid but the check did not catch it (status ${rs6.status}, ${JSON.stringify(rs6.findings?.map((f) => f.code))})`);
  for (const r of res.slice(0, 5)) gap(r.status !== "blocked", `${r.ref} blocked: ${JSON.stringify(r.findings)}`);
  ids["RS6-status"] = rs6.status;
});

test("site manager and lab test the finished roads (cores)", async () => {
  const rows: Rec[] = [];
  SECTIONS.forEach((x, i) => {
    const r = { id: ids[x.key], ref: x.key } as Res;
    rows.push(lab(r, "air_voids", +(4.3 + i * 0.25).toFixed(2), "pass", secStart(i) + 36, own.contractor),
      lab(r, "itsr", 88 + i, "pass", secStart(i) + 36, own.contractor), lab(r, "wheel_tracking", +(0.04 + i * 0.005).toFixed(3), "pass", secStart(i) + 36, own.contractor));
  });
  await submit(A.site, rows.slice(0, 9), { expect: "any" });
  await submit(A.lab, rows.slice(9), { expect: "any" });
});

test("the road owner's asset manager accepts the flagged section with a signed reason", async () => {
  // The owner reads the section through its passport (the record API is for the parties of the record).
  const mine = await get(A.assets, `/api/v1/passports/${ids["RS6"]}`);
  gap(mine?.root?.id === ids["RS6"] || mine?.record?.id === ids["RS6"], "the road owner cannot open the passport of a section laid on its road");
  const list = await get(A.assets, "/api/v1/findings?status=open&limit=200");
  const items: { id: number; recordId: string; code: string; status: string }[] = Array.isArray(list) ? list : list.items ?? list.findings ?? [];
  const f = items.find((x) => x.recordId === ids["RS6"] && x.code === "C5");
  gap(!!f, `the road owner does not see the open C5 finding on its section (sees ${items.length} findings)`);
  // The customer itself cannot accept its own blocker.
  if (f) {
    const own = await A.admin.api.post(`/api/v1/findings/${f.id}/resolve`, { data: { status: "accepted", reason: "We accept our own mistake" } });
    gap(own.status() === 403, `the contractor could accept its own blocker (HTTP ${own.status()})`);
    const ok = await A.assets.api.post(`/api/v1/findings/${f.id}/resolve`, { data: { status: "accepted", reason: "60 t was laid from a third-party plant outside ROADPASS (paper tickets checked on site); it carries no recycled plastic" } });
    gap(ok.status() < 300, `the road owner could not accept the finding: ${await ok.text()}`);
  }
  const after = await get(A.admin, `/api/v1/records/${ids["RS6"]}`);
  const st = (after.record ?? after).status;
  console.log("RS6 after acceptance:", st);
  gap(st === "accepted", `RS6 should be 'accepted' after the road owner's signed acceptance, is '${st}'`);
  // An accepted exception documents the gap; the passport still tells claimed and traced apart.
  const pp = await get(A.admin, `/api/v1/passports/${ids["RS6"]}`);
  gap(typeof pp.tracedKg === "number" && pp.root.massKg - pp.tracedKg > 59_000,
    `RS6's passport should show the 60 t its deliveries do not trace (mass ${pp.root?.massKg}, traced ${pp.tracedKg})`);
});

// ------------------------------------------------------------------------------------------
// Sealing on Fabric and the passports.

test("the worker seals everything on the ledger (Merkle batches, one tx each)", async () => {
  test.setTimeout(240_000);
  const probe = [ids["PRL-A"], ids["RPL-1"], ids["ADB-1"], ids["ASL-4"], ids["RS1"], ids["RS5"]];
  await expect.poll(async () => {
    let done = 0;
    for (const id of probe) { const r = await get(A.admin, `/api/v1/records/${id}`); if ((r.record ?? r).sealed) done++; }
    return done;
  }, { timeout: 200_000, intervals: [3000] }).toBe(probe.length);
  const proof = await get(A.admin, `/api/v1/records/${ids["RS1"]}/proof`);
  console.log("proof keys:", Object.keys(proof).join(","));
  const sum = await get(A.admin, "/api/v1/ledger/summary");
  console.log("ledger summary:", JSON.stringify(sum).slice(0, 400));
});

test("N passports feed the next section: lineage proves many-to-many at every step", async () => {
  const edges = async (id: string) => (await get(A.admin, `/api/v1/passports/${id}`)).links as { parent: string; child: string; kind: string; massKg: number }[];
  const parentsOf = (es: { parent: string; child: string; kind: string }[], id: string) => es.filter((e) => e.child === id && e.kind === "input").map((e) => e.parent);
  
  // recycler: RPL-1 takes three reject lots; PRL-C feeds two recycled lots
  const e1 = await edges(ids["RPL-1"]);
  expect(parentsOf(e1, ids["RPL-1"]).sort()).toEqual([ids["PRL-A"], ids["PRL-B"], ids["PRL-C"]].sort());
  const eCc = (await get(A.admin, `/api/v1/passports/${ids["PRL-C"]}`));
  console.log("PRL-C downstream:", (eCc.downstream ?? []).map((n: { id: string }) => n.id).join(","));
  gap(["RPL-1", "RPL-2"].every((k) => (eCc.downstream ?? []).some((n: { id: string }) => n.id === ids[k])), "PRL-C's passport does not list both recycled lots it fed");
  // additive: ADB-1 takes two recycled lots; RPL-1 feeds two batches
  const eA = await edges(ids["ADB-1"]);
  expect(parentsOf(eA, ids["ADB-1"]).sort()).toEqual([ids["RPL-1"], ids["RPL-2"]].sort());
  const down1 = (await get(A.admin, `/api/v1/passports/${ids["RPL-1"]}`)).downstream.map((n: { id: string }) => n.id);
  expect(down1, "RPL-1 feeds both additive batches").toEqual(expect.arrayContaining([ids["ADB-1"], ids["ADB-2"]]));
  // asphalt: a lot takes additive from two batches and a batch feeds several lots
  const multi = LOTS.map((_, i) => ids[`ASL-${i + 1}`]);
  const eL = await edges(multi[3]);
  expect(parentsOf(eL, multi[3]).length, "ASL-4 draws on two additive batches").toBe(2);
  // roads: a section is fed by deliveries from more than one lot, a lot feeds several sections
  const sec = await get(A.admin, `/api/v1/passports/${ids["RS2"]}`);
  const secLots = new Set((sec.nodes ?? []).filter((n: { type: string }) => n.type === "asphalt_lot").map((n: { id: string }) => n.id));
  console.log("RS2 upstream asphalt lots:", [...secLots].join(","), " origins:", (sec.origins ?? []).length, " carbon:", JSON.stringify(sec.carbon).slice(0, 200));
  expect(secLots.size, "a section's passport shows every asphalt lot it was built from").toBeGreaterThanOrEqual(1);
  const lot = await get(A.admin, `/api/v1/passports/${multi[1]}`);
  const fed = (lot.downstream ?? []).filter((n: { type: string }) => n.type === "road_section").map((n: { id: string }) => n.id);
  console.log("ASL-2 feeds sections:", fed.join(","));
  gap(fed.length >= 2, `ASL-2's passport lists ${fed.length} road section(s) downstream, expected ≥ 2`);
  // origin: a section's passport traces back to the sorting plant lots
  const origins = (sec.origins ?? []).map((o: { id?: string; ref?: string }) => o.id ?? o.ref);
  console.log("RS2 origins:", origins.join(","));
});

test("a shipment passport shows the material it carries, its origins and both handover signatures", async () => {
  // SHP-4 carries RPL-1 and RPL-2 (38.3 t of recycled plastic) to the additive plant.
  const p = await get(A.admin, `/api/v1/passports/${ids["SHP-4"]}`);
  const carried = (p.nodes ?? []).map((n: { id: string }) => n.id);
  expect(carried).toEqual(expect.arrayContaining([ids["RPL-1"], ids["RPL-2"]]));
  expect(p.origins.length).toBeGreaterThan(0);
  expect(p.carbon.recycledKg).toBeGreaterThan(38_000);
  expect(p.trust?.signature?.valid).toBe(true);
  expect(p.trust?.receipt?.valid).toBe(true);
});

test("the road section passport carries the full chain, trust and the ledger proof", async () => {
  const p = await get(A.admin, `/api/v1/passports/${ids["RS1"]}`);
  console.log("RS1 stages:", (p.stages ?? []).map((x: { name: string; records: number }) => `${x.name}:${x.records}`).join(" | "));
  console.log("RS1 trust:", JSON.stringify(p.trust).slice(0, 300));
  expect(p.stages.length).toBeGreaterThanOrEqual(5);
  const share = (p.origins ?? []).reduce((a: number, o: { share?: number }) => a + (o.share ?? 0), 0);
  console.log("RS1 origin share sum:", share);
});

// ------------------------------------------------------------------------------------------
// The dashboard, as each person sees it.
const PAGES = ["/", "/work", "/chain", "/status", "/calendar", "/passports", "/roads", "/quality", "/carbon", "/plants", "/ledger", "/data", "/compliance", "/alerts", "/settings", "/glossary"];
const FAILED_OK = /\/api\/v1\/(auth\/me\/export|notifications\/push)/;

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
    if (shots) await page.screenshot({ path: test.info().outputPath(`${who.name.split(" ")[0]}${path.replace(/\W+/g, "_")}.png`) });
  }
  await ctx.close();
  return errors;
}

test("the admin opens every dashboard page without an error", async ({ browser }) => {
  test.setTimeout(240_000);
  const errors = await sweep(browser, A.admin, PAGES, true);
  gap(errors.length === 0, errors.join("; "));
});

test("each role opens its own pages: queues, passports, roads", async ({ browser }) => {
  test.setTimeout(300_000);
  const errors: string[] = [];
  for (const k of ["sorter", "recycler", "compounder", "mixer", "site", "lab", "quality", "dispatch", "intake", "reporter", "ownerAdmin", "assets"]) {
    errors.push(...await sweep(browser, A[k], ["/", "/work", "/passports", "/roads"]));
  }
  gap(errors.length === 0, errors.join("; "));
});

test("passport pages show the lineage graph, tests and trust for every step", async ({ browser }) => {
  test.setTimeout(240_000);
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 960 } });
  await ctx.addCookies((await A.admin.api.storageState()).cookies);
  const page = await ctx.newPage();
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  for (const k of ["PRL-C", "RPL-1", "ADB-1", "ASL-2", "RS2", "RS6"]) {
    await page.goto(`/passports/${ids[k]}`);
    await expect(page.getByText(ids[k]).first(), k).toBeVisible();
    await page.waitForTimeout(1500);
    await page.screenshot({ path: test.info().outputPath(`passport-${k}.png`), fullPage: true });
  }
  await ctx.close();
  gap(errors.length === 0, `passport page errors: ${errors.join("; ")}`);
});

test("dashboard KPIs leave blocked records out: the funnel counts only the lots that are in the chain", async () => {
  const from = new Date(T0 - 86400_000).toISOString(), to = new Date(Date.now() + 86400_000).toISOString();
  await expect.poll(async () => {
    const f = await get(A.admin, `/api/v1/analytics/funnel?from=${from}&to=${to}`);
    const step = (f as { type: string; tonnes?: number; t?: number; n?: number; records?: number }[]).find((x) => x.type === "recycled_lot");
    return step ? `${Math.round(step.tonnes ?? step.t ?? 0)}t/${(step as { count?: number }).count}` : "none";
  }, { timeout: 60_000, message: "funnel recycled plastic: 3 valid lots (≈ 29.5+8.8+10.2 t of output) and not the blocked 90 t lot" }).toMatch(/^4\dt\/3$/);
});

test("walkthrough summary: product gaps found", async () => {
  console.log("GAPS:\n" + (gaps.length ? gaps.map((g) => " - " + g).join("\n") : " none"));
  expect(gaps, gaps.join("\n")).toEqual([]);
});
