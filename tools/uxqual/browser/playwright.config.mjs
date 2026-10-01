// Standalone Playwright config for UX-QUAL-001's supplementary real-browser
// pass. It is scoped entirely to tools/uxqual/browser and needs no webServer: the specs load the static
// documents tools/uxqual/cmd/genfixtures writes to
// tools/uxqual/testdata/rendered/*.html directly via file:// URLs.
//
// Generate the fixtures, then run:
//   go run ./tools/uxqual/cmd/genfixtures
//   npx playwright test --config=tools/uxqual/browser/playwright.config.mjs
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./",
  testMatch: "*.spec.mjs",
  fullyParallel: true,
  // This machine routinely runs many concurrent Claude/Codex sessions each
  // driving their own Chrome/Chromium instances, which starves a freshly
  // launched headless-shell process enough to blow the default 30s
  // navigation/setup timeout under load (observed: "Test timeout of 30000ms
  // exceeded while setting up 'page'" with no assertion ever reached). One
  // retry absorbs that contention without weakening any assertion -- a
  // genuine accessibility failure still fails every attempt.
  retries: 1,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  reporter: "list",
  outputDir: "../../../.artifacts/test/playwright-uxqual",
  use: {
    colorScheme: "light",
    reducedMotion: "reduce",
    // Specs that drive the live dev cell (rather than a file:// fixture) use
    // relative paths and resolve them against this. Reading the override here
    // keeps process.env in the config module, which is the only place the
    // repository's process-env-boundary rule allows it. 127.0.0.1 rather than
    // localhost on purpose: the workspace session cookie is bound to the host
    // the dev login was performed against, and the two are distinct origins.
    baseURL: process.env.HCMNEXT_DEV_URL ?? "http://127.0.0.1:8080",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
