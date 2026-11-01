import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test("a road-section passport traces its plastic and verifies in the browser", async ({ page }) => {
  await signIn(page, "org_sensifai");
  await page.goto("/passports?type=road_section&status=ok");
  const first = page.locator("[role=link]").first();
  await expect(first).toBeVisible();
  await first.click();
  await expect(page.getByText("Road section passport")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Journey" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Where the plastic came from" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Lineage" })).toBeVisible();

  // The newest section may still be waiting for the worker: verify once its seal is
  // on the ledger, or the panel rightly reports the unchecked steps instead.
  const id = page.url().split("/passports/")[1];
  await expect.poll(async () => {
    const r = await page.request.get(`/api/v1/passports/${id}`);
    return r.ok() ? Boolean((await r.json()).trust?.seal?.txId) : false;
  }, { timeout: 30_000 }).toBe(true);
  await page.reload();
  await page.getByRole("button", { name: "Verify in this browser" }).click();
  await expect(page.getByText("Fingerprint recomputed")).toBeVisible();
  await expect(page.getByText(/^Signed by /)).toBeVisible();
  // Once the worker has sealed it, the Merkle path is replayed in the page too.
  await expect(page.getByText("Everything checks out.")).toBeVisible({ timeout: 20_000 });
});

test("the public passport is reached by a random share link, never by the passport id", async ({ page, request }) => {
  const users = await (await request.get("/api/v1/auth/demo-users")).json();
  expect(users.length).toBeGreaterThan(10);
  await signIn(page, "org_sensifai");
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  const res = await page.request.get("/api/v1/records?limit=200");
  const mine = (await res.json()).items.find((r: { orgId: string; type: string }) => r.orgId === me.org.id && ["road_section", "asphalt_lot", "recycled_lot"].includes(r.type));
  test.skip(!mine, "this organisation made no passport to share");
  const id = mine.id as string;
  const W = { "X-Requested-With": "roadpass" };

  // The passport id is not a public address.
  expect((await request.get(`/api/v1/public/passports/${id}`)).status()).toBe(404);

  const share = await (await page.request.post(`/api/v1/passports/${id}/share`, { headers: W })).json();
  expect(share.token).not.toContain(id);
  expect(share.token.length).toBeGreaterThanOrEqual(20);
  expect(share.url).toContain(`/p/${share.token}`);
  // Asking again returns the same link.
  expect((await (await page.request.post(`/api/v1/passports/${id}/share`, { headers: W })).json()).token).toBe(share.token);

  const pub = await request.get(`/api/v1/public/passports/${share.token}`);
  expect(pub.ok()).toBeTruthy();
  const body = await pub.json();
  expect(body.access).toBe("public");
  expect(body.record).toBeUndefined();
  expect(body.nodes).toBeUndefined();
  expect(body.stages.length).toBeGreaterThan(1);
  await page.goto(`/p/${share.token}`);
  await expect(page.getByText("Digital product passport")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Journey" })).toBeVisible();

  // A new link replaces the old one at once; stopping sharing closes the page.
  const fresh = await (await page.request.post(`/api/v1/passports/${id}/share/rotate`, { headers: W })).json();
  expect(fresh.token).not.toBe(share.token);
  expect((await request.get(`/api/v1/public/passports/${share.token}`)).status()).toBe(404);
  expect((await request.get(`/api/v1/public/passports/${fresh.token}`)).ok()).toBeTruthy();
  expect((await page.request.delete(`/api/v1/passports/${id}/share`, { headers: W })).status()).toBe(204);
  expect((await request.get(`/api/v1/public/passports/${fresh.token}`)).status()).toBe(404);
});
