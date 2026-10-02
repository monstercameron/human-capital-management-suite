package chatui

// ChatLane4Styles holds the layout rules of the search, Saved, Moderation and
// composer-tool work (CHATBUG-078, CHATBUG-077, CHATUX-023, CHATBUG-065 and the
// phone and language items). They come last in the stylesheet so they decide
// ties.
const ChatLane4Styles = chatbug078Styles + chatbug077Styles + chatux023Styles + chatux014Styles + chatbug065Styles + chatbug064Styles + chatbug064DrawerStyles + chatlane4Defaults + chatux013Styles + chatbug060Styles + chatux018Styles + chatbug078RowStyles + chatlane9Styles

// chatbug078Styles: one sidebar row is drawn as selected. While the Moderation
// page is showing it is that entry, not the conversation the page covers.
const chatbug078Styles = `.chat-workspace:has([data-chatremove-overlay="page"]) .chat-row.selected,.chat-workspace:has([data-chatremove-overlay="page"]) .chat-row.selected:hover,.chat-workspace[dir="rtl"]:has([data-chatremove-overlay="page"]) .chat-row.selected{background:transparent;color:var(--muted);font-weight:500;box-shadow:none}` +
	`.chat-workspace:has([data-chatremove-overlay="page"]) .chatmod005-row,.chat-workspace:has([data-chatremove-overlay="page"]) .chatmod005-row:hover{background:color-mix(in srgb,var(--accent) 28%,var(--canvas));color:var(--ink);font-weight:600;box-shadow:inset 3px 0 0 var(--accent)}` +
	`.chat-workspace[dir="rtl"]:has([data-chatremove-overlay="page"]) .chatmod005-row{box-shadow:inset -3px 0 0 var(--accent)}`

// chatbug077Styles: a search result looks like the message it is. A line that
// names the conversation opens it, the author and time follow, and the text
// is cut at four lines. The searched words are one soft tint per phrase.
const chatbug077Styles = `.chat-workspace .chatsearch-view .chatsearch-result{display:block;margin:0;padding:10px 12px 12px;border:0;border-bottom:1px solid var(--line);border-radius:var(--hcm-radius-control);cursor:pointer;text-align:start;white-space:normal}` +
	`.chat-workspace .chatsearch-view .chatsearch-result:hover{background:var(--soft)}` +
	`.chat-workspace .chatsearch-view .chatsearch-open{display:flex;align-items:center;gap:6px;min-height:0;max-width:100%;padding:0;border:0;border-radius:var(--hcm-radius-control);background:none;color:var(--muted);font-size:.75rem;font-weight:600;line-height:1.4;text-align:start;cursor:pointer}` +
	`.chat-workspace .chatsearch-view .chatsearch-open .avatar{width:16px;height:16px;font-size:.5rem}` +
	`.chat-workspace .chatsearch-message{display:flex;align-items:flex-start;gap:10px;margin-top:6px}` +
	`.chat-workspace .chatsearch-main{min-width:0;flex:1}` +
	`.chat-workspace .chatsearch-meta{display:flex;flex-wrap:wrap;align-items:baseline;gap:2px 8px}` +
	`.chat-workspace .chatsearch-author,.chat-workspace .chatsearch-kind{font-weight:700}` +
	`.chat-workspace .chatsearch-time,.chat-workspace .chatsearch-private{color:var(--muted);font-size:.75rem}` +
	`.chat-workspace .chatsearch-saved{display:inline-flex;align-self:center;color:var(--accent)}.chat-workspace .chatsearch-saved .chat-icon{width:14px;height:14px}` +
	`.chat-workspace .chatsearch-text{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:4;overflow:hidden;overflow-wrap:anywhere}` +
	`.chat-workspace .chatsearch-text .message-body>:first-child{margin-top:0}.chat-workspace .chatsearch-text .message-body>:last-child{margin-bottom:0}` +
	`.chat-workspace .chatsearch-plain{margin-top:6px}` +
	`.chat-workspace .chatsearch-view mark.search-hit{background:color-mix(in srgb,var(--hcm-color-warning) 32%,transparent);color:inherit;padding:0 1px;border-radius:2px;box-decoration-break:clone;-webkit-box-decoration-break:clone}`

