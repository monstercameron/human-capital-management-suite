import { test, expect } from "@playwright/test";

// Poll preview settings (#3991 radio keyboard pattern, #3944 who can add
// options, #3926 stable relative deadline), against the live dev cell. Each
// test works in its own zz-poll channel. Only #3944 posts, in its own channel.

test.setTimeout(240000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function ownChannel(page, locale = "en-US") {
  const name = `zz-poll-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto(`/workspace/app/chat?locale=${locale}`);
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
  return name;
}

async function openPoll(page, text) {
  const input = page.locator("textarea.composer-input").first();
  await input.fill(text);
  await expect(page.locator(".chatcmd003-preview")).toBeVisible({ timeout: 30000 });
  return input;
}

const active = (page) =>
  page.evaluate(() => document.activeElement && document.activeElement.id);
const checked = (page, group) =>
  page.evaluate(
    (g) =>
      [...document.querySelectorAll(`#chatcmd003-${g} [role=radio]`)]
        .filter((b) => b.getAttribute("aria-checked") === "true")
        .map((b) => b.id),
    group,
  );
const tabStops = (page, group) =>
  page.evaluate(
    (g) =>
      [...document.querySelectorAll(`#chatcmd003-${g} [role=radio]`)]
        .filter((b) => b.tabIndex === 0)
        .map((b) => b.id),
    group,
  );

test("#3991 poll and to-do radio groups are one Tab stop and arrows move choice and focus", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  await openPoll(page, "/poll Which time works?\nMorning\nAfternoon");

  for (const group of ["results", "closes", "add"]) {
    const stops = await tabStops(page, group);
    expect(stops, `${group} has exactly one Tab stop`).toHaveLength(1);
    expect(await checked(page, group)).toEqual(stops);
  }

  await page.locator("#chatcmd003-results-always").focus();
  await page.keyboard.press("ArrowRight");
  await expect.poll(() => active(page)).toBe("chatcmd003-results-after-voting");
  await expect
    .poll(() => checked(page, "results"))
    .toEqual(["chatcmd003-results-after-voting"]);
  await expect
    .poll(() => tabStops(page, "results"))
    .toEqual(["chatcmd003-results-after-voting"]);
  await page.keyboard.press("ArrowLeft");
  await expect.poll(() => active(page)).toBe("chatcmd003-results-always");
  await page.keyboard.press("ArrowLeft");
  await expect.poll(() => active(page)).toBe("chatcmd003-results-after-closing");
  await expect
    .poll(() => checked(page, "results"))
    .toEqual(["chatcmd003-results-after-closing"]);
  await page.keyboard.press("Home");
  await expect.poll(() => active(page)).toBe("chatcmd003-results-always");
  await page.keyboard.press("End");
  await expect.poll(() => active(page)).toBe("chatcmd003-results-after-closing");

  const closesStop = (await tabStops(page, "closes"))[0];
  await page.locator(`#${closesStop}`).focus();
  await page.keyboard.press("ArrowRight");
  await expect.poll(() => active(page)).not.toBe(closesStop);
  const moved = await active(page);
  await expect.poll(() => checked(page, "closes")).toEqual([moved]);

  await page.locator("#chatcmd003-add-members").focus();
  await page.keyboard.press("Space");
  await expect.poll(() => checked(page, "add")).toEqual(["chatcmd003-add-members"]);
  await expect.poll(() => tabStops(page, "add")).toEqual(["chatcmd003-add-members"]);

  await page.locator(".chatcmd003-preview [data-action='chatcmd003-cancel']").click();
});

test("#3991 the to-do completion policy group takes arrows and keeps focus", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  await page
    .locator("textarea.composer-input")
    .first()
    .fill("/todo Prepare employee guide by tomorrow");
  await expect(page.locator("#chatcmd003-tick")).toBeVisible({ timeout: 30000 });
  const tick = (await tabStops(page, "tick"))[0];
  expect(tick).toBe("chatcmd003-tick-anyone");
  await page.locator(`#${tick}`).focus();
  await page.keyboard.press("ArrowRight");
  await expect.poll(() => active(page)).toBe("chatcmd003-tick-assignee");
  await expect.poll(() => checked(page, "tick")).toEqual(["chatcmd003-tick-assignee"]);
  await expect.poll(() => tabStops(page, "tick")).toEqual(["chatcmd003-tick-assignee"]);
  await expect.poll(() => active(page)).toBe("chatcmd003-tick-assignee");
  await page.keyboard.press("ArrowDown");
  await expect.poll(() => active(page)).toBe("chatcmd003-tick-author");
  await page.locator(".chatcmd003-preview [data-action='chatcmd003-cancel']").click();
});

