import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

// The public landing and contact pages: a visitor writes to the platform; the
// platform manager finds the message in its own section and closes it.
test("a visitor sees the landing page, sends a message, and the platform manager handles it", async ({ page, browser }) => {
  const stamp = Date.now();
  const ctx = await browser.newContext();
  const visitor = await ctx.newPage();

  // Landing: public, with the contact link in the navbar and the footer.
  await visitor.goto("/");
  await expect(visitor.getByRole("heading", { level: 1 })).toContainText("Every road knows");
  await expect(visitor.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Contact" })).toBeVisible();
  await expect(visitor.getByRole("contentinfo").getByRole("link", { name: "Contact us" })).toBeVisible();

  // Contact form: invalid input is refused with field errors; valid input is stored.
  await visitor.getByRole("contentinfo").getByRole("link", { name: "Contact us" }).click();
  await expect(visitor).toHaveURL(/\/contact/);
  await visitor.getByRole("button", { name: "Send message" }).click();
  await expect(visitor.getByText("Enter a valid e-mail address.")).toBeVisible();
  await visitor.getByLabel("Your name").fill(`Visitor ${stamp}`);
  await visitor.getByLabel("Work e-mail").fill(`visitor+${stamp}@example.com`);
  await visitor.getByLabel("Organisation").fill("Example Roads BV");
  await visitor.getByLabel("What is it about?").selectOption("pilot");
  await visitor.getByLabel("Message").fill(`We would like to plan a pilot on our network (${stamp}).`);
  await visitor.getByRole("button", { name: "Send message" }).click();
  await expect(visitor.getByRole("heading", { name: "Message sent" })).toBeVisible();

  // The platform manager has a Messages section with the message in it.
  await signIn(page, "org_sensifai");
  await page.goto("/platform/messages");
  await expect(page.getByRole("heading", { name: "Messages", level: 1 })).toBeVisible();
  const row = page.getByTestId("contact-message").filter({ hasText: `Visitor ${stamp}` });
  await expect(row).toBeVisible();
  await expect(row).toContainText("Example Roads BV");
  await expect(row).toContainText("Pilot");
  await expect(row.getByRole("link", { name: `visitor+${stamp}@example.com` })).toBeVisible();

  // Handling moves it out of the open list, and it can be reopened.
  await row.getByRole("button", { name: "Mark handled" }).click();
  await expect(page.getByTestId("contact-message").filter({ hasText: `Visitor ${stamp}` })).toHaveCount(0);
  await page.getByRole("group", { name: "Show" }).getByRole("button", { name: "Handled" }).click();
  const handled = page.getByTestId("contact-message").filter({ hasText: `Visitor ${stamp}` });
  await expect(handled).toBeVisible();
  await handled.getByRole("button", { name: "Reopen" }).click();
  await page.getByRole("group", { name: "Show" }).getByRole("button", { name: "Open", exact: true }).click();
  await expect(page.getByTestId("contact-message").filter({ hasText: `Visitor ${stamp}` })).toBeVisible();

  // Only the platform manager can read messages.
  expect((await visitor.request.get("/api/v1/platform/contact")).status()).toBe(401);
  await ctx.close();
});
