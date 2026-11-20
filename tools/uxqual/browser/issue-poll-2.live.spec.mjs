import { test, expect } from "@playwright/test";

// Poll deadline zone (#3925), results-after-closing note (#3924) and kept
// draft settings (#3906), against the live dev cell. Every test works in its
// own zz-poll channel and cancels its previews; only the #3925 posted-card
// test posts, in its own channel.

test.setTimeout(240000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function createChannel(page) {
  const name = `zz-poll-${Math.random().toString(36).slice(2, 8)}`;
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
  return name;
}

async function ownChannel(page, locale = "en-US") {
  await page.goto(`/workspace/app/chat?locale=${locale}`);
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  return createChannel(page);
}

async function openChannel(page, name) {
  await page
    .locator(`a:has-text("${name}"), button:has-text("${name}")`)
    .first()
    .click();
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 30000 });
}

async function openPoll(page, text) {
  await page.locator("textarea.composer-input").first().fill(text);
  await expect(page.locator(".chatcmd003-preview")).toBeVisible({ timeout: 30000 });
}

async function viaRail(page, selector) {
  const link = selector.first();
  if (!(await link.isVisible())) {
    await page.getByRole("link", { name: "Expand navigation" }).click();
  }
  await link.click();
}

const cancel = (page) =>
  page.locator(".chatcmd003-preview [data-action='chatcmd003-cancel']").click();

const checked = (page, group) =>
  page.evaluate(
    (g) =>
      [...document.querySelectorAll(`#chatcmd003-${g} [role=radio]`)]
        .filter((b) => b.getAttribute("aria-checked") === "true")
        .map((b) => b.id),
    group,
  );

const OFFSET = /UTC([+-]\d{1,2}(:\d{2})?)?/;

for (const [locale, zoneNote, poll] of [
  [
    "en-US",
    /same moment in their own time zone/i,
    "/poll Which time?\nMorning\nAfternoon",
  ],
  ["de-DE", /eigenen Zeitzone/i, "/poll Welche Zeit?\nMorgen\nAbend"],
  ["ar", /بتوقيتهم المحلي/, "/poll س\nأ\nب"],
]) {
  test(`#3925 the closing time names its zone and says it is the viewer's (${locale})`, async ({
    page,
  }) => {
    await signIn(page);
    await ownChannel(page, locale);
    await openPoll(page, poll);
    for (const preset of ["today", "tomorrow"]) {
      await page.locator(`#chatcmd003-closes-${preset}`).click();
      const preview = page.locator(".chatcmd003-preview");
      await expect(preview).toContainText(OFFSET, { timeout: 20000 });
      await expect(preview).toContainText(zoneNote);
      await expect(page.locator("#chatcmd003-zone-note")).toBeVisible();
    }
    await cancel(page);
  });
}

test("#3925 the posted poll card names the zone too", async ({ page }) => {
  await signIn(page);
  await ownChannel(page);
  const marker = `Zone${Date.now().toString(36)}?`;
  await openPoll(page, `/poll ${marker}\nMorning\nAfternoon`);
  await page.locator("#chatcmd003-closes-tomorrow").click();
  await expect(page.locator(".chatcmd003-preview")).toContainText(OFFSET);
  await page.locator("#chatcmd003-post").click();
  const posted = page.locator(".message", { hasText: marker }).first();
  await expect(posted).toBeVisible({ timeout: 40000 });
  await expect(posted).toContainText(/Closes[^\n]*UTC/);
  await expect(posted).toContainText(/your time zone/i);
});

