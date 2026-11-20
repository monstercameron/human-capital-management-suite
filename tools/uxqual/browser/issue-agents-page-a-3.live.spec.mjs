import { test, expect } from "@playwright/test";

// #3969: a private answer is not a private question. In a public channel, asking an
// agent shows that the question stays visible and offers "Ask this agent privately",
// which moves the unsent text into the person's own conversation with the agent.

const channel = "/workspace/app/chat?locale=en-US#channel=general";
test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("#3969 a private answer says the question stays public and offers Ask this agent privately", async ({
  page,
}) => {
  const posts = [];
  page.on("request", (request) => {
    if (request.method() === "POST" && /chat/i.test(request.url()))
      posts.push(request.url());
  });
  await signIn(page);
  await page.goto(channel);
  const composer = page.getByRole("textbox").last();
  await expect(composer).toBeVisible({ timeout: 40000 });
  await composer.click();
  await composer.pressSequentially("@", { delay: 50 });
  // An agent must be offered as a mention for the private-answer row to appear.
  const option = page.getByRole("option").first();
  await expect(option, "agent mention offered").toBeVisible({ timeout: 15000 });
  await option.click();
  await composer.pressSequentially(" zz3969 what is my payroll note", { delay: 10 });

  const row = page.locator(".composer-private-answer").first();
  await expect(row).toBeVisible({ timeout: 10000 });
  await expect(row).toContainText("will still be visible to everyone in");
  const ask = row.getByRole("button", { name: /Ask this agent privately/ });
  await expect(ask).toBeVisible();
  await expect(ask).toBeEnabled();
  const before = posts.length;
  await ask.click();
  const moved = page.locator("[data-private-ask='moved']");
  await expect(moved).toBeVisible({ timeout: 15000 });
  await expect(moved).toContainText("Not sent yet");
  await expect(page.getByRole("textbox").last()).toHaveValue(
    /zz3969 what is my payroll note/,
  );
  expect(posts.length).toBe(before);
  await moved.getByRole("button", { name: "Back to channel" }).click();
  await expect(page.getByRole("textbox").last()).toHaveValue(/zz3969/, {
    timeout: 10000,
  });
});
