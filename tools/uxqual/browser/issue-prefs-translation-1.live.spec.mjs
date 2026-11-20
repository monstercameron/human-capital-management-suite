import { test, expect } from "@playwright/test";

// #3946 scope is part of the accessible name of the reword controls,
// #3884 the enabled translate switch has no "not available" tooltip,
// #3842 the reword choices never break inside a word. Read-only: nothing is
// toggled or saved.

test.setTimeout(180000);

async function signIn(page, persona = "Rafael Torres") {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openPrefs(page, width = 1440, height = 900) {
  await page.setViewportSize({ width, height });
  await signIn(page);
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page
    .getByRole("button", { name: /Chat preferences/i })
    .first()
    .click();
  const layer = page.locator(".chat-settings-layer:visible").first();
  await expect(layer).toBeVisible({ timeout: 30000 });
  return layer;
}

// Resolve an element's accessible name from aria-labelledby the way a browser does.
const nameOf = (el) => {
  const ids = (el.getAttribute("aria-labelledby") || "").split(/\s+/).filter(Boolean);
  if (!ids.length) return el.getAttribute("aria-label") || el.textContent.trim();
  return ids
    .map((id) => document.getElementById(id)?.textContent.trim() ?? "")
    .join(" ")
    .trim();
};

test("#3946 workspace and channel reword groups and switches have distinct, scoped names", async ({
  page,
}) => {
  const layer = await openPrefs(page);
  const info = await layer.evaluate((root, fnSrc) => {
    const nameOf = eval(`(${fnSrc})`);
    const groups = [...root.querySelectorAll("[role=group][aria-labelledby]")].map(
      (g) => ({
        name: nameOf(g),
        pressed: [...g.querySelectorAll("button[aria-pressed]")].map(
          (b) => `${b.textContent.trim()}=${b.getAttribute("aria-pressed")}`,
        ),
      }),
    );
    const switches = [...root.querySelectorAll("[role=switch]")].map((s) => ({
      name: nameOf(s),
      checked: s.getAttribute("aria-checked"),
    }));
    return { groups, switches, text: root.innerText };
  }, nameOf.toString());
  const rewordGroups = info.groups.filter((g) => /reword/i.test(g.name));
  expect(rewordGroups.length, JSON.stringify(info)).toBeGreaterThanOrEqual(2);
  const names = rewordGroups.map((g) => g.name);
  expect(new Set(names).size, names.join(" | ")).toBe(names.length);
  expect(names.some((n) => /workspace/i.test(n))).toBe(true);
  expect(names.some((n) => /channel/i.test(n))).toBe(true);
  for (const g of rewordGroups) expect(g.pressed.length).toBe(3);
  const asWritten = info.switches.filter((s) => /as written/i.test(s.name));
  expect(asWritten.length, JSON.stringify(info.switches)).toBeGreaterThanOrEqual(2);
  const swNames = asWritten.map((s) => s.name);
  expect(new Set(swNames).size, swNames.join(" | ")).toBe(swNames.length);
  expect(swNames.some((n) => /workspace/i.test(n))).toBe(true);
  expect(swNames.some((n) => /channel/i.test(n))).toBe(true);
});

test("#3884 the enabled translate switch carries no 'not available' tooltip", async ({
  page,
}) => {
  const layer = await openPrefs(page);
  const sw = layer
    .getByRole("switch", { name: /Translate messages into my language/i })
    .first();
  await expect(sw).toBeVisible({ timeout: 30000 });
  const state = await sw.evaluate((el) => ({
    title: el.getAttribute("title"),
    disabled: el.disabled || el.getAttribute("aria-disabled") === "true",
    anyTitle: [
      ...el.closest("form, .chat-settings-layer, div").querySelectorAll("[title]"),
    ].map((t) => t.getAttribute("title")),
  }));
  if (!state.disabled) {
    expect(state.title || "", JSON.stringify(state)).not.toMatch(/not available/i);
  } else {
    expect(state.title || "").toMatch(/not available/i);
  }
  const text = await layer.evaluate((el) =>
    [...el.querySelectorAll("[title]")].map((t) => t.getAttribute("title")),
  );
  if (!state.disabled) expect(text.join("|")).not.toMatch(/not available here yet/i);
});

for (const [w, h] of [
  [1440, 900],
  [800, 700],
]) {
  test(`#3842 at ${w}px the reword choices never break inside a word and the popover is usable`, async ({
    page,
  }) => {
    const layer = await openPrefs(page, w, h);
    const group = layer
      .locator("[role=group][aria-labelledby]")
      .filter({ hasText: "Offered to writers" })
      .first();
    await expect(group).toBeVisible({ timeout: 30000 });
    const rects = await group.locator("button").evaluateAll((bs) =>
      bs.map((b) => {
        const r = b.getBoundingClientRect();
        const range = document.createRange();
        range.selectNodeContents(b);
        const lines = new Set(
          [...range.getClientRects()].map((q) => Math.round(q.top)),
        );
        const cs = window.getComputedStyle(b);
        return {
          text: b.textContent.trim(),
          w: r.width,
          h: r.height,
          lines: lines.size,
          sw: b.scrollWidth,
          cw: b.clientWidth,
          wrap: cs.overflowWrap,
        };
      }),
    );
    for (const r of rects) {
      // One word per line at most: "Off" and "On" are a single line; the long
      // label breaks only between words.
      if (/^(Off|On)$/.test(r.text)) expect(r.lines, JSON.stringify(rects)).toBe(1);
      expect(r.sw, JSON.stringify(r)).toBeLessThanOrEqual(r.cw + 1);
      expect(r.wrap).not.toBe("anywhere");
    }
    const popover = await layer.evaluate((el) => {
      const panel =
        el.querySelector("[role=dialog], .chat-settings-popover, .chat-settings") ?? el;
      return panel.getBoundingClientRect().width;
    });
    expect(popover).toBeGreaterThanOrEqual(220);
  });
}
