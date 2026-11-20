package chatui

// Issue3891Styles keeps Save for later and More actions in a thread's root and
// replies reachable at phone width. The narrow rules before it hide those
// controls with display:none and visibility:hidden, which takes them out of the
// tab order and the accessibility tree; a keyboard user has no long-press. At
// phone width they now sit in flow under the message, always visible, as
// 44px targets. Wider layouts keep the hover and focus bar.
const Issue3891Styles = `@media(max-width:767px){` +
	`.chat-workspace .thread-pane .thread-message-actions{display:flex;visibility:visible;pointer-events:auto;position:static;transform:none;justify-content:flex-start;gap:4px;max-width:none;margin-block-start:4px;opacity:1}` +
	`.chat-workspace .thread-pane .thread-message-actions .message-action,.chat-workspace .thread-pane .thread-message-actions .thread-more{display:inline-flex;align-items:center;justify-content:center;min-width:44px;min-height:44px;opacity:1}` +
	`.chat-workspace .thread-pane .thread-message-actions .message-action[hidden]{display:none}}`
