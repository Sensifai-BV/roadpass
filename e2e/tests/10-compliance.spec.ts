import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

const H = { "X-Requested-With": "roadpass" };
const ORG = "org_aglomerados-meseta";

test("quality sets a limit, a responsible person signs statements and the month's report", async ({ page, browser }) => {
  await signIn(page, ORG);
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));

  // A limit on the additive dosage of asphalt lots is checked over the period.
  const created = await page.request.post("/api/v1/compliance/limits", { headers: H, data: {
    name: `Dosage E2E ${Date.now()}`, source: "records", measure: "dosage", types: ["asphalt_lot"], basis: "instant", min: 0.45, max: 0.6, note: "mix design", enabled: true,
  } });
  expect(created.status()).toBe(200);
  const limit = await created.json();
  const bad = await page.request.post("/api/v1/compliance/limits", { headers: H, data: { name: "x", source: "records", measure: "mass_kg; DROP TABLE records", basis: "instant", max: 1 } });
  expect(bad.status()).toBe(422);
  const status = await (await page.request.get("/api/v1/compliance/status?from=2025-10-01&to=2026-10-01")).json();
  const mine = status.limits.find((l: { limit: { id: string } }) => l.limit.id === limit.id);
  expect(mine.stat.count).toBeGreaterThan(0);
  expect(mine.exceedances.length).toBeGreaterThan(0);
  for (const e of mine.exceedances) expect(e.value < 0.45 || e.value > 0.6).toBe(true);

  await page.goto("/compliance");
  await expect(page.getByRole("heading", { level: 1, name: "Compliance" })).toBeVisible();
  await expect(page.getByText(limit.name)).toBeVisible();

  // EU register: a statement is signed with the member's personal key and verifies in the browser.
  const att = await page.request.post("/api/v1/compliance/attestations", { headers: H, data: {
    requirementId: "cpr.dop", status: "met", note: `Declarations of performance issued for every mix (E2E ${Date.now()})`, evidenceUrl: "https://docs.example.org/dop",
  } });
  expect(att.status()).toBe(201);
  expect((await att.json()).keyId).toMatch(/^usr_/);
  // A statement cannot lift a check that comes from the data.
  expect((await page.request.post("/api/v1/compliance/attestations", { headers: H, data: { requirementId: "cpr.fpc", status: "met", note: "trust us, it is fine" } })).status()).toBe(422);
  await page.goto("/compliance?tab=register");
  const dop = page.locator("li", { hasText: "Declaration of performance for each mix" });
  await expect(dop.getByText("Met", { exact: true })).toBeVisible();
  await expect(dop.getByText("signature valid")).toBeVisible();

  // The month's report: a stale fingerprint is refused, the current one signs.
  const period = "2026-08";
  const st = await (await page.request.get(`/api/v1/compliance/report?period=${period}`)).json();
  expect(st.canSign).toBe(true);
  expect((await page.request.post("/api/v1/compliance/report/sign", { headers: H, data: { period, note: "", fingerprint: "0".repeat(64) } })).status()).toBe(409);
  expect((await page.request.post("/api/v1/compliance/report/sign", { headers: H, data: { period, note: "E2E", fingerprint: st.fingerprint } })).status()).toBe(201);
  expect((await (await page.request.get(`/api/v1/compliance/report?period=${period}`)).json()).state).toBe("signed");
  const pdf = await page.request.get(`/api/v1/reports/compliance?format=pdf&period=${period}`);
  expect(pdf.headers()["content-type"]).toBe("application/pdf");
  expect(pdf.headers()["x-report-fingerprint"]).toBe(st.fingerprint);
  // A month that has not ended cannot be signed.
  expect((await page.request.post("/api/v1/compliance/report/sign", { headers: H, data: { period: new Date().toISOString().slice(0, 7), fingerprint: st.fingerprint } })).status()).toBe(422);

  // Data protection: own export; erasure only after removal.
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  const exp = await page.request.get("/api/v1/auth/me/export");
  expect(exp.ok()).toBe(true);
  expect((await exp.json()).data.account.id).toBe(me.user.id);
  expect((await page.request.post(`/api/v1/compliance/privacy/${me.user.id}/erase`, { headers: H })).status()).toBe(409);

  // Reporters inside a company read but cannot change limits; the oversight reporter reads any company.
  const observer = await browser.newPage();
  await signIn(observer, ORG, (u) => u.role === "viewer");
  expect((await observer.request.post("/api/v1/compliance/limits", { headers: H, data: { name: "x", source: "records", measure: "dosage", basis: "instant", max: 1 } })).status()).toBe(403);
  expect((await observer.request.get("/api/v1/compliance/register")).ok()).toBe(true);
  expect((await observer.request.get("/api/v1/compliance/privacy")).status()).toBe(403);
  const reporter = await browser.newPage();
  await signIn(reporter, "org_programme-office", () => true);
  const reg = await (await reporter.request.get(`/api/v1/compliance/register?org=${ORG}`)).json();
  expect(reg.org.id).toBe(ORG);
  expect(reg.canAttest).toBe(false);

  expect((await page.request.delete(`/api/v1/compliance/limits/${limit.id}`, { headers: H })).status()).toBe(204);
  expect(errors).toEqual([]);
});
