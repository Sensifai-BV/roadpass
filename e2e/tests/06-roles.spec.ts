import { test, expect } from "@playwright/test";
import { signIn, now } from "./helpers";

test("the reporter sees the state of the whole chain and writes nothing", async ({ page }) => {
  await signIn(page, "org_programme-office", () => true);
  await page.goto("/status");
  await expect(page.getByRole("heading", { level: 1, name: "Chain status" })).toBeVisible();
  for (const label of ["In transit", "To release", "Blocked", "Sealed"]) {
    await expect(page.getByText(label).first()).toBeVisible();
  }
  // One tile per step narrows the list to that step.
  await page.getByRole("button", { name: /^Recycle/ }).click();
  await expect(page.getByText("Recycle · Recyclers")).toBeVisible();
  await expect(page.getByRole("link", { name: "My work" })).toHaveCount(0);
  const w = await page.request.post("/api/v1/records", {
    data: { records: [{ type: "reject_lot", ref: "X", occurredAt: now(), massKg: 1, data: {} }] },
    headers: { "X-Requested-With": "roadpass" },
  });
  expect(w.status()).toBe(403);
});

test("the platform manager onboards an organisation; its admin joins; it is sealed only once verified", async ({ page, browser }) => {
  const name = `Northfield Recycling E2E ${Date.now()}`;
  const email = `admin+${Date.now()}@northfield.example`;
  await signIn(page, "org_sensifai");
  await page.goto("/");
  // The platform manager's navigation is about the platform, not the chain.
  await expect(page.getByRole("heading", { level: 1, name: "Platform overview" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Passports" })).toHaveCount(0);
  await page.getByRole("link", { name: "Organisations" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Organisations" })).toBeVisible();
  await page.getByRole("button", { name: "New organisation" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Company name").fill(name);
  await dialog.getByRole("button", { name: "Recycler" }).click();
  await dialog.getByLabel("Country").selectOption("NL");
  await dialog.getByLabel("First admin's e-mail").fill(email);
  await dialog.getByRole("button", { name: "Create and invite" }).click();
  const link = (await page.getByRole("dialog").locator(".mono").textContent())!.trim();
  expect(link).toContain("/join/");

  // The invited admin joins in their own browser.
  const ctx = await browser.newContext();
  const admin = await ctx.newPage();
  await admin.goto(new URL(link).pathname);
  await admin.getByLabel("Your name").fill("Femke de Vries");
  await admin.getByLabel("Choose a password").fill("correct-horse-battery-staple");
  await admin.getByRole("button", { name: "Join" }).click();
  await expect(admin.getByText("Awaiting verification.")).toBeVisible();
  const me = await (await admin.request.get("/api/v1/auth/me")).json();
  expect(me.org.name).toBe(name);
  expect(me.org.status).toBe("pending");
  expect(me.user.role).toBe("admin");

  // There is no public sign-up any more.
  expect([404, 405]).toContain((await admin.request.post("/api/v1/auth/signup", { data: {}, headers: { "X-Requested-With": "roadpass" } })).status());

  // The manager verifies it; the company now has a key the ledger will register.
  await page.goto("/platform/organisations");
  const row = page.locator("tr", { hasText: name });
  await row.getByRole("button", { name: "Verify" }).click();
  await expect(row.getByText("verified")).toBeVisible();
  const after = await (await admin.request.get("/api/v1/auth/me")).json();
  expect(after.org.status).toBe("verified");
  await ctx.close();
});

test("every member signs with a personal key held in the vault", async ({ page }) => {
  await signIn(page, "org_sel-pinto", (u) => u.role === "editor" && u.duties.includes("produce"));
  const keys = await (await page.request.get("/api/v1/auth/me/keys")).json();
  const active = keys.keys.find((k: { status: string }) => k.status === "active");
  expect(active.custody).toBe("vault");
  const res = await page.request.post("/api/v1/records", {
    headers: { "X-Requested-With": "roadpass" },
    data: { records: [{ type: "reject_lot", ref: `KEY-${Date.now()}`, siteId: "site_sel-pinto_1", occurredAt: now(), massKg: 15000, note: "Signed by me",
      data: { source: "municipal_packaging", ldpe_share_pct: 70, avoided_route: "landfill", moisture_pct: 9, contamination_pct: 8 } }] },
  });
  expect(res.status()).toBe(201);
  const id = (await res.json()).results[0].id;
  const proof = await (await page.request.get(`/api/v1/records/${id}/proof`)).json();
  expect(proof.signature.valid).toBe(true);
  expect(proof.actorSignature.valid).toBe(true);
  expect(proof.actorSignature.keyId).toBe(active.id);
  expect(proof.hashDoc.note).toBe("Signed by me");
});
