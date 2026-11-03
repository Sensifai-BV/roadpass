import { test, expect, request as pwRequest, type APIRequestContext, type Browser, type Page } from "@playwright/test";

// The public demo workspace (Capital Paving, built by the server itself when DEMO_TENANT=1) as a prospect
// would meet it: the shared password, every named job role, every dashboard page with data, the limits on
// what a visitor may enter, and the platform manager who cannot suspend, edit or disable it.
//
// Needs a stack started with DEMO_TENANT=1 (PM_EMAIL / PM_PASSWORD only for the platform-manager checks).
test.describe.configure({ mode: "serial" });

const H = { "X-Requested-With": "roadpass" };
const BASE = process.env.BASE_URL ?? "http://localhost:8080";
const PASSWORD = process.env.DEMO_PASSWORD ?? "roadpass-demo-2026";
const ORG_ID = "org_capital-paving-demo";
const D = "capital-demo.com";
const ROLES: Record<string, string> = {
  admin: `admin@${D}`, intake: `dana.intake@${D}`, sorter: `jordan.sorting@${D}`, recycler: `riley.recycling@${D}`,
  compounder: `casey.additive@${D}`, mixer: `taylor.mixing@${D}`, site: `sam.paving@${D}`, lab: `quinn.lab@${D}`,
  quality: `avery.quality@${D}`, dispatch: `drew.dispatch@${D}`, assets: `jamie.assets@${D}`, reporter: `robin.reporter@${D}`,
};
const PM_EMAIL = process.env.PM_EMAIL ?? "manager@platform.example";
const PM_PASSWORD = process.env.PM_PASSWORD ?? "a-long-enough-password";

const api: Record<string, APIRequestContext> = {};
async function login(email: string, password = PASSWORD): Promise<APIRequestContext> {
  const c = await pwRequest.newContext({ baseURL: BASE, extraHTTPHeaders: H });
  const r = await c.post("/api/v1/auth/login", { data: { email, password } });
  expect(r.ok(), `login ${email}: ${await r.text()}`).toBeTruthy();
  return c;
}
const get = async (who: string, path: string) => { const r = await api[who].get(path); expect(r.ok(), `${path}: ${r.status()}`).toBeTruthy(); return r.json(); };

async function uiLogin(page: Page, email: string) {
  await page.goto("/login");
  await page.getByLabel("Work e-mail").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

test("the workspace is built and every named role signs in with the shared password", async () => {
  for (const [k, email] of Object.entries(ROLES)) api[k] = await login(email);
  const me = await get("admin", "/api/v1/auth/me");
  expect(me.org.id).toBe(ORG_ID);
  expect(me.org.demo).toBe(true);
  expect(me.org.status).toBe("verified");
  expect(me.org.country).toBe("US");
  expect(me.org.kinds.sort()).toEqual(["additive_producer", "asphalt_plant", "contractor", "lab", "recycler", "road_owner", "sorting_plant"]);
  const wrong = await pwRequest.newContext({ baseURL: BASE, extraHTTPHeaders: H });
  expect((await wrong.post("/api/v1/auth/login", { data: { email: ROLES.admin, password: "not-the-password" } })).status()).toBe(401);
});

test("the sign-in page works in the browser and the banner says it is a demo", async ({ page }) => {
  await uiLogin(page, ROLES.admin);
  await expect(page.getByText("Demo workspace.")).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("overview.png"), fullPage: true });
});

test("every part of the chain has data, including real and simulated provenance", async () => {
  const types = ["reject_lot", "recycled_lot", "additive_batch", "asphalt_lot", "delivery", "road_section", "shipment", "lab_test", "release"];
  const counts: Record<string, number> = {};
  for (const t of types) {
    const r = await get("admin", `/api/v1/records?type=${t}&limit=1`);
    counts[t] = r.items?.length ?? 0;
    expect(counts[t], `${t} records`).toBeGreaterThan(0);
  }
  console.log("records:", JSON.stringify(counts));
  const secs = await get("admin", "/api/v1/records?type=road_section&limit=50");
  const items = secs.items ?? secs.records ?? [];
  const real = items.filter((r: { provenance: string }) => r.provenance === "PublicReference");
  expect(real.length, "the three Route 740 sections carry the PublicReference label").toBe(3);
  expect(items.length).toBeGreaterThanOrEqual(8);
  const kg = real.map((r: { massKg: number }) => r.massKg).sort((a: number, b: number) => a - b);
  expect(Math.round(kg[0] / 907.18474 * 10) / 10).toBe(835.9); // 50PE, as published by MoDOT
});

