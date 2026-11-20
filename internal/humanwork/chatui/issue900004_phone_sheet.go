package chatui

// Issue900004Styles repairs two phone layouts (#900004).
//
// Thread panel: the narrow bar rules give each reply's Save for later and More
// buttons 44px targets in flow, but an earlier phone rule still pins the bar
// itself to height:24px, so the buttons spilled 20px out of the bar and sat on
// the next reply's author row. The bar now sizes to its buttons, and the root
// message's header is a wrapping row: the name keeps its own width and the date
// and View in channel move to the next line when they do not fit, instead of
// squeezing the name into two lines. Logical properties only, so it holds in
// right-to-left.
//
// Poll and to-do preview: the settings scroll inside the preview above the
// Post and Cancel row, which never scrolls away. The scrolling area keeps a
// minimum height, so a short screen (320x640) with a draft and the toolbar
// below still shows a working window onto the settings, can be swiped, and
// leaves room under the last setting. The preview may use nearly the whole
// screen, and Post and Cancel clear the home indicator and are 44px targets.
const Issue900004Styles = `@media(max-width:767px){` +
	`.chat-workspace .thread-pane .thread-message-actions{height:auto;min-block-size:44px;max-block-size:none;overflow:visible;padding:0;inset-block-start:auto;inset-inline-end:auto}` +
	`.chat-workspace .thread-pane .thread-message-body,.chat-workspace .thread-pane .thread-root-body{display:flow-root}` +
	`.chat-workspace .thread-pane .thread-message-actions .message-action,.chat-workspace .thread-pane .thread-message-actions .thread-more{height:auto;position:static;flex:none}` +
	`.chat-workspace .thread-pane .thread-root-body .message-meta,.chat-workspace .thread-pane .thread-message-body .message-meta{display:flex;flex-wrap:wrap;align-items:baseline;gap:2px 8px;padding-inline-end:0}` +
	`.chat-workspace .thread-pane .message-meta .message-author{flex:0 1 auto;min-inline-size:0;max-inline-size:100%}` +
	`.chat-workspace .thread-pane .message-meta .message-time,.chat-workspace .thread-pane .message-meta .thread-view-in-channel{position:static;flex:0 0 auto;margin:0;white-space:nowrap}` +
	`.chat-workspace form.chat-composer.chat-composer:has(.chatcmd003-preview){max-block-size:min(94dvh,calc(100dvh - 56px))}` +
	`.chat-workspace form.chat-composer.chat-composer .chatcmd003-slot>.chatcmd003-preview>.chatcmd003-body{min-block-size:min(9rem,32dvh);padding-block-end:16px;scroll-padding-block-end:16px;touch-action:pan-y;-webkit-overflow-scrolling:touch}` +
	`.chat-workspace form.chat-composer.chat-composer .chatcmd003-slot>.chatcmd003-preview>.chatcmd003-actions{padding-block-end:max(12px,env(safe-area-inset-bottom))}` +
	`.chat-workspace .chatcmd003-actions .button{min-block-size:44px;min-inline-size:44px}}`
