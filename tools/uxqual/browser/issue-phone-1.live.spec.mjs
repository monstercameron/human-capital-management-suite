/* global getComputedStyle, console */
import { test, expect, devices } from "@playwright/test";

// Phone lane, chunk 1: #3867 full-screen panels own keyboard focus, #3768 Back
// from a conversation shows the list first, #3764 44px touch targets. Runs
// against the dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default).

test.use({
  viewport: { width: 354, height: 767 },
  hasTouch: true,
  isMobile: true,
  colorScheme: "dark",
  userAgent: devices["Pixel 7"].userAgent,
});
test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChat(page, hash = "#channel=general") {
  await page.goto("/workspace/app/chat?locale=en-US" + hash);
  // The first load races the sidebar save on a busy server: reload once.
  await page.reload();
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 90000,
  });
}

// Where focus is, relative to a panel selector.
async function focusIn(page, selector) {
  return page.evaluate((sel) => {
    const panel = document.querySelector(sel);
    const el = document.activeElement;
    return {
      inside: !!panel && !!el && panel.contains(el),
      desc: el
        ? `${el.tagName}.${el.className}|${el.getAttribute("aria-label") || ""}`
        : "none",
    };
  }, selector);
}

async function tabStaysIn(page, selector, presses = 12) {
  for (let i = 0; i < presses; i++) {
    await page.keyboard.press("Tab");
    const state = await focusIn(page, selector);
    expect(state.inside, `Tab ${i + 1} left ${selector}: ${state.desc}`).toBe(true);
  }
}

test("#3867 thread, details and Saved panels own focus and return it", async ({
  page,
}) => {
  await signIn(page);
  await openChat(page, "#channel=zz-thread-a-7u6xu");

  // 1. Thread from a "N replies" control.
  const replies = page.locator(".message-stats").first();
  await expect(replies).toBeVisible({ timeout: 30000 });
  await replies.focus();
  await page.keyboard.press("Enter");
  const thread = page.locator(".chat-side.thread-pane");
  await expect(thread).toBeVisible();
  await expect
    .poll(() => focusIn(page, ".chat-side.thread-pane").then((s) => s.inside))
    .toBe(true);
  await tabStaysIn(page, ".chat-side.thread-pane");
  await thread.getByRole("button", { name: "Close thread" }).focus();
  await page.keyboard.press("Enter");
  await expect(thread).toBeHidden();
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          const el = document.activeElement;
          return `${el.tagName}|${el.getAttribute("aria-label") || el.textContent.trim().slice(0, 30)}`;
        }),
      { message: "focus returns to the reply control", timeout: 10000 },
    )
    .toMatch(/repl/i);

  // 2. Conversation details.
  const details = page.getByRole("button", { name: "Conversation details" });
  await details.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator(".chat-side.chat-details")).toBeVisible();
  await expect
    .poll(() => focusIn(page, ".chat-side.chat-details").then((s) => s.inside))
    .toBe(true);
  await tabStaysIn(page, ".chat-side.chat-details");
  await page
    .locator(".chat-side.chat-details")
    .getByRole("button", { name: /^close/i })
    .first()
    .focus();
  await page.keyboard.press("Enter");
  await expect(page.locator(".chat-side.chat-details")).toBeHidden();
  await expect
    .poll(
      () =>
        page.evaluate(() => document.activeElement.getAttribute("aria-label") || ""),
      {
        message: "focus returns to Conversation details",
        timeout: 10000,
      },
    )
    .toBe("Conversation details");

  // 3. Saved from the list.
  await page.getByRole("button", { name: /Open conversations/ }).click();
  const saved = page.getByRole("button", { name: /^Saved/ }).first();
  await expect(saved).toBeVisible();
  await saved.click();
  await expect(page.locator(".chatsave-panel:not([hidden])")).toBeVisible();
  await expect
    .poll(() => focusIn(page, ".chatsave-panel").then((s) => s.inside))
    .toBe(true);
  await page.keyboard.press("Tab");
  expect((await focusIn(page, ".chatsave-panel")).inside, "first Tab in Saved").toBe(
    true,
  );
  await tabStaysIn(page, ".chatsave-panel", 8);
});