// chatux023Styles: on a wide window the conversation keeps one column. The
// messages, their cards, the day dividers, the chip row and the composer share
// the --chat-column measure and the same left edge, so the Send button sits
// under the end of the text. The message box reaches 16px further than the
// column (its own 8px of padding and -8px of margin on each side), which puts
// the end of its text on the composer's right edge.
const chatux023Styles = `.chat-workspace{--chat-column:960px;--chat-measure:calc(var(--chat-column) - 66px)}` +
	`.chat-workspace .message-list .message{max-width:calc(var(--chat-column) + 16px)}` +
	`.chat-workspace .message-list .day-divider,.chat-workspace .message-list .unread-divider{max-width:var(--chat-column)}` +
	`.chat-workspace .message-body,.chat-workspace .agent-reply-answer{max-width:var(--chat-measure)}` +
	`.chat-workspace .channel-tray{box-sizing:border-box;width:100%;max-width:calc(var(--chat-column) + 40px)}` +
	`.chat-workspace .chat-composer{box-sizing:border-box;width:min(calc(100% - 40px),var(--chat-column));margin-inline:20px auto}` +
	`@media(max-width:767px){.chat-workspace .chat-composer{width:auto;margin-inline:12px}}`

// chatux014Styles: the thread composer shows the same tools as the
// conversation composer. Its formatting row (every button, including code,
// list and quote), its Enter hint and its Send label follow the same rules; it
// folds to one row when it holds neither focus nor text, as it always did.
const chatux014Styles = `.chat-workspace .thread-composer .composer-format-row{display:none;align-items:center;flex:none;min-width:0;padding:2px 6px 0}` +
	`.chat-workspace .thread-composer[data-format-row="shown"] .composer-format-row{display:flex}` +
	`@media(min-width:800px){.chat-workspace .thread-composer[data-format-row="auto"] .composer-format-row{display:flex}}` +
	`.chat-workspace .thread-composer .composer-format-row .format-tools{display:flex;flex-wrap:wrap;gap:2px;margin:0;padding:0;border:0}` +
	`.chat-workspace .thread-composer .format-button[data-extra=code],.chat-workspace .thread-composer .format-button[data-extra=bullets],.chat-workspace .thread-composer .format-button[data-extra=quote]{display:inline-flex}` +
	`.chat-workspace .thread-composer .composer-tools{display:flex;align-items:center;flex-wrap:wrap;gap:2px;min-width:0}` +
	`.chat-workspace .thread-composer .composer-toolbar .composer-help{display:none}` +
	// The Enter hint is shown whole or not at all, the way the conversation
	// composer's is: a wrapping row one line tall led by an empty strut, so a
	// sentence that does not fit beside the tools and Send moves to a second
	// line and that line is clipped. The thread panel is narrow, and a hint cut
	// to "Enter to send, Shift+En…" told nobody anything.
	`@media(min-width:600px){.chat-workspace .thread-composer .composer-toolbar .composer-help{display:flex;flex-wrap:wrap;align-content:flex-start;justify-content:flex-end;position:static;flex:1 1 0%;min-width:0;block-size:1.5em;line-height:1.5;margin:0;padding-inline:8px;overflow:hidden;white-space:nowrap;text-overflow:clip;color:var(--muted);font-size:.75rem;opacity:1}` +
	`.chat-workspace .thread-composer .composer-toolbar .composer-help::before{content:"";flex:none;inline-size:0;block-size:1.5em}}` +
	`.chat-workspace .thread-composer .send-button{inline-size:auto;min-inline-size:0;padding:0 12px;margin-inline-start:auto}.chat-workspace .thread-composer .send-label{display:inline}` +
	`.chat-workspace .thread-also{display:flex;align-items:center;gap:8px;min-block-size:28px;padding:2px 10px;color:var(--muted);font-size:.8125rem;cursor:pointer}.chat-workspace .thread-also input{margin:0}` +
	`@container chat (max-width:1100px){.chat-workspace .thread-composer:not(:focus-within) :is(.composer-tools,.composer-help,.composer-format-row,.thread-also){display:none}}` +
	`@media(pointer:coarse){.chat-workspace .thread-composer .composer-tools .tool-button,.chat-workspace .thread-composer .composer-format-row .tool-button{min-inline-size:44px;min-block-size:44px}.chat-workspace .thread-also{min-block-size:44px}}`

