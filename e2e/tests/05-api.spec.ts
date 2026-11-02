import { test, expect } from "@playwright/test";
import { signIn, now } from "./helpers";

test("partners submit by JSON, NGSI-LD and CSV with an API key", async ({ page, playwright, baseURL }) => {
  await signIn(page, "org_sel-pinto");
  const keyRes = await page.request.post("/api/v1/org/api-keys", {
    data: { name: "e2e", scopes: ["records:read", "records:write"], duties: ["produce"] },
    headers: { "X-Requested-With": "roadpass" },
  });
  expect(keyRes.status()).toBe(201);
  const secret = (await keyRes.json()).secret as string;
  const api = await playwright.request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } });
  const ref = `E2E-${Date.now()}`;

  const batch = { records: [{ type: "reject_lot", ref, siteId: "site_sel-pinto_1", occurredAt: now(), massKg: 18000,
    data: { source: "municipal_packaging", ldpe_share_pct: 74, avoided_route: "landfill", moisture_pct: 10, contamination_pct: 9 } }] };
  const json = await api.post("/api/v1/records", { headers: { "Idempotency-Key": ref }, data: batch });
  expect(json.status()).toBe(201);
  // A client retry (same key, same body) replays the first result instead of creating a duplicate.
  const replay = await api.post("/api/v1/records", { headers: { "Idempotency-Key": ref }, data: batch });
  expect(replay.status()).toBe(200);
  expect(replay.headers()["idempotent-replay"]).toBe("true");

  // The key holds only the produce duty: shipping is refused.
  const ship = await api.post("/api/v1/records", { data: { records: [{ type: "shipment", ref: ref + "-S", occurredAt: now(), massKg: 1,
    inputs: [{ id: "PRL-x", massKg: 1 }], data: { to_site_id: "site_sel-pinto_1", distance_km: 10 } }] } });
  expect(ship.status()).toBe(422);

  const ld = await api.post("/ngsi-ld/v1/entities", {
    headers: { "Content-Type": "application/ld+json" },
    data: { "@context": `${baseURL}/ngsi-ld/v1/context.jsonld`, type: "PlasticRejectLot", reference: ref + "-LD",
      dateObserved: now(), mass: { type: "Property", value: 17.5, unitCode: "TNE" },
      atSite: { type: "Relationship", object: "urn:ngsi-ld:Site:site_sel-pinto_1" },
      source: "commercial_film", ldpeSharePct: 80, avoidedRoute: "incineration", moisturePct: 9, contaminationPct: 8 },
  });
  expect(ld.status()).toBe(201);
  const loc = ld.headers()["location"];
  const back = await api.get(`${loc}?options=keyValues`);
  expect(back.ok()).toBeTruthy();
  expect((await back.json()).mass).toBe(17500);

  const csv = "ref;site_id;occurred_at;mass_kg;source;ldpe_share_pct;avoided_route\n" +
    `${ref}-C1;site_sel-pinto_1;2026-10-01 10:00;19000;municipal_packaging;72,5;landfill\n`;
  const up = await api.post("/api/v1/imports?type=reject_lot", { multipart: { file: { name: "lots.csv", mimeType: "text/csv", buffer: Buffer.from(csv) } } });
  expect(up.status()).toBe(201);
});
