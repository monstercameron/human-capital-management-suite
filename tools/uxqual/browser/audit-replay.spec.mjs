// Replays fix-nav.md / ui9 audit findings CHAT-01..08, DOCS-01..07, CROSS-01,
// ACCESS-01/02, and a handful of standalone one-off fixes, against the LIVE
// review server (baseURL from HCMNEXT_DEV_URL / playwright.config.mjs's
// default). One test per finding id, screenshotting after each assertion
// step. See tools/uxqual/browser/nav-history.spec.mjs for NAV-01/CHAT-03,
// which is a separate spec re-run alongside this one, not duplicated here.
//
// Selectors below were read directly out of the product source that renders
// this server (internal/humanwork/chatui, internal/humanwork/productui) and
// cross-checked against the live DOM, not guessed:
//   - chat message actions/menu: internal/humanwork/chatui/render.go
//     (.message-actions, .message-menu, data-action="edit"/"cancel-edit",
//     .message-edit textarea#edit-<id>)
//   - reactions: reactionRow()/quick-react toolbar in the same file
//     (data-action="react-with"/"react-pick"/"toggle-reaction", aria-label
//     "Add reaction" / "React with {emoji}")
//   - create/browse dialogs, add-members: internal/humanwork/chatui/dialogs.go
//   - channel poll: internal/humanwork/chatui/channel_poll.go (#chat-poll-section)
//   - channel todo: render.go (#chat-todo-new, "Add task")
//   - rail "more" menu / Copy API curl: render.go railMenu() (CHAT-08 comment)
//   - docs table/rows, owner fragment, PDF cards, chat chips, compare
//     versions, remove/undo, share role select: internal/humanwork/productui
//     (docs_library.go, docs_media.go, docs_chat_chips.go, docs_page.go,
//     docs_remove.go, docs_share.go)
/* global getComputedStyle */

import { test, expect } from "@playwright/test";

test.describe.configure({ mode: "serial" });

function ts() {
  return Date.now().toString(36);
}

async function signInAs(page, name) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${name}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForLoadState("networkidle");
}

// A first click right after the mount indicator appears is occasionally
// swallowed (event wiring finishes a beat after the DOM does); most call
// sites also retry their own click, but this gives WASM a moment up front.
const POST_MOUNT_SETTLE_MS = 600;

async function gotoChat(page) {
  await page.goto("/workspace/app/chat");
  await page.waitForLoadState("networkidle");
  // The WASM app mounts several seconds after networkidle; wait for a real
  // rail row (not just the server-rendered shell) before interacting.
  await expect(
    page.locator('.chat-rail-row [data-action="select"]').first(),
  ).toBeVisible({
    timeout: 20000,
  });
  await page.waitForTimeout(POST_MOUNT_SETTLE_MS);
}

// Like gotoChat, but for a persona that may have zero conversations (no rail
// row to wait on) -- waits for the WASM-mounted rail shell instead (the
// "New conversation" button, which only exists post-mount).
async function gotoChatShell(page) {
  await page.goto("/workspace/app/chat");
  await page.waitForLoadState("networkidle");
  await expect(page.locator('[data-action="open-browse"]').first()).toBeVisible({
    timeout: 20000,
  });
  await page.waitForTimeout(POST_MOUNT_SETTLE_MS);
}

async function gotoDocs(page, path = "/workspace/app/docs") {
  await page.goto(path);
  await page.waitForLoadState("networkidle");
  await expect(page.locator("a.docs-title-link").first()).toBeVisible({
    timeout: 20000,
  });
  await page.waitForTimeout(POST_MOUNT_SETTLE_MS);
}

function selectChatRoom(page, name) {
  return page
    .locator('.chat-rail-row [data-action="select"]', { hasText: name })
    .first()
    .click();
}

function chatHeading(page) {
  return page.locator(".conversation-heading h1");
}

async function shot(page, name) {
  await page
    .screenshot({ path: `test-results/uxqual-audit/${name}.png`, fullPage: true })
    .catch(() => {});
}

// Creates a private channel and leaves it selected. The open-create click is
// occasionally swallowed right after the rail first becomes interactive (a
// WASM event-wiring race, same shape as CHAT-04's picker click), so this
// retries opening the dialog rather than failing on the first miss.
// Clicks `trigger` until `target` appears (a WASM event-wiring race can
// swallow the very first click after a rail/tray becomes interactive).
async function clickUntilVisible(page, trigger, target, attempts = 8) {
  for (let i = 0; i < attempts && !(await target.isVisible().catch(() => false)); i++) {
    await trigger.click();
    await page.waitForTimeout(500);
  }
  await expect(target).toBeVisible({ timeout: 5000 });
}

async function createPrivateChannel(page, name) {
  const dialog = page.locator(".create-dialog");
  for (let attempt = 0; attempt < 4 && (await dialog.count()) === 0; attempt++) {
    await page.locator('[data-action="open-create"]').first().click();
    await page.waitForTimeout(400);
  }
  await expect(dialog).toBeVisible({ timeout: 5000 });
  await dialog
    .locator('[data-action="create-kind"][data-id="private-channel"]')
    .click();
  await dialog.locator("#new-chat-name").fill(name);
  await dialog.getByRole("button", { name: "Create channel" }).click();
  await expect(dialog).toHaveCount(0, { timeout: 10000 });
  await expect(chatHeading(page)).toHaveText(name, { timeout: 10000 });
}

