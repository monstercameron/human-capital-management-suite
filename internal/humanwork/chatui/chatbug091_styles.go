package chatui

// ChatBug091Styles is the one selected-row look of the sidebar: a tint and a
// bar at the start edge (--rail-selected, set with the sidebar's other tokens in
// ChatUX040Styles). A conversation, Saved and Moderation all draw it. Moderation
// is a page that takes the conversation's place, so the conversation row it
// covers is drawn as an ordinary row. Saved is a panel over the conversation,
// which stays open behind it: its row keeps a quiet mark (the bar in a neutral
// tone, no tint) while Saved itself wears the selected look, so exactly one row
// is selected and the open conversation is still found (CHATUX-040).
const ChatBug091Styles = `
.chat-workspace .chat-rail .chatsave-sidebar-row[aria-expanded="true"],.chat-workspace .chat-rail .chatsave-sidebar-row[aria-expanded="true"]:hover,.chat-workspace:has([data-chatremove-overlay="page"]) .chat-rail .chatmod005-row,.chat-workspace:has([data-chatremove-overlay="page"]) .chat-rail .chatmod005-row:hover{background:var(--rail-selected);color:var(--ink);font-weight:600;box-shadow:inset 3px 0 0 var(--accent)}
.chat-workspace[dir="rtl"] .chat-rail .chatsave-sidebar-row[aria-expanded="true"],.chat-workspace[dir="rtl"]:has([data-chatremove-overlay="page"]) .chat-rail .chatmod005-row{box-shadow:inset -3px 0 0 var(--accent)}
.chat-workspace:has(.chatsave-panel:not([hidden])) .chat-rail .chat-row.selected:not(.chatsave-sidebar-row),.chat-workspace:has(.chatsave-panel:not([hidden])) .chat-rail .chat-row.selected:not(.chatsave-sidebar-row):hover{background:transparent;color:var(--ink);font-weight:600;box-shadow:inset 3px 0 0 color-mix(in srgb,var(--ink) 22%,transparent)}
.chat-workspace[dir="rtl"]:has(.chatsave-panel:not([hidden])) .chat-rail .chat-row.selected:not(.chatsave-sidebar-row){box-shadow:inset -3px 0 0 color-mix(in srgb,var(--ink) 22%,transparent)}
.chatsave-empty-link{display:inline;margin-inline-start:4px;padding:0;border:0;background:none;color:var(--accent);font:inherit;text-decoration:underline;text-underline-offset:3px;cursor:pointer}
.chatsave-empty p{margin-block:0}
`
