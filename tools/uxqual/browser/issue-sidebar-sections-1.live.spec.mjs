import { test, expect } from "@playwright/test";

// Lane sidebar-sections, chunk 1: #3890 section rename focus/scroll, #3714 blank
// name feedback, #3772 unread count pills. Runs against the dev cell (127.0.0.1:8080).

test.setTimeout(240000);
test.use({ viewport: { width: 1440, height: 900 } });

async function signIn(page, persona = "Rafael Torres") {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChat(page, hash = "channel=general") {
  await page.goto(`/workspace/app/chat?locale=en-US#${hash}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
}

async function openSectionForm(page) {
  const trigger = page.locator("button.section-menu-trigger").first();
  await expect(trigger).toBeVisible({ timeout: 90000 });
  await trigger.click();
  await page.locator("[data-action=open-section-create]").first().click();
  const input = page.locator("#chat-new-section");
  await expect(input).toBeVisible({ timeout: 10000 });
  return input;
}

async function makeSection(page, name) {
  const input = await openSectionForm(page);
  await input.fill(name);
  await page.locator(".section-create-form button[type=submit]").click();
  await expect(
    page.locator(`.rail-scroll`).getByText(name, { exact: true }).first(),
  ).toBeVisible({
    timeout: 20000,
  });
}

test("#3714 blank section and channel names say a name is required", async ({
  page,
}) => {
  await signIn(page);
  await openChat(page);
  // Section form: empty, then spaces only.
  for (const blank of ["", "   "]) {
    const input = await openSectionForm(page);
    await input.fill(blank);
    await page.locator(".section-create-form button[type=submit]").click();
    const alert = page.locator("#chat-new-section-error");
    await expect(alert).toBeVisible({ timeout: 5000 });
    await expect(alert).toHaveText(/name/i);
    await expect(input).toBeFocused();
    expect(await input.getAttribute("aria-invalid")).toBe("true");
    expect(await input.getAttribute("aria-describedby")).toContain(
      "chat-new-section-error",
    );
    await page.keyboard.press("Escape");
  }
  // Channel dialog.
  await page
    .getByRole("button", { name: /New conversation/i })
    .first()
    .click();
  const channelChoice = page.getByRole("menuitem", { name: /Channel/i }).first();
  if (await channelChoice.count()) await channelChoice.click();
  const dialog = page.getByRole("dialog").first();
  await expect(dialog).toBeVisible({ timeout: 10000 });
  const create = dialog.getByRole("button", { name: /^Create/i }).first();
  await create.click({ force: true });
  await expect(dialog).toBeVisible();
  const msg = dialog.getByRole("alert").first();
  await expect(msg).toBeVisible({ timeout: 5000 });
  await expect(msg).toHaveText(/name/i);
});

test("#3890 rename section focuses the field, shows it fully, and Cancel returns focus", async ({
  page,
}) => {
  await signIn(page);
  await openChat(page);
  const name = `zz-sidebar-${Math.random().toString(36).slice(2, 7)}`;
  await makeSection(page, name);
  try {
    const opts = page.locator(
      `button[data-action='rail-menu'][title='Options for section ${name}']`,
    );
    await expect(opts).toBeVisible();
    const id = await opts.getAttribute("data-id");
    // Scroll the section near the top edge of the rail's scroll area.
    await page.evaluate((dataId) => {
      const sc = document.querySelector(".rail-scroll");
      const b = document.querySelector(`button[data-id='${dataId}']`);
      sc.scrollTop +=
        b.getBoundingClientRect().top - sc.getBoundingClientRect().top - 2;
    }, id);
    for (const closer of ["Cancel", "Escape"]) {
      await page.locator(`button[data-id='${id}']`).focus();
      await page.keyboard.press("Enter");
      await page
        .getByRole("menuitem", { name: /Rename section/i })
        .first()
        .focus();
      await page.keyboard.press("Enter");
      const input = page.locator("#chat-rename-section");
      await expect(input).toBeVisible({ timeout: 10000 });
      await expect(input).toBeFocused({ timeout: 5000 });
      const form = input.locator("xpath=ancestor::form[1]");
      const geo = await page.evaluate(() => {
        const sc = document.querySelector(".rail-scroll").getBoundingClientRect();
        const f = document.activeElement.closest("form").getBoundingClientRect();
        const l = document.activeElement.closest("form").querySelector("label");
        const lb = l ? l.getBoundingClientRect() : f;
        return {
          scTop: sc.top,
          scBottom: sc.bottom,
          fTop: Math.min(f.top, lb.top),
          fBottom: f.bottom,
        };
      });
      expect(geo.fTop, JSON.stringify(geo)).toBeGreaterThanOrEqual(geo.scTop - 0.5);
      expect(geo.fBottom, JSON.stringify(geo)).toBeLessThanOrEqual(geo.scBottom + 0.5);
      if (closer === "Cancel")
        await form.getByRole("button", { name: /Cancel/i }).click();
      else await page.keyboard.press("Escape");
      await expect(input).toHaveCount(0);
      const active = await page.evaluate(() => {
        const a = document.activeElement;
        return a ? a.getAttribute("data-id") || a.tagName : null;
      });
      expect(active, `after ${closer}`).toBe(id);
    }
  } finally {
    // Leave no section of ours behind in the shared demo.
    const mine = page.locator(
      "button.section-more[title^='Options for section zz-sidebar']",
    );
    for (let i = 0; i < 10 && (await mine.count()) > 0; i++) {
      await mine.first().click();
      await page
        .getByRole("menuitem", { name: /Delete section/i })
        .first()
        .click();
      await page.waitForTimeout(1500);
    }
  }
});

test("#3772 unread counts sit in pills; mentions and DMs use accent", async ({
  page,
}) => {
  await signIn(page);
  await openChat(page, "channel=general");
  let badges = page.locator(".chat-rail-row .chat-badge");
  if ((await badges.count()) === 0) {
    // Make one ordinary unread count in the administrator's own read state.
    await page.goto("/workspace/app/chat?locale=en-US#channel=engineering");
    const others = page.locator(".message[data-message-id]:not(.own)");
    await expect(others.first()).toBeVisible({ timeout: 60000 });
    await others.last().hover();
    await page.locator("button[data-action='menu']").first().click();
    await page
      .getByRole("menuitem", { name: /Mark unread/i })
      .first()
      .click();
    await openChat(page, "channel=general");
    badges = page.locator(".chat-rail-row .chat-badge");
  }
  await expect(badges.first()).toBeVisible({ timeout: 30000 });
  const info = await page.evaluate(() => {
    const accent = (() => {
      const probe = document.createElement("i");
      probe.style.background = window
        .getComputedStyle(document.querySelector(".chat-workspace") || document.body)
        .getPropertyValue("--accent");
      document.body.appendChild(probe);
      const c = window.getComputedStyle(probe).backgroundColor;
      probe.remove();
      return c;
    })();
    const read = (e) => {
      const cs = window.getComputedStyle(e);
      return {
        bg: cs.backgroundColor,
        radius: parseFloat(cs.borderTopLeftRadius),
        h: e.getBoundingClientRect().height,
      };
    };
    const real = [...document.querySelectorAll(".chat-rail-row .chat-badge")].map(
      (e) => ({
        cls: e.className,
        ...read(e),
      }),
    );
    // The same rows with the two accent variants applied, to read the real stylesheet.
    const first = document.querySelector(".chat-rail-row .chat-badge");
    const variants = {};
    for (const v of ["mention", "direct"]) {
      const clone = first.cloneNode(true);
      clone.classList.add(v);
      first.parentNode.appendChild(clone);
      variants[v] = read(clone);
      clone.remove();
    }
    return { accent, real, variants };
  });
  expect(info.real.length).toBeGreaterThan(0);
  for (const b of info.real) {
    expect(b.bg, b.cls).not.toBe("rgba(0, 0, 0, 0)");
    expect(b.radius, `pill ${b.cls}`).toBeGreaterThanOrEqual(b.h / 2 - 1);
  }
  const neutral = info.real.find((b) => !/mention|direct/.test(b.cls));
  expect(neutral, "an ordinary unread pill exists").toBeTruthy();
  expect(neutral.bg, "neutral pill is not the accent").not.toBe(info.accent);
  expect(info.variants.mention.bg, "mention uses accent").toBe(info.accent);
  expect(info.variants.direct.bg, "direct message uses accent").toBe(info.accent);
});
