import { test, expect } from "@playwright/test";

// Chat issue sweep, lane thread-a chunk 3: #3872 (thread Reply stays enabled
// while a nonempty draft is previewed). Runs against the dev cell
// (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default). Nothing is posted.

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openThread(page, channel) {
  await page.goto(`/workspace/app/chat?locale=en-US#channel=${channel}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 45000,
  });
  const link = page
    .getByRole("button", { name: /[0-9]+\s*repl|repl\S*\s*[0-9]+/i })
    .first();
  await expect(link).toBeVisible({ timeout: 15000 });
  await link.click();
  await expect(page.locator(".thread-composer").first()).toBeVisible({
    timeout: 15000,
  });
}

test("#3872 thread Reply stays enabled while a nonempty draft is previewed, like channel Send", async ({
  page,
}) => {
  await signIn(page);

  // Channel composer: Send is the reference behaviour.
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  const cin = page.locator("#chat-composer").first();
  await expect(cin).toBeVisible({ timeout: 45000 });
  await cin.click();
  await page.keyboard.type("zz-3872 Temporary **preview** draft");
  await page.locator("form.chat-composer .composer-preview-toggle").first().click();
  const channelSend = page.locator("form.chat-composer .send-button").first();
  await expect(channelSend).toBeEnabled();
  await page.locator("form.chat-composer .composer-preview-toggle").first().click();
  await page.locator("#chat-composer").first().fill("");

  await openThread(page, "engineering");
  const form = page.locator("form.thread-composer").first();
  const reply = form.locator(".send-button").first();
  const toggle = form.locator(".composer-preview-toggle").first();

  // Empty draft: Reply is disabled.
  await expect(reply).toBeDisabled();

  await form.locator("#thread-composer").click();
  await page.keyboard.type("zz-3872 Temporary **preview** draft");
  await expect(reply).toBeEnabled();

  // Preview on: the formatted draft shows and Reply is still enabled.
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-pressed", "true");
  await expect(form.locator("#thread-composer")).toHaveCount(0);
  await expect(page.locator("form.thread-composer .send-button").first()).toBeEnabled();

  // Preview off: same draft, still enabled.
  await page.locator("form.thread-composer .composer-preview-toggle").first().click();
  const tin = page.locator("form.thread-composer #thread-composer").first();
  await expect(tin).toHaveValue(/zz-3872 Temporary/);
  await expect(page.locator("form.thread-composer .send-button").first()).toBeEnabled();

  // Clearing the draft disables Reply again. Nothing was posted.
  await tin.fill("");
  await expect(
    page.locator("form.thread-composer .send-button").first(),
  ).toBeDisabled();
});
