import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test("records are sealed on the ledger and the explorer shows the blocks", async ({ page }) => {
  await signIn(page, "org_sensifai");
  // The worker seals continuously; wait until the platform has seals on chain.
  await expect.poll(async () => {
    const s = await (await page.request.get("/api/v1/ledger/summary")).json();
    return s.online && s.platform.seals > 0 && s.chain.height > 3;
  }, { timeout: 180_000, intervals: [3000] }).toBeTruthy();

  await page.goto("/ledger");
  await expect(page.getByText("Block height")).toBeVisible();
  // The page opens on blocks by organisation; a seal's detail opens from the Seals tab.
  await page.getByRole("button", { name: "Seals", exact: true }).click();
  await page.locator("tbody tr").first().click();
  await expect(page.getByText("root matches the chaincode's")).toBeVisible();

  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Blocks" }).click();
  await expect(page.locator("tbody tr").first()).toBeVisible();
  await page.getByRole("button", { name: "Signing keys" }).click();
  await expect(page.getByText("Signing keys on the ledger")).toBeVisible();
});

test("an organisation sees only its own seals and masked blocks", async ({ page }) => {
  await signIn(page, "org_firmes-del-sur");
  const seals = await (await page.request.get("/api/v1/ledger/seals")).json();
  for (const s of seals) expect(s.mine).toBeGreaterThan(0);
  const blocks = await page.request.get("/api/v1/ledger/blocks");
  expect(blocks.status()).toBe(403);
});
