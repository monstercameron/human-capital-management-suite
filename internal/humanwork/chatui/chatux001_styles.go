package chatui

// ChatUX001Styles is the conversation header of CHATUX-001: the name as a
// button, the muted purpose line, the count-bearing icon buttons and the compact
// to-do button. It is joined after the other header rules so it wins ties.
const ChatUX001Styles = `
.conversation-name-button{display:block;max-width:100%;min-width:0;margin:0;padding:0;border:0;background:none;color:inherit;font:inherit;text-align:start;cursor:pointer;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;border-radius:var(--hcm-radius-control)}
.conversation-name-button:hover:not(:disabled){text-decoration:underline;text-underline-offset:3px}
.conversation-name-button:disabled{cursor:default}
.conversation-topic .topic-purpose{display:inline}
.conversation-header .icon-button.chatux001-action{width:auto;min-width:34px;padding:0 8px;gap:4px;font:inherit;font-size:.75rem;font-weight:600;color:var(--hcm-color-text-muted)}
.conversation-header .icon-button.chatux001-action:hover{color:var(--hcm-color-text)}
.chatux001-count{font-variant-numeric:tabular-nums;line-height:1}
.conversation-header .channel-todo-trigger .channel-todo-count{display:inline-flex;align-items:center;justify-content:center;min-width:18px;height:18px;padding:0 5px;border-radius:9px;background:var(--hcm-color-brand-soft);color:var(--hcm-color-text);font-size:.6875rem;font-weight:700;line-height:1;font-variant-numeric:tabular-nums}
@container chatmain (max-width:479px){.conversation-header .icon-button.chatux001-pinned,.conversation-header .icon-button.chatux001-members{display:none}}
`