test("work queues hold something for every duty", async () => {
  for (const [who, queue] of [["dispatch", "ship"], ["intake", "receive"], ["recycler", "process"], ["lab", "release"], ["quality", "release"]] as const) {
    const w = await get(who, "/api/v1/work");
    expect(w.queues[queue]?.items?.length ?? 0, `${who} queue ${queue}`).toBeGreaterThan(0);
  }
});

test("quality: open findings, one resolved, one blocker; compliance, calendar and alerts have content", async () => {
  const f = await get("admin", "/api/v1/findings?limit=200");
  const codes = new Set(f.items.map((x: { code: string }) => x.code));
  for (const c of ["C1", "C4", "C6", "C9"]) expect(codes.has(c), `finding ${c}`).toBeTruthy();
  expect(f.items.some((x: { status: string }) => x.status === "resolved")).toBeTruthy();
  const from = new Date(Date.now() - 30 * 86400_000).toISOString(), to = new Date(Date.now() + 14 * 86400_000).toISOString();
  const cal = await get("admin", `/api/v1/calendar?from=${from}&to=${to}`);
  expect(cal.plans.length).toBeGreaterThanOrEqual(4);
  expect(cal.plans.some((p: { overdue: boolean }) => p.overdue)).toBeTruthy();
  const reg = await get("admin", "/api/v1/compliance/register");
  expect(reg.register.items.length).toBeGreaterThan(0);
  const st = await get("admin", `/api/v1/compliance/status?from=${from.slice(0, 10)}&to=${new Date().toISOString().slice(0, 10)}`);
  expect(st.limits.length).toBeGreaterThan(0);
  const rules = await get("admin", "/api/v1/alerts/rules");
  expect((rules.rules ?? rules).length).toBeGreaterThan(1);
});

test("the worker projects the telemetry and seals the records on the ledger", async () => {
  test.setTimeout(240_000);
  await expect.poll(async () => {
    const r = await api.admin.get(`/api/v1/analytics/extruder?site=site_capdemo_recycler&from=${new Date(Date.now() - 7 * 86400_000).toISOString()}&to=${new Date().toISOString()}`);
    return r.ok() ? JSON.stringify(await r.json()).length : 0;
  }, { timeout: 120_000, intervals: [3000] }).toBeGreaterThan(300);
  const dose = await api.admin.get(`/api/v1/analytics/dosing?site=site_capdemo_asphalt&from=${new Date(Date.now() - 200 * 86400_000).toISOString()}&to=${new Date().toISOString()}`);
  expect(dose.ok(), "dosing analytics").toBeTruthy();
  await expect.poll(async () => {
    const rec = await get("admin", "/api/v1/records?type=road_section&limit=50");
    const items: { sealed: boolean }[] = rec.items ?? rec.records ?? [];
    return items.filter((x) => x.sealed).length;
  }, { timeout: 200_000, intervals: [4000] }).toBeGreaterThanOrEqual(8);
});

test("a Route 740 passport traces to the reject film and verifies", async () => {
  const secs = await get("admin", "/api/v1/records?type=road_section&limit=50");
  const s = (secs.items ?? secs.records).find((x: { ref: string }) => x.ref === "R740-SEC-50PE");
  const p = await get("admin", `/api/v1/passports/${s.id}`);
  expect(p.stages.length).toBeGreaterThanOrEqual(5);
  expect(p.trust?.signature?.valid).toBe(true);
  const sh = await get("admin", `/api/v1/passports/${s.id}/share`);
  expect(JSON.stringify(sh)).toMatch(/token|url|link/i);
});

const PAGES = ["/", "/work", "/chain", "/status", "/calendar", "/passports", "/roads", "/quality", "/carbon", "/plants", "/ledger", "/data", "/compliance", "/alerts", "/settings", "/glossary"];
async function sweep(browser: Browser, who: string, paths: string[], shots = false) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 960 } });
  await ctx.addCookies((await api[who].storageState()).cookies);
  const page = await ctx.newPage();
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(`${who}: page error ${e.message}`));
  page.on("response", (r) => { if (r.url().includes("/api/") && r.status() >= 500) errors.push(`${who}: ${r.status()} ${r.url()}`); });
  for (const path of paths) {
    await page.goto(path);
    await page.waitForLoadState("domcontentloaded");
    await page.waitForTimeout(1200);
    const body = (await page.locator("body").innerText()).slice(0, 4000);
    if (/Something went wrong|Unexpected (Application )?Error|Cannot read propert/i.test(body)) errors.push(`${who}: error screen on ${path}`);
    if (shots) await page.screenshot({ path: test.info().outputPath(`${who}${path.replace(/\W+/g, "_")}.png`), fullPage: true });
  }
  await ctx.close();
  return errors;
}

test("the administrator opens every dashboard page without an error", async ({ browser }) => {
  test.setTimeout(300_000);
  expect(await sweep(browser, "admin", PAGES, true)).toEqual([]);
});

