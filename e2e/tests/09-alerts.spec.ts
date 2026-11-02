import { test, expect } from "@playwright/test";
import { createHmac } from "node:crypto";
import { createServer } from "node:http";
import { signIn, now } from "./helpers";

const H = { "X-Requested-With": "roadpass" };
// The test container shares the app's network: Mailpit by name, our receiver on loopback.
const MAILPIT = process.env.MAILPIT_URL ?? "http://mailpit:8025";

async function mailTo(to: string, subject: RegExp, timeoutMs = 45_000): Promise<string> {
  const end = Date.now() + timeoutMs;
  while (Date.now() < end) {
    const r = await fetch(`${MAILPIT}/api/v1/search?query=${encodeURIComponent(`to:${to}`)}`);
    const m = (await r.json()).messages?.find((x: { Subject: string }) => subject.test(x.Subject));
    if (m) return m.Subject as string;
    await new Promise((res) => setTimeout(res, 1500));
  }
  throw new Error(`no mail to ${to} matching ${subject}`);
}

test("alerts: a verified e-mail channel, a signed webhook, a rule that fires, in-app and e-mail delivery, acknowledgement", async ({ page }) => {
  test.setTimeout(150_000);
  await signIn(page, "org_nuevafilm");
  const to = `ops+${Date.now()}@nuevafilm.example`;

  // E-mail channels prove the address with a code.
  const ch = await (await page.request.post("/api/v1/alerts/channels", { headers: H, data: { type: "email", name: "Ops e-mail", config: { to } } })).json();
  expect(ch.channel.verified).toBe(false);
  expect((await page.request.post(`/api/v1/alerts/channels/${ch.channel.id}/test`, { headers: H })).status()).toBe(502); // unverified: refused
  expect((await page.request.post(`/api/v1/alerts/channels/${ch.channel.id}/verify`, { headers: H })).status()).toBe(204);
  const code = (await mailTo(to, /verification code/)).match(/(\d{6})/)![1];
  expect((await page.request.post(`/api/v1/alerts/channels/${ch.channel.id}/verify/confirm`, { headers: H, data: { code: code === "000000" ? "111111" : "000000" } })).status()).toBe(422);
  expect((await page.request.post(`/api/v1/alerts/channels/${ch.channel.id}/verify/confirm`, { headers: H, data: { code } })).status()).toBe(204);

  // Webhooks are signed: the receiver checks t=…,v1=HMAC-SHA256(secret, "t.body").
  let received: { sig: string; body: string } | null = null;
  const srv = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => { received = { sig: String(req.headers["x-roadpass-signature"]), body }; res.end("ok"); });
  }).listen(9911, "127.0.0.1");
  try {
    const wh = await (await page.request.post("/api/v1/alerts/channels", { headers: H, data: { type: "webhook", name: "ERP", config: { url: "http://127.0.0.1:9911/hook" } } })).json();
    const secret = wh.secret.signingSecret as string;
    expect(secret).toMatch(/^whsec_/);
    expect((await page.request.post(`/api/v1/alerts/channels/${wh.channel.id}/test`, { headers: H })).status()).toBe(204);
    await expect.poll(() => received, { timeout: 10_000 }).not.toBeNull();
    const { sig, body } = received!;
    const [t, v1] = sig.split(",").map((p) => p.split("=")[1]);
    expect(v1).toBe(createHmac("sha256", secret).update(`${t}.${body}`).digest("hex"));
    expect(JSON.parse(body).event).toBe("test");
  } finally {
    srv.close();
  }

  // A rule that fires on the next engine tick.
  const org = await (await page.request.get("/api/v1/org")).json();
  const rec = await page.request.post("/api/v1/records", { headers: H, data: { records: [{ type: "recycled_lot", ref: `ALERT-E2E-${Date.now()}`, siteId: org.sites[0].id,
    occurredAt: now(), massKg: 8000, data: { form: "agglomerate", mfi_g10min: 1.1, moisture_pct: 0.7 } }] } });
  expect(rec.status()).toBe(201);
  const name = `E2E lot watch ${Date.now()}`;
  const rule = await page.request.post("/api/v1/alerts/rules", { headers: H, data: {
    name, kind: "metric", severity: "critical", audience: ["role:admin"], channelIds: [ch.channel.id], forMinutes: 0, repeatMinutes: 0, notifyResolved: true, escalation: [], enabled: true,
    params: { query: { source: "records", types: ["recycled_lot"], measure: "records", agg: "count" }, windowMinutes: 60, op: ">=", value: 1 } } });
  expect(rule.status()).toBe(201);
  const ruleId = (await rule.json()).id;

  // Delivered in-app, live under the bell…
  await page.goto("/alerts");
  await expect(page.getByRole("button", { name: /Notifications, \d+ unread/ })).toBeVisible({ timeout: 60_000 });
  await expect(page.getByText(name).first()).toBeVisible({ timeout: 30_000 });
  // …and by e-mail.
  await mailTo(to, new RegExp(name));

  // Acknowledge from the Active tab.
  const card = page.locator(".card", { hasText: name }).first();
  await card.getByRole("button", { name: "Acknowledge" }).click();
  await expect(card.getByText(/acknowledged by/)).toBeVisible();

  // Members cannot change rules; a viewer cannot act on alerts.
  const viewer = await page.context().browser()!.newPage();
  await signIn(viewer, "org_nuevafilm", (u) => u.role === "viewer");
  expect((await viewer.request.post("/api/v1/alerts/rules", { headers: H, data: {} })).status()).toBe(403);
  expect((await viewer.request.post("/api/v1/alerts/action", { headers: H, data: { ruleId, key: "value", action: "ack" } })).status()).toBe(403);
  expect((await (await viewer.request.get("/api/v1/alerts/active")).json()).some((a: { ruleId: string }) => a.ruleId === ruleId)).toBe(true);

  expect((await page.request.delete(`/api/v1/alerts/rules/${ruleId}`, { headers: H })).status()).toBe(204);
});

