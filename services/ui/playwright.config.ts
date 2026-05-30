import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright configuration for UI e2e tests.
 *
 * Tests run against a live Next.js + NGINX deployment.
 * Set PLAYWRIGHT_BASE_URL to point at the running instance:
 *   - Kind cluster:  http://localhost:<port-forward-port>
 *   - Local dev:     http://localhost:3000
 *
 * The default assumes a local dev server on port 3000.
 */
const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000";

export default defineConfig({
  testDir: "./test/e2e",
  outputDir: "test-results",
  // Use a single worker by default so tests run sequentially. This is required
  // when running against a kubectl port-forward (local Kind dev), which cannot
  // handle the burst of concurrent TCP connections that multiple Chromium
  // workers produce. CI sets PLAYWRIGHT_WORKERS=5 to parallelise against the
  // local Next.js server where there is no such constraint.
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.PLAYWRIGHT_WORKERS ? parseInt(process.env.PLAYWRIGHT_WORKERS, 10) : 1,
  reporter: process.env.CI ? "github" : "list",
  timeout: 30_000,
  use: {
    baseURL,
    trace: "on-first-retry",
    headless: true,
    extraHTTPHeaders: {
      Accept: "application/json",
    },
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
