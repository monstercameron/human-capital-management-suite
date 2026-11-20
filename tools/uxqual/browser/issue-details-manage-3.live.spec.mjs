import { test, expect } from "@playwright/test";

// Issues #3874 (focus return from person details), #3870 (section jumps below
// the sticky details header) and #3763 (Arabic counts in details headings).

test.setTimeout(240000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChat(page, channel, locale = "en-US") {
  await signIn(page);
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${channel}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 90000,
  });
}

const active = (page) =>
  page.evaluate(() => {
    const a = document.activeElement;
    return a
      ? {
          tag: a.tagName,
          action: a.getAttribute("data-action"),
          id: a.getAttribute("data-id"),
          cls: a.className,
          text: (a.textContent || "").trim().slice(0, 40),
        }
      : null;
  });

async function keyActivate(page, locator) {
  await locator.focus();
  await page.keyboard.press("Enter");
}

test("#3874 Close returns to the author after following a manager; Back returns into details", async ({
  page,
}) => {
  await openChat(page, "general");
  // Find an author control whose person has a Manager link in the pane.
  const authors = page.locator(".message [data-action='open-person']");
  const n = Math.min(await authors.count(), 6);
  let origin = null;
  for (let i = 0; i < n; i++) {
    const a = authors.nth(i);
    await keyActivate(page, a);
    const pane = page.locator(".person-pane");
    await expect(pane).toBeVisible({ timeout: 15000 });
    await page.waitForTimeout(1500);
    const mgr = pane.locator("[data-action='open-person']").first();
    if ((await mgr.count()) > 0) {
      origin = { id: await a.getAttribute("data-id") };
      await keyActivate(page, mgr);
      await page.waitForTimeout(2500);
      break;
    }
    await keyActivate(page, pane.locator("[data-action='close-person']"));
    await page.waitForTimeout(1500);
  }
  expect(origin, "an author with a manager link").not.toBeNull();
  for (let i = 0; i < 4 && (await page.locator(".person-pane").count()) > 0; i++) {
    await keyActivate(page, page.locator("[data-action='close-person']").first());
    await page.waitForTimeout(3000);
  }
  await expect(page.locator(".person-pane")).toHaveCount(0, { timeout: 15000 });
  await page.waitForTimeout(2000);
  const after = await active(page);
  expect(after.action).toBe("open-person");
  expect(after.id).toBe(origin.id);

  // Path 2: details -> member -> Back.
  await keyActivate(page, page.locator("[data-action='header-details']").first());
  await expect(page.locator("[data-action='close-details']").first()).toBeVisible({
    timeout: 15000,
  });
  await page.waitForTimeout(2000);
  const member = page.locator(".chat-side [data-action='open-person']").first();
  await expect(member).toBeVisible();
  const memberId = await member.getAttribute("data-id");
  await keyActivate(page, member);
  await expect(page.locator(".person-pane")).toBeVisible({ timeout: 15000 });
  await page.waitForTimeout(1500);
  await keyActivate(page, page.locator("[data-action='person-back']").first());
  await page.waitForTimeout(3000);
  const back = await active(page);
  expect(back.tag).not.toBe("BODY");
  expect(back.action === "open-person" && back.id === memberId).toBeTruthy();
});

test("#3870 section jumps park the heading below the sticky details header", async ({
  page,
}) => {
  await openChat(page, "engineering");
  for (const action of ["header-members", "header-pinned"]) {
    const opener = page.locator(`[data-action='${action}']`).first();
    if ((await opener.count()) === 0 || !(await opener.isVisible())) continue;
    await keyActivate(page, opener);
    await expect(page.locator(".chat-side .side-heading").first()).toBeVisible({
      timeout: 15000,
    });
    await page.waitForTimeout(2500);
    const res = await page.evaluate(
      (id) => {
        const head = document.querySelector(".chat-side .side-heading");
        const hb = head.getBoundingClientRect();
        const sec = document.getElementById(id);
        const first = sec ? sec.firstElementChild : null;
        const hr = first ? first.getBoundingClientRect() : null;
        return {
          headerBottom: hb.bottom,
          headingTop: hr ? hr.top : null,
          focusOnSection: document.activeElement === sec,
          pad: window.getComputedStyle(document.querySelector(".chat-side"))
            .scrollPaddingTop,
        };
      },
      action === "header-members" ? "chat-details-members" : "chat-details-pinned",
    );
    expect(res.headingTop, JSON.stringify(res)).not.toBeNull();
    expect(res.headingTop).toBeGreaterThanOrEqual(res.headerBottom - 1);
    const close = page.locator("[data-action='close-details']").first();
    await keyActivate(page, close);
    await page.waitForTimeout(2500);
  }
});

test("#3763 Arabic details headings carry the count in parentheses, no dot", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await openChat(page, "engineering", "ar");
  const opener = page.locator("[data-action='header-details']").first();
  await keyActivate(page, opener);
  await expect(page.locator("[data-action='close-details']").first()).toBeVisible({
    timeout: 15000,
  });
  await page.waitForTimeout(2500);
  const heads = (
    await page
      .locator(
        ".chat-side h2, .chat-side h3, .chat-side h4, .chat-side .details-section > :first-child",
      )
      .allInnerTexts()
  )
    .map((s) => s.trim())
    .filter((s) => /[٠-٩]/.test(s) && s.length < 40);
  expect(heads.length).toBeGreaterThan(0);
  for (const h of heads) expect(h).not.toMatch(/·\s*[\u0660-\u0669]/);
  expect(heads.some((h) => /\([\u0660-\u0669]+\)/.test(h))).toBeTruthy();
});