// ---------------------------------------------------------------------------
// CHAT findings
// ---------------------------------------------------------------------------
test.describe("CHAT findings", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await signInAs(page, "Rafael Torres");
  });

  test("CHAT-01: edit Cancel/Escape close without saving; switching channel drops the open editor", async ({
    page,
  }) => {
    test.setTimeout(90000);
    await gotoChat(page);
    // #general is a seeded public channel Rafael belongs to (chat_seed.go).
    await selectChatRoom(page, "general");
    await expect(chatHeading(page)).toHaveText("general");

    const original = `CHAT-01 original ${ts()}`;
    const composer = page.locator("#chat-composer");
    const posted = page.locator(".message", { hasText: original }).last();
    for (let i = 0; i < 3 && (await posted.count()) === 0; i++) {
      await composer.click();
      await composer.pressSequentially(original);
      await composer.press("Enter");
      await posted.waitFor({ state: "visible", timeout: 6000 }).catch(() => {});
    }
    await expect(posted).toBeVisible({ timeout: 10000 });
    const msgId = await posted.getAttribute("data-message-id");
    expect(msgId, "own message must carry a data-message-id").toBeTruthy();
    // Once editing starts, the message's own textContent no longer contains
    // `original` (it moves into the textarea's value, not a text node), so a
    // hasText-filtered locator would stop matching. Address the article by
    // its stable data-message-id from here on instead.
    const article = page.locator(`article[data-message-id="${msgId}"]`);

    async function openEditor() {
      await article.hover();
      const menu = page.locator(`.message-menu[data-message-menu="${msgId}"]`);
      const menuTrigger = article.locator('.message-action[data-action="menu"]');
      for (let i = 0; i < 4 && (await menu.count()) === 0; i++) {
        await menuTrigger.click();
        await page.waitForTimeout(300);
      }
      await expect(menu).toBeVisible({ timeout: 5000 });
      const editor = page.locator(`#edit-${msgId}`);
      for (let i = 0; i < 4 && (await editor.count()) === 0; i++) {
        await menu.locator('[data-action="edit"]').click();
        await page.waitForTimeout(300);
      }
      await expect(editor).toBeVisible({ timeout: 5000 });
      return editor;
    }

    // -- Cancel --
    let editor = await openEditor();
    await editor.fill(original + " EDITED VIA CANCEL");
    await shot(page, "CHAT-01-editing-before-cancel");
    const cancelBtn = article.locator('.message-edit [data-action="cancel-edit"]');
    await expect(cancelBtn).toBeEnabled();
    await cancelBtn.click();
    await expect(page.locator(`#edit-${msgId}`)).toHaveCount(0);
    await expect(article.locator(".message-body")).toContainText(original);
    await expect(article.locator(".message-body")).not.toContainText(
      "EDITED VIA CANCEL",
    );
    await shot(page, "CHAT-01-after-cancel");

    // -- Escape --
    editor = await openEditor();
    await editor.fill(original + " EDITED VIA ESCAPE");
    await editor.press("Escape");
    await expect(page.locator(`#edit-${msgId}`)).toHaveCount(0);
    await expect(article.locator(".message-body")).toContainText(original);
    await expect(article.locator(".message-body")).not.toContainText(
      "EDITED VIA ESCAPE",
    );
    await shot(page, "CHAT-01-after-escape");

    // -- switch channel while editing --
    await openEditor();
    await selectChatRoom(page, "benefits");
    await expect(chatHeading(page)).toHaveText("benefits");
    await selectChatRoom(page, "general");
    await expect(chatHeading(page)).toHaveText("general");
    await expect(page.locator(".message-edit")).toHaveCount(0);
    await shot(page, "CHAT-01-after-channel-switch");
  });

  test("CHAT-02: a new private channel appears in the rail immediately, with no sidebar-save toast", async ({
    page,
  }) => {
    test.setTimeout(60000);
    await gotoChat(page);
    const name = `uxqual-private-${ts()}`;

    await createPrivateChannel(page, name);
    await shot(page, "CHAT-02-create-dialog");

    const message = `CHAT-02 hello ${ts()}`;
    const composer = page.locator("#chat-composer");
    await composer.click();
    await composer.pressSequentially(message);
    await composer.press("Enter");
    await expect(page.locator(".message", { hasText: message })).toBeVisible({
      timeout: 10000,
    });

    // The new channel must show up in the rail right away.
    await expect(page.locator(".chat-rail-row", { hasText: name }).first()).toBeVisible(
      { timeout: 10000 },
    );

    // No "couldn't save your sidebar" (or any sidebar-save-failure) toast.
    await expect(page.getByText(/couldn.t save.*sidebar/i)).toHaveCount(0);
    await expect(page.locator('[role="alert"]', { hasText: /sidebar/i })).toHaveCount(
      0,
    );
    await shot(page, "CHAT-02-channel-in-rail");

    // (a) The URL fragment must name the new channel, and a reload must
    // stay on it (not fall back to a default/no channel).
    await expect(page).toHaveURL(new RegExp(`#channel=${name}$`));
    await page.reload();
    await page.waitForLoadState("networkidle");
    await expect(
      page.locator('.chat-rail-row [data-action="select"]').first(),
    ).toBeVisible({
      timeout: 20000,
    });
    await expect(page).toHaveURL(new RegExp(`#channel=${name}$`));
    await expect(chatHeading(page)).toHaveText(name, { timeout: 10000 });
    await shot(page, "CHAT-02a-url-survives-reload");
  });

  test("CHAT-04: react from the timeline and from the thread panel; no error toast, reaction persists after reload", async ({
    page,
  }) => {
    test.setTimeout(60000);
    await gotoChat(page);
    await selectChatRoom(page, "Q4 hiring huddle");
    await expect(chatHeading(page)).toHaveText("Q4 hiring huddle");

    const lastArticle = page.locator("article.message").last();
    await expect(lastArticle).toBeVisible({ timeout: 10000 });
    await lastArticle.hover();

    // React from the timeline via the quick-react toolbar.
    const quickReact = lastArticle.getByRole("button", { name: "React with 👍" });
    await quickReact.click();
    await expect(page.locator('[role="alert"]')).toHaveCount(0);
    await expect(
      lastArticle.getByRole("button", { name: /reacted with 👍/ }),
    ).toBeVisible({ timeout: 10000 });
    await shot(page, "CHAT-04-timeline-reaction");

    // Open the thread and react again from the thread panel's root message.
    // Prefer the "N replies" stats button when the seed already has replies;
    // fall back to the toolbar's "Reply in thread" action otherwise. Both
    // can be present on the same message, so this must not be one ambiguous
    // role query.
    const statsReplies = lastArticle.locator(".message-stats");
    const repliesBtn =
      (await statsReplies.count()) > 0
        ? statsReplies.first()
        : lastArticle.getByRole("button", { name: "Reply in thread" });
    await repliesBtn.click();
    const threadRoot = page.locator(".thread-root");
    await expect(threadRoot).toBeVisible({ timeout: 10000 });
    const addReaction = threadRoot.getByRole("button", { name: "Add reaction" });
    await expect(addReaction).toBeVisible({ timeout: 10000 });
    const picker = threadRoot.locator(".reaction-picker");
    // "Add reaction" is a toggle. Server-held UI state (PickerID) can carry
    // over from an earlier run against this live server, so the picker may
    // already be open here; only click to open it if it is not. The click
    // is occasionally swallowed (WASM event wiring race), so retry it a
    // couple of times rather than failing on the first miss.
    for (let attempt = 0; attempt < 4 && (await picker.count()) === 0; attempt++) {
      await addReaction.click();
      await page.waitForTimeout(400);
    }
    await expect(picker).toBeVisible({ timeout: 5000 });
    // Picker options are role="menuitem" (a <button role="menuitem">), not
    // the default button role.
    await picker.getByRole("menuitem", { name: "React with 🎉" }).click();
    await expect(page.locator('[role="alert"]')).toHaveCount(0);
    await expect(
      threadRoot.getByRole("button", { name: /reacted with 🎉/ }),
    ).toBeVisible({ timeout: 10000 });
    await shot(page, "CHAT-04-thread-reaction");

    // Persists after reload.
    await page.reload();
    await page.waitForLoadState("networkidle");
    await expect(
      page.locator('.chat-rail-row [data-action="select"]').first(),
    ).toBeVisible({
      timeout: 20000,
    });
    const lastArticleAfter = page.locator("article.message").last();
    await expect(
      lastArticleAfter.getByRole("button", { name: /reacted with 👍/ }),
    ).toBeVisible({ timeout: 10000 });
    await shot(page, "CHAT-04-after-reload");
  });

  test("CHAT-05: the poll create form's Create poll button stays inside the visible tray card at 1440x900", async ({
    page,
  }) => {
    await gotoChat(page);
    const name = `uxqual-poll-${ts()}`;
    await createPrivateChannel(page, name);

    const pollSection = page.locator("#chat-poll-section");
    await clickUntilVisible(
      page,
      page.locator('[data-action="open-poll"], [data-action="tray-poll"]').first(),
      pollSection,
    );
    await expect(page.locator("#channel-poll-question")).toBeVisible({ timeout: 5000 });

    const createBtn = pollSection.getByRole("button", { name: "Create poll" });
    await expect(createBtn).toBeVisible();
    // The poll form renders inside the channel-tray card above the
    // timeline (channel_tray.go), not the Details side pane.
    const panel = page.locator(".channel-tray-card");
    const panelBox = await panel.boundingBox();
    const btnBox = await createBtn.boundingBox();
    expect(panelBox, "the Details panel must be measurable").toBeTruthy();
    expect(btnBox, "the Create poll button must be measurable").toBeTruthy();
    await shot(page, "CHAT-05-poll-form");
    expect(btnBox.x).toBeGreaterThanOrEqual(panelBox.x - 1);
    expect(btnBox.y).toBeGreaterThanOrEqual(panelBox.y - 1);
    expect(btnBox.x + btnBox.width).toBeLessThanOrEqual(
      panelBox.x + panelBox.width + 1,
    );
    expect(btnBox.y + btnBox.height).toBeLessThanOrEqual(
      panelBox.y + panelBox.height + 1,
    );
  });

  test("CHAT-06: a new poll shows '0 votes'", async ({ page }) => {
    await gotoChat(page);
    const name = `uxqual-poll2-${ts()}`;
    await createPrivateChannel(page, name);

    const pollSection = page.locator("#chat-poll-section");
    await clickUntilVisible(
      page,
      page.locator('[data-action="open-poll"], [data-action="tray-poll"]').first(),
      pollSection,
    );
    await page.locator("#channel-poll-question").fill(`Best snack ${ts()}?`);
    await page.locator("#channel-poll-options").fill("Chips\nFruit\nPretzels");
    await pollSection.getByRole("button", { name: "Create poll" }).click();

    await expect(pollSection.locator(".channel-poll-question")).toBeVisible({
      timeout: 10000,
    });
    await expect(pollSection).toContainText("0 votes");
    await shot(page, "CHAT-06-zero-votes");
  });

  test("CHAT-07: Add task is disabled with an empty title", async ({ page }) => {
    await gotoChat(page);
    await selectChatRoom(page, "general");
    const input = page.locator("#chat-todo-new");
    await clickUntilVisible(page, page.locator('[data-action="open-todo"]'), input);
    await expect(input).toHaveValue("");
    const addBtn = page.getByRole("button", { name: "Add task" });
    await expect(addBtn).toBeDisabled();
    await shot(page, "CHAT-07-add-task-disabled");
  });

  test("CHAT-08: 'Copy API curl' is not in a normal member's everyday menu (Thomas Baker, a public channel he doesn't own)", async ({
    page,
  }) => {
    await signInAs(page, "Thomas Baker");
    await gotoChatShell(page);
    // Thomas Baker (a finance/payroll persona) is not seeded into any
    // channel; join the public #general he does not own so there is a
    // "normal member, not owner" case to check the menu against.
    let row = page.locator(".chat-rail-row", { hasText: "general" }).first();
    // Give the rail a moment to finish populating before deciding Thomas
    // Baker isn't a member yet (repeat runs against this live server leave
    // him joined from a prior run, so this must not assume membership either
    // way).
    const alreadyMember = await row
      .waitFor({ state: "visible", timeout: 5000 })
      .then(() => true)
      .catch(() => false);
    if (!alreadyMember) {
      await page.locator('[data-action="open-browse"]').first().click();
      const browseDialog = page.locator(".browse-dialog");
      await expect(browseDialog).toBeVisible({ timeout: 5000 });
      const generalRow = browseDialog
        .locator(".browse-row", { hasText: "general" })
        .first();
      await expect(generalRow).toBeVisible({ timeout: 15000 });
      const joinBtn = generalRow.locator('[data-action="join"]');
      if ((await joinBtn.count()) > 0) {
        await joinBtn.click();
        // Joining opens a "Join #general?" confirm step, then auto-selects
        // the channel and closes the browse dialog.
        await page.locator('[data-action="join-confirm"]').click();
        await expect(browseDialog).toHaveCount(0, { timeout: 10000 });
      } else {
        await generalRow.locator('[data-action="browse-open"]').click();
      }
      row = page.locator(".chat-rail-row", { hasText: "general" }).first();
    }
    await expect(row).toBeVisible({ timeout: 10000 });
    await row.locator('[data-action="rail-menu"]').click();
    const menu = page.locator(".rail-row-menu");
    await expect(menu.first()).toBeVisible({ timeout: 5000 });
    await shot(page, "CHAT-08-rail-menu-member");
    await expect(page.getByRole("menuitem", { name: "Copy API curl" })).toHaveCount(0);

    // (b, member side): Details must not show an Integrations/Copy API curl
    // section for a non-owner, non-admin member either.
    await page.keyboard.press("Escape");
    await row.click();
    await page.locator('[data-action="details"]').first().click();
    await expect(page.locator(".chat-side.chat-details")).toBeVisible({
      timeout: 10000,
    });
    await expect(page.locator(".integrations-section")).toHaveCount(0);
    await expect(page.getByText("Copy API curl")).toHaveCount(0);
    await shot(page, "CHAT-08b-details-member-no-integrations");
  });

  test("CHAT-08 (owner side): 'Copy API curl' is absent from More options even for the owner, and lives in Details -> Integrations instead", async ({
    page,
  }) => {
    await gotoChat(page);
    const name = `uxqual-curl-owner-${ts()}`;
    await createPrivateChannel(page, name);

    // More options, even for the channel's own owner, must not offer it.
    const row = page.locator(".chat-rail-row", { hasText: name }).first();
    await expect(row).toBeVisible({ timeout: 10000 });
    await row.locator('[data-action="rail-menu"]').click();
    await expect(page.locator(".rail-row-menu").first()).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole("menuitem", { name: "Copy API curl" })).toHaveCount(0);
    await shot(page, "CHAT-08c-rail-menu-owner");
    await page.keyboard.press("Escape");

    // Details -> Integrations must offer it for the owner.
    await page.locator('[data-action="details"]').first().click();
    const integrations = page.locator(".integrations-section");
    await expect(integrations).toBeVisible({ timeout: 10000 });
    await expect(integrations.getByText("Integrations")).toBeVisible();
    const curlBtn = integrations.locator('[data-action="copy-conversation-api-curl"]');
    await expect(curlBtn).toBeVisible();
    await expect(curlBtn).toContainText("Copy API curl");
    await shot(page, "CHAT-08d-details-owner-integrations");
  });
});

