import { test, expect, type Page } from "@playwright/test";
import { signIn, withDuty } from "./helpers";

// One lot through the recycler step, a different person per duty, all in the UI.
test.describe.configure({ mode: "serial" });

async function savedIds(page: Page): Promise<string[]> {
  const banner = page.getByText(/Saved and signed: /);
  await expect(banner).toBeVisible();
  return ((await banner.textContent()) ?? "").replace(/.*Saved and signed: /, "").split(", ").map((s) => s.trim());
}

async function fill(page: Page, label: string, value: string) {
  await page.getByLabel(label, { exact: false }).first().fill(value);
}

let rejectLot = "";
let shipment = "";
let recycledLot = "";

test("sorting plant records a reject lot and ships it to a partner site", async ({ page }) => {
  await signIn(page, "org_sel-pinto");
  await page.goto("/work");
  await page.getByRole("button", { name: /Record plastic reject lot/ }).click();
  await fill(page, "Mass (kg)", "19400");
  await page.getByLabel("Source").selectOption("municipal_packaging");
  await fill(page, "Film (LDPE) share", "76");
  await page.getByLabel("Would otherwise go to").selectOption("landfill");
  await fill(page, "Moisture", "12");
  await fill(page, "Contamination", "11");
  await page.getByRole("button", { name: "Save and sign" }).click();
  [rejectLot] = await savedIds(page);
  expect(rejectLot).toMatch(/^PRL-\d{2}-\d{6}$/);

  // Receivers come from the company's own address book, never from other customers.
  await page.getByLabel(`Select ${rejectLot}`).check();
  await page.getByRole("button", { name: /Ship 1 selected/ }).click();
  await expect(page.getByLabel("Receiving site").locator("option", { hasText: "Nuevafilm" })).toHaveCount(0);
  await page.getByRole("button", { name: "Add partner site" }).click();
  const add = page.getByRole("dialog", { name: /Add a partner site/ });
  const partner = `E2E Film Recycling ${Date.now()}`;
  await add.getByLabel("Company").fill(partner);
  await add.getByLabel("Site name").fill("Main plant");
  await add.getByLabel("Site type").selectOption("recycler");
  await add.getByRole("button", { name: "Add partner site" }).click();
  await expect(page.getByLabel("Receiving site")).toHaveValue(/^site_/);
  await expect(page.getByText(/recorded without a receiver signature/)).toBeVisible();
  await fill(page, "Distance (km)", "610");
  await page.getByRole("button", { name: "Record shipment" }).click();
  const banner = page.getByText(/Shipment SHP-/);
  await expect(banner).toBeVisible();
  shipment = ((await banner.textContent()) ?? "").match(/SHP-\d{2}-\d{6}/)![0];

  await page.goto(`/passports/${shipment}`);
  await expect(page.getByText(`Main plant · ${partner} (external)`)).toBeVisible();
});

test("recycler production turns received material into a recycled plastic lot", async ({ page }) => {
  await signIn(page, "org_nuevafilm", withDuty("produce"));
  await page.goto("/work");
  await page.getByLabel(/^Select PRL-/).first().check();
  await page.getByRole("button", { name: /Record recycled plastic lot from 1 input/ }).click();
  // The company has two sites that fit: the form asks which one (none is pre-chosen).
  const site = page.getByLabel("Site", { exact: true });
  await expect(site).toHaveValue("");
  await site.selectOption({ index: 1 });
  await page.getByLabel("Form").selectOption("agglomerate");
  await fill(page, "Melt flow index", "1.3");
  await fill(page, "Moisture", "0.9");
  await fill(page, "Electricity used", "7400");
  await page.getByRole("button", { name: "Save and sign" }).click();
  [recycledLot] = await savedIds(page);
  expect(recycledLot).toMatch(/^RPL-/);
});

test("recycler quality releases it", async ({ page }) => {
  await signIn(page, "org_nuevafilm", withDuty("release"));
  await page.goto("/work");
  const row = page.locator("li", { hasText: recycledLot });
  await row.getByRole("button", { name: "Release" }).click();
  // The lot has no lab test, so the decision says why it is released anyway.
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("No lab test recorded.", { exact: false })).toBeVisible();
  await dialog.getByLabel("Note").fill("Released on the supplier's certificate of analysis.");
  await dialog.getByRole("button", { name: "Sign release" }).click();
  await expect(page.getByText(`${recycledLot} released.`)).toBeVisible();
});

test("recycler dispatch moves it to its other site", async ({ page }) => {
  await signIn(page, "org_nuevafilm", withDuty("ship"));
  await page.goto("/work");
  await page.getByLabel(`Select ${recycledLot}`).check();
  await page.getByRole("button", { name: /Ship 1 selected/ }).click();
  const to = page.getByLabel("Receiving site");
  const own = await to.locator("optgroup[label='Our sites'] option").first().getAttribute("value");
  await to.selectOption(own!);
  await expect(page.getByText(/Your receiving team confirms/)).toBeVisible();
  await fill(page, "Distance (km)", "260");
  await page.getByRole("button", { name: "Record shipment" }).click();
  const banner = page.getByText(/Shipment SHP-/);
  await expect(banner).toBeVisible();
  shipment = ((await banner.textContent()) ?? "").match(/SHP-\d{2}-\d{6}/)![0];
});

test("recycler intake signs the receipt of the internal transfer", async ({ page }) => {
  await signIn(page, "org_nuevafilm", withDuty("receive"));
  await page.goto("/work");
  await expect(page.getByText("Intake").first()).toBeVisible();
  const row = page.locator("li", { hasText: shipment });
  await row.getByRole("button", { name: "Receive" }).click();
  await fill(page, "Weighbridge (kg)", "19350");
  await page.getByRole("button", { name: "Sign receipt" }).click();
  await expect(page.getByText(`Receipt for ${shipment} signed.`)).toBeVisible();
});

test("the passport shows every hand-off", async ({ page }) => {
  await signIn(page, "org_nuevafilm");
  await page.goto(`/passports/${recycledLot}`);
  await expect(page.getByText("Recycled plastic lot passport")).toBeVisible();
  await expect(page.getByText(`Released by quality: ${recycledLot}`)).toBeVisible();
  await expect(page.getByText(`Shipped ${recycledLot}`)).toBeVisible();
});

test("customers do not see each other's companies or partner sites", async ({ page }) => {
  await signIn(page, "org_sel-pinto");
  const orgs: { id: string; kind: string }[] = await (await page.request.get("/api/v1/orgs")).json();
  // No other sorting plant ever shares a record with this one.
  expect(orgs.filter((o) => o.kind === "sorting_plant").map((o) => o.id)).toEqual(["org_sel-pinto"]);
  const sites: { orgId: string; partner?: string }[] = await (await page.request.get("/api/v1/sites")).json();
  expect(sites.filter((s) => s.partner && s.orgId !== "org_sel-pinto")).toHaveLength(0);
  await signIn(page, "org_nuevafilm");
  const theirs: { orgId: string; partner?: string }[] = await (await page.request.get("/api/v1/sites")).json();
  expect(theirs.filter((s) => s.orgId === "org_sel-pinto" && s.partner)).toHaveLength(0);
});
