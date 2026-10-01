// WFPAGE-008 browser proof for the real workflow history route. All controls
// are read-only GET filters; this spec never starts or changes a workflow.
import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const widths = [1280, 800, 390];
const screenshotDir = resolve(
  process.cwd(),
  ".artifacts/lanes/ux_refinement/screenshots/wfpage008",
);

async function signIn(page) {
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForFunction(() =>
    performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
  );
}

async function inspectLayout(page, width) {
  await page.setViewportSize({ width, height: 850 });
  await expect(page.locator(".workflow-history-filters")).toBeVisible();
  return page.evaluate(() => {
    const box = (element) => {
      const r = element.getBoundingClientRect();
      return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width, height: r.height };
    };
    const controls = [...document.querySelectorAll(".history-filter-controls > *")]
      .filter((element) => getComputedStyle(element).display !== "none")
      .map((element) => ({ name: element.id || element.className || element.tagName, ...box(element) }));
    const overlaps = [];
    for (let i = 0; i < controls.length; i++) {
      for (let j = i + 1; j < controls.length; j++) {
        const a = controls[i], b = controls[j];
        if (a.left < b.right - 1 && a.right > b.left + 1 && a.top < b.bottom - 1 && a.bottom > b.top + 1) {
          overlaps.push([a.name, b.name]);
        }
      }
    }
    const dateFields = [...document.querySelectorAll('input[type="date"]')];
    const tableViewport = document.querySelector(".workflow-history-results .data-table-scroll");
    return {
      viewportWidth: document.documentElement.clientWidth,
      documentWidth: document.documentElement.scrollWidth,
      controls,
      overlaps,
      labels: [...document.querySelectorAll(".history-filter-controls label")].map((el) => el.textContent.trim()),
      dateWidths: dateFields.map((el) => el.getBoundingClientRect().width),
      tableViewport: tableViewport ? {
        clientWidth: tableViewport.clientWidth,
        scrollWidth: tableViewport.scrollWidth,
        overflowX: getComputedStyle(tableViewport).overflowX,
      } : null,
      headers: [...document.querySelectorAll("#workflow-history-table th")].map((el) => el.textContent.trim()),
    };
  });
}

test("[WFPAGE-008] workflow history filters remain usable at desktop, tablet and phone widths", async ({ page }) => {
  test.setTimeout(120000);
  mkdirSync(screenshotDir, { recursive: true });
  const diagnostics = [];
  page.on("pageerror", (error) => diagnostics.push({ type: "pageerror", message: error.message }));
  page.on("console", (message) => {
    if (message.type() === "error") diagnostics.push({ type: "console", message: message.text() });
  });
  page.on("requestfailed", (request) => diagnostics.push({ type: "requestfailed", url: request.url(), error: request.failure()?.errorText }));

  await page.setViewportSize({ width: 1280, height: 850 });
  await signIn(page);
  await page.goto("/workspace/app/workflows/history");
  await page.waitForFunction(() =>
    performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
  );
  await expect(page.getByRole("search")).toBeVisible();

  const layout = [];
  for (const width of widths) {
    const metrics = await inspectLayout(page, width);
    layout.push({ width, ...metrics });
    await page.screenshot({ path: resolve(screenshotDir, `workflow-history-${width}px.png`), fullPage: false, animations: "disabled" });
    expect(metrics.viewportWidth, `viewport must apply at ${width}px`).toBe(width);
    expect(metrics.documentWidth, `page must not overflow horizontally at ${width}px`).toBeLessThanOrEqual(width);
    expect(metrics.overlaps, `filter controls must not overlap at ${width}px`).toEqual([]);
    expect(metrics.labels.length, `all filters remain labelled at ${width}px`).toBeGreaterThanOrEqual(8);
    expect(metrics.dateWidths, `date controls remain usable at ${width}px`).toHaveLength(2);
    expect(metrics.dateWidths.every((fieldWidth) => fieldWidth >= 115), `date fields should not collapse at ${width}px`).toBe(true);
    if (metrics.tableViewport) {
      expect(metrics.tableViewport.overflowX, "table should own horizontal scrolling").toBe("auto");
      expect(metrics.headers.length, "table headers should remain in the DOM").toBeGreaterThanOrEqual(6);
    }
  }

  const query = page.locator("#workflow-history-query");
  await query.fill("promotion");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page).toHaveURL(/history_q=promotion/);
  await test.info().attach("wfpage008-filter-url-state", {
    body: JSON.stringify({
      url: page.url(),
      queryControlValue: await query.inputValue(),
      visibleSearchName: await query.getAttribute("aria-label"),
      searchLabel: await query.evaluate((input) => input.labels?.[0]?.textContent?.trim()),
      browserDiagnostics: diagnostics,
    }, null, 2),
    contentType: "application/json",
  });
  await expect(query).toHaveValue("promotion");
  await page.reload();
  await expect(page.locator(".workflow-history-filters")).toBeVisible();
  await expect(page.locator("#workflow-history-query")).toHaveValue("promotion");

  await page.locator("#workflow-history-sort").selectOption("updated");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page).toHaveURL(/history_sort=updated/);
  await expect(page).toHaveURL(/history_q=promotion/);
  await expect(page.locator("#workflow-history-query")).toHaveValue("promotion");
  await page.screenshot({
    path: resolve(screenshotDir, "workflow-history-sort-updated-state.png"),
    fullPage: false,
    animations: "disabled",
  });
  await test.info().attach("wfpage008-sort-url-state", {
    body: JSON.stringify({
      url: page.url(),
      queryControlValue: await page.locator("#workflow-history-query").inputValue(),
      sortControlValue: await page.locator("#workflow-history-sort").inputValue(),
      browserDiagnostics: diagnostics,
    }, null, 2),
    contentType: "application/json",
  });
  await expect(page.locator("#workflow-history-sort")).toHaveValue("updated");
  await page.locator("#workflow-history-query").focus();
  await page.keyboard.press("Tab");
  const focused = await page.evaluate(() => document.activeElement?.tagName.toLowerCase());
  expect(focused, "keyboard focus remains on an interactive control").toMatch(/^(input|select|button|a)$/);

  await test.info().attach("wfpage008-layout", {
    body: JSON.stringify({ widths: layout, diagnostics }, null, 2),
    contentType: "application/json",
  });
  expect(diagnostics, "no browser console, page or network errors").toEqual([]);
});