test("#3991 in Arabic the Right arrow moves the other way", async ({ page }) => {
  await signIn(page);
  await ownChannel(page, "ar");
  await openPoll(page, "/poll س\nأ\nب");
  await page.locator("#chatcmd003-results-after-voting").focus();
  await page.keyboard.press("ArrowRight");
  await expect.poll(() => active(page)).toBe("chatcmd003-results-always");
  await page.keyboard.press("ArrowLeft");
  await expect.poll(() => active(page)).toBe("chatcmd003-results-after-voting");
});

test("#3944 the poll preview offers who can add options and the posted poll shows it", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  const marker = `Time${Date.now().toString(36)}?`;
  await openPoll(page, `/poll ${marker}\nMorning\nAfternoon`);

  const group = page.locator("#chatcmd003-add");
  await expect(group).toBeVisible();
  await expect(group).toHaveAttribute("role", "radiogroup");
  await expect(group.getByRole("radio")).toHaveCount(2);
  await expect.poll(() => checked(page, "add")).toEqual(["chatcmd003-add-author"]);
  const name = await group.getAttribute("aria-labelledby");
  await expect(page.locator(`#${name}`)).toHaveText(/add options/i);

  await page.setViewportSize({ width: 320, height: 640 });
  const box = await group.boundingBox();
  expect(box.x + box.width).toBeLessThanOrEqual(321);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth + 1,
    ),
  ).toBe(true);
  await page.setViewportSize({ width: 1440, height: 900 });

  await page.locator("#chatcmd003-add-members").click();
  await expect.poll(() => checked(page, "add")).toEqual(["chatcmd003-add-members"]);
  await expect(page.locator(".chatcmd003-preview")).toContainText(
    /Anyone in the conversation can add options/,
  );
  await page.locator("#chatcmd003-post").click();
  const posted = page.locator(".message", { hasText: marker }).first();
  await expect(posted).toBeVisible({ timeout: 40000 });
  await expect(posted).toContainText(/Anyone in the conversation can add options/);
});

test("#3926 a relative closing time does not drift while editing", async ({ page }) => {
  await signIn(page);
  await ownChannel(page);
  const input = await openPoll(page, "/poll Which time?\nMorning\nAfternoon");
  await page.locator("#chatcmd003-closes-hour").click();
  const closesText = async () => {
    const t = await page.locator(".chatcmd003-preview").innerText();
    const m = t.match(/Closes[^\n]*\d{1,2}:\d{2}[^\n]*/);
    return m && m[0];
  };
  await expect.poll(closesText, { timeout: 20000 }).not.toBeNull();
  const first = await closesText();

  // Let the minute hand cross a boundary, then edit.
  await page.waitForTimeout(61000 - (Date.now() % 60000));
  await input.focus();
  await input.press("Control+End");
  await page.keyboard.type("!");
  await expect(input).toHaveValue(/Afternoon!$/);
  await page.waitForTimeout(1500);
  expect(await closesText()).toBe(first);
});

test("#3926 a stale relative time asks for a second Post press", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  await signIn(page);
  await ownChannel(page);
  await page.clock.resume();
  await openPoll(page, "/poll Which stale time?\nMorning\nAfternoon");
  await page.locator("#chatcmd003-closes-hour").click();
  await page.clock.fastForward("10:00");
  await page.locator("#chatcmd003-post").click();
  await expect(page.locator(".chatcmd003-preview")).toContainText(
    /closing time was updated/i,
    {
      timeout: 20000,
    },
  );
  await expect(page.locator(".chatcmd003-preview")).toBeVisible();
});
