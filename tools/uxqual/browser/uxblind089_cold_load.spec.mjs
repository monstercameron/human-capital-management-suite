// UXBLIND-089 real-browser budget: a cold sign-in reaches Home content, not
// the loading skeleton, in under three seconds.
//
// Runs against the live dev cell (HCMNEXT_DEV_URL, see playwright.config.mjs).
// Each attempt uses a fresh browser context, so neither the HTTP cache nor the
// loader's Cache Storage copy of the bundle can help: this is the first visit.
// The budget is measured from the Home document's own navigation start to the
// first frame in which #app holds the resolved page (a <main> and no
// .loading-proxy). The loader and the Go client mark every phase between
// (hcm:loader ... hcm:load-done), and the phase table is attached to the
// report so a regression names its phase.
import { test, expect } from "@playwright/test";

// Keep in step with workspace.ColdContentBudget (a Go test pins the two).
const COLD_CONTENT_BUDGET_MS = 3000;
const LOADER_PHASES = ["loader", "manifest", "wasm-response", "compiled", "instantiated"];
const CLIENT_PHASES = ["go-main", "dial", "hydrate-start", "load-start", "hydrated", "load-done"];

function watchForContent() {
  const state = { skeleton: undefined, content: undefined };
  window.__uxblind089 = state;
  const check = () => {
    const app = document.getElementById("app");
    if (!app || state.content !== undefined) return;
    if (app.querySelector(".loading-proxy")) {
      if (state.skeleton === undefined) state.skeleton = performance.now();
      return;
    }
    if (state.skeleton !== undefined && app.querySelector("main")) state.content = performance.now();
  };
  new MutationObserver(check).observe(document, { subtree: true, childList: true, attributes: true, attributeFilter: ["class"] });
}

test("[UXBLIND-089] cold sign-in shows Home content within the budget", async ({ browser }) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  try {
    await page.goto("/workspace/login?company=ironridge-demo");
    await page.addInitScript(watchForContent);
    await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
    await page.waitForURL(/\/workspace\/app\//);
    await page.waitForFunction(() => window.__uxblind089 && window.__uxblind089.content !== undefined, null, {
      timeout: 30_000,
      polling: 50,
    });
    const result = await page.evaluate(() => {
      const marks = {};
      for (const mark of performance.getEntriesByType("mark")) {
        if (mark.name.startsWith("hcm:")) marks[mark.name.slice(4)] = Math.round(mark.startTime);
      }
      return { content: Math.round(window.__uxblind089.content), skeleton: Math.round(window.__uxblind089.skeleton), marks };
    });
    await test.info().attach("cold-load-phases", { body: JSON.stringify(result, null, 2), contentType: "application/json" });

    // The skeleton is replaced by the page, never left in place.
    await expect(page.locator("#app .loading-proxy")).toHaveCount(0);
    // Every phase ran, in order, before the content frame.
    let previous = -1;
    for (const phase of [...LOADER_PHASES, "go-main", "dial", "hydrate-start", "load-start"]) {
      expect(result.marks[phase], `phase ${phase} was not marked`).toBeDefined();
      expect(result.marks[phase], `phase ${phase} ran out of order`).toBeGreaterThanOrEqual(previous);
      previous = result.marks[phase];
    }
    for (const phase of CLIENT_PHASES) {
      expect(result.marks[phase], `phase ${phase} ran after content`).toBeLessThanOrEqual(result.content);
    }
    expect(result.content, `content after ${result.content} ms`).toBeLessThan(COLD_CONTENT_BUDGET_MS);
  } finally {
    await context.close();
  }
});
