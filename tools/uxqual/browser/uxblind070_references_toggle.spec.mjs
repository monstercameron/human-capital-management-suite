import { test, expect } from "@playwright/test";

test.describe("UXBLIND-070 reference workflow visibility", () => {
  test("hydrated toggle updates the URL and visible catalog", async ({ page }) => {
    const diagnostics = [];
    page.on("pageerror", (error) => diagnostics.push(`pageerror: ${error.message}`));
    page.on("console", (message) => {
      if (message.type() === "error" || message.type() === "warning") {
        diagnostics.push(`console ${message.type()}: ${message.text()}`);
      }
    });

    await page.goto("/workspace/login?company=ironridge-demo");
    await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
    await page.waitForURL(/\/workspace\/app\//);
    await page.waitForFunction(
      () => performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
      null,
      { timeout: 30000 },
    );
    await page.goto("/workspace/app/admin/workflows");
    await page.waitForFunction(
      () => performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
      null,
      { timeout: 30000 },
    );

    const checkbox = page.locator(".workflow-list-references input[type=checkbox]");
    await expect(checkbox).toBeVisible();
    await expect(checkbox).not.toBeChecked();
    await expect(page.locator(".workflow-list-filter-chip").first()).toContainText("5");
    await expect(page.locator(".workflow-list-data-row")).toHaveCount(5);

    await checkbox.evaluate((input) => {
      input.addEventListener("input", (event) => {
        window.__uxblindToggleEvents.push({ type: event.type, checked: event.target.checked });
      });
      input.addEventListener("change", (event) => {
        window.__uxblindToggleEvents.push({ type: event.type, checked: event.target.checked });
      });
      window.__uxblindToggleEvents = [];
    });
    await checkbox.check();
    await expect(page.locator(".workflow-list-data-row")).toHaveCount(6);
    const trace = await page.evaluate(() => ({
      events: window.__uxblindToggleEvents,
      url: location.href,
      checked: document.querySelector(".workflow-list-references input[type=checkbox]")?.checked,
      allChip: document.querySelector(".workflow-list-filter-chip")?.innerText,
      rows: document.querySelectorAll(".workflow-list-data-row").length,
      references: Array.from(document.querySelectorAll(".workflow-list-data-row")).map((row) => row.innerText),
    }));
    const settled = await page.evaluate(() => ({
      events: window.__uxblindToggleEvents,
      url: location.href,
      checked: document.querySelector(".workflow-list-references input[type=checkbox]")?.checked,
      checkedAttribute: document.querySelector(".workflow-list-references input[type=checkbox]")?.getAttribute("checked"),
      rows: document.querySelectorAll(".workflow-list-data-row").length,
    }));
    await test.info().attach("reference-toggle-trace", {
      body: JSON.stringify({ trace, settled, diagnostics }, null, 2),
      contentType: "application/json",
    });

    await expect(checkbox).toBeChecked();
    await expect(page).toHaveURL(/workflow_references=1/);
    await expect(page.locator(".workflow-list-filter-chip").first()).toContainText("6");
    await expect(page.locator(".workflow-list-data-row")).toHaveCount(6);
    await expect(page.getByRole("row", { name: /Prototype promotion approval/ })).toBeVisible();
    expect(diagnostics).toEqual([]);
    await test.info().attach("reference-workflows-enabled", {
      body: await page.screenshot(),
      contentType: "image/png",
    });

    await page.reload();
    await page.waitForFunction(
      () => performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
      null,
      { timeout: 30000 },
    );
    await expect(page.locator(".loading-proxy")).toHaveCount(0, { timeout: 30000 });
    const reloadTrace = await page.evaluate(() => ({
      url: location.href,
      checked: document.querySelector(".workflow-list-references input[type=checkbox]")?.checked,
      allChip: document.querySelector(".workflow-list-filter-chip")?.innerText,
      rows: document.querySelectorAll(".workflow-list-data-row").length,
      references: Array.from(document.querySelectorAll(".workflow-list-data-row")).map((row) => row.innerText),
    }));
    await test.info().attach("reference-toggle-reload-trace", {
      body: JSON.stringify(reloadTrace, null, 2),
      contentType: "application/json",
    });
    await expect(page.locator(".workflow-list-references input[type=checkbox]")).toBeChecked();
    await expect(page.locator(".workflow-list-data-row")).toHaveCount(6);
    await expect(page.getByRole("row", { name: /Prototype promotion approval/ })).toBeVisible();

    await checkbox.uncheck();
    await expect(checkbox).not.toBeChecked();
    await expect(page).not.toHaveURL(/workflow_references=1/);
    await expect(page.locator(".workflow-list-filter-chip").first()).toContainText("5");
    await expect(page.locator(".workflow-list-data-row")).toHaveCount(5);
    await test.info().attach("reference-workflows-hidden", {
      body: await page.screenshot(),
      contentType: "image/png",
    });
    expect(diagnostics).toEqual([]);
  });
});
