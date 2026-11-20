import { test, expect } from "@playwright/test";

// Drafts, chunk 2 (#3943 restored mention of a non-member, #3939 a cleared or
// cancelled draft stays cleared across a quick reload, #3907 the preview shows
// a picked mention as a chip), against the live dev cell. Everything that
// writes happens in the test's own zz-drafts channel.

test.setTimeout(240000);
test.use({ locale: "en-US" });

const composerSel = "textarea.composer-input";

async function signIn(page, persona = "Darius Bennett") {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function ownChannel(page) {
  const name = `zz-drafts-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator(composerSel).first().waitFor({ timeout: 40000 });
  return name;
}

// Reloads the page and waits for the composer; a load that stalls under the
// shared dev cell's load is retried rather than failing the assertion.
async function reloadAndWait(page) {
  await expect(async () => {
    await page.reload();
    await page.locator(composerSel).first().waitFor({ timeout: 30000 });
  }).toPass({ timeout: 150000 });
}

async function openChannel(page, name) {
  await page.goto(`/workspace/app/chat?locale=en-US#channel=${name}`);
  await reloadAndWait(page);
}

async function pickMention(page, typed, person) {
  const input = page.locator(composerSel).first();
  // The page is a WASM app: typing before it has hydrated is lost, so retry
  // until the suggestion list answers.
  await expect(async () => {
    await input.click();
    await input.fill("");
    await input.pressSequentially(typed, { delay: 30 });
    await expect(
      page.locator(".mention-menu .mention-option", { hasText: person }).first(),
    ).toBeVisible({ timeout: 4000 });
  }).toPass({ timeout: 60000 });
  await expect(async () => {
    if (new RegExp(`@${person}`).test(await input.inputValue())) return;
    await page
      .locator(".mention-menu .mention-option", { hasText: person })
      .first()
      .click({ timeout: 3000 });
    await expect(input).toHaveValue(new RegExp(`@${person}`), { timeout: 2000 });
  }).toPass({ timeout: 30000 });
}

test("#3907 the preview draws a picked @mention as a person chip, plain text stays plain", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  await page.goto("/workspace/app/chat?locale=en-US#channel=engineering");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  // An earlier run may have left a draft in this room: start from empty.
  await page.locator(composerSel).first().fill("");
  await page.waitForTimeout(500);
  await pickMention(page, "Please ask @Elena", "Elena Vega");
  const form = page.locator("form.chat-composer").first();
  await form.locator(".composer-preview-toggle").click();
  const preview = form.locator(".composer-preview-body");
  await expect(preview).toBeVisible();
  await expect(preview).toContainText("Elena Vega");
  const chip = preview.locator(".mention-chip").first();
  await expect(chip).toBeVisible();
  await expect(chip).toContainText("Elena Vega");
  // An accessible name on the chip's own control.
  const named = await preview.evaluate((el) => {
    const c = el.querySelector(".mention-chip");
    const b =
      c &&
      (c.matches("[aria-label]")
        ? c
        : c.closest("[aria-label]") || c.querySelector("[aria-label]"));
    return b ? b.getAttribute("aria-label") : "";
  });
  expect(named).toMatch(/Elena Vega/);
  // Text that only resembles a mention stays plain.
  await form.locator(".composer-preview-toggle").click();
  const input = page.locator(composerSel).first();
  await input.fill("Please ask @Nobodyatall about it");
  await form.locator(".composer-preview-toggle").click();
  await expect(form.locator(".composer-preview-body")).toContainText("@Nobodyatall");
  await expect(form.locator(".composer-preview-body .mention-chip")).toHaveCount(0);
  await form.locator(".composer-preview-toggle").click();
  await input.fill("");
});

test("#3943 a restored draft that mentions a non-member warns and offers Add", async ({
  page,
}) => {
  await signIn(page);
  const name = await ownChannel(page);
  await pickMention(page, "Hello @Evelyn", "Evelyn Morgan");
  const note = page.locator(".composer-outside-note");
  await expect(note).toBeVisible({ timeout: 30000 });
  await expect(note).toContainText("Evelyn Morgan");
  // Let the draft persist (debounced write), then reload.
  await page.waitForTimeout(3500);
  await openChannel(page, name);
  const input = page.locator(composerSel).first();
  await expect(input).toHaveValue(/@Evelyn Morgan/, { timeout: 30000 });
  const restored = page.locator(".composer-outside-note");
  await expect(restored).toBeVisible({ timeout: 30000 });
  await expect(restored).toContainText("Evelyn Morgan");
  await expect(restored.getByRole("button", { name: /add/i }).first()).toBeVisible();
  // Typing on keeps the warning.
  await input.press("End");
  await input.pressSequentially(" thanks", { delay: 20 });
  await expect(page.locator(".composer-outside-note")).toContainText("Evelyn Morgan");
  // No one was added: leave the draft empty again.
  await input.fill("");
});

test("#3939 a cleared draft and a cancelled /todo preview stay cleared after an immediate reload", async ({
  page,
}) => {
  await signIn(page);
  const name = await ownChannel(page);
  const input = page.locator(composerSel).first();

  // Ordinary draft: store it, then clear and reload at once.
  await input.fill("zz-drafts ordinary draft marker");
  await page.waitForTimeout(3500);
  await openChannel(page, name);
  await expect(page.locator(composerSel).first()).toHaveValue(
    "zz-drafts ordinary draft marker",
    {
      timeout: 30000,
    },
  );
  await page.locator(composerSel).first().fill("");
  // A quick reload: well inside the 1.5 s typing debounce, but after the
  // clear is visible (a person cannot reload in the same millisecond).
  await page.waitForTimeout(300);
  await reloadAndWait(page);
  await expect(page.locator(composerSel).first()).toHaveValue("");

  // Cancelled /todo preview.
  const field = page.locator(composerSel).first();
  await field.fill("/todo zz-drafts cancel marker");
  const box = page.locator(".chatcmd003-preview");
  await expect(box).toBeVisible({ timeout: 30000 });
  await page.waitForTimeout(3500);
  await reloadAndWait(page);
  await expect(page.locator(composerSel).first()).toHaveValue(
    /zz-drafts cancel marker/,
    {
      timeout: 30000,
    },
  );
  await expect(page.locator(".chatcmd003-preview")).toBeVisible({ timeout: 30000 });
  await page
    .locator(".chatcmd003-preview [data-action='chatcmd003-cancel']")
    .first()
    .click();
  await expect(page.locator(".chatcmd003-preview")).toBeHidden();
  await page.waitForTimeout(300);
  await reloadAndWait(page);
  await expect(page.locator(composerSel).first()).toHaveValue("");
  await expect(page.locator(".chatcmd003-preview")).toHaveCount(0);
});