test("notifications: preferences, the service worker and the push key", async ({ page, request }) => {
  await signIn(page, "org_nuevafilm");
  const prefs = { browser: true, email: "daily", minSeverity: "warning", quietFrom: 22 * 60, quietTo: 7 * 60, muted: ["billing"] };
  expect((await page.request.put("/api/v1/notifications/prefs", { headers: H, data: prefs })).status()).toBe(204);
  expect((await (await page.request.get("/api/v1/notifications/prefs")).json()).prefs).toMatchObject(prefs);
  expect((await page.request.put("/api/v1/notifications/prefs", { headers: H, data: { ...prefs, email: "hourly" } })).status()).toBe(422);
  const key = (await (await page.request.get("/api/v1/notifications/push/key")).json()).publicKey as string;
  expect(key).toMatch(/^[A-Za-z0-9_-]{80,}$/); // an uncompressed P-256 point, base64url
  // The body browsers send (PushSubscription.toJSON) is accepted as is, expirationTime included.
  const sub = { endpoint: `https://push.example.org/send/e2e-${Date.now()}`, expirationTime: null, keys: { p256dh: "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM", auth: "tBHItJI5svbpez7KI4CCXg" } };
  const added = await page.request.post("/api/v1/notifications/push", { headers: H, data: sub });
  expect(added.status()).toBeLessThan(300);
  const sw = await request.get("/sw.js");
  expect(sw.ok()).toBeTruthy();
  expect(sw.headers()["cache-control"]).toBe("no-cache");
  expect(await sw.text()).toContain("showNotification");
  await page.goto("/settings?tab=notifications");
  await expect(page.getByRole("heading", { name: "Notifications" })).toBeVisible();
  const t = await (await page.request.post("/api/v1/notifications/test", { headers: H })).json();
  expect(t.inApp).toBe(1);
});
