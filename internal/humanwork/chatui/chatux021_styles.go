package chatui

// ChatUX021Styles is the Conversation details panel's in-place purpose editor,
// the notification radios, the pinned rows with their time and Unpin, the
// membership system line and the person pane's two header buttons.
const ChatUX021Styles = `
.details-purpose{margin-block-start:4px}
.details-purpose-row{display:flex;align-items:baseline;gap:8px;inline-size:100%;min-block-size:34px;padding:6px 8px;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--hcm-color-text);font:inherit;font-size:.8125rem;text-align:start;cursor:pointer}
.details-purpose-row:hover:not(:disabled){background:var(--soft)}
.details-purpose-row:disabled{cursor:default}
.details-purpose-label{flex:none;color:var(--hcm-color-text-muted);font-size:.75rem;font-weight:600}
.details-purpose-value{flex:1;min-inline-size:0;overflow-wrap:anywhere}
.details-purpose-cue{flex:none;display:inline-flex;color:var(--hcm-color-text-muted);opacity:0}
.details-purpose-cue .chat-icon{inline-size:14px;block-size:14px}
.details-purpose-row:hover .details-purpose-cue,.details-purpose-row:focus-visible .details-purpose-cue{opacity:1}
.details-purpose-form{display:flex;flex-direction:column;gap:6px;padding:6px 8px}
.details-purpose-form .details-purpose-label{display:block}
.details-purpose-actions{display:flex;flex-wrap:wrap;align-items:center;gap:6px}
.details-purpose-hint{color:var(--hcm-color-text-muted);font-size:.75rem}
.details-purpose-saved{display:flex;align-items:center;gap:4px;margin:2px 8px 0;color:var(--hcm-color-success);font-size:.75rem;font-weight:600}
.details-purpose-saved .chat-icon{inline-size:14px;block-size:14px}
.details-notify-options{display:grid;gap:2px;padding-block-end:8px}
.details-notify-option{display:flex;align-items:center;gap:8px;min-block-size:36px;padding:6px 8px;border-radius:var(--hcm-radius-control);font-size:.8125rem;cursor:pointer}
.details-notify-option:hover{background:var(--soft)}
.details-notify-option.selected{font-weight:600}
.details-notify-radio{flex:none;margin:0;accent-color:var(--hcm-color-brand-primary)}
.details-notify-text{flex:1;min-inline-size:0}
.details-notify-check{display:inline-flex;color:var(--hcm-color-brand-primary)}
.details-notify-check .chat-icon{inline-size:14px;block-size:14px}
.pinned-row .pinned-link{gap:2px}
.pinned-row .pinned-author{font-size:.75rem}
.pinned-row .pinned-preview{display:block;min-inline-size:0;max-inline-size:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--hcm-color-text)}
.pinned-row .pinned-preview code{padding:0 3px;border-radius:3px;background:var(--soft);font-size:.9em}
.pinned-row .pinned-meta{font-size:.6875rem;color:var(--hcm-color-text-muted)}
.chatux021-system-line{display:flex;flex-wrap:wrap;align-items:baseline;justify-content:center;gap:8px;padding:6px 16px;color:var(--hcm-color-text-muted);font-size:.8125rem;text-align:center}
.chatux021-system-line .message-time{font-size:.6875rem}
.person-pane-head{gap:4px}
.person-pane-head .person-pane-heading{flex:1;min-inline-size:0}
`

// ChatBug048Styles is the one shape of a row in Manage channel: a 12 px label,
// the current value at the right and a chevron, under an 11 px group caption.
const ChatBug048Styles = `
.details-manage .manage-caption{margin:12px 0 2px;padding-inline:8px;font-size:.6875rem;font-weight:600;letter-spacing:.04em;text-transform:uppercase;color:var(--hcm-color-text-muted)}
.details-manage .manage-caption:first-child{margin-block-start:4px}
.details-manage .manage-row,.details-manage .manage-wrapped{border-top:1px solid var(--hcm-color-border)}
.details-manage .manage-row-summary,.details-manage .manage-wrapped>.channel-widget-roster>.chat-disclosure-button,.details-manage .chatfilter-entry .chat-disclosure-button,.details-manage .chatlangadmin-entry .chat-disclosure-button{display:flex;align-items:center;gap:8px;inline-size:100%;min-block-size:36px;margin:0;padding:6px 8px;border:0;border-radius:0;background:transparent;color:var(--hcm-color-text);font:inherit;font-size:.75rem;font-weight:400;text-align:start;cursor:pointer}
.details-manage .manage-row-summary:hover:not(.manage-row-static>.manage-row-summary),.details-manage .manage-wrapped>.channel-widget-roster>.chat-disclosure-button:hover,.details-manage .chatfilter-entry .chat-disclosure-button:hover,.details-manage .chatlangadmin-entry .chat-disclosure-button:hover{background:var(--soft)}
.details-manage .manage-row-static>.manage-row-summary{cursor:default}
.details-manage .manage-row-summary>span:first-child,.details-manage .manage-wrapped>.channel-widget-roster>.chat-disclosure-button>span{flex:1;min-inline-size:0;font-size:.75rem;font-weight:600}
.details-manage .manage-row-value{flex:none;max-inline-size:55%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--hcm-color-text-muted);font-size:.75rem}
.details-manage .manage-row-summary>.chat-icon,.details-manage .chat-disclosure-button>.chat-icon{flex:none;inline-size:14px;block-size:14px}
.details-manage .manage-row-summary[aria-expanded=true]>.chat-icon,.details-manage .chat-disclosure-button[aria-expanded=true]>.chat-icon{transform:rotate(180deg)}
.details-manage .manage-row-body{padding:4px 8px 10px}
.details-manage .manage-note{margin:4px 8px 8px;font-size:.75rem;line-height:1.4;color:var(--hcm-color-text-muted)}
.details-manage .manage-status-block{padding:0}
.details-manage .chatstate-badge{margin:0;color:inherit;font-size:.75rem}
.details-manage .chatstate-form{display:flex;flex-direction:column;gap:6px}
.details-manage .chatstate-form label{margin:6px 0 0;font-size:.75rem;color:var(--hcm-color-text-muted)}
.details-manage .chatstate-form select,.details-manage .chatstate-form input,.details-manage .chatstate-form textarea{inline-size:100%;min-block-size:34px;font-size:.8125rem}
.details-manage .chatstate-effect{margin:0;font-size:.75rem;line-height:1.4;color:var(--hcm-color-text-muted)}
.details-manage .chatstate-effect:empty{display:none}
.details-manage .chatstate-form-error{margin:0;font-size:.75rem;color:var(--hcm-color-danger)}
.details-manage .chatstate-form-error:empty{display:none}
.details-manage .chatstate-form .button{align-self:flex-start}
.details-manage .details-section{padding:0;border:0}
.details-manage .chatfilter-entry,.details-manage .chatlangadmin-entry{border-top:1px solid var(--hcm-color-border)}
`
