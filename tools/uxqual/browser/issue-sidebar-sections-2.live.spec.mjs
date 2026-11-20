import { test, expect } from "@playwright/test";

// Lane sidebar-sections, chunk 2: #3721 unread badge, #3603 denied link recovery,
// #3892 skip link keeps the address. Runs against the dev cell (127.0.0.1:8080).

test.setTimeout(120000);
test.use({ viewport: { width: 1440, height: 900 } });

async function signIn(page, persona = "Rafael Torres") {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("#3721 unread badge counts or explains how it counts", async ({ page }) => {
  await signIn(page);
  await page.goto("/workspace/app/chat?locale=en-US#channel=engineering");
  const others = page.locator(".message[data-message-id]:not(.own)");
  await expect(others.first()).toBeVisible({ timeout: 60000 });
  const first = others.last();
  await first.hover();
  await page.locator("button[data-action='menu']").first().click();
  await page
    .getByRole("menuitem", { name: /Mark unread/i })
    .first()
    .click();
  // Leave the channel, then read the sidebar row of the one just marked.
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  const badge = page
    .getByRole("button", { name: /engineering/ })
    .locator(".chat-badge")
    .first();
  await expect(badge).toBeVisible({ timeout: 20000 });
  const label = (await badge.getAttribute("aria-label")) || "";
  const title = (await badge.getAttribute("title")) || "";
  const count = parseInt(label, 10);
  expect(count).toBeGreaterThanOrEqual(1);
  // Expected: the count reflects the marked point, or the UI explains the rule.
  const explains = /other people|own messages/i.test(title);
  expect(count > 1 || explains, `badge ${label} title=${title}`).toBeTruthy();
  expect(explains, `badge ${label} title=${title}`).toBeTruthy();
});

test("#3603 denied private link does not leave Chat stuck as not set up", async ({
  page,
}) => {
  await signIn(page, "Linh Tran");
  await page.goto("/workspace/app/chat?locale=en-US");
  await expect(
    page.getByRole("button", { name: /New conversation/i }).first(),
  ).toBeVisible({
    timeout: 40000,
  });
  await page.goto("/workspace/app/chat?locale=en-US#channel=leadership-private");
  await expect(page.getByText("You do not have access to this channel.")).toBeVisible({
    timeout: 40000,
  });
  const body1 = await page.locator("body").innerText();
  expect(body1).not.toMatch(/Executive|leadership-private roadmap/i);
  // The ordinary shell Chat link: no reload, the hash goes away.
  await page.getByRole("link", { name: "Chat", exact: true }).click();
  await expect(page).toHaveURL(/\/workspace\/app\/chat\?locale=en-US$/);
  await expect(
    page.getByRole("button", { name: /New conversation/i }).first(),
  ).toBeVisible({
    timeout: 20000,
  });
  await expect(page.getByText("You do not have access to this channel.")).toHaveCount(
    0,
  );
  await expect(page.getByText("Not set up for this workspace")).toHaveCount(0);
});

test("#3892 Skip to conversation keeps the address and the title", async ({ page }) => {
  await signIn(page);
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 40000,
  });
  const urlBefore = page.url();
  const titleBefore = await page.title();
  const skip = page.locator("a.chat-skip");
  await skip.focus();
  await expect(skip).toBeFocused();
  await page.keyboard.press("Enter");
  expect(page.url()).toBe(urlBefore);
  expect(await page.title()).toBe(titleBefore);
  expect(page.url()).toContain("#channel=general");
  const active = await page.evaluate(
    () => document.activeElement && document.activeElement.id,
  );
  expect(active).toBe("chat-main");
});
