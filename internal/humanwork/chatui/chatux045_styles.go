package chatui

// ChatUX045Styles is what the tablet and phone widths still got wrong.
//
// A side panel never takes room from the conversation below the width where the
// conversation cannot spare it: Saved is drawn over the conversation (it is
// already fixed to the page edge), so the composer keeps the width it had. At
// the narrowest tablet widths the conversation list steps aside while Saved is
// open, the way it does for the details panel, and the header's Conversations
// button brings it back as a drawer.
//
// A direct conversation with an agent keeps its whole name on one line: the
// Agent mark and the privacy mark wrap under the name instead of squeezing it,
// and the name is cut with an ellipsis only when it alone is wider than the
// header. The header grows to hold the lines.
//
// The conversation list is the one phone screen that holds the workspace
// controls, so it keeps the bell and the account menu: the shell's own two
// buttons are drawn at the end of the list's bar (chatPhoneShellStylesheet)
// and the bar leaves room for them.
const ChatUX045Styles = `@media(min-width:761px){@container chat (max-width:1100px){` +
	`.chat-workspace.chat-workspace:has(>.chatsave-panel:popover-open) .chat-layout{grid-template-columns:minmax(220px,var(--chat-rail)) minmax(0,1fr)}` +
	`.chat-workspace .chatsave-panel{inline-size:min(360px,calc(100vw - 16px))}` +
	`}}` +
	`@media(min-width:761px){@container chat (max-width:760px){` +
	`.chat-workspace.chat-workspace:has(>.chatsave-panel:popover-open) .chat-layout{grid-template-columns:minmax(0,1fr)}` +
	`.chat-workspace:has(>.chatsave-panel:popover-open) .chat-rail{display:none}` +
	`.chat-workspace:has(>.chatsave-panel:popover-open) .rail-pill.mobile-chat-toggle{display:inline-flex}` +
	`.chat-workspace[data-sidebar-open="true"]:has(>.chatsave-panel:popover-open) .chat-rail{display:flex;position:fixed;inset-block:0;inset-inline:0 auto;inline-size:min(85vw,320px);z-index:1200;box-shadow:var(--hcm-shadow-raised)}` +
	`.chat-workspace[data-sidebar-open="true"]:has(>.chatsave-panel:popover-open) .chat-scrim{display:block;position:fixed;inset:0;background:color-mix(in srgb,var(--ink) 55%,transparent);border:0;z-index:1199;cursor:pointer}` +
	`}}` +
	`@media(max-width:760px){` +
	`.chat-workspace .conversation-header:has(.agent-identity-line){block-size:auto;min-block-size:56px;padding-block:var(--hcm-space-1)}` +
	`.chat-workspace .agent-identity-line{flex-wrap:wrap;row-gap:0;max-inline-size:100%}` +
	`.chat-workspace .agent-identity-line h1{flex:0 1 auto;min-inline-size:0;max-inline-size:100%}` +
	`.chat-workspace[data-sidebar-open="true"] .rail-head{padding-inline-end:calc(2 * var(--hcm-control-height) + var(--hcm-space-1))}` +
	`}`
