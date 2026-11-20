import { test, expect } from "@playwright/test";

// Agents page, chunk 1: #3961 (attachment receipt on agent answers), #3776
// (Agents page participant card) and #3758 (agent task results in Chat
// search). Runs against the dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by
// default). Agents are switched off for the demo tenant, so #3961 and #3776
// can only be exercised when an environment has agents turned on; they skip
// (with the reason) otherwise instead of passing on nothing.

test.setTimeout(180000);

async function signIn(page, name) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${name}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openAgents(page, locale = "en-US") {
  await page.goto(`/workspace/app/chat/agents?locale=${locale}`);
  await page.locator("#main-content").waitFor({ timeout: 60000 });
  await page.waitForFunction(
    () => !document.body.innerText.includes("Loading your workspace data"),
    null,
    { timeout: 90000 },
  );
  return page.locator("#main-content");
}

test("#3961 an agent answer says when an attached file was not read", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  const main = await openAgents(page);
  const off = await main
    .getByText("Agents are turned off for your organization")
    .count();
  test.skip(
    off > 0,
    "agents are turned off for this tenant: no agent run can produce an answer card to carry the attachment receipt",
  );
  // When agents are on and an answer card exists, a reference-only attachment
  // must be said plainly and must offer a Documents link.
  const receipt = page.locator("[data-attachment-receipt]").first();
  await expect(receipt).toContainText(/Reference only/);
});

test("#3776 the Agents Ask view introduces the agent as a participant", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  const main = await openAgents(page);
  const off = await main
    .getByText("Agents are turned off for your organization")
    .count();
  test.skip(
    off > 0,
    "agents are turned off for this tenant: the Ask view (and its participant card) is not rendered",
  );
  const card = main.locator("[data-agent-participant]");
  await expect(card).toBeVisible();
  await expect(card).toHaveAttribute("aria-label", /.+/);
  await expect(card).toContainText("What it can use");
  const avatar = card.locator("[class*=avatar]").first();
  const colour = await avatar.evaluate(
    (el) => window.getComputedStyle(el).backgroundColor,
  );
  expect(colour).not.toMatch(/^rgb\(2[0-9]{2}, [0-9]{1,2}, [0-9]{1,2}\)$/);
});

test("#3758 an agent task found in Chat search opens the Agents page on that task", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 60000 });
  await page.locator("#chat-search").fill("parental leave");
  const results = page.locator(".chat-search-results");
  await expect(results).toBeVisible({ timeout: 60000 });
  await page.waitForFunction(
    () => {
      const r = document.querySelector(".chat-search-results");
      return r && !/Searching…/.test(r.innerText);
    },
    null,
    { timeout: 90000 },
  );
  // No result may point at the page that does not exist.
  const hrefs = await results
    .locator("a[href]")
    .evaluateAll((links) => links.map((a) => a.getAttribute("href")));
  for (const href of hrefs) {
    expect(href, "result link").not.toMatch(/^\/workspace\/app\/agents(\?|#|$)/);
  }
  const group = results.getByText("Your agent tasks");
  test.skip(
    (await group.count()) === 0,
    "no 'Your agent tasks' result for this tenant (agents off, no seeded tasks): the click-through cannot be exercised",
  );
  const task = results.locator("a[href*='/workspace/app/chat/agents']").first();
  await expect(task).toBeVisible();
  const href = await task.getAttribute("href");
  expect(href).toMatch(/task=/);
  expect(href).toMatch(/locale=en-US/);
  await task.click();
  await page.waitForURL(/\/workspace\/app\/chat\/agents\?.*task=/);
  await expect(page.locator("body")).not.toContainText("This page does not exist");
});
