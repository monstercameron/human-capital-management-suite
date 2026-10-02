package chatui

// ChatBug091Styles is the one selected-row look of the sidebar: a tint and a
// bar at the start edge. A conversation, Saved and Moderation all draw it, and
// while Saved or Moderation is open the conversation row it covers is drawn
// as an ordinary row, so exactly one row is selected at a time.
const ChatBug091Styles = `
.chat-workspace .chat-rail .chatsave-sidebar-row[aria-expanded="true"],.chat-workspace .chat-rail .chatsave-sidebar-row[aria-expanded="true"]:hover,.chat-workspace:has([data-chatremove-overlay="page"]) .chat-rail .chatmod005-row,.chat-workspace:has([data-chatremove-overlay="page"]) .chat-rail .chatmod005-row:hover{background:color-mix(in srgb,var(--accent) 28%,var(--canvas));color:var(--ink);font-weight:600;box-shadow:inset 3px 0 0 var(--accent)}
.chat-workspace[dir="rtl"] .chat-rail .chatsave-sidebar-row[aria-expanded="true"],.chat-workspace[dir="rtl"]:has([data-chatremove-overlay="page"]) .chat-rail .chatmod005-row{box-shadow:inset -3px 0 0 var(--accent)}
.chat-workspace:has(.chatsave-panel:not([hidden])) .chat-rail .chat-row.selected:not(.chatsave-sidebar-row),.chat-workspace:has(.chatsave-panel:not([hidden])) .chat-rail .chat-row.selected:not(.chatsave-sidebar-row):hover{background:transparent;color:var(--muted);font-weight:500;box-shadow:none}
.chatsave-empty-link{display:inline;margin-inline-start:4px;padding:0;border:0;background:none;color:var(--accent);font:inherit;text-decoration:underline;text-underline-offset:3px;cursor:pointer}
.chatsave-empty p{margin-block:0}
`
