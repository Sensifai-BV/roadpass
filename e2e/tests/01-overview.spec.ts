import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test("sign-in page offers demo users grouped by role", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await expect(page.getByText("Demo · sign in as")).toBeVisible();
  await expect(page.getByText("Platform operator").first()).toBeVisible();
});

test("the reporter's overview shows the chain at platform scale", async ({ page }) => {
  await signIn(page, "org_programme-office", () => true);
  await page.goto("/");
  await expect(page.getByText("Platform-wide figures")).toBeVisible();
  for (const label of ["Recycled plastic laid", "CO₂e avoided", "Asphalt with additive", "Road surfaced"]) {
    await expect(page.getByText(label)).toBeVisible();
  }
  await expect(page.getByRole("heading", { name: "The journey, this period" })).toBeVisible();
  // figures come from ClickHouse over the seeded dataset: never zero
  await expect(page.locator(".hero-num").first()).not.toHaveText("0");
});

test("each page renders without errors", async ({ page }) => {
  await signIn(page, "org_sensifai");
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  for (const [path, heading] of [["/chain", "Supply chain"], ["/passports", "Passports"], ["/roads", "Roads"], ["/quality", "Data checks"],
    ["/carbon", "Carbon & impact"], ["/plants", "Plant data"], ["/ledger", "Ledger"], ["/data", "Data in"], ["/settings", "Settings"],
    ["/settings?tab=organisation", "Settings"], ["/settings?tab=team", "Settings"], ["/settings?tab=security", "Settings"],
    ["/status", "Chain status"], ["/", "Platform overview"], ["/platform/organisations", "Organisations"],
    ["/platform/users", "Users"], ["/platform/billing", "Billing"], ["/platform/system", "System health"], ["/platform/audit", "Audit log"],
    ["/glossary", "Glossary"]] as const) {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 }).filter({ hasText: heading })).toBeVisible();
  }
  expect(errors).toEqual([]);
});
