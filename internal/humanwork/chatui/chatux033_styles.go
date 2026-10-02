package chatui

// ChatUX033Styles is Chat on a phone (760 px and narrower).
//
// One bar: the workspace bar above the page is hidden by the shell while a
// conversation is open (productui chatShellStylesheet), so the conversation
// header is the only bar. The conversation list is the screen behind the back
// button: it fills the chat area under the workspace bar instead of sliding
// over it, so the workspace menu, the bell and the account stay reachable from
// the list.
//
// A message is a 28 px avatar and a text column that starts at 48 px (12 px of
// padding, the avatar and an 8 px gap). The composer at rest is one row, with
// the add menu at its start, the field growing in the middle and mention, emoji
// and send at its end; with focus or text it is the field over its tool row.
// Every control a finger lands on is at least 44 px: buttons by their size, text
// targets (names, chips, links, reactions) by an invisible 44 px hit area.
//
// Each composer rule names the three states a composer can be in (any, not
// focused, not focused and empty) because the older rules do, and a rule that
// names fewer loses to them on specificity.
const ChatUX033Styles = `@media(max-width:760px){` +
	`.chat-workspace[data-sidebar-open="true"] .chat-rail{position:absolute;inset:0;width:100%;box-shadow:none;z-index:20}` +
	`.chat-workspace[data-sidebar-open="true"] .chat-scrim{display:none}` +
	`.chat-workspace .message{grid-template-columns:28px minmax(0,1fr);column-gap:8px}` +
	`.chat-workspace .message .avatar{width:28px;height:28px;font-size:.6875rem}` +
	`.chat-workspace .conversation-header .chat-favorite-toggle{display:none}` +
	`.chat-workspace .conversation-header .icon-button{width:44px;height:44px}` +
	`.chat-workspace .chat-composer .composer-input,.chat-workspace .chat-composer:not(:focus-within) .composer-input,.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .composer-input{field-sizing:content}` +
	`.chat-workspace .chat-composer .tool-button,.chat-workspace .chat-composer:not(:focus-within) .tool-button,.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .tool-button{min-width:44px;min-height:44px}` +
	`.chat-workspace .chat-composer .send-button,.chat-workspace .chat-composer:not(:focus-within) .send-button,.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .send-button{min-width:44px;min-height:44px;padding:0 12px}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown){align-items:center;gap:0;padding:4px 6px}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .composer-input{padding-inline:4px}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .composer-toolbar,.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .composer-tools{display:contents}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .composer-add{order:-1}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) :is(.composer-mention-button,.emoji-control){order:1}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .send-button{order:2}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .composer-format-toggle{display:none}` +
	`.chat-workspace :is(.reaction,.message-stats,.mention-chip,.person-avatar-button,.attachment-download,.chat-doc-reference,.agent-reply-source-link,.agent-cite){position:relative}` +
	`.chat-workspace :is(.reaction,.message-stats,.mention-chip,.person-avatar-button,.attachment-download,.chat-doc-reference,.agent-reply-source-link,.agent-cite)::after{content:"";position:absolute;inset:-10px -4px;min-width:44px;min-height:44px}` +
	`.chat-workspace .agent-feedback-button,.chat-workspace .agent-reply-action,.chat-workspace .jump-newest{min-width:44px;min-height:44px}` +
	`.chat-workspace .message-author,.chat-workspace .conversation-name-button{padding-block:10px;margin-block:-10px}` +
	`}`
