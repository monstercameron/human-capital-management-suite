import { test, expect } from "@playwright/test";

// Live checks for the New conversation dialog: #3914 (sticky heading is
// opaque and sits above scrolled cards), #3877 (arrow keys in the type radio
// group), #3876 (Escape closes recipient suggestions before the dialog).

const chat = "/workspace/app/chat?locale=en-US#channel=general";
test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openCreate(page) {
  await page.goto(chat);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  const drawer = page.getByRole("button", { name: "Open conversations" });
  if (await drawer.isVisible()) await drawer.click();
  await page.getByRole("button", { name: "New conversation" }).first().click();
  const dialog = page.locator(".create-dialog");
  await expect(dialog).toBeVisible();
  return dialog;
}

for (const size of [
  { width: 320, height: 740 },
  { width: 390, height: 844 },
]) {
  test(`#3914 sticky heading is opaque and above the cards at ${size.width}px`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
      viewport: size,
      hasTouch: true,
      isMobile: true,
    });
    const page = await context.newPage();
    await signIn(page);
    const dialog = await openCreate(page);
    await dialog.locator("[data-action='create-kind'][data-id='group']").click();
    await dialog.locator("#new-chat-member-search").fill("a");
    const heading = dialog.locator(".side-heading").first();
    const info = await heading.evaluate((el) => {
      const cs = window.getComputedStyle(el);
      return { bg: cs.backgroundColor, z: cs.zIndex, pos: cs.position };
    });
    expect(info.pos).toBe("sticky");
    expect(info.bg).not.toMatch(/rgba\(0, 0, 0, 0\)|transparent/);
    expect(Number(info.z)).toBeGreaterThanOrEqual(2);
    // Scroll the dialog so the cards pass under the heading; the element at
    // the heading's centre must be the heading itself (or a child), not a card.
    await dialog.evaluate((el) => {
      const s = [el, ...el.querySelectorAll("*")].find(
        (n) =>
          n.scrollHeight > n.clientHeight + 5 &&
          /auto|scroll/.test(window.getComputedStyle(n).overflowY),
      );
      if (s) s.scrollTop = 120;
    });
    const hit = await heading.evaluate((el) => {
      const r = el.getBoundingClientRect();
      const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return el.contains(top);
    });
    expect(hit).toBe(true);
    await context.close();
  });
}

test("#3877 arrow keys move and select among conversation types", async ({ page }) => {
  await signIn(page);
  const dialog = await openCreate(page);
  const cards = dialog.locator(".kind-card");
  expect(await cards.count()).toBe(4);
  const state = () =>
    page.evaluate(() => {
      const cs = [...document.querySelectorAll(".create-dialog .kind-card")];
      return {
        checked: cs.findIndex((c) => c.getAttribute("aria-checked") === "true"),
        focus: cs.findIndex((c) => c === document.activeElement),
        tabbable: cs.filter((c) => c.tabIndex === 0).length,
      };
    });
  const expectState = async (checked) => {
    await expect
      .poll(state, { timeout: 10000 })
      .toMatchObject({ checked, focus: checked, tabbable: 1 });
  };
  // Nothing is chosen at first: exactly one card is a tab stop.
  expect((await state()).tabbable).toBe(1);
  await cards.first().focus();
  await page.keyboard.press("ArrowRight");
  await expectState(0);
  await page.keyboard.press("ArrowRight");
  await expectState(1);
  await page.keyboard.press("ArrowDown");
  await expectState(2);
  await page.keyboard.press("ArrowUp");
  await expectState(1);
  await page.keyboard.press("ArrowLeft");
  await expectState(0);
  await page.keyboard.press("ArrowLeft");
  await expectState(3);
  await page.keyboard.press("ArrowRight");
  await expectState(0);
  await page.keyboard.press("End");
  await expectState(3);
  await page.keyboard.press("Home");
  await expectState(0);
  // Tab leaves the group.
  await page.keyboard.press("Tab");
  await expect.poll(async () => (await state()).focus).toBe(-1);
  // The form follows the selection: Group message shows the people search.
  await cards.nth(2).click();
  await expect(dialog.locator("#new-chat-member-search")).toBeVisible();
});

test("#3876 Escape closes recipient suggestions first and keeps the form", async ({
  page,
}) => {
  await signIn(page);
  const dialog = await openCreate(page);
  await dialog.locator("[data-action='create-kind'][data-id='group']").click();
  const name = dialog.locator("#new-chat-name");
  await name.fill("zz-escape-check");
  const search = dialog.locator("#new-chat-member-search");
  await search.fill("Darius");
  const options = dialog.locator("#new-chat-member-options [role='option']");
  await expect(options.first()).toBeVisible();
  await options.first().click();
  await expect(dialog.locator(".person-chip")).toHaveCount(1);
  await dialog.locator("#new-chat-member-search").fill("Camila");
  await expect(
    dialog.locator("#new-chat-member-options [role='option']").first(),
  ).toBeVisible();
  await dialog.locator("#new-chat-member-search").press("Escape");
  await expect(dialog).toBeVisible();
  await expect(dialog.locator("#new-chat-member-options")).toHaveCount(0);
  await expect(dialog.locator("#new-chat-member-search")).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  await expect(dialog.locator(".person-chip")).toHaveCount(1);
  await expect(name).toHaveValue("zz-escape-check");
  await expect(dialog.locator("#new-chat-member-search")).toHaveValue("Camila");
  // Typing reopens the list; Escape twice closes the dialog.
  await dialog.locator("#new-chat-member-search").press("Escape");
  await expect(dialog).toBeHidden();
});