// ---------------------------------------------------------------------------
// DOCS findings
// ---------------------------------------------------------------------------
test.describe("DOCS findings", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await signInAs(page, "Rafael Torres");
  });

  test("DOCS-01 / DOCS-06: create a document, save a 2nd version (private-doc message), compare versions", async ({
    page,
  }) => {
    test.setTimeout(90000);
    await gotoDocs(page);
    const title = `UXQUAL compare ${ts()}`;

    const createDialog = page.locator("#docs-create-dialog");
    await clickUntilVisible(
      page,
      page.getByRole("button", { name: "New document" }),
      createDialog,
    );
    await createDialog.getByPlaceholder("Untitled document").fill(title);
    await createDialog
      .getByLabel("Content (Markdown supported)")
      .fill("Version one content.");
    await createDialog.getByRole("button", { name: "Create draft" }).click();
    // Creating a draft adds the row to the list; it does not itself
    // navigate to the reader, so open the new document explicitly.
    await expect(createDialog).toHaveCount(0, { timeout: 10000 });
    const newRow = page.locator("a.docs-title-link", { hasText: title });
    await expect(newRow).toBeVisible({ timeout: 10000 });
    await newRow.click();
    await expect(page.getByRole("heading", { name: title })).toBeVisible({
      timeout: 15000,
    });

    // Access must read "Only you" for a fresh, unshared draft (sets up DOCS-06).
    await expect(page.getByText("Only you")).toBeVisible();
    await shot(page, "DOCS-01-created");

    // Save a second version.
    await page.getByRole("button", { name: "Edit document" }).click();
    const editor = page.getByRole("textbox", { name: "Formatted document" });
    await expect(editor).toBeVisible({ timeout: 10000 });
    await editor.click();
    await page.keyboard.press("Control+a");
    await page.keyboard.type("Version two content, changed for comparison.");
    await page.getByRole("button", { name: "Save new version" }).click();

    // DOCS-06: the save confirmation for an "Only you" document must not
    // mention shared readers.
    await expect(page.getByText("New version saved.")).toBeVisible({ timeout: 10000 });
    await expect(page.getByText(/shared readers/i)).toHaveCount(0);
    await shot(page, "DOCS-06-private-save-message");

    await page.getByRole("button", { name: "Cancel" }).last().click();

    // DOCS-01: Actions -> Compare versions. The menu item lives inside a
    // TransientPopover panel (docs_page.go) that is only reachable after
    // opening its own trigger, not a hover-reveal like chat's toolbars.
    const actionsTrigger = page.locator(".docs-row-menu-trigger");
    const compareTrigger = page.locator('[data-docs-action="compare"]');
    const compareDialog = page.locator("#docs-compare-dialog");
    for (let i = 0; i < 4 && (await compareDialog.count()) === 0; i++) {
      if ((await compareTrigger.isVisible().catch(() => false)) === false) {
        await actionsTrigger.scrollIntoViewIfNeeded().catch(() => {});
        await actionsTrigger.click().catch(() => {});
        await page.waitForTimeout(300);
      }
      await compareTrigger.click({ force: true }).catch(() => {});
      await page.waitForTimeout(400);
    }
    await expect(compareDialog).toBeVisible({ timeout: 5000 });
    const fromSelect = compareDialog.locator("select").first();
    const toSelect = compareDialog.locator("select").nth(1);
    // The version list loads asynchronously after the dialog opens. The
    // placeholder option ("Choose a version") carries no value attribute,
    // so its DOM .value falls back to its text content per the HTML spec --
    // excluding it needs the disabled flag, not just a truthy-value filter.
    const realOptionValues = (opts) =>
      opts
        .filter((o) => !o.disabled)
        .map((o) => o.value)
        .filter((v) => v);
    await expect
      .poll(
        async () =>
          (await fromSelect.locator("option").evaluateAll(realOptionValues)).length,
        {
          timeout: 10000,
        },
      )
      .toBeGreaterThanOrEqual(2);
    const fromValues = await fromSelect.locator("option").evaluateAll(realOptionValues);
    expect(fromValues.length).toBeGreaterThanOrEqual(2);
    expect(
      fromValues.every((v) => v.startsWith("docv-")),
      "real version options must be docv- ids",
    ).toBeTruthy();
    // The <select>'s enabled state can lag its options being populated by a
    // render tick; retry the real selection (a native selectOption, so the
    // framework's own OnInput handler sees a normal browser input/change
    // pair) until it settles rather than fighting the framework's own
    // re-render with a one-shot DOM write.
    async function selectByValue(locator, value) {
      for (let i = 0; i < 8; i++) {
        const enabled = await locator
          .locator("option")
          .evaluateAll(
            (options, expected) =>
              options.some((option) => option.value === expected && !option.disabled),
            value,
          );
        if (enabled) {
          await locator.selectOption(value, { timeout: 2000 });
        }
        await page.waitForTimeout(300);
        if ((await locator.inputValue()) === value) return;
      }
      expect(await locator.inputValue(), `select should settle on ${value}`).toBe(
        value,
      );
    }
    await selectByValue(fromSelect, fromValues[0]);
    await selectByValue(toSelect, fromValues[fromValues.length - 1]);
    await shot(page, "DOCS-01-compare-dialog");
    await compareDialog.getByRole("button", { name: "Compare versions" }).click();

    await expect(page.getByText("Version one content.").first()).toBeVisible({
      timeout: 10000,
    });
    await expect(
      page.getByText("Version two content, changed for comparison.").first(),
    ).toBeVisible();
    await shot(page, "DOCS-01-compare-result");
  });

  test("DOCS-02 (390x844): document rows show a titled owner/date line, never a bare 2-3 char owner fragment", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await gotoDocs(page);
    const metas = page.locator(".docs-row-phone-meta");
    const count = await metas.count();
    expect(count).toBeGreaterThan(0);
    const bad = [];
    for (let i = 0; i < Math.min(count, 20); i++) {
      const text = (await metas.nth(i).textContent())?.trim() ?? "";
      const ownerPart = text.split("·")[0].trim();
      if (ownerPart !== "You" && /^[A-Z]{2,3}$/.test(ownerPart)) {
        bad.push(text);
      }
      if (!(await metas.nth(i).isVisible())) {
        bad.push(`(hidden) ${text}`);
      }
    }
    await shot(page, "DOCS-02-mobile-rows");
    expect(
      bad,
      "no row's phone meta line should be a bare initials fragment or hidden",
    ).toEqual([]);

    // Titles are CSS-clamped to at most 2 lines (-webkit-line-clamp: 2 with
    // overflow: hidden); a raw bounding-box/line-height estimate is fragile
    // against line-height "normal" and sub-pixel rounding, so assert the
    // clamp itself is in force instead of re-deriving a line count.
    const titles = page.locator(".docs-title-link");
    const titleCount = Math.min(await titles.count(), 20);
    for (let i = 0; i < titleCount; i++) {
      const el = titles.nth(i);
      const clamp = await el.evaluate((node) => {
        const style = getComputedStyle(node);
        return {
          lineClamp: style.webkitLineClamp || style.lineClamp,
          overflow: style.overflow,
        };
      });
      expect(clamp.lineClamp, `title ${i} must be clamped to 2 lines`).toBe("2");
      expect(clamp.overflow, `title ${i} must hide overflow past the clamp`).toBe(
        "hidden",
      );
    }
  });

  test("DOCS-03: docs-chat-chip pills never break mid-word", async ({ page }) => {
    await gotoDocs(page);
    const link = page.locator("a.docs-title-link", {
      hasText: "People Ops chat highlights: this month",
    });
    await link.click();
    await expect(page.locator(".docs-chat-chip").first()).toBeVisible({
      timeout: 15000,
    });
    const chips = page.locator(".docs-chat-chip");
    const count = await chips.count();
    expect(count).toBeGreaterThan(0);
    const bad = [];
    for (let i = 0; i < count; i++) {
      const chip = chips.nth(i);
      const { height, lineHeight } = await chip.evaluate((node) => {
        const style = getComputedStyle(node);
        return {
          height: node.getBoundingClientRect().height,
          lineHeight: parseFloat(style.lineHeight) || parseFloat(style.fontSize) * 1.2,
        };
      });
      if (height > lineHeight * 1.6) {
        bad.push({ i, height, lineHeight, text: await chip.textContent() });
      }
    }
    await shot(page, "DOCS-03-chat-chips");
    expect(bad, "no chip should be tall enough to have wrapped").toEqual([]);
  });

  test("DOCS-04: the PDF card's Open action does something (new page, download, or an inline message)", async ({
    page,
    context,
  }) => {
    await gotoDocs(page);
    const link = page.locator("a.docs-title-link", {
      hasText: "Open enrollment 2027: questions and answers",
    });
    await link.click();
    await expect(
      page.getByRole("heading", { name: /Open enrollment 2027/ }),
    ).toBeVisible({
      timeout: 15000,
    });
    const openBtn = page.locator('[data-media-action="open"]').first();
    await expect(openBtn).toBeVisible({ timeout: 10000 });

    const pagePromise = context
      .waitForEvent("page", { timeout: 8000 })
      .catch(() => null);
    const downloadPromise = page
      .waitForEvent("download", { timeout: 8000 })
      .catch(() => null);
    const responsePromise = page
      .waitForResponse((r) => /\/v1\/documents\/media\//.test(r.url()), {
        timeout: 8000,
      })
      .catch(() => null);
    await openBtn.click();
    const [newPage, download, response] = await Promise.all([
      pagePromise,
      downloadPromise,
      responsePromise,
    ]);
    const statusText =
      (await page
        .locator(".docs-media-card-status")
        .first()
        .textContent()
        .catch(() => "")) ?? "";
    await shot(page, "DOCS-04-pdf-open");

    if (newPage) {
      await newPage.close().catch(() => {});
    }
    const somethingHappened =
      Boolean(newPage) ||
      Boolean(download) ||
      Boolean(response) ||
      statusText.trim() !== "";
    expect(
      somethingHappened,
      "Open must produce a new page, a download, a successful media request, or an inline status message",
    ).toBeTruthy();
  });

  test("DOCS-05 (390x844): the Search in / Sort labels are visible", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await gotoDocs(page);
    await expect(page.getByText("Search in", { exact: true })).toBeVisible();
    await expect(page.getByText("Sort", { exact: true })).toBeVisible();
    await shot(page, "DOCS-05-mobile-labels");
  });

  test("DOCS-07: an owned document can be removed and Undo restores it", async ({
    page,
  }) => {
    test.setTimeout(60000);
    await gotoDocs(page);
    const title = `UXQUAL remove ${ts()}`;
    const createDialog = page.locator("#docs-create-dialog");
    await clickUntilVisible(
      page,
      page.getByRole("button", { name: "New document" }),
      createDialog,
    );
    await createDialog.getByPlaceholder("Untitled document").fill(title);
    await createDialog
      .getByLabel("Content (Markdown supported)")
      .fill("Removable draft.");
    await createDialog.getByRole("button", { name: "Create draft" }).click();
    await expect(createDialog).toHaveCount(0, { timeout: 10000 });
    const newRow = page.locator("a.docs-title-link", { hasText: title });
    await expect(newRow).toBeVisible({ timeout: 10000 });
    await newRow.click();
    await expect(page.getByRole("heading", { name: title })).toBeVisible({
      timeout: 15000,
    });

    // Same TransientPopover pattern as DOCS-01's Compare trigger.
    const actionsTrigger = page.locator(".docs-row-menu-trigger");
    const removeTrigger = page.locator('[data-docs-action="remove"]');
    const removeDialog = page.locator("#docs-remove-dialog");
    for (let i = 0; i < 4 && (await removeDialog.count()) === 0; i++) {
      if ((await removeTrigger.isVisible().catch(() => false)) === false) {
        await actionsTrigger.scrollIntoViewIfNeeded().catch(() => {});
        await actionsTrigger.click().catch(() => {});
        await page.waitForTimeout(300);
      }
      await removeTrigger.click({ force: true }).catch(() => {});
      await page.waitForTimeout(400);
    }
    await expect(removeDialog).toBeVisible({ timeout: 5000 });
    await shot(page, "DOCS-07-remove-confirm");
    const removedToast = page.getByText("Document removed.");
    const confirmBtn = page.locator("#docs-remove-confirm");
    for (let i = 0; i < 3 && (await removedToast.count()) === 0; i++) {
      await confirmBtn.click();
      await page.waitForTimeout(800);
    }
    await expect(removedToast).toBeVisible({ timeout: 10000 });
    const undoBtn = page.getByRole("button", { name: "Undo" });
    await expect(undoBtn).toBeVisible({ timeout: 5000 });
    await shot(page, "DOCS-07-removed-undo-toast");
    await undoBtn.click();
    await expect(page.getByText("Document restored.")).toBeVisible({ timeout: 10000 });

    await gotoDocs(page, "/workspace/app/docs?collection=private");
    await expect(page.locator("a.docs-title-link", { hasText: title })).toBeVisible({
      timeout: 10000,
    });
    await shot(page, "DOCS-07-restored-in-my-documents");
  });
});

