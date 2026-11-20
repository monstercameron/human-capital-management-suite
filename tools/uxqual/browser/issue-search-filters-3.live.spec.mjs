import { test, expect } from "@playwright/test";

// #3398 Refine search fields carry visible labels and accessible names.
// #3928 the recent-searches popover can clear the stored history.

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

const fields = ["channel", "person", "before", "after", "on", "kind"];

for (const [locale, width, height] of [
  ["en-US", 1440, 900],
  ["de-DE", 390, 844],
  ["ar", 320, 640],
]) {
  test(`#3398 Refine search fields are labelled (${locale} ${width}px)`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height });
    await signIn(page);
    await page.goto(`/workspace/app/chat?locale=${locale}#search=budget`);
    const results = page.locator("#chatsearch-results[data-active='true']");
    await expect(results).toBeVisible({ timeout: 60000 });
    const form = results.locator("form[data-chatsearch-form='filters']");
    await results.locator("[data-chat-disclosure-toggle]").first().click();
    await expect(form).toBeVisible();
    const names = [];
    for (const field of [...fields, "written"]) {
      const control = form.locator(`#chatsearch-${field}`);
      await expect(control).toBeVisible();
      const info = await control.evaluate((el) => {
        const labels = Array.from(el.labels || []).map((l) =>
          (l.querySelector("span")?.textContent || "").trim(),
        );
        return { labels, id: el.id, aria: el.getAttribute("aria-label") };
      });
      expect(info.labels.length, field).toBeGreaterThan(0);
      expect(info.labels[0].length, `${field} label text`).toBeGreaterThan(0);
      names.push(info.labels[0]);
    }
    expect(new Set(names).size, "names distinct").toBe(names.length);
    // Distinct after entering values too.
    await form.locator("#chatsearch-before").fill("2026-12-01");
    await form.locator("#chatsearch-after").fill("2026-01-01");
    await form.locator("#chatsearch-on").fill("2026-06-01");
    const again = await form.locator("label span").allInnerTexts();
    for (const n of names) expect(again).toContain(n);
    // Accessible names via the browser's own computation.
    const snap = await form.locator("#chatsearch-before").ariaSnapshot();
    expect(snap).toContain(names[2]);
    // No horizontal overflow at phone width.
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    );
    expect(overflow).toBeLessThanOrEqual(1);
  });
}

test("#3928 recent searches popover clears stored history", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  // Linh Tran's own history is used so the administrator's recent searches,
  // which other checks read, are left alone.
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Linh Tran" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  const deletes = [];
  page.on("request", (r) => {
    if (r.method() === "DELETE" && r.url().includes("/api/chat/search/recent"))
      deletes.push(r.url());
  });
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  const box = page.locator("#chat-search");
  await expect(box).toBeVisible({ timeout: 60000 });
  const marker = `zzsf${Date.now().toString(36)}`;
  for (const q of [marker + "a", marker + "b"]) {
    await box.click();
    await box.fill("");
    await box.pressSequentially(q, { delay: 60 });
    await page.waitForTimeout(1500);
    await box.press("Enter");
    await expect(page.locator("#chatsearch-results[data-active='true']")).toBeVisible({
      timeout: 30000,
    });
    await page.waitForTimeout(500);
  }
  await box.fill("");
  await page.waitForTimeout(500);
  await box.blur();
  await box.focus();
  const popover = page.locator(".rail-search-recent");
  await expect(popover).toBeVisible({ timeout: 15000 });
  await expect(
    popover.locator(".rail-search-recent-item", { hasText: marker }),
  ).not.toHaveCount(0);
  const clear = popover.getByRole("button", { name: "Clear recent searches" });
  await expect(clear).toBeVisible();
  await expect(popover).toContainText(/account|device/i);
  await clear.focus();
  await page.keyboard.press("Enter");
  await expect.poll(() => deletes.length).toBeGreaterThan(0);
  await expect(
    popover.locator(".rail-search-recent-item", { hasText: marker }),
  ).toHaveCount(0);
  await expect(box).toBeFocused();
  // Persistence across reload (server store).
  await page.reload();
  const box2 = page.locator("#chat-search");
  await expect(box2).toBeVisible({ timeout: 60000 });
  await box2.focus();
  await page.waitForTimeout(1000);
  await expect(
    page.locator(".rail-search-recent-item", { hasText: marker }),
  ).toHaveCount(0);
});
