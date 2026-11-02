import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test("anyone can verify a fingerprint without an account", async ({ page, browser }) => {
  await signIn(page, "org_sensifai");
  const recs = await (await page.request.get("/api/v1/records?type=asphalt_lot&status=ok&to=2024-06-30&limit=1")).json();
  const hash = recs.items[0].contentHash as string;
  const anon = await browser.newPage();
  await anon.goto("/verify");
  await anon.getByLabel("Fingerprint").fill(hash);
  await anon.getByRole("button", { name: "Verify" }).click();
  await expect(anon.getByText("A ROADPASS record has this fingerprint")).toBeVisible();
  await expect(anon.getByText("Merkle path replayed in this browser reaches the seal root")).toBeVisible();
});
