// CHATUX-046: Chat's rules are a stylesheet of their own that only a Chat page
// links. A person who reaches Chat from another page, by a click or by Back and
// Forward, must never see the Chat tree before that sheet is applied.
//
// The page is instrumented before any script runs: a MutationObserver watches
// for the first .chat-workspace element and, in the same task the element is
// inserted (before the browser can paint it), records whether a stylesheet whose
// address starts with /workspace/assets/chat- is applied. The sheet's response
// is delayed so the window in which an unstyled Chat could show is wide.
//
// The Chat entry the walk arrives at by history is made with window.history.pushState
// on the Home document, so it is an entry the click path never saw: its first
// arrival is by Back or Forward with the sheet not yet loaded.
//
// Not run by the lane that wrote it (lanes may not drive a browser); run it
// against the dev server like the other specs in this directory.

import { test, expect } from "@playwright/test";

const HOME = "/workspace/app/home";
const CHAT = "/workspace/app/chat";

async function signInAsRafael(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForLoadState("networkidle");
}

async function watchFirstChatPaint(page) {
  await page.addInitScript(() => {
    window.__chatSheetSamples = [];
    const chatSheetApplied = () =>
      [...document.styleSheets].some(
        (sheet) =>
          sheet.href &&
          new window.URL(sheet.href).pathname.startsWith("/workspace/assets/chat-"),
      );
    const check = (root) => {
      const found = root.matches?.(".chat-workspace")
        ? root
        : root.querySelector?.(".chat-workspace");
      if (found) {
        window.__chatSheetSamples.push({
          applied: chatSheetApplied(),
          path: window.location.pathname,
        });
      }
    };
    new window.MutationObserver((records) => {
      for (const record of records) {
        for (const node of record.addedNodes) {
          if (node.nodeType === 1) check(node);
        }
      }
    }).observe(document, { childList: true, subtree: true });
  });
}

test.describe("CHATUX-046 Chat is styled on first paint after Home, Chat, Back, Forward", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    // Make the sheet slow so an unstyled first paint would be visible.
    await page.route("**/workspace/assets/chat-*.css", async (route) => {
      await new Promise((resolve) => globalThis.setTimeout(resolve, 800));
      await route.continue();
    });
    await watchFirstChatPaint(page);
    await signInAsRafael(page);
  });

  test("the Home document names the sheet and does not load it", async ({ page }) => {
    await page.goto(HOME);
    await page.waitForLoadState("networkidle");
    await expect(page.locator('meta[name="hcm-chat-stylesheet"]')).toHaveCount(1);
    await expect(
      page.locator('link[rel="stylesheet"][href^="/workspace/assets/chat-"]'),
    ).toHaveCount(0);
  });

  test("a click into Chat waits for the sheet, and Back then Forward arrive styled", async ({
    page,
  }) => {
    await page.goto(HOME);
    await page.waitForLoadState("networkidle");
    await page.locator(`a[href="${CHAT}"]`).first().click();
    await expect(page.locator(".chat-workspace")).toBeVisible();
    await page.goBack();
    await expect(page).toHaveURL(new RegExp(`${HOME}$`));
    await page.goForward();
    await expect(page.locator(".chat-workspace")).toBeVisible();
    const samples = await page.evaluate(() => window.__chatSheetSamples);
    expect(samples.length).toBeGreaterThan(0);
    for (const sample of samples)
      expect(sample.applied, JSON.stringify(sample)).toBe(true);
  });

  test("a first arrival at Chat by history is held until the sheet is applied", async ({
    page,
  }) => {
    await page.goto(HOME);
    await page.waitForLoadState("networkidle");
    // A Chat entry the click path never saw, then back to Home, then forward.
    await page.evaluate((chat) => {
      window.history.pushState({}, "", chat);
      window.history.back();
    }, CHAT);
    await expect(page).toHaveURL(new RegExp(`${HOME}$`));
    await page.evaluate(() => window.history.forward());
    await expect(page).toHaveURL(new RegExp(`${CHAT}$`));
    await expect(page.locator(".chat-workspace")).toBeVisible({ timeout: 10_000 });
    const samples = await page.evaluate(() => window.__chatSheetSamples);
    expect(samples.length).toBeGreaterThan(0);
    for (const sample of samples)
      expect(sample.applied, JSON.stringify(sample)).toBe(true);
    // The sheet was loaded once, by the gate, with its integrity value.
    const link = page.locator(
      'link[rel="stylesheet"][href^="/workspace/assets/chat-"]',
    );
    await expect(link).toHaveCount(1);
    await expect(link).toHaveAttribute("integrity", /^sha256-/);
  });

  test("no Content-Security-Policy violation is reported while the sheet loads", async ({
    page,
  }) => {
    const violations = [];
    page.on("console", (message) => {
      if (
        /Content Security Policy|Refused to (load|apply) the stylesheet/i.test(
          message.text(),
        )
      ) {
        violations.push(message.text());
      }
    });
    await page.goto(HOME);
    await page.waitForLoadState("networkidle");
    await page.locator(`a[href="${CHAT}"]`).first().click();
    await expect(page.locator(".chat-workspace")).toBeVisible();
    expect(violations).toEqual([]);
  });
});
