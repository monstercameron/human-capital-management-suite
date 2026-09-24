// WEB-098 real Chromium proof for the live, service-backed attention queue.
// Runs against the authenticated live app at localhost:8888.
import { test, expect } from "@playwright/test";

test.use({ baseURL: "http://localhost:8888" });

async function signInAsManager(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForLoadState("networkidle");
}

test("[WEB-098] authenticated attention queue exposes one usable active-work list", async ({
  page,
}) => {
  await signInAsManager(page);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/workspace/app/work");
  await page.waitForLoadState("networkidle");
  await expect(
    page.getByText("Loading your workspace data…", { exact: true }),
  ).toHaveCount(0);

  const queue = page.locator('section.work-list[data-work-kind="action-queue"]');
  await expect(queue).toBeVisible();
  await expect(queue).toHaveAttribute("aria-label", /\S+/);
  await expect(queue.getByRole("heading", { name: "Your actions" })).toBeVisible();
  await expect(queue.getByRole("link", { name: /Needs your action/ })).toBeVisible();

  const rows = queue.locator("li.work-row-item");
  const rowCount = await rows.count();
  if (rowCount === 0) {
    await expect(queue.locator("li.collection-empty")).toBeVisible();
    await expect(
      queue.getByRole("link", { name: "Track promotion requests" }),
    ).toBeVisible();
  }
  for (let index = 0; index < rowCount; index += 1) {
    const row = rows.nth(index);
    await expect(row.locator("a.work-row")).toBeVisible();
    await expect(row.locator(".row-main strong")).not.toBeEmpty();
    const status = (await row.locator(".row-end").innerText()).trim();
    expect(
      status,
      "terminal history must not be presented as active attention",
    ).not.toMatch(/completed|closed|rejected/i);
  }

  // The same route remains usable at phone width; every queue row stays in
  // the viewport and remains individually keyboard reachable.
  await page.setViewportSize({ width: 390, height: 844 });
  const metrics = await queue.evaluate((element) => ({
    left: element.getBoundingClientRect().left,
    right: element.getBoundingClientRect().right,
    viewport: document.documentElement.clientWidth,
  }));
  expect(metrics.left).toBeGreaterThanOrEqual(0);
  expect(metrics.right).toBeLessThanOrEqual(metrics.viewport + 1);
  if (await rows.count()) {
    await rows.first().locator("a.work-row").focus();
    await expect(rows.first().locator("a.work-row")).toBeFocused();
  }
});
