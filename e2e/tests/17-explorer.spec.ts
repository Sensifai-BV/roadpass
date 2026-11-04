import { test, expect, request as pwRequest, type APIRequestContext } from "@playwright/test";
import { demoUsers, signIn } from "./helpers";

// The public explorer (/explorer): anyone can browse every block and transaction. A private organisation
// (the default) shows hashes only; an organisation whose admin switched "public ledger" on shows its details,
// supply chain and records. The demo workspace (Capital Paving) is public from the start.
//
// Needs a stack started with DEMO_TENANT=1 and DEMO_MODE=1 (the simulated organisations).
test.describe.configure({ mode: "serial" });

const BASE = process.env.BASE_URL ?? "http://localhost:8080";
const H = { "X-Requested-With": "roadpass" };
const DEMO_ORG = "org_capital-paving-demo";
/** Every block of the chain, newest first, read page by page like the explorer does. */
async function allBlocks(request: APIRequestContext) {
  const out: { number: number; txs: Record<string, any>[] }[] = [];
  let before = 0;
  for (let i = 0; i < 20; i++) {
    const r = await request.get(`/api/v1/public/explorer/blocks?limit=50${before ? `&before=${before}` : ""}`);
    const page = (await r.json()).blocks;
    if (!page.length) break;
    out.push(...page);
    before = page[page.length - 1].number;
  }
  return out;
}
let privateOrg = "";
let privateSeal = { id: "", blockNumber: 0, merkleRoot: "" };

test("the landing navigation has an Explorer link that opens the public explorer", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Explorer" }).click();
  await expect(page).toHaveURL(/\/explorer$/);
  await expect(page.getByRole("heading", { name: /Explore the blockchain ledger/ })).toBeVisible();
});

test("anonymously, only the demo workspace is listed by default", async () => {
  const anon = await pwRequest.newContext({ baseURL: BASE });
  const orgs = await (await anon.get("/api/v1/public/explorer/orgs")).json();
  expect(orgs.map((o: { id: string }) => o.id)).toEqual([DEMO_ORG]);
  const sum = await (await anon.get("/api/v1/public/explorer/summary")).json();
  expect(sum.orgs).toBe(1);
  expect(sum.seals).toBeGreaterThan(0);
});

test("every block is listed, private organisations as hashes only", async ({ request }) => {
  const txs = (await allBlocks(request)).flatMap((b) => b.txs);
  const priv = txs.filter((t: { public: boolean; merkleRoot?: string }) => !t.public && t.merkleRoot);
  const pub = txs.filter((t: { public: boolean; sealId?: string }) => t.public && t.sealId);
  expect(txs.some((t: { public: boolean; keyId?: string }) => !t.public && !t.keyId)).toBeTruthy();
  expect(priv.length).toBeGreaterThan(0);
  expect(pub.length).toBeGreaterThan(0);
  for (const t of priv) {
    for (const k of ["orgId", "orgName", "sealId", "records", "provenance", "keyId"]) expect(t[k], `private tx leaks ${k}`).toBeUndefined();
  }
  expect(JSON.stringify(priv)).not.toMatch(/org_|SEAL-/);
  for (const t of pub) expect(t.orgName).toContain("Capital Paving");
});

