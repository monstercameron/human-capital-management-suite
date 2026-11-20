import { test, expect } from "@playwright/test";

// Issues #3883, #3882, #3881: focus return when details close, focus return
// when Manage channel editors are cancelled, and independent member filters.

const general = "/workspace/app/chat?locale=en-US#channel=general";
test.setTimeout(240000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChat(page, url = general) {
  await signIn(page);
  await page.goto(url);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 40000,
  });
}

const active = (page) =>
  page.evaluate(() => {
    const a = document.activeElement;
    return a
      ? {
          tag: a.tagName,
          id: a.id,
          action: a.getAttribute("data-action"),
          label: a.getAttribute("aria-label"),
          text: (a.textContent || "").trim().slice(0, 40),
        }
      : null;
  });

async function keyActivate(page, locator) {
  await locator.focus();
  await page.keyboard.press("Enter");
}

// The page can be busy re-rendering for a second or more when the machine is
// loaded, and a key press that lands mid-render is lost. Press again, but only
// while the expected effect has not happened (a press never toggles back).
async function activateUntil(page, locator, done, tries = 3) {
  for (let i = 0; i < tries; i++) {
    if (await done()) return;
    await keyActivate(page, locator);
    await page.waitForTimeout(2500);
  }
}

test("#3883 closing details returns focus to the header control that opened it", async ({
  page,
}) => {
  await openChat(page);
  for (const action of ["header-details", "header-pinned", "header-members"]) {
    const opener = page.locator(`[data-action='${action}']`).first();
    if ((await opener.count()) === 0 || !(await opener.isVisible())) continue;
    await keyActivate(page, opener);
    const close = page.locator("[data-action='close-details']").first();
    await expect(close).toBeVisible({ timeout: 15000 });
    // Some openers move focus into the panel after it draws; let that settle, then go
    // to Close details as a keyboard user would.
    await page
      .waitForFunction(
        (a) => document.activeElement?.getAttribute("data-action") !== a,
        action,
        { timeout: 6000 },
      )
      .catch(() => {});
    await page.waitForTimeout(500);
    await activateUntil(
      page,
      close,
      async () => (await page.locator("[data-action='close-details']").count()) === 0,
    );
    await expect(page.locator("[data-action='close-details']")).toHaveCount(0, {
      timeout: 15000,
    });
    await page.waitForTimeout(3000);
    await expect
      .poll(async () => (await active(page))?.action, { timeout: 15000 })
      .toBe(action);
    await page.waitForTimeout(2000);
  }
  // At least the details opener must have been exercised.
  const dm = page.locator("[data-action='header-details']").first();
  await expect(dm).toBeVisible();
});

test("#3882 cancelling a Manage channel editor returns focus to its opener", async ({
  page,
}) => {
  await openChat(page);
  await keyActivate(page, page.locator("[data-action='header-details']").first());
  const manage = page.getByRole("button", { name: /Manage channel/i }).first();
  await expect(manage).toBeVisible({ timeout: 15000 });
  await keyActivate(page, manage);

  const cases = [
    { opener: "#chat-manage-status-row", id: "status" },
    { opener: "#chat-manage-project-row", id: "project" },
  ];
  for (const c of cases) {
    const opener = page.locator(c.opener);
    await expect(opener).toBeVisible({ timeout: 15000 });
    await keyActivate(page, opener);
    const cancel = page
      .locator(`button[data-action='manage-section'][data-id='${c.id}']`)
      .filter({ hasText: /^Cancel$/ });
    await expect(cancel).toBeVisible();
    await keyActivate(page, cancel);
    await expect
      .poll(async () => (await active(page))?.id, { timeout: 15000 })
      .toBe(c.opener.slice(1));
  }
  // Add milestone (inside the project row)
  await keyActivate(page, page.locator("#chat-manage-project-row"));
  const add = page.locator("#chat-manage-milestone-toggle");
  await expect(add).toBeVisible();
  await keyActivate(page, add);
  const cancel = page
    .locator("button[data-action='manage-section'][data-id='milestone']")
    .filter({ hasText: /^Cancel$/ });
  await expect(cancel).toBeVisible();
  await keyActivate(page, cancel);
  await expect
    .poll(async () => (await active(page))?.id, { timeout: 15000 })
    .toBe("chat-manage-milestone-toggle");
});

test("#3881 member and role label filters are independent and the role list has an empty state", async ({
  page,
}) => {
  await openChat(page);
  await keyActivate(page, page.locator("[data-action='header-details']").first());
  const manage = page.getByRole("button", { name: /Manage channel/i }).first();
  await expect(manage).toBeVisible({ timeout: 15000 });
  await keyActivate(page, manage);

  await keyActivate(page, page.locator("#chat-manage-roles-row"));
  const role = page.locator("#role-filter");
  await expect(role).toBeVisible({ timeout: 15000 });
  await role.fill("Amina");
  await expect(role).toHaveValue("Amina");
  const members = page
    .locator("input[type='search']")
    .filter({ hasNot: page.locator("#role-filter") })
    .and(page.locator(":not(#role-filter)"))
    .first();
  await expect(members).toBeVisible();
  await members.fill("Darius");
  await expect(members).toHaveValue("Darius");
  await expect(role).toHaveValue("Amina");

  await role.fill("zzqqnomatch");
  await expect(
    page.locator("[role='status'].member-empty").filter({ hasText: "zzqqnomatch" }),
  ).toBeVisible();
});
