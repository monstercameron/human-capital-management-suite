/* global URL */
import { test, expect } from "@playwright/test";

// Issues #3837 and #3682: a select whose chosen option is not the last one must
// show that option in the real DOM. The search page's "Written in" showed Hindi
// (and Apply then added lang:"hi"); the reading-language select of Chat
// preferences showed Hindi for a person who reads English.
//
// The markup always chose the right option. A few hundred milliseconds after
// the select was first drawn, its own options were appended to it again in one
// call, and the browser left the last one chosen. So one early look at the
// select can pass before the defect shows: every check here watches the select
// for a short while, on first render and again after the view is drawn again.
//
// Runs against the dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default).

test.setTimeout(180000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

// rememberFirstChoice records what each select chose the moment it first held
// options. That is the render's own choice, before anything could move it.
async function rememberFirstChoice(page) {
  await page.addInitScript(() => {
    window.__firstChoice = {};
    const note = () => {
      for (const select of document.querySelectorAll("select[id]")) {
        if (select.options.length > 0 && !(select.id in window.__firstChoice)) {
          window.__firstChoice[select.id] = select.value;
        }
      }
    };
    new window.MutationObserver(note).observe(document, {
      subtree: true,
      childList: true,
    });
  });
}

const readChoice = (page, id) =>
  page.evaluate((selectID) => {
    const select = document.getElementById(selectID);
    if (!select) return null;
    return {
      value: select.value,
      index: select.selectedIndex,
      text: select.selectedOptions[0]?.textContent ?? "",
      chosen: select.selectedOptions.length,
    };
  }, id);

// expectChoiceHolds waits for the select to show the wanted option and then
// watches it for three seconds: the choice must not move.
async function expectChoiceHolds(page, id, want, when) {
  await expect
    .poll(async () => (await readChoice(page, id))?.value, {
      message: `${id} ${when}`,
      timeout: 30000,
    })
    .toBe(want.value);
  for (let look = 1; look <= 12; look++) {
    expect(await readChoice(page, id), `${id} ${when}, look ${look}`).toMatchObject({
      ...want,
      chosen: 1,
    });
    await page.waitForTimeout(250);
  }
}

async function openSearch(page, query) {
  await page.goto(
    `/workspace/app/chat?locale=en-US#search=${encodeURIComponent(query)}`,
  );
  const view = page.locator(".chatsearch-view");
  await expect(view).toBeVisible({ timeout: 60000 });
  await expect(view).toHaveAttribute("aria-busy", "false", { timeout: 60000 });
  return view;
}

async function openRefine(page) {
  const form = page.locator("[data-chatsearch-form='filters']");
  if (!(await form.isVisible())) {
    await page
      .locator(".chatsearch-view")
      .getByText("Refine search", { exact: true })
      .click();
  }
  await expect(form).toBeVisible();
  return form;
}

const hashQuery = (page) =>
  decodeURIComponent(
    new URL(page.url()).hash.replace(/^#search=/, "").replace(/\+/g, " "),
  );

const anyLanguage = { value: "", index: 0, text: "Any language" };
const allKinds = { value: "", index: 0, text: "All kinds" };

test("#3837 Written in and Kind show their first option on first render and after re-render, and Apply adds no lang filter", async ({
  page,
}) => {
  await signIn(page);
  const view = await openSearch(page, "payroll");

  await expectChoiceHolds(page, "chatsearch-written", anyLanguage, "on first render");
  await expectChoiceHolds(page, "chatsearch-kind", allKinds, "on first render");

  // Apply with nothing chosen: the query gains no language and no kind.
  let form = await openRefine(page);
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expectChoiceHolds(page, "chatsearch-written", anyLanguage, "after Apply");
  await expectChoiceHolds(page, "chatsearch-kind", allKinds, "after Apply");
  expect(hashQuery(page)).not.toMatch(/lang:/);
  expect(hashQuery(page)).not.toMatch(/kind:/);
  await expect(view).toHaveAttribute("aria-busy", "false", { timeout: 60000 });

  // A choice that is neither first nor last survives the redraw it causes.
  form = await openRefine(page);
  await form.locator("#chatsearch-written").selectOption("de");
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expect.poll(() => hashQuery(page), { timeout: 30000 }).toMatch(/lang:"?de/);
  await expect(view).toHaveAttribute("aria-busy", "false", { timeout: 60000 });
  await expectChoiceHolds(
    page,
    "chatsearch-written",
    { value: "de", text: "German" },
    "after filtering to German",
  );

  // Back to Any language: the filter leaves the query and the select agrees.
  form = await openRefine(page);
  await form.locator("#chatsearch-written").selectOption("");
  await form.getByRole("button", { name: "Apply filters" }).click();
  await expect.poll(() => hashQuery(page), { timeout: 30000 }).not.toMatch(/lang:/);
  await expect(view).toHaveAttribute("aria-busy", "false", { timeout: 60000 });
  await expectChoiceHolds(
    page,
    "chatsearch-written",
    anyLanguage,
    "after clearing the language",
  );
});

test("#3682 the reading-language select of Chat preferences keeps the language it was rendered with", async ({
  page,
}) => {
  await rememberFirstChoice(page);
  await signIn(page);

  const pages = [
    {
      name: "search page",
      url: "/workspace/app/chat?locale=en-US#search=payroll",
      ready: page.locator(".chatsearch-view"),
    },
    {
      name: "channel page",
      url: "/workspace/app/chat?locale=en-US#channel=general",
      ready: page.locator(".message[data-message-id]").first(),
    },
  ];
  for (const stop of pages) {
    await page.goto(stop.url);
    await page.reload();
    await expect(stop.ready).toBeVisible({ timeout: 60000 });
    const reading = page.locator("#chatrender-reading");
    await reading.waitFor({ state: "attached", timeout: 60000 });

    const rendered = await page.evaluate(
      () => window.__firstChoice["chatrender-reading"],
    );
    expect(rendered, `${stop.name}: the language rendered as chosen`).toMatch(
      /^(en|de|fr|es|pt|ar|ja|hi)$/,
    );
    await expectChoiceHolds(
      page,
      "chatrender-reading",
      { value: rendered },
      `on first render (${stop.name})`,
    );

    // Open the preferences panel as a person does, close it and open it again.
    const gear = page.locator(".rail-prefs-control .chatux002-gear");
    await gear.click();
    await expect(reading).toBeVisible();
    await expectChoiceHolds(
      page,
      "chatrender-reading",
      { value: rendered },
      `with preferences open (${stop.name})`,
    );
    await page.keyboard.press("Escape");
    await expect(reading).toBeHidden();
    await gear.click();
    await expect(reading).toBeVisible();
    await expectChoiceHolds(
      page,
      "chatrender-reading",
      { value: rendered },
      `after reopening preferences (${stop.name})`,
    );
    await page.keyboard.press("Escape");
  }
});
