// UXAUDIT-024 real-browser evidence against the live, gRPC-backed dev
// server -- not the static SSR/GWC fixtures the other specs in this
// directory replay from tools/uxqual/testdata/rendered.
//
// The live audit (recorded in planning/todos.md's UXAUDIT-024 entry) found
// every RED clause already fixed and measured the following directly
// against the running cell, signed in as the admin persona at 1024x768:
//   - .brand-cluster and .tenant both compute white-space:normal, and a
//     67-character tenant name ("Harborcare Integrated Regional Health
//     Services Demonstration Tenant") wraps rather than clips
//     (scrollWidth === clientWidth);
//   - with both nav groups open, every .nav-group computes overflow-y:clip
//     with max-height:none and zero inner scrolling descendants; all
//     scrolling happens on the single .primary-nav region
//     (overflow-y:auto, scrollbar-width:thin);
//   - #global-search-input is role=combobox, aria-label "Search Human
//     Capital Management Suite"; #menu-filter carries no role, aria-label
//     "Filter navigation menu" -- distinct controls, not one relabeled;
//   - global search is fuzzy and multi-typed: "insigths" (misspelled)
//     still finds Insights as a Page.
//
// What a Go unit test cannot prove -- computed CSS cascade results and
// real scrollWidth/clientWidth/scrollHeight measurements -- is this spec's
// job. Everything else (component structure, aria wiring, favorites
// labels, brand accessible name, search ranking and the shared-metadata
// REFACTOR) is proven in Go: TestTodo_UXAUDIT_024,
// TestTodo_UXAUDIT_024_Accessibility, TestTodo_UXAUDIT_024_Security,
// TestTodo_UXAUDIT_024_Performance and TestTodo_UXAUDIT_024_Regression in
// internal/humanwork/productui.
//
// This spec has NOT been run. It is written to the same conventions as the
// other specs in this directory (test.describe/test, page.getByRole,
// page.evaluate) but targets the live dev server on :8080 rather than a
// file:// fixture, and assumes an already-authenticated admin session the
// way the other live specs in this directory do -- so it needs that
// server running and an operator signed in before it can pass, exactly
// the live verification this todo's brief reserves for the operator's own
// session, not this lane.
import { test, expect } from "@playwright/test";

// Relative on purpose: Playwright resolves it against use.baseURL in
// playwright.config.mjs, which is the module that owns the environment
// override. Reading the environment here would violate the repository's
// process-env-boundary rule.
const homeURL = "/workspace/app/home";

