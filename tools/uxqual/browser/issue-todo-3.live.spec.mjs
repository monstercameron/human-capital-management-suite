import { test, expect } from "@playwright/test";

// To-do previews (#3931, #3923) against the live dev cell. Each test works in its
// own channel; every preview is cancelled and nothing is posted.

test.setTimeout(240000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function ownChannel(page, locale = "en-US") {
  const name = `zz-todo-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto(`/workspace/app/chat?locale=${locale}`);
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
  return name;
}

async function previewOf(page, text) {
  const input = page.locator("textarea.composer-input").first();
  // The preview opens as the line is typed; Enter would post the card it shows.
  await input.fill(text);
  const preview = page.locator(".chatcmd003-preview");
  await expect(preview).toBeVisible({ timeout: 60000 });
  await expect(preview.locator(".chatcmd003-choice[disabled]")).toHaveCount(0, {
    timeout: 60000,
  });
  return preview;
}

async function cancel(page) {
  await page.locator(".chatcmd003-preview [data-action='chatcmd003-cancel']").click();
  await expect(page.locator(".chatcmd003-preview")).toBeHidden();
}

test("#3931 an inferred weekday can be declined, same weekday offers today, next Friday keeps its qualifier", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);

  let preview = await previewOf(page, "/todo Discuss the schedule for Friday");
  const none = preview.getByRole("radio", {
    name: /No deadline, keep .Friday. in the task/i,
  });
  await expect(none).toBeVisible();
  const infer = preview.getByRole("radio", { name: /^Due .*read from .Friday./i });
  await expect(infer).toBeVisible();
  await expect(infer).toHaveAttribute("aria-checked", "true");
  await expect(none).toBeEnabled({ timeout: 30000 });
  await none.click();
  await expect(
    preview.getByRole("radio", { name: /No deadline, keep/i }),
  ).toHaveAttribute("aria-checked", "true");
  const declined = await preview.innerText();
  expect(declined).toMatch(/Discuss the schedule for Friday/);
  expect(declined).not.toMatch(/\bDue\s+\d{4}-\d{2}-\d{2}\s*$/m);
  await preview.getByRole("radio", { name: /^Due .*read from/i }).click();
  await expect(
    preview.getByRole("radio", { name: /^Due .*read from/i }),
  ).toHaveAttribute("aria-checked", "true");
  await cancel(page);

  const weekday = new Date().toLocaleDateString("en-US", { weekday: "long" });
  preview = await previewOf(page, `/todo Review by ${weekday}`);
  await expect(preview.getByRole("radio", { name: /^Today/i })).toBeVisible();
  await cancel(page);

  preview = await previewOf(page, "/todo Submit timecard by next Friday");
  const next = await preview.innerText();
  expect(next).toMatch(/Submit timecard by next Friday/);
  expect(next).not.toMatch(/by next\s*$/m);
  await expect(
    preview.locator("[role='status']").filter({ hasText: /next Friday/i }),
  ).toBeVisible();
  expect(
    await preview.getByRole("radio", { name: /^Due /i }).count(),
  ).toBeGreaterThanOrEqual(2);
  await cancel(page);
});

test("#3923 assignee-only with an unassigned item blocks Post until an owner or anyone is chosen", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  const preview = await previewOf(
    page,
    "/todo Confirm renewal @Sofia Petrov tomorrow; Prepare guide Friday",
  );
  const post = preview.locator("[data-action='chatcmd003-post']");
  const assignee = preview.getByRole("radio", { name: /The assignee/i });
  await expect(assignee).toBeEnabled({ timeout: 30000 });
  await assignee.click();
  await expect(post).toBeDisabled();
  const text = await preview.innerText();
  expect(text).toMatch(/Prepare guide/);
  const anyone = preview.getByRole("button", { name: /Let anyone complete items/i });
  await expect(anyone).toBeVisible();
  // Each unassigned item offers the people of this conversation; nobody is preselected.
  const owners = preview.getByRole("radio", { name: /^Rafael Torres$/ });
  expect(await owners.count()).toBeGreaterThanOrEqual(2);
  await expect(post).toBeDisabled();
  // Picking an owner for every item is the other way out.
  for (let i = (await owners.count()) - 1; i >= 0; i--) {
    await expect(owners.nth(i)).toBeEnabled({ timeout: 30000 });
    await owners.nth(i).click();
  }
  await expect(post).toBeEnabled({ timeout: 30000 });
  await cancel(page);

  // The explicit alternative: let anyone complete items.
  const again = await previewOf(
    page,
    "/todo Confirm renewal @Sofia Petrov tomorrow; Prepare guide Friday",
  );
  await expect(again.getByRole("radio", { name: /The assignee/i })).toBeEnabled({
    timeout: 30000,
  });
  await again.getByRole("radio", { name: /The assignee/i }).click();
  const anyoneAgain = again.getByRole("button", { name: /Let anyone complete items/i });
  await expect(again.locator("[data-action='chatcmd003-post']")).toBeDisabled();
  await expect(anyoneAgain).toBeEnabled({ timeout: 30000 });
  await anyoneAgain.click();
  await expect(again.locator("[data-action='chatcmd003-post']")).toBeEnabled({
    timeout: 30000,
  });
  await cancel(page);
});
