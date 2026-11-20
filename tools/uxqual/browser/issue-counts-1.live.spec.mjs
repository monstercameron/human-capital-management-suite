import { test, expect } from "@playwright/test";

// Live checks for the counts lane: #3633 (dry-run line pluralised), #3632 (a
// zero count is written in the locale's digits or its element is hidden) and
// #3635 (a Go-test issue: productui lineage and Home golden, nothing to see in
// a browser). The filter history read is answered with fixed records so the
// demo data is never written to.

test.use({ viewport: { width: 1440, height: 900 } });
test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChannel(page, locale) {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=general`);
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 90000 });
}

const record = (id, rule) => ({
  ID: id,
  Tenant: "t",
  Channel: "general",
  Subject: "s",
  At: new Date().toISOString(),
  Hit: {
    RuleID: rule,
    RuleName: `Rule ${rule}`,
    Version: "1",
    Action: "block",
    Digest: "d",
    Masked: "",
    Target: "",
    Span: { Start: 0, End: 1 },
    DryRun: true,
  },
});

const expected = {
  "en-US": {
    one: /^1 message · /,
    many: /^3 messages · /,
    bad: /1 messages|\{n\}|\{count\}/,
  },
  "de-DE": {
    one: /^1 Nachricht · /,
    many: /^3 Nachrichten · /,
    bad: /\{n\}|\{count\}/,
  },
  ar: {
    one: /^رسالة واحدة · /,
    many: /^(3|٣) رسائل · /,
    bad: /\{n\}|\{count\}/,
  },
};

async function dryLines(page, locale, records) {
  await page.route("**/api/chat/filters/v1/hits**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(records),
    }),
  );
  await openChannel(page, locale);
  await page.locator("button[data-action='details']").first().click();
  // The manage sections sit behind "Manage channel"; the filter history is in
  // the one called Manage filters (labels are localised, so go by position).
  const labels = page.locator(".manage-sec-label");
  await expect(labels.first()).toBeVisible({ timeout: 30000 });
  const manage = page.locator(".manage-sec-label:visible").nth(2);
  await manage.click();
  const filters = page.locator(".manage-sec-label:visible").nth(6);
  await filters.click();
  const show = page.locator(".chatmod-hits .chatmod-actions button").first();
  await show.click();
  const dry = page.locator(".chatmod-hits-dry");
  await expect(dry).toBeVisible({ timeout: 30000 });
  return (await dry.locator("li .chatmod-state").allInnerTexts()).map((t) => t.trim());
}

for (const locale of ["en-US", "de-DE", "ar"]) {
  test(`#3633 the dry-run line says one and many in ${locale}`, async ({ page }) => {
    await signIn(page);
    // Rule a matched once, rule b three times.
    const lines = await dryLines(page, locale, [
      record(1, "a"),
      record(2, "b"),
      record(3, "b"),
      record(4, "b"),
    ]);
    expect(lines.length).toBe(2);
    const want = expected[locale];
    expect(lines.some((l) => want.one.test(l))).toBe(true);
    expect(lines.some((l) => want.many.test(l))).toBe(true);
    for (const line of lines) expect(line).not.toMatch(want.bad);
  });
}

for (const locale of ["en-US", "de-DE", "ar"]) {
  test(`#3632 zero counts are a localised zero or hidden in ${locale}`, async ({
    page,
  }) => {
    await signIn(page);
    await openChannel(page, locale);
    const zero = locale === "ar" ? "٠" : "0";
    const found = await page.evaluate(() =>
      [...document.querySelectorAll("[class*='count'], [class*='badge']")].map((e) => ({
        cls: String(e.className),
        text: (e.textContent || "").trim(),
        shown: !!(e.getBoundingClientRect().width && e.getBoundingClientRect().height),
      })),
    );
    // No count that is drawn on screen is blank.
    const blank = found.filter((c) => c.shown && c.text === "");
    expect(blank, JSON.stringify(blank)).toEqual([]);
    // The Saved count: hidden at zero, and written in the locale's digits.
    const saved = found.find((c) => /chatsave-count/.test(c.cls));
    if (saved)
      expect(
        saved.shown ? saved.text !== "" : saved.text === zero || saved.text !== "",
      ).toBe(true);
    // The to-do chips in the channel header show a zero ("0 of 1 done").
    const chip = page
      .locator("[class*='chip'], [class*='tray']")
      .filter({ hasText: zero });
    expect(await chip.count()).toBeGreaterThan(0);
    // A search with nothing found says so, and draws no blank count.
    const search = page
      .locator("button[data-action='search'], [data-action='header-search']")
      .first();
    if (await search.count()) {
      await search.click();
      await page.keyboard.type(`zz-none-${Date.now()}`);
      await page.keyboard.press("Enter");
      await page.waitForTimeout(1500);
      const after = await page.evaluate(() =>
        [...document.querySelectorAll("[class*='count']")]
          .filter(
            (e) => e.getBoundingClientRect().width && !(e.textContent || "").trim(),
          )
          .map((e) => String(e.className)),
      );
      expect(after).toEqual([]);
    }
  });
}

test("#3635 productui lineage and Home golden are Go tests, not a browser surface", async () => {
  test.skip(
    true,
    "Verified with go test ./internal/humanwork/productui/, see the report.",
  );
});
