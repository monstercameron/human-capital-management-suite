package chatui

// ChatUX032Styles is the shared look of the panels beside the conversation: one
// 52 px header with a 1rem title and an inline subtitle, one 32 px close button
// whose focus ring shows for keyboard focus only, and one disabled look for
// primary buttons whose text keeps at least 3:1 against its background.
const ChatUX032Styles = `
.chat-workspace .side-heading.chat-panel-head{display:flex;align-items:center;justify-content:space-between;gap:8px;height:52px;min-height:52px;box-sizing:border-box;padding-block:0;margin-block:0;border-block-start:0}
.chat-panel-head .chat-panel-title,.chat-panel-head .thread-heading-text{display:flex;flex-direction:row;align-items:baseline;gap:8px;flex:1 1 auto;min-width:0}
.chat-panel-head h2{flex:0 1 auto;min-width:0;margin:0;font-size:1rem;font-weight:600;line-height:1.25;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.chat-panel-head h2:focus{outline:none}
.chat-panel-head.person-pane-head h2{flex:1 1 auto}
.chat-panel-head .chat-panel-sub,.chat-panel-head .thread-channel-link{flex:0 1 auto;min-width:0;margin:0;font-size:.8125rem;font-weight:400;line-height:1.25;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.chat-panel-head .chat-panel-sub{color:var(--muted);font-variant-numeric:tabular-nums}
.chat-panel-head .side-heading-actions{flex:none;display:flex;align-items:center;gap:4px}
.chat-panel-head .chat-panel-close.chat-panel-close.icon-button{box-sizing:border-box;inline-size:32px;min-inline-size:32px;block-size:32px;min-block-size:32px;padding:0;border:0;border-radius:var(--hcm-radius-control);background:transparent;box-shadow:none;color:var(--muted);display:inline-flex;align-items:center;justify-content:center}
.chat-workspace .chat-panel-close:hover:not(:disabled){background:var(--soft);color:var(--ink)}
.chat-workspace .chat-panel-close:focus:not(:focus-visible){outline:none;box-shadow:none}
.chat-workspace .chat-panel-close:focus-visible{outline:2px solid var(--accent);outline-offset:1px}
.chat-panel-close .chat-icon{inline-size:18px;block-size:18px}
@media(pointer:coarse){.chat-panel-head .chat-panel-close.chat-panel-close.icon-button{inline-size:44px;min-inline-size:44px;block-size:44px;min-block-size:44px}}
@media(max-width:767px){.chat-panel-head .chat-panel-close.chat-panel-close.icon-button.thread-back{inline-size:auto;padding-inline:10px}}
.chat-workspace .side-heading.chatsave-header{position:static;margin:0;padding-inline:0;border:0}
.chatmod005-heading.chat-panel-head{position:static;margin-inline:0;padding-inline:16px}
.chat-workspace .chat-composer:has(.composer-input:placeholder-shown) .send-button,.chat-workspace .thread-composer:has(.composer-input:placeholder-shown) .send-button,.chat-workspace .send-button:disabled,.chat-workspace .button:not(.secondary):disabled,.chat-workspace .button:not(.secondary)[aria-disabled="true"]{background:var(--soft);border-color:var(--line);color:var(--muted);opacity:1;cursor:not-allowed;filter:none}
`
