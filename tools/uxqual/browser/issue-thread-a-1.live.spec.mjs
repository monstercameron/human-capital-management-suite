import { Buffer } from "node:buffer";
import { test, expect } from "@playwright/test";

// Chat issue sweep, lane thread-a chunk 1: #3997 (thread reply plus channel
// copy), #3996 (tall thread root), #3965 (open thread survives reload).
// Runs against the dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default).
// All writes go to a private channel named zz-thread-a-<random>.

test.setTimeout(180000);

const rand = () => Math.random().toString(36).slice(2, 7);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function makeChannel(page, name) {
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
  const composer = page.locator("form.chat-composer textarea.composer-input").first();
  await expect(composer).toBeEnabled({ timeout: 60000 });
  await composer.click();
  await composer.fill(text);
  await page.keyboard.press("Enter");
}

async function openThreadOf(page, marker) {
  const message = page
    .locator(".message[data-message-id]", { hasText: marker })
    .first();
  await expect(message).toBeVisible({ timeout: 30000 });
  await message.locator(".message-body").first().hover();
  await message.getByRole("button", { name: "Reply in thread" }).first().click();
  await expect(page.locator(".thread-composer").first()).toBeVisible({
    timeout: 15000,
  });
  return message;
}

async function threadReply(page, text) {
  const box = page.locator("form.thread-composer #thread-composer").first();
  await box.click();
  await box.fill(text);
  await page.keyboard.press("Enter");
  await expect(
    page.locator(".thread-pane, [class*=thread]", { hasText: text }).first(),
  ).toBeVisible({ timeout: 30000 });
}

test("#3996 a tall thread root opens as a clipped summary with replies visible and a working toggle", async ({
  page,
}) => {
  await signIn(page);
  const name = `zz-thread-a-${rand()}`;
  await makeChannel(page, name);
  const tall = Array.from(
    { length: 6 },
    (_, i) =>
      `${name} paragraph ${i + 1} of a deliberately long root message about onboarding.`,
  ).join("\n\n");
  await post(page, tall);
  await openThreadOf(page, `${name} paragraph 1`);
  await threadReply(page, `${name} first reply`);
  await threadReply(page, `${name} second reply`);

  // Close and reopen so the root renders in its opening state.
  await page.goto(`/workspace/app/chat?locale=en-US#channel=${name}`);
  await openThreadOf(page, `${name} paragraph 1`);

  const content = page.locator("#thread-root-content");
  const toggle = page.locator("#thread-root-toggle");
  await expect(content).toBeVisible({ timeout: 15000 });
  await expect(content).toHaveAttribute("data-collapsed", "true");
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await expect(toggle).toHaveText("Show full message");
  const clipped = await content.boundingBox();
  expect(clipped.height).toBeLessThan(200);
  // The first reply is on screen without scrolling the page.
  const reply = page.getByText(`${name} first reply`).last();
  await expect(reply).toBeInViewport();
  // The full text stays in the DOM for assistive tech.
  await expect(content).toContainText(`${name} paragraph 6`);

  // Keyboard operable: focus the toggle, press Enter.
  await toggle.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#thread-root-toggle")).toHaveAttribute(
    "aria-expanded",
    "true",
  );
  await expect(page.locator("#thread-root-toggle")).toHaveText("Show less");
  const full = await page.locator("#thread-root-content").boundingBox();
  expect(full.height).toBeGreaterThan(clipped.height + 50);
  await page.locator("#thread-root-toggle").click();
  await expect(page.locator("#thread-root-content")).toHaveAttribute(
    "data-collapsed",
    "true",
  );

  // A short root with replies is not clipped.
  await post(page, `${name} short root`);
  await openThreadOf(page, `${name} short root`);
  await threadReply(page, `${name} short reply`);
  await expect(page.locator("#thread-root-content")).toHaveCount(0);
});

test("#3965 an open thread is in the address and returns after reload; a bogus thread drops with a notice", async ({
  page,
}) => {
  await signIn(page);
  const name = `zz-thread-a-${rand()}`;
  await makeChannel(page, name);
  await post(page, `${name} root`);
  await openThreadOf(page, `${name} root`);
  await threadReply(page, `${name} reply`);

  await expect.poll(() => page.url(), { timeout: 10000 }).toMatch(/openthread=[^&]+/);
  const url = page.url();
  expect(url).toContain(`channel=${name}`);

  await page.reload();
  await expect(page.locator(".thread-composer").first()).toBeVisible({
    timeout: 60000,
  });
  await expect(page.getByText(`${name} reply`).first()).toBeVisible();
  expect(page.url()).toMatch(/openthread=/);

  // Bogus thread id: channel opens, no thread, notice, address loses the thread.
  await page.goto("about:blank");
  await page.goto(
    `/workspace/app/chat?locale=en-US#channel=${name}&openthread=00000000-0000-0000-0000-000000000000`,
  );
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  await expect(page.locator(".thread-composer")).toHaveCount(0);
  await expect(page.getByText(/no longer available/i).first()).toBeVisible({
    timeout: 15000,
  });
  await expect.poll(() => page.url(), { timeout: 10000 }).not.toMatch(/openthread=/);
  await expect(page).toHaveURL(new RegExp(`channel=${name}`));
});

