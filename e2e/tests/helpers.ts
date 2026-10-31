import { expect, type APIRequestContext, type Page } from "@playwright/test";

export interface DemoUser { userId: string; name: string; role: string; duties: string[]; orgId: string; org: string; kind: string }

export async function demoUsers(request: APIRequestContext): Promise<DemoUser[]> {
  const r = await request.get("/api/v1/auth/demo-users");
  expect(r.ok()).toBeTruthy();
  return r.json();
}

/** Signs the page in as a demo user picked by organisation and a predicate. */
export async function signIn(page: Page, orgId: string, pick: (u: DemoUser) => boolean = (u) => u.role === "admin") {
  const users = await demoUsers(page.request);
  const u = users.find((x) => x.orgId === orgId && pick(x));
  if (!u) throw new Error(`no demo user in ${orgId}`);
  const r = await page.request.post("/api/v1/auth/demo-login", {
    data: { userId: u.userId }, headers: { "X-Requested-With": "roadpass" },
  });
  expect(r.ok()).toBeTruthy();
  return u;
}

export const withDuty = (d: string) => (u: DemoUser) => u.role === "editor" && u.duties.includes(d);

export const now = () => new Date().toISOString().replace(/\.\d+Z$/, "Z");
