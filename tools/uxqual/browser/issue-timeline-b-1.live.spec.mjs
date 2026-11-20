import { test, expect } from "@playwright/test";

// Timeline fixes against the live dev cell: pinning your own message in a
// channel you did not create (#3838), Copy text (#3718) and Arabic body
// alignment (#3841). Writes happen only in channels created here.

test.setTimeout(180000);

async function signIn(page, persona) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function createChannel(page, name) {
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
}

async function post(page, text) {
  const input = page.locator("textarea.composer-input").first();
  await input.fill(text);
  await input.press("Enter");
  const msg = page.locator(".message[data-message-id]", { hasText: text }).first();
  await expect(msg).toBeVisible({ timeout: 30000 });
  return msg;
}

async function openMenu(page, msg) {
  await msg.hover();
  await msg.getByRole("button", { name: "More", exact: true }).click();
  const menu = page.locator(".message-menu").first();
  await expect(menu).toBeVisible();
  return menu;
}

test("#3838 a member pins their own message in a channel they did not create", async ({
  browser,
}) => {
  const name = `zz-timeline-b-${Math.random().toString(36).slice(2, 8)}`;
  const owner = await browser.newContext();
  const op = await owner.newPage();
  await signIn(op, "Rafael Torres");
  await createChannel(op, name);

  const member = await browser.newContext();
  const mp = await member.newPage();
  await signIn(mp, "Linh Tran");
  await mp.goto(`/workspace/app/chat?locale=en-US#channel=${name}`);
  await mp
    .locator("textarea.composer-input, [data-action='join']")
    .first()
    .waitFor({ timeout: 80000 });
  const join = mp.locator("[data-action='join'], [data-action='join-channel']").first();
  if (await join.isVisible().catch(() => false)) await join.click();
  const marker = `zz-3838 mine ${Date.now()}`;
  const msg = await post(mp, marker);
  const menu = await openMenu(mp, msg);
  await menu.locator("[data-action='pin']").click();
  // No refusal toast; the message now shows as pinned and offers Unpin.
  await expect(mp.locator("body")).not.toContainText(/do not have access/i, {
    timeout: 5000,
  });
  const again = await openMenu(mp, msg);
  await expect(again.locator("[data-action='unpin']")).toBeVisible({ timeout: 15000 });
  await owner.close();
  await member.close();
});

test("#3718 Copy text confirms with 'Text copied' and copies plain text", async ({
  browser,
}) => {
  const context = await browser.newContext();
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const page = await context.newPage();
  await signIn(page, "Rafael Torres");
  const name = `zz-timeline-b-${Math.random().toString(36).slice(2, 8)}`;
  await createChannel(page, name);
  const tag = `zz3718${Date.now()}`;
  const msg = await post(page, `${tag} **bold** and \`code\` text`);
  // A stale link toast first, to prove it is replaced.
  let menu = await openMenu(page, msg);
  await menu.locator("[data-action='copy-link']").click();
  menu = await openMenu(page, msg);
  await menu.locator("[data-action='copy-contents']").click();
  await expect(page.getByText("Text copied", { exact: true }).first()).toBeVisible({
    timeout: 10000,
  });
  await expect(page.getByText("Link copied")).toHaveCount(0);
  const clip = await page.evaluate(() => navigator.clipboard.readText());
  expect(clip).toContain(`${tag} bold and code text`);
  expect(clip).not.toContain("**");
  expect(clip).not.toContain("`");
  await context.close();
});

test("#3841 Arabic: English message body stays under its author on the start side", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await signIn(page, "Rafael Torres");
  await page.goto("/workspace/app/chat?locale=ar#channel=general");
  const msg = page.locator(".message[data-message-id]").first();
  await expect(msg).toBeVisible({ timeout: 80000 });
  expect(
    await page.evaluate(() => getComputedStyle(document.documentElement).direction),
  ).toBe("rtl");
  const result = await page.evaluate(() => {
    const out = [];
    for (const m of document.querySelectorAll(".message[data-message-id]")) {
      const body = m.querySelector(".message-body");
      const author = m.querySelector(".message-author, .author, [class*='author']");
      if (!body || !author) continue;
      const b = body.getBoundingClientRect();
      const a = author.getBoundingClientRect();
      const mr = m.getBoundingClientRect();
      out.push({
        bodyRight: b.right,
        bodyLeft: b.left,
        authorRight: a.right,
        msgRight: mr.right,
        msgLeft: mr.left,
      });
    }
    return out;
  });
  expect(result.length).toBeGreaterThan(0);
  for (const r of result.slice(0, 12)) {
    // The body block is anchored beside the author (right side), not detached at the left edge.
    expect(r.bodyRight).toBeGreaterThan(r.authorRight - 40);
    expect(r.bodyLeft).toBeGreaterThan(r.msgLeft + 1);
  }
});