test("#3768 Back from a conversation shows the list, then the previous conversation", async ({
  page,
}) => {
  await signIn(page);
  await page.goto("/workspace/app/home");
  await openChat(page, "#channel=general");
  const listVisible = async () =>
    page.evaluate(() => {
      const r = document.querySelector(".chat-rail");
      if (!r) return false;
      const b = r.getBoundingClientRect();
      const cs = getComputedStyle(r);
      return (
        cs.display !== "none" &&
        cs.visibility !== "hidden" &&
        b.right > 20 &&
        b.width > 100
      );
    });
  await expect(page.getByRole("button", { name: /Open conversations/ })).toBeVisible({
    timeout: 60000,
  });
  await expect
    .poll(
      async () => {
        if (!(await listVisible())) {
          await page.getByRole("button", { name: /Open conversations/ }).click();
          await page.waitForTimeout(500);
        }
        return listVisible();
      },
      { timeout: 60000 },
    )
    .toBe(true);
  const row = page
    .locator(".chat-rail .chat-row")
    .filter({ hasText: /#engineering/ })
    .first();
  await row.click();
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 30000,
  });
  expect(await listVisible(), "list closed after choosing").toBe(false);
  const chatUrl = page.url();

  await page.goBack();
  await expect
    .poll(listVisible, { message: "the list shows after one Back", timeout: 30000 })
    .toBe(true);
  expect(page.url(), "still inside Chat after one Back").toContain(
    "/workspace/app/chat",
  );

  await page.goBack();
  await expect
    .poll(listVisible, { message: "second Back leaves the list", timeout: 30000 })
    .toBe(false);
  expect(page.url(), "second Back stays in Chat").toContain("/workspace/app/chat");

  await page.goForward();
  await page.goForward();
  await expect
    .poll(() => page.url(), { message: "Forward replays", timeout: 30000 })
    .toBe(chatUrl);
  await expect.poll(listVisible, { timeout: 30000 }).toBe(false);
});

test("#3764 phone touch targets are at least 44px", async ({ page }) => {
  await signIn(page);
  await page.setViewportSize({ width: 375, height: 812 });
  await openChat(page, "#channel=zz-thread-a-7u6xu");
  const measure = (sel) =>
    page.evaluate(
      (s) =>
        [...document.querySelectorAll(s)]
          .filter((e) => e.getClientRects().length)
          .map((e) => {
            const b = e.getBoundingClientRect();
            return {
              label: (e.getAttribute("aria-label") || e.textContent || "")
                .trim()
                .slice(0, 30),
              w: Math.round(b.width),
              h: Math.round(b.height),
            };
          }),
      sel,
    );
  const small = [];
  const check = (name, items) => {
    for (const i of items)
      if (i.h < 44 || i.w < 44) small.push(`${name}: ${i.label} ${i.w}x${i.h}`);
    return items.length;
  };

  const n1 = check(
    "reply link",
    await measure(".message-stats, .message-meta button, .thread-notify"),
  );
  const n2 = check("reaction", await measure(".reaction"));
  console.log("conversation controls measured", n1, n2);

  await page.getByRole("button", { name: /Open conversations/ }).click();
  await page.waitForTimeout(800);
  const rows = check("row", await measure(".chat-rail .chat-row"));
  const menus = check("row menu", await measure(".chat-rail .rail-row-more"));
  const sect = check("section menu", await measure(".chat-rail .section-menu-trigger"));
  console.log("list controls measured", rows, menus, sect);

  await page
    .getByRole("button", { name: /^Saved/ })
    .first()
    .click();
  await expect(page.locator(".chatsave-panel:not([hidden])")).toBeVisible();
  const tabs = check("saved tab", await measure(".chatsave-seg-button"));
  console.log("saved tabs measured", tabs);

  console.log("undersized:", JSON.stringify(small, null, 1));
  expect(
    rows + menus + sect + n1 + n2 + tabs,
    "some controls were measured",
  ).toBeGreaterThan(10);
  expect(small, "controls under 44px").toEqual([]);
});
