// #62: advertised workflow start links survive both server routing and the
// hydrated HistoryRouter, while unknown or malformed IDs remain not found.
import { test, expect } from "@playwright/test";
import { URL } from "node:url";

async function signInAsRafael(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("workflow start deep links load, navigate back to the catalog, and reject unknown paths", async ({
  page,
}) => {
  test.setTimeout(120000);
  await signInAsRafael(page);

  await page.goto("/workspace/app/workflows");
  const heading = page.locator("#workflow-start-heading");
  await expect(heading).toBeVisible();
  const startLinks = page.locator("a.workflow-start-open");
  await expect(startLinks.first()).toBeVisible();
  const href = await startLinks.first().getAttribute("href");
  expect(href).toMatch(/^\/workspace\/app\/workflows\/start\/[^/?#]+/);

  await startLinks.first().click();
  await expect(page).toHaveURL(
    new RegExp(`${href.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`),
  );
  await expect(heading).toBeVisible();
  await expect(page.locator(".workflow-start-state")).toHaveCount(0);

  await page.goBack();
  await expect(page).toHaveURL(/\/workspace\/app\/workflows(?:\?.*)?$/);
  await expect(heading).toBeVisible();
  await expect(startLinks.first()).toBeVisible();

  // A cold document request exercises the server resolver before the WASM
  // router hydrates the same advertised route.
  await page.goto(href);
  await page.waitForFunction(() =>
    window.performance
      .getEntriesByType("mark")
      .some((mark) => mark.name === "hcm:hydrated"),
  );
  await expect(heading).toBeVisible();
  await expect(page.locator(".workflow-start-state")).toHaveCount(0);

  // Available workflow links intentionally return to the catalog presentation.
  // Search is owned by the hydrated WASM view, so changing it proves that the
  // router loaded the dynamic path rather than leaving only server markup.
  const deepLinkURL = new URL(href, page.url());
  const firstCardTitle = (
    await page.locator(".workflow-start-card h3").first().textContent()
  )?.trim();
  expect(firstCardTitle).toBeTruthy();
  const search = page.locator("#workflow-start-search-input");
  await search.fill(firstCardTitle);
  await expect(page.locator(".workflow-start-card h3")).toHaveText([firstCardTitle]);
  await expect(page).toHaveURL(deepLinkURL.toString());

  // If this seeded principal has a known unavailable workflow, its deep link
  // must hydrate into the existing unavailable state, preserving the ID.
  await page.goto("/workspace/app/workflows");
  const unavailableCards = page.locator(".workflow-start-card").filter({
    has: page.locator(".workflow-start-unavailable"),
  });
  if (await unavailableCards.count()) {
    const unavailableID = await unavailableCards
      .first()
      .getAttribute("data-workflow-id");
    expect(unavailableID).toBeTruthy();
    const unavailablePath = `/workspace/app/workflows/start/${encodeURIComponent(unavailableID)}`;
    const response = await page.goto(unavailablePath);
    expect(response?.status()).toBe(200);
    await page.waitForFunction(() =>
      window.performance
        .getEntriesByType("mark")
        .some((mark) => mark.name === "hcm:hydrated"),
    );
    await expect(page.locator(".workflow-start-state")).toBeVisible();
  }

  for (const invalidPath of [
    "/workspace/app/workflows/start/hcmnext.workflows.unknown",
    "/workspace/app/workflows/start/hcmnext.workflows.new_hire/extra",
  ]) {
    const response = await page.goto(invalidPath);
    expect(response?.status(), `${invalidPath} should remain not found`).toBe(404);
  }
});
