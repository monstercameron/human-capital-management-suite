import { test, expect } from "@playwright/test";

// Chat search sweep, lane search-filters chunk 1: #3859 (reversed date range),
// #3837 (Written in defaults to Any language), #3691 (from:@First Last).

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openSearch(page, query) {
  await page.goto(
    `/workspace/app/chat?locale=en-US#search=${encodeURIComponent(query)}`,
  );
  const view = page.locator(".chatsearch-view");
  await expect(view).toBeVisible({ timeout: 60000 });
  await expect(view).toHaveAttribute("aria-busy", "false", { timeout: 60000 });
  return view;
}

async function openRefine(page) {
  const form = page.locator("[data-chatsearch-form='filters']");
  if (!(await form.isVisible())) {
    await page
      .locator(".chatsearch-view")
      .getByText("Refine search", { exact: true })
      .click();
  }
  await expect(form).toBeVisible();
  return form;
}

const hashQuery = (page) =>
  decodeURIComponent(
    new URL(page.url()).hash.replace(/^#search=/, "").replace(/\+/g, " "),
  );

test("#3859 a reversed date range names the fields, drops the stale count and recovers", async ({
  page,
}) => {
  await signIn(page);
  const view = await openSearch(page, "payroll");
  const form = await openRefine(page);
  await form.locator("#chatsearch-after").fill("2000-01-01");
  await form.locator("#chatsearch-before").fill("2099-01-01");
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expect
    .poll(() => hashQuery(page), { timeout: 30000 })
    .toMatch(/after:/);
  await expect(view).toHaveAttribute("aria-busy", "false", { timeout: 60000 });
  const validQuery = hashQuery(page);
  await expect(view).toContainText(/\d+ results?/);

  // Reverse the range.
  const form2 = await openRefine(page);
  await form2.locator("#chatsearch-after").fill("2026-11-10");
  await form2.locator("#chatsearch-before").fill("2026-11-01");
  await form2.getByRole("button", { name: "Apply filters" }).click();

  await expect(page.locator("#chatsearch-after-error")).toHaveText(
    /After must be earlier than Before/,
    { timeout: 30000 },
  );
  await expect(page.locator("#chatsearch-before-error")).toHaveText(
    /Before must be later than After/,
  );
  await expect(page.locator("#chatsearch-after")).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  await expect(page.locator("#chatsearch-before")).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  await expect(page.locator("#chatsearch-after")).toHaveValue("2026-11-10");
  await expect(page.locator("#chatsearch-before")).toHaveValue("2026-11-01");
  // Summary alert, no stale count, no generic retry.
  await expect(view.getByRole("alert")).toContainText(/earlier than the Before/);
  await expect(view).not.toContainText(/\d+ results?/);
  await expect(view.getByRole("button", { name: "Try again" })).toHaveCount(0);
  // Focus is on the After field.
  expect(
    await page.evaluate(() => document.activeElement && document.activeElement.id),
  ).toBe("chatsearch-after");

  // Recovery: re-apply the previously applied dates without Try again.
  await page.locator("#chatsearch-after").fill("2000-01-01");
  await page.locator("#chatsearch-before").fill("2099-01-01");
  await page
    .locator("[data-chatsearch-form='filters']")
    .getByRole("button", { name: "Apply filters" })
    .click();
  await expect(page.locator("#chatsearch-after-error")).toHaveCount(0, {
    timeout: 30000,
  });
  await expect(view).toContainText(/\d+ results?/, { timeout: 30000 });
  expect(hashQuery(page)).toBe(validQuery);
});

test("#3837 Written in and Content kind default to the empty option and Apply adds no lang filter", async ({
  page,
}) => {
  await signIn(page);
  await openSearch(page, "open enrollment");
  const form = await openRefine(page);
  await expect(form.locator("#chatsearch-written")).toHaveValue("");
  await expect(
    form.locator("#chatsearch-written option:checked"),
  ).toHaveText("Any language");
  await expect(form.locator("#chatsearch-kind")).toHaveValue("");
  await expect(form.locator("#chatsearch-kind option:checked")).toHaveText(
    "All kinds",
  );
  await form.getByRole("button", { name: "Apply filters" }).click();
  await page.waitForTimeout(1500);
  expect(hashQuery(page)).not.toMatch(/lang:/);
  expect(hashQuery(page)).not.toMatch(/kind:/);
  await expect(page.locator(".chatsearch-view")).toHaveAttribute(
    "aria-busy",
    "false",
    { timeout: 60000 },
  );
  await expect(
    page.locator("#chatsearch-written option:checked"),
  ).toHaveText("Any language");
});

test("#3691 from:@Julian Miller shows the full display name, returns messages and no unavailable banner", async ({
  page,
}) => {
  await signIn(page);
  const view = await openSearch(page, "from:@Julian Miller payroll");
  await expect(view).not.toContainText(/sources are unavailable/i);
  const chips = view.locator(".chatsearch-chip, .search-chip, [class*=chip]");
  const chipText = (await chips.allInnerTexts()).join(" | ");
  expect(chipText).toMatch(/Julian Miller/);
  expect(chipText).not.toMatch(/@Julian(?! Miller)/);
  // Person field shows the name.
  const form = await openRefine(page);
  await expect(form.locator("#chatsearch-person")).toHaveValue(/Julian Miller/);
  // Message results are returned.
  await expect(view.locator("[data-message-id], .chatsearch-result").first()).toBeVisible();
  await expect(view).not.toContainText(/hc-026-julian-miller/);
  // Removing the chip removes the whole name.
  const remove = view
    .getByRole("button", { name: /Remove filter/i })
    .first();
  if (await remove.count()) {
    await remove.click();
    await expect
      .poll(() => hashQuery(page), { timeout: 30000 })
      .not.toMatch(/Julian|Miller/);
  }
});
