package chatui

// ChatDetailsStyles finishes the Conversation details panel: the status block
// reads like the panel's other sections, the notification choices and the
// agent rows in the member list use the panel's disclosure rows, and the
// message-filter settings sit inside the panel without a second border.
const ChatDetailsStyles = `
.details-section.chatstate-section{padding:12px 0;color:inherit;background:transparent}
.details-section.chatstate-section .chatstate-badge{margin-block-start:4px;color:var(--ink);font-size:.875rem}
.details-section.chatstate-section .chatstate-history{margin:8px 0 0;gap:4px 8px;font-size:.8125rem}
.details-section.chatstate-section .chatstate-history dt{color:var(--muted)}
.details-about .chatstate-history{display:block;margin:4px 0 0;font-size:.8125rem}
.details-about .chatstate-history-row{display:flex;align-items:baseline;gap:8px;margin-block-start:4px}
.details-about .chatstate-history dt{flex:none;color:var(--muted)}
.details-about .chatstate-history dd{flex:1;min-inline-size:0;margin:0;overflow-wrap:anywhere}
.details-section.chatstate-section .chat-disclosure-button{min-block-size:34px;padding:6px 8px;border:0;background:transparent}
.details-section.chatstate-section .chat-disclosure-button:hover{background:var(--soft)}
.details-notify-options{display:grid;gap:4px;padding-block-end:8px}
.details-notify-option{justify-content:flex-start;inline-size:100%;text-align:start}
.details-notify-option[aria-pressed=true]{border-color:var(--accent);color:var(--accent);font-weight:600}
.persona-member-row>[data-chat-disclosure]{flex:1;min-width:0}
.persona-member-label{gap:8px}
.persona-member-name{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:600}
.persona-member-label .agent-badge{flex:none}
.chatfilter-entry .chat-disclosure-button{padding-inline:0}
.chatfilter-entry .chatfilter-panel{border:0;padding:0;background:transparent}
.chatfilter-entry .chatfilter-panel>h3,.chatfilter-entry .chatfilter-panel>p:nth-of-type(2){display:none}
`
