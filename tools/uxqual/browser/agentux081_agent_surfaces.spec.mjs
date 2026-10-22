import { test, expect } from "@playwright/test";

// AGENTUX-081: an agent's name opens the agent's own profile (with Ask), Ask
// and the mention list offer only agents that answer questions, Activity
// states each agent's real state, every Agents-page read ends in content or
// the shared retry state, and Edit brings its form into view with focus in
// the first field. Written for the lane seed (Walt Brennan, agent owner);
// needs a running server and browser, so it is specified here and not
// executed in unit runs.

test.setTimeout(180000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("an agent name opens the agent profile, never the person panel", async ({
  page,
}) => {
  await signIn(page);
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  const author = page.locator(
    '.message-author[data-action="agent-profile-open"]',
  );
  await expect(author.first()).toBeVisible({ timeout: 30000 });
  await author.first().click();
  const dialog = page.locator(".agent-profile-dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("Ask");
  await expect(dialog).not.toContainText("Person details");
  await expect(dialog).not.toContainText("Not available");
});

test("Ask offers only agents that answer questions", async ({ page }) => {
  await signIn(page);
  await page.goto("/workspace/app/chat/agents?locale=en-US");
  const choices = page.locator(".agents-agent-choices");
  await expect(choices).toBeVisible({ timeout: 30000 });
  await expect(choices).toContainText("Policy Helper");
  await expect(choices).not.toContainText("Birthday Buddy");
  await expect(choices).not.toContainText("Task Catcher");
});

test("Edit brings its form into view with focus in the first field", async ({
  page,
}) => {
  await signIn(page);
  await page.goto("/workspace/app/admin/personas?locale=en-US");
  const edit = page.locator('[data-persona-editor-toggle][aria-controls]');
  await expect(edit.first()).toBeVisible({ timeout: 30000 });
  await edit.first().click();
  const form = page.locator("form.persona-admin-version-editor:not([hidden])");
  await expect(form).toBeVisible();
  await expect(form).toBeInViewport();
  const focused = page.locator(
    "form.persona-admin-version-editor:not([hidden]) input[name='display_name']:focus",
  );
  await expect(focused).toBeVisible();
});

test("a failed Cost read ends in Try again and retries", async ({ page }) => {
  await signIn(page);
  await page.goto("/workspace/app/admin/agents?locale=en-US&tab=cost");
  const panel = page.locator("#agent-cost");
  await expect(panel).toBeVisible({ timeout: 30000 });
  await page.route("**/api/agent-cost/v1/report", (route) => route.abort());
  await page.reload();
  const retry = panel.locator('[data-agent-cost-action="retry"]');
  await expect(retry).toBeVisible({ timeout: 60000 });
  await page.unroute("**/api/agent-cost/v1/report");
  await retry.click();
  await expect(
    panel.locator('[data-agent-cost-state="ready"], [data-agent-cost-state="empty"]'),
  ).toBeVisible({ timeout: 60000 });
});