// chatbug065Styles: every composer tool has a place of its own. The voice and
// location openers are not drawn (the Add menu presses them), but they stay in
// the page as the anchors their panels open from and return focus to. They are
// a point, not a button laid over the + button, so no two controls of the
// composer share a rectangle at any width.
const chatbug065Styles = `.chat-workspace .chat-composer .composer-tools>.chatvoice-tool>.tool-button,.chat-workspace .chat-composer .composer-tools>.chatmap-control>.tool-button{position:absolute;inset-block-start:50%;inset-inline-start:15px;inline-size:0;block-size:0;min-inline-size:0!important;min-block-size:0!important;margin:0;padding:0;border:0;overflow:hidden;pointer-events:none}` +
	`.chat-workspace .chat-composer .composer-tools>.chatvoice-tool>.tool-button:focus-visible,.chat-workspace .chat-composer .composer-tools>.chatmap-control>.tool-button:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:14px;border-radius:50%}`

// chatbug064Styles: below 768 px a message's menu is a sheet at the foot of the
// screen, led by the reaction row, then Reply in thread and Save. The layer
// code places a menu beside its opener with inline styles, so the sheet's own
// placement is !important. Above 768 px the reaction row is not drawn: the
// hover bar carries it.
const chatbug064Styles = `.chat-workspace .message-menu .menu-reaction-row{display:none}` +
	`@media(max-width:767px){` +
	`.chat-workspace .message-menu{position:fixed!important;top:auto!important;bottom:0!important;left:0!important;right:0!important;width:100%!important;max-width:100%!important;max-height:min(78dvh,560px)!important;box-sizing:border-box;display:flex;flex-direction:column;gap:2px;padding:8px 8px calc(8px + env(safe-area-inset-bottom,0px));overflow-y:auto;border-radius:16px 16px 0 0;box-shadow:0 -8px 32px color-mix(in srgb,var(--ink) 28%,transparent)}` +
	`.chat-workspace .message-menu .menu-item{min-height:48px;font-size:1rem}` +
	`.chat-workspace .message-menu .menu-reaction-row{display:flex;order:-3;gap:6px;margin-bottom:4px;padding:4px 4px 8px;border-bottom:1px solid var(--line)}` +
	`.chat-workspace .message-menu .menu-reaction{flex:1 1 0;min-width:44px;min-height:48px;padding:0;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-size:1.375rem;line-height:1;cursor:pointer}` +
	`.chat-workspace .message-menu .menu-reaction .chat-icon{width:24px;height:24px}` +
	`.chat-workspace .message-menu [data-action="reply"]{order:-2}` +
	`.chat-workspace .message-menu .menu-item.chatsave-action{order:-1}` +
	`.chat-workspace .reaction-picker{position:fixed!important;top:auto!important;bottom:0!important;left:0!important;right:0!important;width:100%!important;max-width:100%!important;border-radius:16px 16px 0 0}` +
	`}`

// chatbug064DrawerStyles: the conversation drawer on a phone is flush with the
// screen edge. mobile_rail_focus_js.go measures how far the workspace sits in
// from it and sets --chat-edge-start and --chat-edge-end.
const chatbug064DrawerStyles = `@media(max-width:767px){` +
	`.chat-workspace[data-sidebar-open="true"] .chat-rail{left:calc(-1 * var(--chat-edge-start,0px))}` +
	`.chat-workspace[dir="rtl"][data-sidebar-open="true"] .chat-rail{left:auto;right:calc(-1 * var(--chat-edge-end,0px))}` +
	`}`