test("every role opens its own pages", async ({ browser }) => {
  test.setTimeout(400_000);
  const errors: string[] = [];
  for (const k of Object.keys(ROLES).filter((x) => x !== "admin")) errors.push(...await sweep(browser, k, ["/", "/work", "/passports", "/roads"]));
  expect(errors).toEqual([]);
});

test("visitors can enter data within a daily budget", async () => {
  const site = (await get("admin", "/api/v1/org")).sites.find((s: { kind: string }) => s.kind === "sorting_plant").id;
  const row = (i: number) => ({ type: "reject_lot", ref: `VISITOR-${Date.now()}-${i}`, siteId: site, occurredAt: new Date(Date.now() - 3600_000).toISOString(), massKg: 1000 + i, provenance: "Simulated",
    data: { source: "municipal_packaging", ldpe_share_pct: 80, avoided_route: "landfill" } });
  const ok = await api.sorter.post("/api/v1/records", { data: { records: [row(1), row(2)] } });
  expect(ok.status(), await ok.text()).toBe(201);
  const tooMany = await api.sorter.post("/api/v1/records", { data: { records: Array.from({ length: 200 }, (_, i) => row(100 + i)) } });
  expect(tooMany.status()).toBe(429);
  expect((await tooMany.json()).detail).toMatch(/resets every day/);
});

test("invitations, API keys, passwords, alert channels and the company profile stay closed", async () => {
  const closed: [string, string, unknown][] = [
    ["POST", "/api/v1/org/invitations", { email: "x@example.com", role: "viewer", duties: [] }],
    ["POST", "/api/v1/org/api-keys", { name: "k", scopes: ["records:read"] }],
    ["POST", "/api/v1/auth/password", { current: PASSWORD, password: "another-long-password" }],
    ["POST", "/api/v1/alerts/channels", { type: "webhook", name: "w", config: { url: "https://example.com/hook" } }],
    ["PUT", "/api/v1/org", { name: "Hacked" }],
    ["PUT", "/api/v1/compliance/schedule", { enabled: true }],
  ];
  for (const [m, path, data] of closed) {
    const r = await api.admin.fetch(path, { method: m, data });
    expect(r.status(), `${m} ${path}`).toBe(403);
    expect((await r.json()).title).toBe("Disabled in the demo");
  }
});

test("the platform manager cannot suspend, edit, invite or disable the demo workspace", async ({ page }) => {
  const pm = await login(PM_EMAIL, PM_PASSWORD);
  for (const [m, path, data] of [
    ["POST", `/api/v1/platform/orgs/${ORG_ID}/status`, { status: "suspended", reason: "test" }],
    ["PUT", `/api/v1/platform/orgs/${ORG_ID}`, { name: "Renamed", kind: "asphalt_plant", country: "US" }],
    ["POST", `/api/v1/platform/orgs/${ORG_ID}/invitations`, { email: "x@example.com", role: "admin" }],
    ["PUT", `/api/v1/platform/orgs/${ORG_ID}/subscription`, { planId: "starter", status: "cancelled" }],
  ] as const) {
    const r = await pm.fetch(path, { method: m, data });
    expect(r.status(), `${m} ${path}`).toBe(403);
    expect((await r.json()).title).toBe("Managed automatically");
  }
  const users = await (await pm.get(`/api/v1/platform/users?q=${encodeURIComponent(ROLES.quality)}`)).json();
  const u = users.find((x: { email: string }) => x.email === ROLES.quality);
  expect(u.orgDemo).toBe(true);
  const dis = await pm.post(`/api/v1/platform/users/${u.id}/status`, { data: { disabled: true } });
  expect(dis.status()).toBe(403);
  // the console hides the buttons
  await page.goto("/login");
  await page.getByLabel("Work e-mail").fill(PM_EMAIL);
  await page.getByLabel("Password", { exact: true }).fill(PM_PASSWORD);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/);
  await page.goto("/platform/organisations");
  const row = page.locator("tr", { hasText: "Capital Paving (public-data demo)" });
  await expect(row.getByText("Managed automatically")).toBeVisible();
  await expect(row.getByRole("button", { name: "Suspend" })).toHaveCount(0);
  await page.screenshot({ path: test.info().outputPath("platform-orgs.png"), fullPage: true });
});

test("the demo is still verified and usable after the manager's attempts", async () => {
  const me = await get("admin", "/api/v1/auth/me");
  expect(me.org.name).toBe("Capital Paving (public-data demo)");
  expect(me.org.status).toBe("verified");
  expect((await api.quality.get("/api/v1/auth/me")).ok()).toBeTruthy(); // still signed in and not disabled
});
