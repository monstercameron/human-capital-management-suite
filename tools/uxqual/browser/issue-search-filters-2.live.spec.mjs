import { test, expect } from "@playwright/test";

// Chat search sweep, lane search-filters chunk 2: #3690 (Refine search mirrors
// the query and removes cleared filters), #3679 (is:mine does not make the
// thread-reply source unavailable), #3403 (default controls keep the query).

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
  await expect(page.locator(".chatsearch-view")).toBeVisible({ timeout: 60000 });
  await expect(page.locator(".chatsearch-view")).toHaveAttribute("aria-busy", "false", {
    timeout: 60000,
  });
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
    new globalThis.URL(page.url()).hash.replace(/^#search=/, "").replace(/\+/g, " "),
  );

test("#3690 Refine search mirrors the query and removes cleared filters", async ({
  page,
}) => {
  await signIn(page);
  await openSearch(
    page,
    'payroll in:announcements from:"Julian Miller" lang:en is:mine',
  );
  const form = await openRefine(page);
  await expect(form.locator("#chatsearch-channel")).toHaveValue(/announcements/);
  await expect(form.locator("#chatsearch-person")).toHaveValue(/Julian Miller/);
  await expect(form.locator("#chatsearch-written")).toHaveValue("en");
  await expect(form.locator("#chatsearch-mine")).toBeChecked();

  // Clear the channel, Any language, untick Mine only, Apply.
  await form.locator("#chatsearch-channel").fill("");
  await form.locator("#chatsearch-written").selectOption("");
  await form.locator("#chatsearch-mine").uncheck();
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expect
    .poll(() => hashQuery(page), { timeout: 30000 })
    .not.toMatch(/in:|lang:|is:mine/);
  const q = hashQuery(page);
  expect(q).toContain("payroll");
  expect(q).toMatch(/from:/);

  // A later plain search does not inherit a ticked box.
  await openSearch(page, "payroll");
  const form2 = await openRefine(page);
  await expect(form2.locator("#chatsearch-link")).not.toBeChecked();
  await expect(form2.locator("#chatsearch-mine")).not.toBeChecked();
  await expect(form2.locator("#chatsearch-channel")).toHaveValue("");
  await form2.locator("#chatsearch-link").check();
  await form2.getByRole("button", { name: "Apply filters" }).click();
  await expect.poll(() => hashQuery(page)).toMatch(/has:link/);
  await openSearch(page, "payroll");
  const form3 = await openRefine(page);
  await expect(form3.locator("#chatsearch-link")).not.toBeChecked();
  await form3.getByRole("button", { name: "Apply filters" }).click();
  await page.waitForTimeout(1500);
  expect(hashQuery(page)).not.toMatch(/has:link/);

  // Unquoted two-word name applied untouched stays a person filter, no stray word.
  await openSearch(page, "payroll from:@Julian Miller");
  const form4 = await openRefine(page);
  await form4.getByRole("button", { name: "Apply filters" }).click();
  await page.waitForTimeout(1500);
  const q4 = hashQuery(page);
  expect(q4).not.toMatch(/^Miller|\sMiller\s+payroll/);
  expect(q4.replace(/from:("[^"]*"|\S+)/, "").trim()).toBe("payroll");
});

test("#3679 is:mine does not report thread replies as unavailable", async ({
  page,
}) => {
  await signIn(page);
  await openSearch(page, "open enrollment");
  const form = await openRefine(page);
  await form.locator("#chatsearch-mine").check();
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expect.poll(() => hashQuery(page), { timeout: 30000 }).toMatch(/is:mine/);
  const view = page.locator(".chatsearch-view");
  await expect(view).toHaveAttribute("aria-busy", "false", { timeout: 60000 });
  await page.waitForTimeout(3000);
  await expect(view.getByText(/content sources are unavailable/i)).toHaveCount(0);
  await expect(view.getByRole("button", { name: "Try again" })).toHaveCount(0);
});

test("#3403 default controls keep the query; date-only and checkbox-only apply", async ({
  page,
}) => {
  await signIn(page);
  await openSearch(page, "budget");
  const form = await openRefine(page);
  await expect(form.locator("#chatsearch-kind")).toHaveValue("");
  await expect(form.locator("#chatsearch-written")).toHaveValue("");
  await form.getByRole("button", { name: "Apply filters" }).click();
  await page.waitForTimeout(2000);
  await expect(
    page.getByText("Check your filters and dates, then try again."),
  ).toHaveCount(0);
  expect(hashQuery(page)).toBe("budget");
  expect(hashQuery(page)).not.toMatch(/kind:|lang:/);

  // Date-only.
  const f2 = await openRefine(page);
  await f2.locator("#chatsearch-after").fill("2020-01-01");
  await f2.getByRole("button", { name: "Apply filters" }).click();
  await expect.poll(() => hashQuery(page), { timeout: 30000 }).toMatch(/after:/);
  expect(hashQuery(page)).not.toMatch(/kind:|lang:/);
  await expect(
    page.getByText("Check your filters and dates, then try again."),
  ).toHaveCount(0);

  // Checkbox-only.
  await openSearch(page, "budget");
  const f3 = await openRefine(page);
  await f3.locator("#chatsearch-link").check();
  await f3.getByRole("button", { name: "Apply filters" }).click();
  await expect.poll(() => hashQuery(page), { timeout: 30000 }).toMatch(/has:link/);
  expect(hashQuery(page)).not.toMatch(/kind:|lang:/);
  await expect(
    page.getByText("Check your filters and dates, then try again."),
  ).toHaveCount(0);
});
