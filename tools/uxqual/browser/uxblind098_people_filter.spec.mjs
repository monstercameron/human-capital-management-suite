// UXBLIND-098 live-browser probe. This requires the authenticated dev cell
// seeded with Ironridge's Walt Brennan persona (HCMNEXT_DEV_URL).
import { test, expect } from "@playwright/test";

test("[UXBLIND-098] hydrated People search filters and clears the directory", async ({ page }) => {
  const diagnostics = [];
  page.on("pageerror", (error) => diagnostics.push(`pageerror: ${error.message}`));
  page.on("requestfailed", (request) => diagnostics.push(`requestfailed: ${request.url()} ${request.failure()?.errorText ?? ""}`));
  page.on("console", (message) => {
    if (message.type() === "error" || message.type() === "warning") diagnostics.push(`console ${message.type()}: ${message.text()}`);
  });
  test.setTimeout(90000);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForFunction(
    () => performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
    null,
    { timeout: 30000, polling: 50 },
  );
  await page.waitForFunction(
    () => performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:load-done"),
    null,
    { timeout: 30000, polling: 50 },
  );

  await page.goto("/workspace/app/people");
  await page.waitForFunction(
    () => performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
    null,
    { timeout: 30000, polling: 50 },
  );
  await page.waitForFunction(
    () => performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:load-done"),
    null,
    { timeout: 30000, polling: 50 },
  );
  await expect(page.locator(".loading-proxy")).toHaveCount(0, { timeout: 30000 });
  const peopleRows = page.locator(".people-row-item");
  await expect(page.getByText("38 people", { exact: true })).toBeVisible();
  await expect(peopleRows).toHaveCount(20, { timeout: 30000 });

  const search = page.locator("#people-filter");
  await expect(search).toBeVisible();
  await search.fill("Curtis");
  try {
    await expect(page).toHaveURL(/[?&]q=Curtis(?:&|$)/, { timeout: 15000 });
  } catch (error) {
    const state = await page.evaluate(() => ({
      url: location.href,
      queryValue: document.querySelector("#people-filter")?.value,
      rows: document.querySelectorAll(".people-row-item").length,
      marks: performance.getEntriesByType("mark").map((mark) => mark.name).filter((name) => name.startsWith("hcm:")),
    }));
    await test.info().attach("people-filter-failure", {
      body: JSON.stringify({ state, diagnostics }, null, 2),
      contentType: "application/json",
    });
    console.log("[UXBLIND-098 filter failure]", JSON.stringify({ state, diagnostics }));
    throw error;
  }
  await expect(peopleRows).not.toHaveCount(20, { timeout: 15000 });
  await expect(page.getByText("Ana", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Andrew", { exact: true })).toHaveCount(0);
  await expect(peopleRows.first()).toContainText("Curtis");

  await page.getByRole("link", { name: "Clear", exact: true }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("q"), { timeout: 15000 }).toBe("");
  await expect(page.getByText("38 people", { exact: true })).toBeVisible();
  await expect(peopleRows).toHaveCount(20, { timeout: 15000 });

  await test.info().attach("people-filter-diagnostics", {
    body: JSON.stringify({ url: page.url(), diagnostics }, null, 2),
    contentType: "application/json",
  });
  expect(diagnostics, "the hydrated People filter journey should not emit runtime errors").toEqual([]);
});
