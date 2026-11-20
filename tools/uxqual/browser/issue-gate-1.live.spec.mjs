import { test, expect } from "@playwright/test";

// Gate issues #3609, #3824 and #3821 against the live dev cell. Everything that
// writes happens in a zz-gate channel this spec creates; nothing seeded is touched.

test.setTimeout(420000);

async function signIn(page, name) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name }).first().click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function ready(page) {
  await page
    .locator(".message[data-message-id], textarea.composer-input")
    .first()
    .waitFor({ timeout: 200000 });
}

// A person with no open conversation has no composer or message to wait for;
// the sidebar's add-or-find button is the sign the app is up.
async function appUp(page) {
  await page
    .getByRole("button", { name: "Add or find channels" })
    .first()
    .waitFor({ timeout: 200000 });
}

const gateJoinButton = ".browse-list button[data-action='gate-join']";

// Presses the gated channel's button in Browse as the signed-in applicant. The
// channel list can still be settling just after load, so a button that is gone
// again a moment later sends the attempt round again from a fresh page.
async function pressGateJoin(page, channel) {
  for (let attempt = 0; attempt < 5; attempt++) {
    if (attempt > 0) {
      await page.goto("/workspace/app/chat?locale=en-US");
      await appUp(page);
    }
    await page.getByRole("button", { name: "Add or find channels" }).first().click();
    await page.locator("[data-action='open-browse']:visible").first().click();
    await page.locator(".browse-list").waitFor({ timeout: 60000 });
    await page.locator("#browse-filter").fill(channel);
    const button = page.locator(gateJoinButton).first();
    if (
      !(await button.waitFor({ timeout: 20000 }).then(
        () => true,
        () => false,
      ))
    )
      continue;
    await page.waitForTimeout(1500);
    await page.evaluate(() => {
      window.__joinSeen = false;
      new window.MutationObserver(() => {
        if (/Join #/.test(document.body.innerText)) window.__joinSeen = true;
      }).observe(document.body, {
        childList: true,
        subtree: true,
        characterData: true,
      });
    });
    const clicked = await button.click({ timeout: 8000 }).then(
      () => true,
      () => false,
    );
    if (clicked) return;
  }
  throw new Error("the gated channel never stayed in the applicant's Browse list");
}

async function ownChannel(page, locale = "en-US") {
  const name = `zz-gate-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto(`/workspace/app/chat?locale=${locale}`);
  await ready(page);
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().fill(`zz marker ${name}`);
  await page.keyboard.press("Enter");
  // A recent message keeps the channel in the front of Browse's list; a slow
  // post is not what this spec is about.
  await page
    .locator(".message[data-message-id]")
    .first()
    .waitFor({ timeout: 40000 })
    .catch(() => {});
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
  await page.waitForTimeout(4000);
}

const shared = { channel: "" };

async function geometry(page) {
  return page.evaluate(() => {
    const overlay = document.querySelector(".chatgate-overlay");
    const close = [...document.querySelectorAll(".chatgate-overlay button")].find((b) =>
      /close form|formular schlie|إغلاق النموذج/i.test(
        b.textContent + (b.getAttribute("aria-label") || ""),
      ),
    );
    const o = overlay.getBoundingClientRect();
    const c = close ? close.getBoundingClientRect() : null;
    const active = document.activeElement;
    return {
      position: window.getComputedStyle(overlay).position,
      overlay: { x: o.x, y: o.y, w: o.width, h: o.height },
      close: c && { x: c.x, y: c.y, w: c.width, h: c.height },
      vw: window.innerWidth,
      vh: window.innerHeight,
      focusInside: !!active && overlay.contains(active),
      scrolls: overlay.scrollHeight > overlay.clientHeight,
      overflowY: window.getComputedStyle(overlay).overflowY,
    };
  });
}

test("#3609 Joining questions opens the Gate form inside the viewport", async ({
  page,
}) => {
  await signIn(page, /^Continue as Rafael Torres/);
  shared.channel = await ownChannel(page);
  const sizes = [
    [1280, 720],
    [390, 844],
    [320, 640],
  ];
  for (const [w, h] of sizes) {
    await page.setViewportSize({ width: w, height: h });
    const opener = page.locator("a[data-gate-open]").first();
    if (!(await opener.isVisible())) {
      await page.locator("[data-action='header-details']").first().click();
    }
    await opener.waitFor({ timeout: 30000 });
    await opener.click();
    await page.locator(".chatgate-overlay").waitFor({ timeout: 150000 });
    const g = await geometry(page);
    expect(g.position, `${w}px position`).toBe("fixed");
    expect(g.overlay.y).toBeGreaterThanOrEqual(0);
    expect(g.overlay.y + g.overlay.h).toBeLessThanOrEqual(g.vh + 1);
    expect(g.overlay.h).toBeGreaterThan(g.vh * 0.9);
    expect(g.close, `${w}px close control`).not.toBeNull();
    expect(g.close.y).toBeGreaterThanOrEqual(0);
    expect(g.close.y + g.close.h).toBeLessThanOrEqual(g.vh);
    expect(g.close.x + g.close.w).toBeLessThanOrEqual(g.vw);
    expect(g.focusInside, `${w}px focus inside the form`).toBe(true);
    // The form scrolls inside itself, not the page.
    expect(g.overflowY).toMatch(/auto|scroll/);
    // Close form hands focus back and the overlay goes.
    await page.keyboard.press("Escape");
    await expect(page.locator(".chatgate-overlay")).toBeHidden({ timeout: 20000 });
    const back = await page.evaluate(() => {
      const a = document.activeElement;
      return a
        ? a.getAttribute("data-gate-open") !== null ||
            /joining|gate|details/i.test(
              a.textContent || a.getAttribute("aria-label") || "",
            )
        : false;
    });
    expect(back, `${w}px focus returns to the opener`).toBe(true);
  }
});

test("#3609 the form also sits in the viewport in Arabic (RTL) and dark", async ({
  page,
}) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await signIn(page, /^Continue as Rafael Torres/);
  await ownChannel(page, "ar");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator("[data-action='header-details']").first().click();
  await page.locator("a[data-gate-open]").first().click();
  await page.locator(".chatgate-overlay").waitFor({ timeout: 150000 });
  const g = await geometry(page);
  expect(g.position).toBe("fixed");
  expect(g.overlay.y + g.overlay.h).toBeLessThanOrEqual(g.vh + 1);
  expect(g.close).not.toBeNull();
  expect(g.close.x).toBeGreaterThanOrEqual(0);
  expect(g.close.x + g.close.w).toBeLessThanOrEqual(g.vw);
  expect(g.focusInside).toBe(true);
});

test("#3821 a gated channel's button in Browse opens the form with no Join dialog", async ({
  page,
  browser,
}) => {
  await signIn(page, /^Continue as Rafael Torres/);
  shared.channel = await ownChannel(page);
  await openGate(page);
  await publishGate(page);

  const ctx = await browser.newContext();
  const p2 = await ctx.newPage();
  await signIn(p2, /^Continue as Linh Tran/);
  await p2.goto("/workspace/app/chat?locale=en-US");
  await appUp(p2);
  await pressGateJoin(p2, shared.channel);
  await p2.locator(".chatgate-overlay").waitFor({ timeout: 60000 });
  await p2.waitForTimeout(1500);
  expect(await p2.evaluate(() => window.__joinSeen)).toBe(false);
  const text = await p2.evaluate(() => document.body.innerText);
  expect(text).not.toMatch(/Join #\S+\?/);
  expect(text).not.toMatch(/keep it in your left sidebar/i);
  // Browse closed when the form opened.
  expect(await p2.locator(".browse-list").count()).toBe(0);
  // Closing the form returns to a clean page: no confirmation left over.
  await p2.keyboard.press("Escape");
  await expect(p2.locator(".chatgate-overlay")).toBeHidden({ timeout: 20000 });
  const after = await p2.evaluate(() => document.body.innerText);
  expect(after).not.toMatch(/Join #\S+\?/);
  await ctx.close();
});

test("#3824 'Who has seen my answers' lists only others, once each, by full name", async ({
  browser,
}) => {
  const setup = await browser.newContext();
  const admin = await setup.newPage();
  await signIn(admin, /^Continue as Rafael Torres/);
  shared.channel = await ownChannel(admin);
  await openGate(admin);
  await publishGate(admin);
  await setup.close();
  const lctx = await browser.newContext();
  const linh = await lctx.newPage();
  await signIn(linh, /^Continue as Linh Tran/);
  await linh.goto("/workspace/app/chat?locale=en-US");
  await appUp(linh);
  const openForm = async () => {
    await pressGateJoin(linh, shared.channel);
    await linh.locator(".chatgate-overlay").waitFor({ timeout: 60000 });
  };
  await openForm();
  // Open the form several times so the person's own views would pile up.
  for (let i = 0; i < 2; i++) {
    await linh.keyboard.press("Escape");
    await expect(linh.locator(".chatgate-overlay")).toBeHidden({ timeout: 20000 });
    await openForm();
  }
  // Submit answers so there is something for an administrator to read.
  const answer = linh
    .locator("#gate-answer-form input, #gate-answer-form textarea")
    .first();
  await answer.fill("Zz answer for the sweep");
  await linh.locator("[data-gate-action='submit']").first().click();
  await linh.waitForTimeout(4000);

  // The administrator opens the review queue and the answers view (a read of Linh's answers).
  const rctx = await browser.newContext();
  const raf = await rctx.newPage();
  await signIn(raf, /^Continue as Rafael Torres/);
  await raf.goto(`/workspace/app/chat?locale=en-US#channel=${shared.channel}`);
  await ready(raf);
  await raf.locator("[data-action='header-details']").first().click();
  await raf.locator("a[data-gate-open]").first().click();
  await raf.locator(".chatgate-overlay").waitFor({ timeout: 150000 });
  await raf.waitForTimeout(4000);
  await rctx.close();

  // Linh reloads her form and reads the list.
  await linh.keyboard.press("Escape").catch(() => {});
  await linh.goto("/workspace/app/chat?locale=en-US");
  await appUp(linh);
  await openForm();
  const section = linh
    .locator(".chatgate-overlay section:has(> h3)")
    .filter({ has: linh.locator("h3", { hasText: "Who has seen my answers" }) });
  await expect(section).toBeVisible({ timeout: 30000 });
  const rows = (await section.locator("p").allInnerTexts())
    .map((t) => t.trim())
    .filter(Boolean);
  expect(
    rows.some((r) => /^Linh/.test(r)),
    "none of the lines is Linh's own read",
  ).toBe(false);
  expect(new Set(rows).size, "no line repeats").toBe(rows.length);
  for (const r of rows) expect(r).toMatch(/^\S+ \S+.* · .+ · \d{4}-\d{2}-\d{2}$/);
  await lctx.close();
});
