import { test, expect } from "@playwright/test";

// Live checks for the Report message dialog: #3949 (focus after the nested
// report and its menu close), #3948 (discard question) and #3887 (loading and
// failure states inside a dialog). Nothing is ever sent.

test.use({ viewport: { width: 1440, height: 900 } });
test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChannel(page) {
  await signIn(page);
  await page.goto("/workspace/app/chat?locale=en-US#channel=engineering");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 90000 });
}

// Someone else's message has a Report item in its More actions menu. The action
// bar is drawn for the row the pointer is over, so move the real mouse there.
async function openReportMenu(page) {
  const message = page.locator(".message[data-message-id]:not(.own)").last();
  await message.evaluate((e) => e.scrollIntoView({ block: "center" }));
  const box = await message.boundingBox();
  await page.mouse.move(box.x + 300, box.y + box.height / 2);
  await page.mouse.move(box.x + 320, box.y + box.height / 2 + 2);
  const more = message.getByRole("button", { name: "More actions" });
  await more.click();
  const report = page.locator("[role=menu] [data-chatremove-open*='action=report']");
  await expect(report).toBeVisible();
  return { message, more, report };
}

test("#3949 Escape from the report then from its menu leaves focus on the message", async ({
  page,
}) => {
  await openChannel(page);
  const { message, report } = await openReportMenu(page);
  const id = await message.getAttribute("data-message-id");
  await report.focus();
  await page.keyboard.press("Enter");
  const dialog = page.locator("[data-chatremove-overlay=dialog]");
  await expect(dialog).toBeVisible({ timeout: 30000 });
  await expect(
    dialog.getByRole("button", { name: /Close|Cancel/ }).first(),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await page.keyboard.press("Escape");
  await expect(page.locator(".message-menu")).toBeHidden();
  const where = await page.evaluate(() => {
    const a = document.activeElement;
    const row = a && a.closest(".message[data-message-id]");
    return {
      tag: a && a.tagName,
      label: a && (a.getAttribute("aria-label") || a.textContent || "").trim(),
      row: row ? row.getAttribute("data-message-id") : "",
    };
  });
  expect(where.tag).not.toBe("BODY");
  expect(where.row).toBe(id);
});

test("#3948 a started report asks before it is discarded; an empty one closes at once", async ({
  page,
}) => {
  await openChannel(page);
  const posts = [];
  page.on("request", (r) => {
    if (r.method() !== "GET" && /moderation|report/i.test(r.url())) posts.push(r.url());
  });
  let { message, report } = await openReportMenu(page);
  const id = await message.getAttribute("data-message-id");
  await report.click();
  const overlay = page.locator("[data-chatremove-overlay=dialog]");
  await expect(overlay).toBeVisible({ timeout: 30000 });
  const note = overlay.locator("textarea").first();
  await expect(note).toBeVisible();
  const marker = `zz-mod-${Date.now()}`;

  // Empty form: one Escape closes.
  await page.keyboard.press("Escape");
  await expect(overlay).toBeHidden();

  // Started form.
  await page.keyboard.press("Escape");
  ({ message, report } = await openReportMenu(page));
  await report.click();
  await expect(overlay).toBeVisible({ timeout: 30000 });
  const radio = overlay.locator("input[type=radio]").first();
  if (await radio.count()) await radio.check();
  await overlay.locator("textarea").first().fill(marker);
  await page.keyboard.press("Escape");
  const ask = overlay.getByText("Discard this report?");
  await expect(ask).toBeVisible();
  await expect(overlay).toContainText(/Nothing you wrote is saved or shared/);
  const keep = overlay.getByRole("button", { name: "Keep writing" });
  await expect(keep).toBeFocused();

  // Escape on the question keeps the report, text intact.
  await page.keyboard.press("Escape");
  await expect(ask).toBeHidden();
  await expect(overlay.locator("textarea").first()).toHaveValue(marker);

  // Discard report closes with focus on the source message.
  await page.keyboard.press("Escape");
  await overlay.getByRole("button", { name: "Discard report" }).click();
  await expect(overlay).toBeHidden();
  const rowId = await page.evaluate(() => {
    const a = document.activeElement;
    const row = a && a.closest(".message[data-message-id]");
    return row ? row.getAttribute("data-message-id") : "";
  });
  expect(rowId).toBe(id);
  expect(posts).toEqual([]);
});

test("#3887 loading and failure stay inside a dialog with Cancel and Try again", async ({
  page,
}) => {
  await openChannel(page);
  let mode = "slow";
  await page.route("**/api/chat/moderation/page**", async (route) => {
    if (mode === "slow") {
      await page.waitForTimeout(12000);
      return route.continue().catch(() => {});
    }
    return route.abort();
  });
  const { report } = await openReportMenu(page);
  await report.click();
  const pending = page.locator("[data-chatremove-pending]");
  await expect(pending).toBeVisible({ timeout: 10000 });
  const box = await pending.boundingBox();
  const vp = page.viewportSize();
  // A centred dialog, not bars across the top-left of the page.
  expect(box.width).toBeLessThan(vp.width * 0.9);
  expect(box.x).toBeGreaterThan(vp.width * 0.1);
  await expect(pending.getByRole("button", { name: "Cancel" })).toBeVisible();
  await expect(pending.getByRole("heading", { name: "Report message" })).toBeVisible();
  await expect(pending).toContainText(/taking longer than usual/i, { timeout: 20000 });
  await expect(pending.getByRole("button", { name: /Try again/ })).toBeVisible({
    timeout: 15000,
  });
  await pending.getByRole("button", { name: "Cancel" }).click();
  await expect(pending).toBeHidden();

  // Failure.
  mode = "fail";
  const again = await openReportMenu(page);
  await again.report.click();
  const failed = page.locator("[data-chatremove-pending]");
  await expect(failed.getByRole("button", { name: /Try again/ })).toBeVisible({
    timeout: 20000,
  });
  await expect(
    failed.getByRole("button", { name: "Close", exact: true }).first(),
  ).toBeVisible();
  await expect(failed.getByRole("alert")).toBeVisible();
  await page.unrouteAll({ behavior: "ignoreErrors" });
});
