import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test("a passport downloads as PDF and Word, with its details and a link to the public page", async ({ page }) => {
  await signIn(page, "org_programme-office", () => true);
  const list = await (await page.request.get("/api/v1/records?type=road_section&limit=1")).json();
  const id = (list.items ?? list.records ?? list)[0].id as string;
  await page.goto(`/passports/${id}`);
  await page.getByRole("button", { name: "Download report" }).click();
  const [pdf] = await Promise.all([page.waitForEvent("download"), page.getByRole("menuitem", { name: /PDF/ }).click()]);
  expect(pdf.suggestedFilename()).toMatch(/^roadpass-passport-.*\.pdf$/);

  const docx = await page.request.get(`/api/v1/reports/passport?format=docx&id=${id}`);
  expect(docx.headers()["content-type"]).toContain("wordprocessingml");
  const json = await (await page.request.get(`/api/v1/reports/passport?format=json&id=${id}`)).json();
  // The QR link is a random share link when this organisation made the passport, and never the passport id.
  if (json.link) expect(json.link.url).not.toContain(id);
  expect(json.sections.map((s: { heading: string }) => s.heading)).toEqual(expect.arrayContaining(["Details", "Carbon", "Journey", "How to verify"]));
});