// ---------------------------------------------------------------------------
// CROSS / ACCESS / standalone findings
// ---------------------------------------------------------------------------
test.describe("CROSS / ACCESS / misc findings", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await signInAs(page, "Rafael Torres");
  });

  test("CROSS-01: no leaked doc:doc- ids in chat search results or embedded chat cards", async ({
    page,
  }) => {
    await gotoChat(page);
    const search = page.locator("#chat-search");
    await search.click();
    await search.pressSequentially("enrollment");
    await expect(search).toHaveValue("enrollment");
    const results = page.locator(
      '.search-message-result[data-action="open-search-message"]',
    );
    await expect(results.first()).toBeVisible({ timeout: 10000 });
    const resultTexts = await results.allTextContents();
    const leakedInSearch = resultTexts.filter((t) => /doc:doc-/.test(t));
    expect(
      leakedInSearch,
      "no chat search result snippet should contain doc:doc-",
    ).toEqual([]);
    await shot(page, "CROSS-01-chat-search");

    await gotoDocs(page);
    const link = page.locator("a.docs-title-link", {
      hasText: "People Ops chat highlights: this month",
    });
    await link.click();
    await expect(page.locator(".docs-chat-chip").first()).toBeVisible({
      timeout: 15000,
    });
    const bodyText = await page.locator("body").innerText();
    expect(
      bodyText.includes("doc:doc-"),
      "no embedded chat card should show a doc:doc- id",
    ).toBeFalsy();
    await shot(page, "CROSS-01-docs-embed");

    // (c) The embedded chat cards (docs-chat-quote) must show the actual
    // message content, not a placeholder for an unresolved message.
    const quotes = page.locator(".docs-chat-quote");
    const quoteCount = await quotes.count();
    expect(
      quoteCount,
      "the document must have embedded chat quotes to check",
    ).toBeGreaterThan(0);
    const quoteTexts = await quotes.allTextContents();
    const unavailable = quoteTexts.filter((t) => /chat message unavailable/i.test(t));
    expect(
      unavailable,
      "embedded chat cards must show real content, not 'Chat message unavailable'",
    ).toEqual([]);
    await expect(page.getByText("Chat message unavailable")).toHaveCount(0);
    await shot(page, "CROSS-01c-chat-cards-have-content");
  });

  test("ACCESS-01: Details on an owned private channel offers Add people, which opens a picker", async ({
    page,
  }) => {
    await gotoChat(page);
    const name = `uxqual-access-${ts()}`;
    await createPrivateChannel(page, name);

    const addPeople = page.getByRole("button", { name: "Add people" });
    await clickUntilVisible(
      page,
      page.locator('[data-action="details"]').first(),
      addPeople,
    );
    const picker = page.locator(".add-members-dialog");
    await clickUntilVisible(page, addPeople, picker);
    await expect(picker.locator("#new-chat-member-search")).toBeVisible();
    await shot(page, "ACCESS-01-add-people-picker");
  });

  test("ACCESS-02: Share dialog on a shared document lets a recipient's role be changed", async ({
    page,
  }) => {
    await gotoDocs(page);
    const link = page.locator("a.docs-title-link", {
      hasText: "People Ops chat highlights: this month",
    });
    await link.click();
    await expect(
      page.getByRole("heading", { name: "People Ops chat highlights: this month" }),
    ).toBeVisible({
      timeout: 15000,
    });
    // .docs-share-root itself collapses to zero height (its dialog child is
    // position: fixed, so it takes no space in flow) -- assert on the
    // dialog surface, which is what is actually on screen.
    const shareRoot = page.locator("#docs-share-dialog");
    await page.getByRole("button", { name: "Share", exact: true }).click();
    await expect(shareRoot).toBeVisible({ timeout: 10000 });
    const roleSelects = shareRoot.locator(
      'select[data-docs-action="share-role-change"]',
    );
    await expect(roleSelects.first()).toBeVisible({ timeout: 5000 });
    const before = await roleSelects.first().inputValue();
    const options = await roleSelects
      .first()
      .locator("option")
      .evaluateAll((opts) => opts.map((o) => o.value));
    const other = options.find((o) => o !== before);
    expect(
      other,
      "the per-recipient role select must offer more than one role",
    ).toBeTruthy();
    await shot(page, "ACCESS-02-share-dialog-before");
    await roleSelects.first().selectOption(other);
    await expect(roleSelects.first()).toHaveValue(other);
    await shot(page, "ACCESS-02-share-dialog-after");
  });

  test("Lone period: the open-enrollment document has no paragraph whose visible text is just '.'", async ({
    page,
  }) => {
    await gotoDocs(page);
    const link = page.locator("a.docs-title-link", {
      hasText: "Open enrollment 2027: questions and answers",
    });
    await link.click();
    await expect(
      page.getByRole("heading", { name: /Open enrollment 2027/ }),
    ).toBeVisible({
      timeout: 15000,
    });
    const lonePeriods = await page
      .locator(".docs-markdown *")
      .evaluateAll(
        (nodes) =>
          nodes.filter((n) => n.children.length === 0 && n.textContent.trim() === ".")
            .length,
      );
    await shot(page, "lone-period-doc");
    expect(lonePeriods).toBe(0);
  });

  test("Mobile chat search (390x844): results are visible after searching from the open drawer", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/workspace/app/chat");
    await page.waitForLoadState("networkidle");
    const railToggle = page.locator('[data-action="open-rail"]');
    await expect(railToggle).toBeVisible({ timeout: 20000 });
    await expect(railToggle).toBeEnabled({ timeout: 10000 });
    const search = page.locator("#chat-search");
    // This is a toggle: Playwright's own built-in actionability retries on
    // .click() would each be a real click and could flip it back closed, so
    // drive the open/closed check manually instead of one .click() call.
    for (let i = 0; i < 5 && !(await search.isVisible().catch(() => false)); i++) {
      await railToggle.click({ force: true }).catch(() => {});
      await page.waitForTimeout(400);
    }
    await expect(search).toBeVisible({ timeout: 10000 });
    await search.click();
    await search.pressSequentially("enrollment");
    const results = page.locator(
      '.search-message-result[data-action="open-search-message"]',
    );
    await expect(results.first()).toBeVisible({ timeout: 10000 });
    await shot(page, "mobile-chat-search-results");
  });

  test("GIFs in #general: every <img alt$=.gif> either loads or shows the attachment fallback, never a broken empty-src image", async ({
    page,
  }) => {
    test.setTimeout(120000);
    await gotoChat(page);
    await selectChatRoom(page, "general");
    await expect(chatHeading(page)).toHaveText("general");
    await expect(page.locator("article.message").first()).toBeVisible({
      timeout: 10000,
    });

    const gifImgs = page.locator('img[alt$=".gif"]');
    // The message log can still be populating right after the heading
    // updates; give GIF attachments a moment to appear before concluding
    // there are none to check.
    let count = await gifImgs.count();
    for (let i = 0; i < 6 && count === 0; i++) {
      await page.waitForTimeout(500);
      count = await gifImgs.count();
    }
    test.skip(count === 0, "no GIF attachments seeded in #general to check");

    // Images are loading="lazy" AND appear to release their blob src once
    // scrolled back out of view (a virtualization/memory-saving pattern),
    // so each image's loaded state must be captured right after it is
    // scrolled into view -- not re-checked later once other scrolling has
    // moved it off-screen again.
    const bad = [];
    for (let i = 0; i < count; i++) {
      const img = gifImgs.nth(i);
      await img
        .evaluate((node) => node.scrollIntoView({ block: "center" }))
        .catch(() => {});
      const deadline = Date.now() + 5000;
      let state = { naturalWidth: 0, failed: false, fallbackVisible: false, src: "" };
      while (Date.now() < deadline) {
        state = await img.evaluate((node) => {
          const failed = Boolean(
            node.closest(".attachment-image")?.classList.contains("failed"),
          );
          const fallbackVisible = failed
            ? getComputedStyle(
                node.closest(".attachment-image").querySelector(".attachment-fallback"),
              ).display !== "none"
            : false;
          return {
            src: node.getAttribute("src") || "",
            naturalWidth: node.naturalWidth,
            failed,
            fallbackVisible,
          };
        });
        if (state.naturalWidth > 0 || state.failed) break;
        await page.waitForTimeout(300);
      }
      if (state.naturalWidth === 0 && !(state.failed && state.fallbackVisible)) {
        bad.push({ index: i, ...state });
      }
    }
    await shot(page, "gifs-general");
    expect(bad, "every GIF must load or show its fallback, never sit broken").toEqual(
      [],
    );
  });

  test("pre-hydration typing: text typed into #chat-search immediately after DOMContentLoaded survives WASM mount", async ({
    page,
  }) => {
    await page.goto("/workspace/app/chat", { waitUntil: "domcontentloaded" });
    const search = page.locator("#chat-search");
    await search.click();
    await search.pressSequentially("earlytype", { delay: 20 });
    // Now let the WASM app finish mounting.
    await expect(
      page.locator('.chat-rail-row [data-action="select"]').first(),
    ).toBeVisible({
      timeout: 20000,
    });
    await expect(page.locator("#chat-search")).toHaveValue("earlytype");
    await shot(page, "pre-hydration-typing");
  });
});
