package chatui

// ChatMsgListStyles holds the message-list fixes of CHATBUG-030, 031, 032 and
// 035. It is joined after ChatPolishStyles so these rules win ties. The sheets
// of the later message fixes are joined to it here, so that the long chain in
// styles.go does not grow by one name for each of them.
const ChatMsgListStyles = `
.attachment-loading{display:none}
.attachment-image:not(.failed):has(>.attachment-image-open img:not([src])) .attachment-loading{display:flex;position:absolute;inset:0;flex-direction:column;align-items:center;justify-content:center;gap:4px;padding:8px;box-sizing:border-box;min-width:0;color:var(--hcm-color-text-muted);font-size:.8125rem;text-align:center;pointer-events:none}
.attachment-loading .chat-icon{flex:none}
.attachment-loading-name{max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--hcm-color-text)}
.attachment-loading-state{font-size:.75rem}
.attachment-fallback-name{max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.75rem}
.attachment-image.failed.measured[data-frame-width],.attachment-image.failed.unmeasured{width:min(100%,240px);height:auto;min-height:148px;aspect-ratio:auto}
.attachment-image.failed .attachment-pending{height:auto;min-height:148px;padding:28px 12px 12px;box-sizing:border-box}
.attachment-image.failed .attachment-badge{inset-block-end:auto;inset-block-start:6px}
.chat-workspace a.chat-embed{display:grid}
.chat-embed-label,.chat-embed-source,.chat-embed-byline{display:block}
.agent-failure-heading{font-size:.9375rem;font-weight:400;line-height:1.45}
` + ChatBug073Styles + ChatBug028Styles + ChatBug071Styles + ChatBug076Styles + ChatUX022Styles + ChatBug081Styles + ChatBug053Styles + ChatBug085Styles
