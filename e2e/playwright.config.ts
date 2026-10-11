import { defineConfig } from "@playwright/test";

/**
 * Browser E2E — the REAL application stack (v1.4.0).
 *
 * The suite exercises, through a real browser:
 *   the real built frontend (web/static, the same assets the desktop
 *   shell embeds) + the real Go server (headless build of THIS source
 *   tree) + the real HTTP API + the real WebSocket + the real session
 *   lifecycle + REAL native-engine generation (shtn-engine-host
 *   executing the wide-context fixture GGUF).
 *
 * There is no second application implementation and no store-level
 * mocking: the browser talks to the same HTTP/WS surface a user does.
 * Selectors are role/label/visible-name based (Playwright locators,
 * auto-waiting) — no arbitrary sleeps and no React internals.
 */
export default defineConfig({
  testDir: ".",
  timeout: 120_000,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["list"]],
  use: {
    // The server fixture (fixtures/sheytan-server.ts) provides baseURL.
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  outputDir: ".artifacts",
});
