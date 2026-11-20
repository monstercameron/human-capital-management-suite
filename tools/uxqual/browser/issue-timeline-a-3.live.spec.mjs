import { test, expect } from "@playwright/test";

// Live check for the timeline-a lane, chunk 3: #3848. Read-only: it searches
// and opens seeded conversations, never posts.

test.setTimeout(240000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

// Opens search results until one lands on a conversation with newer messages
// still unloaded (the in-list "load newer" button is present).
async function openPartialResult(page, locale) {
  const queries = ["the", "team", "update", "review", "please", "hiring"];
  for (const query of queries) {
    await page.goto(`/workspace/app/chat?locale=${locale}#channel=general`);
    await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
      timeout: 90000,
    });
    const input = page.locator(".chat-rail #chat-search");
    await expect(input).toBeVisible({ timeout: 60000 });
    await input.fill(query);
    await input.press("Enter");
    const results = page.locator(".chatsearch-result");
    try {
      await expect(results.first()).toBeVisible({ timeout: 60000 });
    } catch {
      continue;
    }
    const n = Math.min(await results.count(), 12);
    for (let i = n - 1; i >= 0; i--) {
      await page
        .locator(".chatsearch-result")
        .nth(i)
        .locator(".chatsearch-open")
        .click();
      try {
        await page.locator(".load-newer").first().waitFor({ timeout: 8000 });
        return true;
      } catch {
        // Not a partial view: go back to the results and try the next hit.
        const back = page.getByRole("button", { name: /return to (search )?results/i });
        if (await back.count()) await back.first().click();
        else {
          await page.goto(`/workspace/app/chat?locale=${locale}#channel=general`);
          await input.fill(query);
          await input.press("Enter");
          await expect(results.first()).toBeVisible({ timeout: 60000 });
        }
      }
    }
  }
  return false;
}

const expected = {
  "en-US": { jump: "Skip to the latest message", load: "Show newer messages" },
};

test("#3848 after opening an older search hit, the two newer-message controls are named differently and the search return stays", async ({
  page,
}) => {
  await signIn(page);
  const found = await openPartialResult(page, "en-US");
  expect(found, "a search hit that leaves newer messages unloaded").toBe(true);

  const load = page.locator(".load-newer").first();
  const jump = page.locator(".jump-newest").first();
  await expect(load).toBeVisible();
  await expect(jump).toBeVisible();
  const loadName = (await load.innerText()).trim();
  const jumpText = (await jump.innerText()).trim();
  const jumpAria = await jump.getAttribute("aria-label");
  expect(loadName).toBe(expected["en-US"].load);
  expect(jumpText).toBe(expected["en-US"].jump);
  expect(jumpAria).toBe(expected["en-US"].jump);
  expect(jumpText).not.toBe(loadName);
  expect(await load.getAttribute("title")).toMatch(/next messages here/i);
  // The way back to the results is still offered.
  await expect(
    page.getByRole("button", { name: /return to (search )?results/i }).first(),
  ).toBeVisible();

  // The shortcut still reaches the latest message and then reads "Jump to newest".
  await jump.click();
  await expect(page.locator(".load-newer")).toHaveCount(0, { timeout: 60000 });
});
