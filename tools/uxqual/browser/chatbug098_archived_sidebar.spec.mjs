import { test, expect } from "@playwright/test";

// CHATBUG-098: archived conversations are a normal section at the end of the
// sidebar list; nothing is positioned over rows. Written for the lane seed
// (Rafael Torres, leadership-private has an archived conversation); needs a
// running server and browser, so it is specified here and not executed in
// unit runs.

const channel = "/workspace/app/chat?locale=en-US#channel=leadership-private";

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

function intersects(a, b) {
  return (
    a &&
    b &&
    a.x < b.x + b.width &&
    b.x < a.x + a.width &&
    a.y < b.y + b.height &&
    b.y < a.y + a.height
  );
}

async function archivedSection(page) {
  return page.locator('.rail-scroll [data-section-id="archived"]');
}

// Every conversation row outside Archived keeps clear of the Archived
// section's box: the section takes its own height in the flow.
async function expectNoRowCovered(page, where) {
  const section = archivedSection(page);
  await expect(section).toBeAttached({ timeout: 30000 });
  const sectionBox = await section.boundingBox();
  expect(sectionBox).toBeTruthy();
  const rows = page.locator(".rail-scroll .chat-rail-row");
  const count = await rows.count();
  expect(count).toBeGreaterThan(0);
  for (let i = 0; i < count; i++) {
    const row = rows.nth(i);
    const inside = await row.evaluate((el, sectionEl) => {
      let n = el;
      while (n) {
        if (n === sectionEl) return true;
        n = n.parentElement;
      }
      return false;
    }, await section.elementHandle());
    if (inside) continue;
    const box = await row.boundingBox();
    if (!box) continue;
    expect(intersects(box, sectionBox), `${where}: row ${i} covered`).toBe(false);
  }
  // The fixed footer band is gone entirely.
  await expect(page.locator(".chatstate-archived")).toHaveCount(0);
}

test("opening search, a row menu, preferences and Archived covers no row", async ({
  page,
}) => {
  await signIn(page);
  await page.goto(channel);
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
    { timeout: 30000 },
  );
  await expectNoRowCovered(page, "plain");

  // Search results shown.
  await page.locator("#chat-search").fill("holiday");
  await expectNoRowCovered(page, "searching");

  // A row menu open on the first row.
  await page.locator(".rail-scroll .chat-rail-row .rail-row-more").first().click();
  await expect(page.locator('.rail-row-menu[popover="manual"]')).toBeVisible({ timeout: 10000 });
  await expectNoRowCovered(page, "menu open");
  await page.keyboard.press("Escape");

  // Chat preferences open.
  await page.locator(".rail-prefs-control .chatux002-gear").click();
  await expect(page.locator('[data-chat-layer="chat-prefs"]')).toBeVisible({ timeout: 10000 });
  await expectNoRowCovered(page, "preferences open");
  await page.keyboard.press("Escape");

  // The Archived section open: its rows show the quiet tag and the row menu
  // leads with Open and Restore.
  await (await archivedSection(page)).locator(".section-title").click();
  const rows = (await archivedSection(page)).locator(".chat-rail-row");
  await expect(rows.first()).toBeVisible({ timeout: 10000 });
  await expect(rows.first().locator(".chatstate-badge")).toContainText(/rchiv/i);
  await rows.first().locator(".rail-row-more").click();
  const items = page.locator('.rail-row-menu[popover="manual"] .menu-item');
  await expect(items.nth(0)).toContainText(/pen/i);
  await expect(items.nth(1)).toContainText(/estore/i);
  await page.keyboard.press("Escape");
  await expectNoRowCovered(page, "archived open");
});
