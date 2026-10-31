import { defineConfig, devices } from "@playwright/test";

// End-to-end tests against the full stack (deploy/compose.yml): Postgres,
// ClickHouse, Redis, a real Hyperledger Fabric network, the service and the worker.
export default defineConfig({
  testDir: "./tests",
  timeout: 90_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: process.env.BASE_URL ?? "http://localhost:8080",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    viewport: { width: 1440, height: 960 },
  },
  projects: [{
    name: "chromium",
    use: {
      ...devices["Desktop Chrome"],
      viewport: { width: 1440, height: 960 },
    },
  }],
});
