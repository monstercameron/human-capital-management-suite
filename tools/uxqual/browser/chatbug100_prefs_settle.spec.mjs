import { test, expect } from "@playwright/test";

// CHATBUG-100: a Chat preferences block never loads forever (a failed read
// ends in its retry state within its bound), the languages line reflects the
// Reading setting, and controls sit at the row end. Needs a running server
// and browser, so it is specified here and not executed in unit runs.

const channel = "/workspace/app/chat?locale=en-US#channel=leadership-private";

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openPrefs(page) {
  await page.goto(channel);
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
    { timeout: 30000 },
  );
  await page.locator(".rail-prefs-control .chatux002-gear").click();
  const prefs = page.locator('[data-chat-layer="chat-prefs"]');
  await expect(prefs).toBeVisible({ timeout: 10000 });
  return prefs;
}

test("every preferences block settles within its bound", async ({ page }) => {
  const prefs = await openPrefs(page);
  const voice = prefs.locator('[data-prefs-section="voice"]');
  await expect(voice).toBeAttached({ timeout: 10000 });
  // Within fifteen seconds the voice row is a switch, the retry state, or
  // the one-line not-set-up note: never still loading.
  await expect
    .poll(
      async () =>
        (await voice.locator('[aria-busy="true"]').count()) === 0,
      { timeout: 15000 },
    )
    .toBe(true);
  const settled = await voice.innerText();
  expect(settled.length).toBeGreaterThan(0);
});

test("a failed voice read ends in Try again", async ({ page }) => {
  await signIn(page);
  await page.route("**/api/chat/voice/**", (route) =>
    route.fulfill({ status: 500, body: "voice down" }),
  );
  const prefs = await openPrefs(page);
  const voice = prefs.locator('[data-prefs-section="voice"]');
  await expect(voice.getByRole("button", { name: /try again/i })).toBeVisible({
    timeout: 20000,
  });
  await expect(voice.locator('[aria-busy="true"]')).toHaveCount(0);
});

test("reading controls sit at the row end and agree with each other", async ({
  page,
}) => {
  const prefs = await openPrefs(page);
  const select = prefs.locator("select.chatux031-select");
  await expect(select).toBeVisible({ timeout: 10000 });
  const rowBox = await prefs.locator(".chatux031-row").boundingBox();
  const selectBox = await select.boundingBox();
  const endBox = await prefs.locator(".chatux031-row .chatux031-end").boundingBox();
  expect(rowBox && selectBox && endBox).toBeTruthy();
  // The select ends where the tick slot begins: at the row end.
  expect(Math.abs(selectBox.x + selectBox.width - endBox.x)).toBeLessThan(2);
  expect(endBox.x + endBox.width).toBeLessThanOrEqual(rowBox.x + rowBox.width + 1);
  // The languages line names the setting instead of asking for it.
  const indicator = prefs.locator(".chatrender-indicator");
  await expect(indicator).toBeAttached({ timeout: 10000 });
  const text = (await indicator.innerText()) || "";
  const setting = await select.inputValue();
  if (setting) {
    expect(text).not.toMatch(/set your reading language/i);
  }
});