test("#3997 Also send posts the reply in the thread and exactly one copy in the channel", async ({
  page,
}) => {
  await signIn(page);
  const name = `zz-thread-a-${rand()}`;
  // The app talks gRPC over one WebSocket, so a failure of the second write is
  // injected by dropping the connection as the frame that carries the channel copy is sent.
  await makeChannel(page, name);
  await post(page, `${name} root`);
  await openThreadOf(page, `${name} root`);

  const form = () => page.locator("form.thread-composer").first();
  const inThread = (text) =>
    page.locator(".thread-message[data-message-id]", { hasText: text });
  const inChannel = (text) =>
    page.locator(".message[data-message-id]", { hasText: text });
  async function sendAlso(text) {
    const box = form().locator("#thread-composer");
    await box.click();
    await box.fill(text);
    await form().locator("#thread-also-channel").check();
    await box.click();
    await page.keyboard.press("Enter");
  }

  // Success: one reply in the thread and one copy in the channel.
  const ok = `${name} both-ok`;
  await sendAlso(ok);
  await expect(inThread(ok)).toHaveCount(1, { timeout: 30000 });
  await expect(inChannel(ok)).toHaveCount(1, { timeout: 30000 });
});

test.fixme("#3997 a failed channel copy keeps the reply, says so, and a retry sends only the copy (needs fault injection the transport does not allow)", async ({
  page,
}) => {
  await signIn(page);
  const name = `zz-thread-a-${rand()}`;
  // The app talks gRPC over one WebSocket, so a failure of the second write is
  // injected by dropping the connection as the frame that carries the channel copy is sent.
  let dropMarker = null;
  await page.routeWebSocket(/.*/, (ws) => {
    const server = ws.connectToServer();
    ws.onMessage((m) => {
      const b = typeof m === "string" ? Buffer.from(m) : m;
      const text = b.toString("latin1");
      if (
        dropMarker &&
        text.includes(dropMarker) &&
        text.includes("wasm-reply-copy-")
      ) {
        // The channel copy never reaches the server: the connection drops as
        // it is sent, so that call fails at once. The reply already committed.
        server.close();
        ws.close({ code: 1011, reason: "injected" });
        return;
      }
      server.send(m);
    });
    server.onMessage((m) => ws.send(m));
  });
  await makeChannel(page, name);
  await post(page, `${name} root`);
  await openThreadOf(page, `${name} root`);

  const form = () => page.locator("form.thread-composer").first();
  const inThread = (text) =>
    page.locator(".thread-message[data-message-id]", { hasText: text });
  const inChannel = (text) =>
    page.locator(".message[data-message-id]", { hasText: text });
  async function sendAlso(text) {
    const box = form().locator("#thread-composer");
    await box.click();
    await box.fill(text);
    await form().locator("#thread-also-channel").check();
    await box.click();
    await page.keyboard.press("Enter");
  }

  // Not runnable: the app speaks gRPC over HTTP/2 framed inside one WebSocket.
  // Dropping or withholding a frame desynchronises HPACK and breaks every later
  // call, and closing the socket leaves the page without a reconnect, so the
  // second write cannot be failed on its own from the browser. The ledger
  // behaviour is covered by TestIssue3997_* in tools/uxqual/cmd/journeywasm.
  // Copy fails: the reply stays posted, the words come back, a plain notice
  // says the reply was posted but not copied.
  const fail = `${name} copy-fails`;
  dropMarker = fail;
  await sendAlso(fail);
  await expect(inThread(fail)).toHaveCount(1, { timeout: 30000 });
  await expect(page.getByText(/posted, but it was not also sent/i).first()).toBeVisible(
    {
      timeout: 60000,
    },
  );
  await expect(form().locator("#thread-composer")).toHaveValue(fail);
  await expect(inChannel(fail)).toHaveCount(0);

  // Retry: only the missing copy goes; the reply is not posted twice.
  dropMarker = null;
  await form().locator("#thread-also-channel").check();
  await form().locator("#thread-composer").click();
  await page.keyboard.press("Enter");
  await expect(inChannel(fail)).toHaveCount(1, { timeout: 30000 });
  await expect(inThread(fail)).toHaveCount(1);
  await page.waitForTimeout(3000);
  await expect(inChannel(fail)).toHaveCount(1);
  await expect(inThread(fail)).toHaveCount(1);
});
