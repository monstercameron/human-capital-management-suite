import { test, expect } from "@playwright/test";

// Gate editor retention label (#3820) and reviewer reason error (#3819), against
// the live dev cell. Each test works in its own zz-gate channel; nothing seeded
// is touched. #71 is two Go tests (fixed dates against the real clock) and is
// proved by running them, not in a browser.

test.setTimeout(420000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function ownChannel(page) {
  const name = `zz-gate-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
  return name;
}

async function openGate(page) {
  await page.locator("[data-action='header-details']").first().click();
  await page.locator("a[data-gate-open]").first().click();
  await page.locator(".chatgate-overlay").waitFor({ timeout: 150000 });
}

async function publishGate(page) {
  await page.locator("#gate-purpose").fill("Zz sweep purpose");
  await page.locator("[data-gate-action='question-add']").click();
  await page.locator("#gate-editor-0-label").fill("Why do you want to join");
  await page.locator("#gate-editor-0-purpose").fill("To decide admission");
  await page.locator("#gate-editor-0-required").check();
  await page.locator("[data-gate-action='define']").click();
  await page.locator("#gate-version").fill("1.0.0");
  await page.locator("[data-gate-action='publish']").click();
}

test("#3820 the question editor labels the retention field in plain words", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  await openGate(page);
  await page.locator("[data-gate-action='question-add']").click();
  const input = page.locator("#gate-editor-0-days");
  await expect(input).toBeVisible({ timeout: 20000 });
  const label = page.locator("label[for='gate-editor-0-days']");
  const text = (await label.innerText()).trim();
  expect(text).not.toContain("%d");
  expect(text).toBe("Keep answers for (days)");
  await expect(input).toHaveValue("30");
  await expect(page.getByLabel("Keep answers for (days)")).toHaveValue("30");
});

test("#3819 deciding without a reason asks for one under the reason field", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  await openGate(page);
  await publishGate(page);
  const posts = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && /gate/i.test(r.url())) posts.push(r.url());
  });
  // The bulk reason field is part of the review queue even when it is empty.
  const bulk = page.locator("#gate-bulk-reason");
  await bulk.scrollIntoViewIfNeeded();
  await expect(bulk).toBeVisible({ timeout: 20000 });
  await expect(bulk).toHaveAttribute("aria-invalid", "false");
  const before = posts.length;
  await page
    .locator("[data-gate-action='bulk-admit']")
    .first()
    .click({ timeout: 20000 });
  const error = page.locator("#gate-bulk-reason-error");
  await expect(error).toHaveText("Give a reason for this decision.", {
    timeout: 20000,
  });
  await expect(error).toHaveAttribute("role", "alert");
  await expect(bulk).toHaveAttribute("aria-invalid", "true");
  await expect(bulk).toHaveAttribute("aria-describedby", "gate-bulk-reason-error");
  const focused = await page.evaluate(
    () => document.activeElement && document.activeElement.id,
  );
  expect(focused).toBe("gate-bulk-reason");
  expect(posts.length).toBe(before);
});
