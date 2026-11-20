import { test, expect } from "@playwright/test";

// Live checks for #3935 (link warning in Preview), #3913 (list/quote leave the
// following line alone) and #3903 (Bold toggles). Writes only in a private
// zz-composer channel; nothing is sent.

test.setTimeout(120000);
test.use({ viewport: { width: 1440, height: 900 } });

const composerSel = "textarea[name='message']";

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function ownChannel(page) {
  const name = `zz-composer-${Math.random().toString(36).slice(2, 7)}`;
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await expect(page.locator(composerSel)).toBeVisible({ timeout: 60000 });
  await page
    .getByRole("button", { name: /New conversation/i })
    .first()
    .click();
  const dialog = page.locator(".create-dialog");
  await dialog.getByRole("radio", { name: /Public channel/ }).click();
  await dialog.getByRole("textbox").first().fill(name);
  await dialog.locator("button[type=submit]").click();
  await expect(page.locator(composerSel)).toHaveAttribute(
    "placeholder",
    new RegExp(name),
    { timeout: 30000 },
  );
  return name;
}

async function setValue(box, value) {
  await box.evaluate((el, v) => {
    el.focus();
    el.value = v;
    el.dispatchEvent(new window.Event("input", { bubbles: true }));
  }, value);
}

async function select(box, start, end) {
  await box.evaluate(
    (el, [s, e]) => {
      el.focus();
      el.setSelectionRange(s, e);
    },
    [start, end],
  );
}

test("#3935 Preview names a link that will not render and Edit message returns to the draft", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  const box = page.locator(composerSel);
  await box.click();
  await setValue(box, "Please review the plan");
  await select(box, 18, 22);
  await page.getByRole("button", { name: "Link", exact: true }).click();
  await expect(box).toHaveValue("Please review the [plan](url)");
  // The inserted placeholder is selected; replace it with a scheme-free address.
  const sel = await box.evaluate((el) =>
    el.value.slice(el.selectionStart, el.selectionEnd),
  );
  expect(sel).toBe("url");
  await page.keyboard.type("example.com");
  await expect(box).toHaveValue("Please review the [plan](example.com)");

  await page.getByRole("button", { name: "Preview", exact: true }).click();
  const note = page.locator(".composer-link-note");
  await expect(note).toBeVisible();
  await expect(note).toContainText("plan");
  await expect(note).toContainText("https://example.com");
  await expect(note).toHaveAttribute("aria-live", "polite");

  // Keyboard: Edit message returns to the field, draft intact.
  const edit = note.getByRole("button", { name: "Edit message" });
  await edit.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator(".composer-preview")).toBeHidden();
  await expect(box).toBeFocused();
  await expect(box).toHaveValue("Please review the [plan](example.com)");

  // With a full address there is no warning.
  await setValue(box, "Please review the [plan](https://example.com)");
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  await expect(page.locator(".composer-preview")).toBeVisible();
  await expect(page.locator(".composer-link-note")).toHaveCount(0);
  await expect(page.locator(".composer-preview a", { hasText: "plan" })).toBeVisible();
});

for (const [label, marker, tag] of [
  ["Bulleted list", "- ", "li"],
  ["Quote", "> ", "blockquote"],
]) {
  test(`#3913 ${label} on selected lines leaves Closing outside the ${tag}`, async ({
    page,
  }) => {
    await signIn(page);
    await ownChannel(page);
    const box = page.locator(composerSel);
    await box.click();
    const text = "Intro\nFirst item\nSecond item\nClosing";
    await setValue(box, text);
    await select(box, 6, 28);
    await page.getByRole("button", { name: label, exact: true }).click();
    const value = await box.inputValue();
    expect(value.startsWith("Intro\n")).toBe(true);
    expect(value).toContain(`${marker}First item`);
    expect(value).toContain(`${marker}Second item`);
    // A blank line separates Closing from the formatted lines.
    expect(value).toMatch(/Second item\n\nClosing$/);

    await page.getByRole("button", { name: "Preview", exact: true }).click();
    const body = page.locator(".composer-preview-body");
    await expect(body).toBeVisible();
    await expect(body.locator(tag, { hasText: "First item" })).toBeVisible();
    await expect(body.locator(tag, { hasText: "Closing" })).toHaveCount(0);
    await expect(body.getByText("Closing")).toBeVisible();
  });
}

test("#3903 Bold toggles off on a second click and Ctrl+B does the same", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  const box = page.locator(composerSel);
  await box.click();
  await setValue(box, "Please review this");
  await select(box, 7, 13);
  const bold = page.getByRole("button", { name: "Bold", exact: true });
  await bold.click();
  await expect(box).toHaveValue("Please **review** this");
  const picked = () =>
    box.evaluate((el) => el.value.slice(el.selectionStart, el.selectionEnd));
  expect(await picked()).toBe("review");
  await bold.click();
  await expect(box).toHaveValue("Please review this");
  expect(await picked()).toBe("review");
  // Never nests.
  await bold.click();
  await bold.click();
  await expect(box).toHaveValue("Please review this");

  // Keyboard shortcut toggles the same way.
  await box.focus();
  await page.keyboard.press("Control+b");
  await expect(box).toHaveValue("Please **review** this");
  await page.keyboard.press("Control+b");
  await expect(box).toHaveValue("Please review this");
});
