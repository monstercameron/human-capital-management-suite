package chatui

// Issue3680Styles gives the conversation list the phone's side gutter while the
// workspace is still loading (#3680). With no conversation chosen the list is
// the screen (data-has-selection="false"), but the slide-in rule for the list
// drawer only knew the drawer being open, so the loading list sat 8px off the
// screen edge, with its search box and rows flush against it. The list is not a
// drawer in that state: it takes no offset, and its search box and placeholder
// rows keep 16px at the start and the end, as the rest of the phone layout does.
const Issue3680Styles = `@media(max-width:760px) and (prefers-reduced-motion:no-preference){` +
	`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace[data-has-selection="false"] .chat-rail{transform:none}` +
	`}` +
	`@media(max-width:760px){` +
	`.chat-workspace .chat-rail .rail-search{padding-inline:16px}` +
	`.chat-workspace .chat-rail .rail-search .chat-icon{inset-inline-start:26px}` +
	`.chat-workspace .chat-rail .rail-search .chat-search-shortcut{inset-inline-end:26px}` +
	`.chat-workspace[dir="rtl"] .chat-rail .rail-search .chat-search-shortcut{inset-inline-end:auto;left:26px;right:auto}` +
	`.chat-workspace .chat-rail .rail-skeleton{padding-inline:16px}` +
	`}`
