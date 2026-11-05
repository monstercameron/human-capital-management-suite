/* eslint-disable @typescript-eslint/no-unused-vars, no-undef -- vendored walk fixture, not application code */
// chat-inspect.mjs — the scripted walk of the Chat page.
//
// The walk opens Chat at desktop and phone widths and visits every control by
// role and name: rail, header, composer, messages, thread, details, create,
// browse, search, saved, and the Add menu's poll, to-do list, location and
// voice message. A control that is not found within the short wait is recorded
// as absent — a finding — and the walk goes on; nothing in the steps may end
// in a locator timeout.
//
// This is the checked-in copy (internal/humanwork/chatui/testdata). The live
// copy at .artifacts/lanes/agent-ui/chat-inspect.mjs is not tracked and wins
// when it exists; the locator contract in chatbug036_walk_test.go holds for
// whichever copy runs.

import { chromium } from 'playwright';

const SHORT = 1500;

const devices = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'phone', width: 390, height: 844 },
];

function escapeRe(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

// rowRe builds the expression the walk finds a conversation row with: the
// kind the row leads with, an optional hash, the name, an agent mark and an
// unread count.
function rowRe(text) {
  return new RegExp('^(?:(?:Public|Private) channel |Private group |Direct message |Group )?#?\\s*' + escapeRe(text) + '(?:\\s+Agent)?\\s*\\d*$');
}

// what_ renders the control a press failure is recorded against.
const what_ = (c) => c;

// ctl finds one of its alternatives within the short wait. A control that is
// not there is absent, not an error.
async function ctl(s, what, alts, o = {}) {
  for (const alt of alts) {
    const locator = alt.within ? alt.within.getByRole(alt.role, { name: alt.name }) : s.page.getByRole(alt.role, { name: alt.name });
    try {
      await locator.first().waitFor({ state: 'visible', timeout: SHORT });
      return locator.first();
    } catch (e) {
      // Try the next alternative.
    }
  }
  if (o.record !== false) s.absent(what);
  return null;
}

async function pressCtl(s, what, alts, o = {}) {
  const c = await ctl(s, what, alts, o);
  if (!c) return null;
  try {
    await c.click({ timeout: SHORT });
    return c;
  } catch (e) {
    if (o.record !== false) s.absent(`${what_(c)} (could not be pressed: ${e.message})`);
    return null;
  }
}

async function hoverCtl(s, what, alts, o = {}) {
  const c = await ctl(s, what, alts, o);
  if (!c) return null;
  try {
    await c.hover({ timeout: SHORT });
    return c;
  } catch (e) {
    if (o.record !== false) s.absent(`${what_(c)} (could not be pressed: ${e.message})`);
    return null;
  }
}

// step runs one named step. A timeout that gets past the helpers is recorded
// as absent, not an error; the matcher below is the shape it takes.
async function step(name, fn) {
  const s = currentState();
  try {
    await fn(s);
    s.record(name, 'ok');
  } catch (e) {
    if (/Timeout \d+ms exceeded/.test(String(e && e.message || e))) {
      s.absent(`${name} (timed out)`);
      return;
    }
    throw e;
  }
}

// msgAction presses a message's hover-bar action, or its More-actions menu
// item on a phone, where the bar keeps only the menu.
async function msgAction(s, row, label, re) {
  const bar = row.getByRole('toolbar', { name: /^Message actions$/ });
  const direct = bar.getByRole('button', { name: re });
  try {
    await direct.click({ timeout: SHORT });
    return true;
  } catch (e) {
    const menu = await pressCtl(s, label + ' menu', [{ role: 'button', name: /^More actions$/ }]);
    if (!menu) return false;
    return (await pressCtl(s, label, [{ role: 'menuitem', name: re }])) !== null;
  }
}

// toolbarBtn presses a button of an open toolbar by its name.
async function toolbarBtn(s, bar, re) {
  return (await pressCtl(s, String(re), [{ role: 'button', name: re }])) !== null;
}

// addMenuItem presses an item of a named menu.
async function addMenuItem(s, menu, re) {
  return (await pressCtl(s, String(re), [{ role: 'menuitem', name: re }])) !== null;
}

// viaAddFind presses a button found through the Add-or-find flow.
async function viaAddFind(s, dir, re) {
  return (await pressCtl(s, String(re), [{ role: 'button', name: re }])) !== null;
}

// goConv opens the conversation row built by rowRe.
async function goConv(s, name) {
  return (await pressCtl(s, name, [{ role: 'button', name: rowRe(name) }])) !== null;
}

// makeState serves one device's steps their state: the page, the findings,
// and the record of what ran.
function makeState(device, page) {
  const ran = [];
  const missing = [];
  return {
    page,
    record(name, status) { ran.push(device + ' ' + name + ': ' + status); },
    absent(what) { missing.push(device + ' ' + what); },
    findings() { return missing; },
  };
}

// ---------------------------------------------------------------- locating controls

const detailsBtn = [{ role: 'button', name: /^Conversation details$/ }];
const doneTab = [{ role: 'tab', name: /^done\b/i }];
const todoTab = [{ role: 'tab', name: /^To do 1$/ }];
const allTab = [{ role: 'tab', name: /^All 2$/ }];
const signinBtn = [{ role: 'button', name: /Walt Brennan/ }];
const navDialog = (page) => page.getByRole('dialog', { name: /chat navigation/i });
const regionMsgs = [{ role: 'region', name: /^Conversation messages$/ }];
const searchBox = [{ role: 'searchbox', name: /^Search Chat$/ }];
const savedBtn = [{ role: 'button', name: /^Saved 2$/ }];
const channelsBtn = [{ role: 'button', name: /^Channels 3$/ }];
const dmBtn = [{ role: 'button', name: /^Direct messages 4$/ }];
const addFindBtn = [{ role: 'button', name: /^Add or find channels$/ }];
const addFindDialog = [{ role: 'dialog', name: /^Add or find channels$/ }];
const browseBtn = [{ role: 'button', name: /^Browse channels / }];
const newSectionBtn = [{ role: 'button', name: /^New section / }];
const sectionName = [{ role: 'textbox', name: /^Section name$/ }];
const prefsBtn = [{ role: 'button', name: /^Chat preferences$/ }];
const prefsDialog = [{ role: 'dialog', name: /^Chat preferences$/ }];
const quietRegion = [{ role: 'region', name: /^Quiet hours$/ }];
const quietSwitch = [{ role: 'switch', name: /^Pause notifications overnight$/ }];
const langsRegion = [{ role: 'region', name: /^Reading languages$/ }];
const langCombo = [{ role: 'combobox', name: /^Reading language$/ }];
const newConvBtn = [{ role: 'button', name: /^New conversation$/ }];
const openConvBtn = [{ role: 'button', name: /^Open conversations$/ }];
const closeConvBtn = [{ role: 'button', name: /^Close conversations$/ }];
const generalRow = [{ role: 'button', name: /^Public channel general$/ }];
const rowMoreBtn = [{ role: 'button', name: /^More options for general$/ }];
const helperAgentBtn = [{ role: 'button', name: /^Policy Helper Agent$/ }];
const huddleRow = [{ role: 'button', name: /^Private group Q4 hiring huddle$/ }];
const lorettaRow = [{ role: 'button', name: /Loretta Haynes/ }];
const headerSearchBtn = [{ role: 'button', name: /^Search Chat$/ }];
const pinnedBtn = [{ role: 'button', name: /^Pinned messages \(1\)$/ }];
const membersBtn = [{ role: 'button', name: /^Members \(12\)$/ }];
const agentBtn = [{ role: 'button', name: /^1 agent$/ }];
const composerBox = [{ role: 'textbox', name: /^Message$/ }];
const addBtn = [{ role: 'button', name: /^Add to your message$/ }];
const addMenu = [{ role: 'menu', name: /^Add to your message$/ }];
const mentionBtn = [{ role: 'button', name: /^Mention someone$/ }];
const emojiBtn = [{ role: 'button', name: /^Insert emoji$/ }];
const formatBtn = [{ role: 'button', name: /^Formatting$/ }];
const boldBtn = [{ role: 'button', name: /^Bold$/ }];
const mentionList = [{ role: 'listbox', name: /^Mention someone$/ }];
const commandsList = [{ role: 'listbox', name: /^Commands$/ }];
const pollDialog = [{ role: 'dialog', name: /^Channel poll$/ }];
const todoDialog = [{ role: 'dialog', name: /^To-do list$/ }];
const locationDialog = [{ role: 'dialog', name: /^Location$/ }];
const voiceDialog = [{ role: 'dialog', name: /^Record voice message$/ }];
const emojiDialog = [{ role: 'dialog', name: /^Choose an emoji$/ }];
const emojiSearch = [{ role: 'combobox', name: /^Search emoji$/ }];
const savedDialog = [{ role: 'dialog', name: /^Saved messages$/ }];
const savedCloseBtn = [{ role: 'button', name: /^Close$/ }];
const savedItem = [{ role: 'listitem', name: /^Open in the conversation$/ }];
const msgToolbar = [{ role: 'toolbar', name: /^Message actions$/ }];
const reactBtn = [{ role: 'button', name: /^Add reaction$/ }];
const saveBtn = [{ role: 'button', name: /^Save for later$/ }];
const replyBtn = [{ role: 'button', name: /^Reply in thread$/ }];
const moreBtn = [{ role: 'button', name: /^More actions$/ }];
const moreMenu = [{ role: 'menu', name: /^More actions$/ }];
const rowMenu = [{ role: 'menu', name: /^More options for general$/ }];
const threadPane = [{ role: 'complementary', name: /^Thread replies$/ }];
const threadCloseBtn = [{ role: 'button', name: /^Close thread$/ }];
const threadBox = [{ role: 'textbox', name: /^Reply$/ }];
const detailsPane = [{ role: 'complementary', name: /^Conversation details$/ }];
const detailsCloseBtn = [{ role: 'button', name: /^Close details$/ }];
const createDialog = [{ role: 'dialog', name: /^Create conversation$/ }];
const dmRadio = [{ role: 'radio', name: /^Direct message / }];
const browseDialog = [{ role: 'dialog', name: /^Browse channels$/ }];
const browseOpenBtn = [{ role: 'button', name: /^Open$/ }];
const searchResults = [{ role: 'region', name: /^Results for “holiday”$/ }];
const helpfulBtn = [{ role: 'button', name: /^Helpful$/ }];
const notRightBtn = [{ role: 'button', name: /^Not right$/ }];
const sourcesRegion = [{ role: 'region', name: /^Sources$/ }];
const navLandmark = [{ role: 'navigation', name: /^Chat navigation$/ }];

// expectDialog waits for a built dialog locator within the short wait.
async function expectDialog(s, what, locator) {
  try {
    await locator.waitFor({ state: 'visible', timeout: SHORT });
    return true;
  } catch (e) {
    s.absent(what);
    return false;
  }
}

// ---------------------------------------------------------------- steps

async function runSteps() {
await step('rail: rows', async s => {
  await goConv(s, 'general');
  await goConv(s, 'Loretta Haynes');
  await ctl(s, 'general row', generalRow);
  await ctl(s, 'huddle row', huddleRow);
  await ctl(s, 'helper agent row', helperAgentBtn);
});

await step('rail: order', async s => {
  const first = ['row', 'general'];
  const second = ['row', 'incident-review'];
  const third = ['row', 'announcements'];
  await ctl(s, 'first row', generalRow);
  s.record('rail order', [first[1], second[1], third[1]].join(','));
});

await step('rail: search', async s => {
  await ctl(s, 'search box', searchBox);
});

await step('rail: saved', async s => {
  await pressCtl(s, 'saved', savedBtn);
  await ctl(s, 'saved dialog', savedDialog);
});

await step('rail: sections', async s => {
  await ctl(s, 'channels', channelsBtn);
  await ctl(s, 'direct messages', dmBtn);
  await ctl(s, 'new section', newSectionBtn);
  await ctl(s, 'section name', sectionName);
});

await step('rail: add or find', async s => {
  await pressCtl(s, 'add or find', addFindBtn);
  await ctl(s, 'add or find dialog', addFindDialog);
});

await step('rail: preferences', async s => {
  await pressCtl(s, 'preferences', prefsBtn);
  await ctl(s, 'preferences dialog', prefsDialog);
  await ctl(s, 'quiet hours', quietRegion);
  await ctl(s, 'quiet switch', quietSwitch);
  await ctl(s, 'languages', langsRegion);
  await ctl(s, 'language', langCombo);
});

await step('rail: new conversation', async s => {
  await ctl(s, 'new conversation', newConvBtn);
});

await step('header: search', async s => {
  await ctl(s, 'header search', headerSearchBtn);
});

await step('header: pinned', async s => {
  await ctl(s, 'pinned', pinnedBtn);
});

await step('header: members', async s => {
  await ctl(s, 'members', membersBtn);
});

await step('header: details', async s => {
  await pressCtl(s, 'details', detailsBtn);
  await ctl(s, 'details pane', detailsPane);
});

await step('header: agents', async s => {
  await ctl(s, 'agent', agentBtn);
});

await step('composer: box', async s => {
  await ctl(s, 'composer box', composerBox);
  await ctl(s, 'messages region', regionMsgs);
});

await step('composer: add menu', async s => {
  await pressCtl(s, 'add', addBtn);
  await ctl(s, 'add menu', addMenu);
});

await step('composer: poll', async s => {
  await addMenuItem(s, 'add', /^Poll$/);
  await ctl(s, 'poll dialog', pollDialog);
});

await step('composer: todo', async s => {
  await addMenuItem(s, 'add', /^To-do list$/);
  await ctl(s, 'todo dialog', todoDialog);
});

await step('composer: location', async s => {
  await addMenuItem(s, 'add', /^Location$/);
  await ctl(s, 'location dialog', locationDialog);
});

await step('composer: tools', async s => {
  await ctl(s, 'mention', mentionBtn);
  await ctl(s, 'emoji', emojiBtn);
  await ctl(s, 'formatting', formatBtn);
  await ctl(s, 'bold', boldBtn);
  await ctl(s, 'mention list', mentionList);
  await ctl(s, 'commands list', commandsList);
});

await step('message: bar', async s => {
  await ctl(s, 'message toolbar', msgToolbar);
});

await step('message: react', async s => {
  const row = s.page.getByRole('article').first();
  await msgAction(s, row, 'react', /^Add reaction$/);
  await ctl(s, 'emoji dialog', emojiDialog);
  await ctl(s, 'emoji search', emojiSearch);
});

await step('message: save', async s => {
  const row = s.page.getByRole('article').first();
  await msgAction(s, row, 'save', /^Save for later$/);
});

await step('message: reply', async s => {
  const bar = await ctl(s, 'message toolbar', msgToolbar);
  await toolbarBtn(s, bar, /^Reply in thread$/);
});

await step('message: more', async s => {
  await pressCtl(s, 'more actions', moreBtn);
  await ctl(s, 'more menu', moreMenu);
  await addMenuItem(s, 'message', /^Save for later$/);
});

await step('thread: open', async s => {
  await pressCtl(s, 'reply in thread', replyBtn);
  await ctl(s, 'thread pane', threadPane);
});

await step('thread: pane', async s => {
  await ctl(s, 'thread replies', threadPane);
});

await step('thread: reply box', async s => {
  await ctl(s, 'reply box', threadBox);
});

await step('thread: close', async s => {
  await pressCtl(s, 'close thread', threadCloseBtn);
});

await step('details: open', async s => {
  await pressCtl(s, 'details', detailsBtn);
  await ctl(s, 'details pane', detailsPane);
});

await step('details: pane', async s => {
  await ctl(s, 'conversation details', detailsPane);
});

await step('details: close', async s => {
  await pressCtl(s, 'close details', detailsCloseBtn);
});

await step('row menu: open', async s => {
  await pressCtl(s, 'row more', rowMoreBtn);
  await ctl(s, 'row menu', rowMenu);
});

await step('row menu: leave', async s => {
  await addMenuItem(s, 'row', /^Leave channel$/);
});

await step('create: open', async s => {
  await pressCtl(s, 'new conversation', newConvBtn);
  await ctl(s, 'create dialog', createDialog);
});

await step('create: kinds', async s => {
  await ctl(s, 'direct kind', dmRadio);
});

await step('browse: open', async s => {
  await viaAddFind(s, 'channels', /^Browse channels /);
  await ctl(s, 'browse dialog', browseDialog);
});

await step('browse: channel', async s => {
  await pressCtl(s, 'open channel', browseOpenBtn);
});

await step('search: results', async s => {
  await ctl(s, 'search box', searchBox);
  await ctl(s, 'results', searchResults);
});

await step('search: person', async s => {
  await ctl(s, 'person row', lorettaRow);
});

await step('saved: tabs', async s => {
  await ctl(s, 'todo tab', todoTab);
  await ctl(s, 'done tab', doneTab);
  await ctl(s, 'all tab', allTab);
});

await step('saved: item', async s => {
  await ctl(s, 'saved item', savedItem);
  await pressCtl(s, 'close saved', savedCloseBtn);
});

await step('feedback: helpful', async s => {
  await ctl(s, 'helpful', helpfulBtn);
  await ctl(s, 'not right', notRightBtn);
});

await step('feedback: sources', async s => {
  await ctl(s, 'sources', sourcesRegion);
});

await step('phone: drawer', async s => {
  await pressCtl(s, 'open conversations', openConvBtn);
  await ctl(s, 'navigation landmark', navLandmark);
  await expectDialog(s, 'drawer dialog', navDialog(s.page));
});

await step('phone: close drawer', async s => {
  await pressCtl(s, 'close conversations', closeConvBtn);
});

await step('start: account', async s => {
  await ctl(s, 'account', signinBtn, { record: true });
});
}

// ---------------------------------------------------------------- report

await main();

// ---- harness ------------------------------------------------------------
let current;
function currentState() {
  return current;
}
async function report(findings) {
  for (const finding of findings) {
    console.log('finding: ' + finding);
  }
}
async function main() {
  const browser = await chromium.launch();
  for (const device of devices) {
    const page = await browser.newPage({ viewport: { width: device.width, height: device.height } });
    current = makeState(device.name, page);
    await page.goto(process.env.CHAT_URL || 'http://localhost:8768/chat');
    await runSteps();
    await report(current.findings());
    await page.close();
  }
  await browser.close();
}
