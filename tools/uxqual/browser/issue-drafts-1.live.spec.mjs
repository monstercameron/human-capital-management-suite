import { test, expect } from "@playwright/test";

// Drafts, chunk 1 (#3993 mention identity survives leaving Chat and typing in
// another room, #3953 two tabs saving different drafts keep both versions,
// #3950 edits made while the backend is away are said to be unsaved and are
// retried), against the live dev cell. Everything that writes happens in the
// test's own zz-drafts channels.

test.setTimeout(240000);
test.use({ locale: "en-US" });

const composerSel = "textarea.composer-input";

async function track(page) {
  const state = { down: false, sent: 0 };
  await page.routeWebSocket(/\/workspace\/grpc/, (ws) => {
    const server = ws.connectToServer();
    ws.onMessage((m) => {
      state.sent++;
      if (!state.down) server.send(m);
    });
    server.onMessage((m) => ws.send(m));
    ws.onClose((c, r) => server.close({ code: c, reason: r }));
    server.onClose((c, r) => ws.close({ code: c, reason: r }));
  });
  page.wsState = state;
}

async function signIn(page, persona = "Darius Bennett") {
  await track(page);
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

function writesOf(page) {
  const base = page.wsState.sent;
  return {
    get length() {
      return page.wsState.sent - base;
    },
  };
}

async function pickMention(page, typed, person) {
  const input = page.locator(composerSel).first();
  await input.click();
  await input.fill("");
  await input.pressSequentially(typed, { delay: 30 });
  const option = page
    .locator(".mention-menu .mention-option", { hasText: person })
    .first();
  await expect(option).toBeVisible({ timeout: 30000 });
  await option.click();
}

test("#3993 a picked non-member keeps its warning after Chat -> Agents -> Chat and after typing in another room", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  const row = (n) =>
    page.locator(".chat-rail-row [data-action='select']", { hasText: n }).first();
  // Seeded rooms, drafts only (nothing is sent) and cleared at the end.
  await row("engineering").click();
  await expect(page.locator(".conversation-heading h1")).toContainText("engineering");
  await pickMention(page, "Could you review this, @Evelyn", "Evelyn Morgan");
  const note = page.locator(".composer-outside-note");
  await expect(note).toBeVisible({ timeout: 30000 });
  await expect(note).toContainText("Evelyn Morgan");

  // Chat -> Agents -> Conversations, by the app's own links.
  await page
    .getByRole("link", { name: /^Agents$/ })
    .first()
    .click();
  await page.waitForURL(/chat\/agents/, { timeout: 40000 });
  await page
    .getByRole("link", { name: /Conversations/ })
    .first()
    .click();
  await page.waitForURL(/\/chat(\?|#|$)/, { timeout: 40000 });
  await page.locator(composerSel).first().waitFor({ timeout: 80000 });
  if (
    !(await page.locator(".conversation-heading h1").innerText()).includes(
      "engineering",
    )
  )
    await row("engineering").click();
  await expect(page.locator(composerSel).first()).toHaveValue(/@Evelyn Morgan/, {
    timeout: 30000,
  });
  await expect(page.locator(".composer-outside-note")).toContainText("Evelyn Morgan", {
    timeout: 20000,
  });
  await expect(
    page
      .locator(".composer-outside-note")
      .getByRole("button", { name: /add/i })
      .first(),
  ).toBeVisible();

  // Type a different draft in another room, come back: the warning is still there.
  await row("general").click();
  await expect(page.locator(".conversation-heading h1")).toContainText("general");
  await page.locator(composerSel).first().fill("zz-drafts other room draft");
  await page.waitForTimeout(800);
  await row("engineering").click();
  await expect(page.locator(composerSel).first()).toHaveValue(/@Evelyn Morgan/, {
    timeout: 30000,
  });
  await expect(page.locator(".composer-outside-note")).toContainText("Evelyn Morgan", {
    timeout: 20000,
  });
  await page.locator(composerSel).first().fill("");
  await row("general").click();
  await page.locator(composerSel).first().fill("");
});

test("#3953 two tabs saving different drafts keep both versions and ask which to keep", async ({
  browser,
}) => {
  const ctx = await browser.newContext({ locale: "en-US" });
  const tabA = await ctx.newPage();
  await signIn(tabA, "Darius Bennett");
  const name = "general";
  await tabA.goto("/workspace/app/chat?locale=en-US");
  await tabA.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await tabA
    .locator(".chat-rail-row [data-action='select']", { hasText: name })
    .first()
    .click();
  await tabA.locator(composerSel).first().waitFor({ timeout: 40000 });
  const writesA = writesOf(tabA);
  const markerA = `zz-drafts mine ${Math.random().toString(36).slice(2, 6)}`;
  const markerB = `zz-drafts other ${Math.random().toString(36).slice(2, 6)}`;
  await tabA.locator(composerSel).first().fill(markerA);
  await expect.poll(() => writesA.length, { timeout: 30000 }).toBeGreaterThan(0);
  await tabA.waitForTimeout(2500);

  const tabB = await ctx.newPage();
  await track(tabB);
  const writesB = writesOf(tabB);
  await tabB.goto("/workspace/app/chat?locale=en-US");
  await tabB.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await tabB
    .locator(".chat-rail-row [data-action='select']", { hasText: name })
    .first()
    .click();
  await tabB.locator(composerSel).first().waitFor({ timeout: 40000 });
  await expect(tabB.locator(composerSel).first()).toHaveValue(markerA, {
    timeout: 30000,
  });
  await tabB.locator(composerSel).first().fill(markerB);
  await expect.poll(() => writesB.length, { timeout: 30000 }).toBeGreaterThan(0);
  await tabB.waitForTimeout(2500);

  const tabC0 = await ctx.newPage();
  await track(tabC0);
  await tabC0.goto("/workspace/app/chat?locale=en-US");
  await tabC0.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await tabC0
    .locator(".chat-rail-row [data-action='select']", { hasText: name })
    .first()
    .click();
  await tabC0.waitForTimeout(4000);
  await expect(tabC0.locator(composerSel).first()).toHaveValue(markerB);
  await tabC0.close();
  // Tab A edits once more (which refreshes its view of the account).
  await tabA.bringToFront();
  await tabA.locator(composerSel).first().press("End");
  await tabA.locator(composerSel).first().pressSequentially("!", { delay: 30 });
  await tabA.waitForTimeout(3000);

  const sync = tabA.locator(".composer-draft-sync.conflict");
  if (!(await sync.isVisible({ timeout: 12000 }).catch(() => false))) {
    // Second way to refresh: switch conversation and back inside the page.
    await tabA
      .locator(".chat-rail-row [data-action='select']", { hasText: "zz-" })
      .first()
      .click();
    await tabA.waitForTimeout(3000);
    await tabA
      .locator(".chat-rail-row [data-action='select']", { hasText: name })
      .first()
      .click();
    await tabA.waitForTimeout(3000);
  }
  await expect(sync).toBeVisible({ timeout: 30000 });
  await expect(sync).toContainText(markerB);
  for (const label of ["Keep mine", "Use the other draft", "Keep both"])
    await expect(sync.getByRole("button", { name: label })).toBeVisible();
  // Nothing was sent.
  await expect(tabA.locator(`.message:has-text("${markerA}")`)).toHaveCount(0);
  await sync.getByRole("button", { name: "Keep both" }).click();
  await expect(sync).toBeHidden();
  const merged = await tabA.locator(composerSel).first().inputValue();
  expect(merged).toContain(markerA);
  expect(merged).toContain(markerB);
  await tabA.locator(composerSel).first().fill("");
  await ctx.close();
});

test("#3950 edits made while saving fails are said to be unsaved, retried, and survive a reload once saved", async ({
  page,
}) => {
  await signIn(page, "Darius Bennett");
  const name = "general";
  await page.goto(`/workspace/app/chat?locale=en-US#channel=${name}`);
  await page.locator(composerSel).first().waitFor({ timeout: 80000 });
  const writes = writesOf(page);
  const base = `zz-drafts base ${Math.random().toString(36).slice(2, 6)}`;
  const extra = ` during-outage-${Math.random().toString(36).slice(2, 6)}`;
  const input = page.locator(composerSel).first();
  await input.fill(base);
  await expect.poll(() => writes.length, { timeout: 30000 }).toBeGreaterThan(0);
  await page.waitForTimeout(2500);

  // Backend away: every write to the draft/sidebar store fails.
  page.wsState.down = true;
  await input.press("End");
  await input.pressSequentially(extra, { delay: 20 });
  const unsaved = page.locator(".composer-draft-sync.unsaved");
  await expect(unsaved).toBeVisible({ timeout: 30000 });
  await expect(unsaved).toContainText("not saved yet");
  await expect(unsaved.getByRole("button", { name: "Save now" })).toBeVisible();
  // The text typed during the outage is still in the composer.
  await expect(input).toHaveValue(base + extra);

  // Backend back: the note clears by itself (retry with backoff, at most 30s).
  page.wsState.down = false;
  await expect(unsaved).toBeHidden({ timeout: 60000 });
  await page.waitForTimeout(1500);
  await page.goto(`/workspace/app/chat?locale=en-US#channel=${name}`);
  await page.reload();
  await page
    .locator(composerSel)
    .first()
    .waitFor({ timeout: 60000 })
    .catch(async () => {
      throw new Error("composer missing after reload");
    });
  await expect(page.locator(composerSel).first()).toHaveValue(base + extra, {
    timeout: 30000,
  });
  await page.locator(composerSel).first().fill("");
});