test("#3924 results after closing with no closing time explains itself and offers a closing time", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  await openPoll(page, "/poll Which time?\nMorning\nAfternoon");
  const note = page.locator("#chatcmd003-closes-note");
  await expect(note).toHaveCount(0);

  await page.locator("#chatcmd003-anonymous").click();
  await page.locator("#chatcmd003-results-after-closing").click();
  await expect(note).toBeVisible({ timeout: 20000 });
  await expect(note).toHaveText(/hidden until you close the poll/i);
  await expect(note).toHaveAttribute("role", "status");
  await expect(page.locator("#chatcmd003-post")).toBeEnabled();

  // One press picks a closing time and keeps everything else.
  await page.locator("#chatcmd003-closes-suggest").click();
  await expect(note).toHaveCount(0);
  expect(await checked(page, "closes")).toEqual(["chatcmd003-closes-today"]);
  expect(await checked(page, "results")).toEqual(["chatcmd003-results-after-closing"]);
  await expect(page.locator("#chatcmd003-anonymous")).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await expect(page.locator("textarea.composer-input").first()).toHaveValue(
    /Which time\?[\s\S]*Morning[\s\S]*Afternoon/,
  );
  // Focus did not fall to the page: it is on the chosen closing time.
  await expect
    .poll(
      () => page.evaluate(() => document.activeElement && document.activeElement.id),
      {
        timeout: 10000,
      },
    )
    .toBe("chatcmd003-closes-today");

  // Back to Never the note returns; results that do not wait do not need it.
  await page.locator("#chatcmd003-closes-never").click();
  await expect(note).toBeVisible();
  await page.locator("#chatcmd003-results-always").click();
  await expect(note).toHaveCount(0);
  await cancel(page);
});

test("#3924 the note holds on a 320 px phone", async ({ page }) => {
  await signIn(page);
  await ownChannel(page);
  await page.setViewportSize({ width: 320, height: 640 });
  await openPoll(page, "/poll Which time?\nMorning\nAfternoon");
  await page.locator("#chatcmd003-results-after-closing").click();
  const note = page.locator("#chatcmd003-closes-note");
  await expect(note).toBeVisible();
  const button = await page.locator("#chatcmd003-closes-suggest").boundingBox();
  const box = await note.boundingBox();
  expect(box.x + box.width).toBeLessThanOrEqual(321);
  expect(button.x + button.width).toBeLessThanOrEqual(321);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth + 1,
    ),
  ).toBe(true);
  await cancel(page);
});

test("#3906 poll settings come back after switching channels and after visiting Agents", async ({
  page,
}) => {
  await signIn(page);
  const first = await ownChannel(page);
  await openPoll(page, "/poll Pick a meeting time?\nMorning\nAfternoon");
  await page.locator("#chatcmd003-anonymous").click();
  await page.locator("#chatcmd003-results-after-voting").click();
  await page.locator("#chatcmd003-closes-tomorrow").click();

  const expectKept = async () => {
    await expect(page.locator(".chatcmd003-preview")).toBeVisible({ timeout: 30000 });
    await expect(page.locator("#chatcmd003-anonymous")).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(await checked(page, "results")).toEqual(["chatcmd003-results-after-voting"]);
    expect(await checked(page, "closes")).toEqual(["chatcmd003-closes-tomorrow"]);
  };

  // Another channel, then back.
  await createChannel(page);
  await expect(page.locator(".chatcmd003-preview")).toHaveCount(0);
  await openChannel(page, first);
  await expectKept();

  // Chat -> Agents -> Conversations, through the app's own links.
  await viaRail(page, page.locator("a[href*='chat/agents']"));
  await expect(page).toHaveURL(/chat\/agents/, { timeout: 30000 });
  await viaRail(page, page.getByRole("link", { name: "Conversations", exact: true }));
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 60000 });
  if (!(await page.locator(".chatcmd003-preview").count()))
    await openChannel(page, first);
  await expectKept();
  await cancel(page);
});

test("#3906 the to-do completion rule comes back after switching channels", async ({
  page,
}) => {
  await signIn(page);
  const first = await ownChannel(page);
  await openPoll(page, "/todo Prepare employee guide by tomorrow");
  await page.locator("#chatcmd003-tick-author").click();
  expect(await checked(page, "tick")).toEqual(["chatcmd003-tick-author"]);

  await createChannel(page);
  await openChannel(page, first);
  await expect(page.locator(".chatcmd003-preview")).toBeVisible({ timeout: 30000 });
  expect(await checked(page, "tick")).toEqual(["chatcmd003-tick-author"]);
  await cancel(page);
});

test("#3906 a cancelled draft does not leave its settings behind", async ({ page }) => {
  await signIn(page);
  await ownChannel(page);
  await openPoll(page, "/poll Cancel me?\nYes\nNo");
  await page.locator("#chatcmd003-anonymous").click();
  await cancel(page);
  await expect(page.locator(".chatcmd003-preview")).toBeHidden();
  await openPoll(page, "/poll Fresh poll?\nYes\nNo");
  await expect(page.locator("#chatcmd003-anonymous")).toHaveAttribute(
    "aria-checked",
    "false",
  );
  await cancel(page);
});
