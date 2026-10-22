import { test, expect, devices } from "@playwright/test";

// GitHub issue #61: search results are rendered in chat-main, which the mobile
// conversation drawer hides. Verify the complete query/back/query flow on both
// supported phone widths against the live dev cell.

test.use({
  hasTouch: true,
  isMobile: true,
  userAgent: devices["Pixel 7"].userAgent,
});
test.setTimeout(120000);

async function signInAsRafael(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function runSearch(page, query, expected) {
  const input = page.locator(".chat-rail #chat-search");
  await expect(input).toBeVisible();
  await input.fill(query);
  await input.press("Enter");
  await page.waitForURL(new RegExp(`#search=${query}$`));

  const results = page.getByRole("region", { name: `Results for “${query}”` });
  await expect(results).toBeVisible();
  const matchingResults = results.getByRole("button", { name: expected });
  expect(
    await matchingResults.count(),
    `${query} should return a matching message`,
  ).toBeGreaterThan(0);
  await expect(matchingResults.first()).toBeVisible();
  const bounds = await results.boundingBox();
  expect(bounds?.width, `${query} results must have a visible width`).toBeGreaterThan(
    0,
  );
  expect(bounds?.height, `${query} results must have a visible height`).toBeGreaterThan(
    0,
  );
  await expect(page.locator(".search-results-back")).toBeVisible();
  await expect(input).toBeFocused();

  await page.locator(".search-results-back").focus();
  await page.keyboard.press("Enter");
  await expect(results).toHaveCount(0);
  await expect(page.locator(".chat-rail")).toBeVisible();
}

for (const width of [390, 320]) {
  test(`fresh mobile searches show results, return to conversations and accept a second query at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await signInAsRafael(page);
    await page.goto("/workspace/app/chat?locale=en-US");
    await expect(page.locator(".chat-workspace")).toBeVisible({ timeout: 30000 });
    const searchInput = page.locator(".chat-rail #chat-search");
    if (!(await searchInput.isVisible())) {
      await page.getByRole("button", { name: "Open conversations" }).click();
    }
    await expect(page.locator(".chat-rail")).toBeVisible();
    await expect(searchInput).toBeVisible();
    await runSearch(page, "meeting", /Open message in #general by Theodore Wright/);
    await runSearch(page, "calibration", /Open message/);
  });
}