// chatlane4Defaults gives the properties the browser code sets on the
// workspace a value to fall back to.
const chatlane4Defaults = `.chat-workspace{--chat-edge-start:0px;--chat-edge-end:0px}`

// chatux013Styles: on a phone the application controls above a conversation
// are one row (the logo, the menu, notifications, the account). The row of
// history, workspace search and quick actions, which took a second row, is
// not drawn while Chat is open: the conversation has its own back control and
// search, and the menu leads to every page. A keyboard shortcut hint is drawn
// only where a keyboard is: not on a touch screen.
const chatux013Styles = `@media(hover:none) and (pointer:coarse){.chat-workspace .chat-search-shortcut{display:none}}`

// ChatUX013Global is the part of CHATUX-013 that reaches outside the
// workspace, into the application bar; it is joined to the stylesheet after
// the scoped part (scope.go).
const ChatUX013Global = `@media(max-width:760px){body:has(.chat-workspace) .topbar>.header-navigation-tools{display:none}}` + chatux013BandRule

// chatux013BandRule gives the bar the one row it now has. At 430 px and below
// the shell lays the bar out on two fixed 44 px rows, so hiding the tools that
// filled the second left that row (and the gap above it) as empty space between
// the bar and the conversation header.
const chatux013BandRule = `@media(max-width:760px){body:has(.chat-workspace) .app-shell .topbar,body:has(.chat-workspace) .app-shell.nav-collapsed .topbar{grid-template-rows:minmax(44px,auto);row-gap:0}}`

// chatbug060Styles: in right-to-left the search field's shortcut badge sits at
// the end of the field, which is its left. The badge is written left-to-right
// ("Ctrl+K"), and a logical inset on an element of that direction resolves to
// the right, over the placeholder's first letters; the badge is placed by its
// physical side there, and the field reserves the same side with
// padding-inline-end (chatux010_styles.go).
const chatbug060Styles = `.chat-workspace[dir="rtl"] .rail-search .chat-search-shortcut{inset-inline-end:auto;left:22px;right:auto}` +
	`.chat-workspace[dir="rtl"] :is(.icon-arrow-left,.icon-panel-left,.icon-chevron-left){transform:scaleX(-1)}`

// chatux018Styles: the filter panel keeps the type scale of the details panel
// it sits in (12 px text, 13 px headings), and a blocked draft says so in the
// warning colour with the offending word underlined in the text shown under it.
const chatux018Styles = `.chat-workspace .chat-details .chatmod{font-size:.75rem;gap:12px}` +
	`.chat-workspace .chat-details .chatmod h4{font-size:.8125rem}.chat-workspace .chat-details .chatmod h5{font-size:.75rem}` +
	`.chat-workspace .chat-details .chatmod :is(.chatmod-intro,.chatmod-hint,.chatmod-state,.chatmod-desc,.chatmod-status,.chatmod-error,.chatmod-name,.chatmod-label,.chatmod-field label,.chatmod-field legend,.chatmod-switch-text,.chatmod-outcome,.chatmod-terms,.chatmod-details summary,.chatmod-channels label){font-size:.75rem}` +
	`.chat-workspace .chat-details .chatmod :where(input,select,textarea,button){font-size:.75rem}` +
	`.chat-workspace .chatmod002-blocked{padding-inline-start:8px;border-inline-start:3px solid var(--hcm-color-warning);color:var(--hcm-color-warning);font-weight:600}` +
	`.chat-workspace .chatmod002-draft{margin:2px 0 0;padding-inline-start:11px;color:var(--hcm-color-text-muted);font-size:.8125rem;overflow-wrap:anywhere}` +
	`.chat-workspace .chatmod002-word{background:transparent;color:var(--hcm-color-text);text-decoration:underline wavy var(--hcm-color-warning);text-underline-offset:3px}`

// chatbug078RowStyles: the selected row is the only filled row of the sidebar.
// The Saved entry, while its panel is open, is drawn pressed (accent text, no
// fill) and not as a second selection.
const chatbug078RowStyles = `.chat-workspace .chatsave-sidebar-row[aria-expanded=true]{background:transparent;color:var(--accent);font-weight:600}`
