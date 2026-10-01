import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  testMatch: "*.spec.mjs",
  workers: 1,
  retries: 0,
  reporter: "list",
  outputDir: "../../../.artifacts/test/playwright-persona-admin",
  use: {
    colorScheme: "light",
    reducedMotion: "reduce",
    baseURL: process.env.HCMNEXT_DEV_URL ?? "http://127.0.0.1:8080",
    ...devices["Desktop Chrome"],
  },
});
