/* global getComputedStyle */
import { test, expect, devices } from "@playwright/test";

test.setTimeout(150000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openSearch(page, query) {
  await page.goto("/workspace/app/chat?locale=en-US");
  await expect(page.locator(".chat-workspace")).toBeVisible({ timeout: 40000 });
  const input = page.locator(".chat-rail #chat-search");
  // The app opens a conversation once it has loaded; wait for that, then come back to the list.
  await page
    .locator(".message[data-message-id]")
    .first()
    .waitFor({ state: "visible", timeout: 90000 })
    .catch(() => {});
  if (!(await input.isVisible())) {
    await page.getByRole("button", { name: "Open conversations" }).click();
  }
  await expect(input).toBeVisible();
  await input.fill(query);
  await input.press("Enter");
  await expect(page.locator(".chatsearch-view")).toBeVisible({ timeout: 40000 });
}

test.describe("phone", () => {
  test.use({
    viewport: { width: 320, height: 640 },
    hasTouch: true,
    isMobile: true,
    userAgent: devices["Pixel 7"].userAgent,
    colorScheme: "dark",
  });

  test("#3860 filters are compact wrapping chips and the query is readable", async ({
    page,
  }) => {
    await signIn(page);
    await openSearch(page, "meeting before:2030-01-01 after:2020-01-01 in:general");
    const chips = page.locator(".chatsearch-chips .chatsearch-chip");
    await expect(chips.first()).toBeVisible();
    expect(await chips.count()).toBeGreaterThanOrEqual(2);
    for (const chip of await chips.all()) {
      const box = await chip.boundingBox();
      expect(box.height, "chip compact").toBeLessThanOrEqual(48);
      expect(box.x + box.width).toBeLessThanOrEqual(321);
      const label = await chip.getAttribute("aria-label");
      expect(label).toMatch(/^Remove filter/);
      expect(((await chip.innerText()) || "").startsWith("Remove filter")).toBe(false);
    }
    const scope = page.locator(".chatsearch-scope");
    await expect(scope).toContainText("meeting");
    // first result visible within the viewport
    const result = page
      .locator(".chatsearch-view")
      .locator(".chatsearch-result")
      .first();
    await expect(result).toBeVisible();
    const rb = await result.boundingBox();
    expect(rb.y, "first result above the fold").toBeLessThan(640);
    // chip removal works
    const before = await chips.count();
    await chips.first().click();
    await expect
      .poll(async () => chips.count(), { timeout: 20000 })
      .toBeLessThan(before);
  });

  test("#3858 Apply filters has readable ink in dark mode, also on hover and focus", async ({
    page,
  }) => {
    await signIn(page);
    await openSearch(page, "meeting");
    const refine = page
      .locator(".chatsearch-view")
      .getByText("Refine search", { exact: true })
      .first();
    await expect(refine).toBeVisible();
    await refine.click({ timeout: 20000 });
    const apply = page.getByRole("button", { name: "Apply filters" });
    await expect(apply).toBeVisible();
    const ratio = async () =>
      apply.evaluate((el) => {
        const parse = (v) => {
          const n = v
            .match(/[\d.]+/g)
            .slice(0, 3)
            .map(Number);
          return v.startsWith("color(") ? n.map((x) => x * 255) : n;
        };
        const lum = (c) => {
          const [r, g, b] = c.map((v) => {
            v /= 255;
            return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
          });
          return 0.2126 * r + 0.7152 * g + 0.0722 * b;
        };
        const cs = getComputedStyle(el);
        const a = lum(parse(cs.color));
        const b = lum(parse(cs.backgroundColor));
        return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
      });
    expect(await ratio(), "rest").toBeGreaterThanOrEqual(4.5);
    await apply.focus();
    expect(await ratio(), "focus").toBeGreaterThanOrEqual(4.5);
    await apply.hover();
    expect(await ratio(), "hover").toBeGreaterThanOrEqual(4.5);
  });
});

test.describe("desktop", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("#3775 one search is primary inside Chat", async ({ page }) => {
    await signIn(page);
    await page.goto("/workspace/app/chat?locale=en-US");
    await expect(page.locator(".chat-workspace")).toBeVisible({ timeout: 40000 });
    const list = page.locator(".chat-rail #chat-search");
    await expect(list).toBeVisible();
    const listLabel =
      (await list.getAttribute("placeholder")) +
      " " +
      (await list.getAttribute("aria-label"));
    expect(listLabel).toMatch(/Search messages/);
    const top = page
      .locator(
        "header input[type=search], header [role=search] input, .workspace-search input",
      )
      .first();
    await expect(top).toBeVisible();
    const topLabel =
      (await top.getAttribute("placeholder")) +
      " " +
      (await top.getAttribute("aria-label"));
    expect(topLabel).toMatch(/Search pages and people/);
    const tb = await top.boundingBox();
    const lb = await list.boundingBox();
    expect(tb.width, "top bar field reduced").toBeLessThan(lb.width * 1.5);
    const op = await top.evaluate((el) => {
      let o = 1;
      for (let n = el; n; n = n.parentElement)
        o *= parseFloat(getComputedStyle(n).opacity);
      return o;
    });
    // eslint-disable-next-line no-undef
    console.log("top field width", tb.width, "list width", lb.width, "opacity", op);
  });
});