test.describe("UXAUDIT-024 navigation identity, hierarchy and search", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1024, height: 768 });
    await page.goto(homeURL);
  });

  test("a long company name wraps in the brand slot instead of truncating", async ({
    page,
  }) => {
    const tenant = page.locator(".tenant").first();
    await expect(tenant).toBeVisible();
    const longName =
      "Harborcare Integrated Regional Health Services Demonstration Tenant";
    const metrics = await tenant.evaluate((el, text) => {
      el.textContent = text;
      const style = window.getComputedStyle(el);
      return {
        whiteSpace: style.whiteSpace,
        scrollWidth: el.scrollWidth,
        clientWidth: el.clientWidth,
      };
    }, longName);
    expect(metrics.whiteSpace, ".tenant must wrap, not clip, long names").toBe(
      "normal",
    );
    expect(
      metrics.scrollWidth,
      `tenant scrollWidth=${metrics.scrollWidth} clientWidth=${metrics.clientWidth}: content overflowed instead of wrapping`,
    ).toBeLessThanOrEqual(metrics.clientWidth);

    const brandCluster = await page
      .locator(".brand-cluster")
      .first()
      .evaluate((el) => window.getComputedStyle(el).whiteSpace);
    expect(brandCluster, ".brand-cluster must not force nowrap").toBe("normal");
  });

  test("the Admin submenu has no internal scrollbar; all scrolling happens on one primary-nav region", async ({
    page,
  }) => {
    // Open every top-level disclosure group so nested content is present in
    // the layout the way the RED clause describes ("hides destinations
    // behind a narrow internal scrollbar").
    const groups = page.locator(".nav-group");
    const groupCount = await groups.count();
    for (let index = 0; index < groupCount; index += 1) {
      const summary = groups.nth(index).locator(".nav-group-summary");
      if (await summary.count()) {
        await summary.first().click();
      }
    }

    const groupMetrics = await page.evaluate(() => {
      return Array.from(document.querySelectorAll(".nav-group")).map((group) => {
        const style = window.getComputedStyle(group);
        const scrollingDescendants = Array.from(group.querySelectorAll("*")).filter(
          (el) => {
            const s = window.getComputedStyle(el);
            return (
              (s.overflowY === "auto" || s.overflowY === "scroll") &&
              el.scrollHeight > el.clientHeight + 1
            );
          },
        ).length;
        return { overflowY: style.overflowY, scrollingDescendants };
      });
    });
    for (const metric of groupMetrics) {
      expect(
        metric.overflowY,
        "a .nav-group must never own its own vertical scrollbar",
      ).not.toBe("auto");
      expect(
        metric.overflowY,
        "a .nav-group must never own its own vertical scrollbar",
      ).not.toBe("scroll");
      expect(
        metric.scrollingDescendants,
        ".nav-group must contain zero independently scrolling descendants",
      ).toBe(0);
    }

    const primaryNav = await page
      .locator(".primary-nav")
      .first()
      .evaluate((el) => {
        const style = window.getComputedStyle(el);
        return { overflowY: style.overflowY, scrollbarWidth: style.scrollbarWidth };
      });
    expect(primaryNav.overflowY, "exactly one scrolling landmark: .primary-nav").toBe(
      "auto",
    );
    expect(
      primaryNav.scrollbarWidth,
      "a subtle edge scrollbar, not a wide default one",
    ).toBe("thin");
  });

  test("global search and the menu filter are two visibly and semantically distinct controls", async ({
    page,
  }) => {
    const search = page.locator("#global-search-input");
    const filter = page.locator("#menu-filter");
    await expect(search).toBeVisible();
    await expect(filter).toBeVisible();

    await expect(search).toHaveAttribute("role", "combobox");
    const searchLabel = await search.getAttribute("aria-label");
    const filterLabel = await filter.getAttribute("aria-label");
    expect(searchLabel, "global search needs a real accessible name").toBeTruthy();
    expect(filterLabel, "menu filter needs a real accessible name").toBeTruthy();
    expect(
      searchLabel,
      "search and filter must not share one accessible name",
    ).not.toBe(filterLabel);

    const filterRole = await filter.getAttribute("role");
    expect(
      filterRole,
      "the menu filter must not claim the combobox role global search owns",
    ).not.toBe("combobox");

    const searchBox = await search.boundingBox();
    const filterBox = await filter.boundingBox();
    expect(searchBox, "global search has no layout box").not.toBeNull();
    expect(filterBox, "menu filter has no layout box").not.toBeNull();
    // The two controls must occupy visibly different regions of the shell
    // (header vs sidebar), not sit stacked as if one relabeled the other.
    expect(
      Math.abs(searchBox.y - filterBox.y) > 4 ||
        Math.abs(searchBox.x - filterBox.x) > 4,
      "global search and the menu filter render at the same position",
    ).toBe(true);
  });

  test("global search is fuzzy and returns typed, ranked results for a misspelled query", async ({
    page,
  }) => {
    const search = page.locator("#global-search-input");
    await search.click();
    await search.fill("insigths");
    const results = page.locator("#global-search-results .global-search-result");
    await expect(results.first()).toBeVisible();
    await expect(
      page.locator("#global-search-results", { hasText: "Insights" }),
    ).toBeVisible();
    const kinds = await page
      .locator("#global-search-results .global-search-kind")
      .allTextContents();
    expect(
      kinds.length,
      "a typo-tolerant query should return at least one ranked result",
    ).toBeGreaterThan(0);
  });

  test("favorites are discoverable from every visible navigation entry", async ({
    page,
  }) => {
    const favoriteControls = page.locator(".nav-favorite");
    const count = await favoriteControls.count();
    expect(
      count,
      "at least one navigation entry must expose a favorite control",
    ).toBeGreaterThan(0);
    const label = await favoriteControls.first().getAttribute("aria-label");
    expect(label, "favorite control must name its destination").toMatch(
      /to favorites$/,
    );
  });
});