test("blocks, seal, supply chain and organisation of the demo workspace can be browsed in the browser", async ({ page, request }) => {
  await page.goto("/explorer");
  await expect(page.getByRole("heading", { name: "Latest blocks" })).toBeVisible();
  await expect(page.getByText("Private organisation").first()).toBeVisible();
  await expect(page.getByRole("link", { name: "Capital Paving" }).first()).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("explorer-home.png"), fullPage: true });

  const blocks = await allBlocks(request);
  const sealed = blocks.find((b) => b.txs.some((t) => t.public && t.sealId))!;
  await page.goto(`/explorer/block/${sealed.number}`);
  await expect(page).toHaveURL(/\/explorer\/block\/\d+$/);
  await expect(page.getByRole("heading", { name: "Transactions" })).toBeVisible();
  await expect(page.getByText("SealBatch").first()).toBeVisible();
  await expect(page.getByText("valid", { exact: true }).first()).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("explorer-block.png"), fullPage: true });

  await page.locator("main a[href^='/explorer/seal/']").first().click();
  await expect(page).toHaveURL(/\/explorer\/seal\//);
  await expect(page.getByText("the chaincode holds the same root")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Supply-chain records" })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("explorer-seal.png"), fullPage: true });

  await page.getByRole("link", { name: "Capital Paving" }).first().click();
  await expect(page).toHaveURL(/\/explorer\/org\//);
  await expect(page.getByRole("heading", { name: "Supply chain" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Sites" })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("explorer-org.png"), fullPage: true });

  await page.goto(`/explorer/block/${sealed.number}`);
  await page.locator("main a[href^='/explorer/seal/']").first().click();
  await page.locator("main").getByRole("link", { name: "verify", exact: true }).first().click();
  await expect(page).toHaveURL(/\/verify\?hash=[0-9a-f]{64}/);
  await expect(page.getByText(/anchored|sealed|ledger/i).first()).toBeVisible();
});

test("a private block opens as hashes only in the browser", async ({ page, request }) => {
  const b = (await allBlocks(request)).find((x) => x.txs.some((t) => !t.public && t.merkleRoot))!;
  await page.goto(`/explorer/block/${b.number}`);
  await expect(page.getByText("Private organisation").first()).toBeVisible();
  await expect(page.getByText("Merkle root").first()).toBeVisible();
  await expect(page.locator("main a[href^='/explorer/seal/']")).toHaveCount(0);
  await page.screenshot({ path: test.info().outputPath("explorer-private-block.png"), fullPage: true });
});

test("search finds a block number and a Merkle root", async ({ page, request }) => {
  const blocks = await allBlocks(request);
  const pubTx = blocks.flatMap((b) => b.txs).find((t) => t.public && t.sealId)!;
  const privTx = blocks.flatMap((b) => b.txs.map((t) => ({ ...t, block: b.number }))).find((t) => !t.public && t.merkleRoot)!;
  await page.goto("/explorer");
  await page.getByLabel("Search the explorer").fill(String(blocks[0].number));
  await page.getByRole("button", { name: "Search" }).click();
  await expect(page).toHaveURL(new RegExp(`/explorer/block/${blocks[0].number}$`));

  await page.goto("/explorer");
  await page.getByLabel("Search the explorer").fill(pubTx.merkleRoot);
  await page.getByRole("button", { name: "Search" }).click();
  await expect(page).toHaveURL(/\/explorer\/seal\//); // public: its seal page

  await page.goto("/explorer");
  await page.getByLabel("Search the explorer").fill(privTx.merkleRoot);
  await page.getByRole("button", { name: "Search" }).click();
  await expect(page).toHaveURL(new RegExp(`/explorer/block/${privTx.block}$`)); // private: only its block

  await page.goto("/explorer");
  await page.getByLabel("Search the explorer").fill("not-a-thing");
  await page.getByRole("button", { name: "Search" }).click();
  await expect(page.getByRole("alert")).toContainText("Nothing public matches");
});

test("a private organisation's seal page and details are invisible to the public", async ({ request }) => {
  const users = await demoUsers(request);
  const cand = [...new Set(users.filter((u) => u.orgId !== DEMO_ORG && u.role === "admin").map((u) => u.orgId))];
  const adm = await pwRequest.newContext({ baseURL: BASE, extraHTTPHeaders: H });
  for (const id of cand) {
    const u = users.find((x) => x.orgId === id && x.role === "admin")!;
    const l = await adm.post("/api/v1/auth/demo-login", { data: { userId: u.userId } });
    if (!l.ok()) continue;
    const seals = await (await adm.get("/api/v1/ledger/seals?limit=5")).json();
    const s = seals.find((x: { status: string; blockNumber?: number }) => x.status === "sealed" && x.blockNumber);
    if (s) { privateOrg = id; privateSeal = s; break; }
  }
  expect(privateOrg, "a simulated organisation with a sealed seal").not.toBe("");
  const anon = await pwRequest.newContext({ baseURL: BASE });
  expect((await anon.get(`/api/v1/public/explorer/seals/${privateSeal.id}`)).status()).toBe(404);
  expect((await anon.get(`/api/v1/public/explorer/orgs/${privateOrg}`)).status()).toBe(404);
  const blk = await (await anon.get(`/api/v1/public/explorer/blocks/${privateSeal.blockNumber}`)).json();
  const tx = blk.txs.find((t: { merkleRoot?: string }) => t.merkleRoot === privateSeal.merkleRoot);
  expect(tx.public).toBe(false);
  expect(tx.orgName).toBeUndefined();
});

test("an organisation admin turns its public ledger on in settings, and off again", async ({ page, request }) => {
  const u = await signIn(page, privateOrg);
  expect(u.role).toBe("admin");
  await page.goto("/settings?tab=organisation");
  const sw = page.getByRole("switch", { name: "Show our ledger data in the public explorer" });
  await expect(sw).not.toBeChecked(); // default: private
  await page.screenshot({ path: test.info().outputPath("settings-private.png"), fullPage: true });
  await sw.check();
  await expect(page.getByText("public", { exact: true })).toBeVisible();

  const anon = await pwRequest.newContext({ baseURL: BASE });
  await expect.poll(async () => (await anon.get(`/api/v1/public/explorer/seals/${privateSeal.id}`)).status()).toBe(200);
  const pub = await (await anon.get("/api/v1/public/explorer/orgs")).json();
  expect(pub.map((o: { id: string }) => o.id)).toContain(privateOrg);
  const blk = await (await anon.get(`/api/v1/public/explorer/blocks/${privateSeal.blockNumber}`)).json();
  const tx = blk.txs.find((t: { merkleRoot?: string }) => t.merkleRoot === privateSeal.merkleRoot);
  expect(tx.public).toBe(true);
  expect(tx.orgName).toBe(u.org);

  // visible in a browser without signing in
  const visitor = await page.context().browser()!.newContext();
  const vp = await visitor.newPage();
  await vp.goto(`${BASE}/explorer/seal/${privateSeal.id}`);
  await expect(vp.getByText("the chaincode holds the same root")).toBeVisible();
  await vp.goto(`${BASE}/explorer/org/${privateOrg}`);
  await expect(vp.getByRole("heading", { name: "Supply chain" })).toBeVisible();
  await vp.screenshot({ path: test.info().outputPath("explorer-opted-in-org.png"), fullPage: true });
  await visitor.close();

  await page.reload();
  await page.getByRole("switch", { name: "Show our ledger data in the public explorer" }).uncheck();
  await expect.poll(async () => (await anon.get(`/api/v1/public/explorer/seals/${privateSeal.id}`)).status()).toBe(404);
  const again = await (await anon.get(`/api/v1/public/explorer/blocks/${privateSeal.blockNumber}`)).json();
  expect(again.txs.find((t: { merkleRoot?: string }) => t.merkleRoot === privateSeal.merkleRoot).public).toBe(false);
});

test("the demo workspace cannot be switched off", async ({ page }) => {
  const r = await pwRequest.newContext({ baseURL: BASE, extraHTTPHeaders: H });
  const l = await r.post("/api/v1/auth/login", { data: { email: "admin@capital-demo.com", password: "roadpass-demo-2026" } });
  expect(l.ok()).toBeTruthy();
  expect((await r.put("/api/v1/org/public-ledger", { data: { public: false } })).status()).toBe(403);
  void page;
});
