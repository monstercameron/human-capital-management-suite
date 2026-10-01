// UXBLIND-068 browser regression: the People column dialog remains available
// even though its dialog subtree is present in the DOM while <details> is shut.
import { test, expect } from "@playwright/test";

test.describe("UXBLIND-068 People column chooser", () => {
  test.setTimeout(90000);

  for (const width of [1280, 390, 320]) {
    test(`opens and applies a column choice at ${width}px`, async ({ page }) => {
      const diagnostics = [];
      page.on("pageerror", (error) => diagnostics.push(`pageerror: ${error.message}`));
      page.on("console", (message) => {
        if (message.type() === "error" || message.type() === "warning") {
          diagnostics.push(`console ${message.type()}: ${message.text()}`);
        }
      });
      page.on("requestfailed", (request) => diagnostics.push(`requestfailed: ${request.url()} ${request.failure()?.errorText ?? ""}`));

      await page.setViewportSize({ width, height: 900 });
      await page.goto("/workspace/login?company=ironridge-demo");
      await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
      await page.waitForURL(/\/workspace\/app\//);
      await page.waitForFunction(
        () => performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
        null,
        { timeout: 30000, polling: 50 },
      );
      await page.goto("/workspace/app/people");
      await page.waitForFunction(
        () => performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
        null,
        { timeout: 30000, polling: 50 },
      );
      await expect(page.locator(".loading-proxy")).toHaveCount(0, { timeout: 30000 });

      const chooser = page.locator("details.column-chooser");
      const trigger = page.locator("#people-columns-label");
      await expect(trigger).toBeVisible();
      await trigger.click();
      await expect(chooser, "Choose columns must open its disclosure").toHaveAttribute("open", "");
      const option = chooser.locator(".column-choice input:not(:disabled):not(:checked)").first();
      const label = (await option.locator("..").innerText()).trim();
      await option.check();
      await chooser.getByRole("button", { name: "Apply columns" }).click();
      await expect(chooser).not.toHaveAttribute("open");

      await trigger.click();
      await expect(chooser).toHaveAttribute("open", "");
      await expect(chooser.locator(".column-choice").filter({ hasText: label }).locator("input")).toBeChecked();
      await chooser.getByRole("button", { name: "Apply columns" }).click();
      await expect(chooser).not.toHaveAttribute("open");
      await test.info().attach(`column-diagnostics-${width}`, {
        body: JSON.stringify({ width, url: page.url(), diagnostics }, null, 2),
        contentType: "application/json",
      });
      expect(diagnostics, "the column chooser journey should not emit runtime errors").toEqual([]);
    });
  }
});
