import { test, expect } from "@playwright/test";

// Chat issue sweep, lane search-visual chunk 3: #3844. Search results and
// direct-message rows use the same round, per-person coloured avatar as the
// message list. Read only: nothing is posted or changed.

test.setTimeout(240000);
const note = (...a) =>
  test.info().annotations.push({ type: "note", description: a.join(" ") });
test.use({ viewport: { width: 1440, height: 900 } });

async function signInAsRafael(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

const probe = `(el) => {
  const cs = getComputedStyle(el);
  const r = el.getBoundingClientRect();
  return {
    hue: el.getAttribute("data-avatar-hue"),
    radius: cs.borderRadius,
    bg: cs.backgroundColor,
    w: r.width,
    h: r.height,
    text: (el.textContent || "").trim(),
  };
}`;

function expectRoundAndTinted(info, where) {
  expect(info.hue, `${where}: avatar carries a per-person hue`).toMatch(/^[1-8]$/);
  // 50% or a radius of at least half the box both mean a circle.
  const px = parseFloat(info.radius);
  const round = info.radius.includes("%")
    ? px >= 50
    : px >= Math.min(info.w, info.h) / 2 - 0.5;
  expect(round, `${where}: avatar is round (border-radius ${info.radius})`).toBe(true);
  expect(info.bg, `${where}: avatar has a background`).not.toBe("rgba(0, 0, 0, 0)");
}

test("#3844 search results and direct-message rows use the round per-person avatar", async ({
  page,
}) => {
  await signInAsRafael(page);
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 90000,
  });

  // Reference: the author avatars in the message list, by person name.
  const reference = await page.evaluate(`(() => {
    const out = {};
    for (const m of document.querySelectorAll(".message[data-message-id]")) {
      const a = m.querySelector("[data-avatar-hue]");
      const n = m.querySelector(".message-author, .author, .message-header b, .message-head strong");
      if (a && n) out[n.textContent.trim()] = a.getAttribute("data-avatar-hue");
    }
    return out;
  })()`);

  // Direct-message rows in the conversation list.
  const rail = page.locator(".chat-rail");
  await expect(rail.locator(".chat-row").first()).toBeAttached({ timeout: 60000 });
  const dmAvatars = rail.locator(".chat-row [data-avatar-hue]");
  const dmCount = await dmAvatars.count();
  expect(dmCount, "at least one avatar on a conversation row").toBeGreaterThan(0);
  let dmChecked = 0;
  for (let i = 0; i < dmCount; i++) {
    const el = dmAvatars.nth(i);
    if (!(await el.isVisible())) continue;
    expectRoundAndTinted(
      await el.evaluate(eval(probe)),
      `conversation row avatar ${i}`,
    );
    dmChecked++;
  }
  expect(dmChecked).toBeGreaterThan(0);
  // No person tile on a row lacks the hue attribute.
  const squareTiles = await rail.evaluate((root) =>
    [...root.querySelectorAll(".chat-row .avatar")]
      .filter(
        (e) =>
          !e.hasAttribute("data-avatar-hue") && e.getBoundingClientRect().width > 0,
      )
      .map((e) => e.className + ":" + window.getComputedStyle(e).borderRadius),
  );
  note("rail avatars without hue:", JSON.stringify(squareTiles));

  // Search results (Ctrl+K opens the search; typed into the rail field).
  const input = page.locator("#chat-search").first();
  await expect
    .poll(
      async () => {
        if (page.url().includes("#search=")) return true;
        await page.waitForTimeout(2000);
        await input.click();
        await input.fill("");
        await page.keyboard.type("meeting", { delay: 50 });
        await page.keyboard.press("Enter");
        await page.waitForTimeout(3000);
        return page.url().includes("#search=");
      },
      { timeout: 120000, intervals: [2000] },
    )
    .toBe(true);

  const results = page.locator(".chat-search-results");
  await expect(results).toBeVisible({ timeout: 90000 });
  const avatars = results.locator(".avatar, [data-avatar-hue]");
  await expect(avatars.first()).toBeVisible({ timeout: 30000 });
  const n = await avatars.count();
  expect(n).toBeGreaterThan(0);
  let seen = 0;
  const hues = new Set();
  for (let i = 0; i < n; i++) {
    const el = avatars.nth(i);
    if (!(await el.isVisible())) continue;
    const info = await el.evaluate(eval(probe));
    expectRoundAndTinted(info, `search result avatar ${i}`);
    hues.add(info.hue);
    seen++;
  }
  expect(seen).toBeGreaterThan(0);
  note(
    "search avatars:",
    seen,
    "hues:",
    [...hues].join(","),
    "ref:",
    JSON.stringify(reference),
  );

  // Same person, same colour as in the message list.
  const named = await results.evaluate((root) => {
    const out = [];
    for (const row of root.querySelectorAll("[data-avatar-hue]")) {
      const scope =
        row.closest("li, article, button, .chat-search-result") || row.parentElement;
      out.push({
        hue: row.getAttribute("data-avatar-hue"),
        text: scope ? scope.textContent : "",
      });
    }
    return out;
  });
  let compared = 0;
  for (const [name, hue] of Object.entries(reference)) {
    for (const r of named) {
      if (r.text.includes(name)) {
        expect(r.hue, `${name} keeps hue ${hue} in search`).toBe(hue);
        compared++;
      }
    }
  }
  note("compared people:", compared);
  note(
    "search scopes:",
    JSON.stringify(named.map((r) => [r.hue, r.text.slice(0, 80)])),
  );
  const railNamed = await rail.evaluate((root) =>
    [...root.querySelectorAll(".chat-row [data-avatar-hue]")].map((a) => [
      a.getAttribute("data-avatar-hue"),
      (a.closest(".chat-row")?.textContent || "").trim().slice(0, 60),
    ]),
  );
  note("rail rows:", JSON.stringify(railNamed));

  // Same person, same colour in the message list: Theodore Wright (search
  // result) in #general, Evelyn Morgan (conversation row) in the direct message.
  const hueInMessages = async (name) =>
    page.evaluate((who) => {
      for (const m of document.querySelectorAll(".message[data-message-id]")) {
        const a = m.querySelector("[data-avatar-hue]");
        if (
          a &&
          m.querySelector(`button.message-author[aria-label="View ${who}'s details"]`)
        )
          return a.getAttribute("data-avatar-hue");
      }
      return null;
    }, name);
  // Open the result: the message list it lands in shows the same person.
  await avatars.first().click();
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  await expect
    .poll(() => hueInMessages("Theodore Wright"), { timeout: 30000 })
    .toBe(named[0].hue);
  await rail.locator(".chat-row", { hasText: "Evelyn Morgan" }).first().click();
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  await expect
    .poll(() => hueInMessages("Evelyn Morgan"), { timeout: 30000 })
    .toBe(railNamed[0][0]);
});
