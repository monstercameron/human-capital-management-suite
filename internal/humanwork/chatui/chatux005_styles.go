package chatui

// ChatUX005Styles finishes the reordered Conversation details panel: the agent
// rows with their description and Ask button, the Agents and People subheadings,
// the About lines and the collapsed Manage channel group.
const ChatUX005Styles = `
.details-subheading{margin:12px 0 2px;font-size:.75rem;font-weight:600;color:var(--muted)}
#chat-agents-here:focus-visible,#chat-details-members:focus-visible,#chat-details-pinned:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px;border-radius:var(--hcm-radius-control)}
.persona-member-row{display:flex;flex-wrap:wrap;align-items:center;column-gap:8px;row-gap:2px}
.agent-list>.persona-member-row+.persona-member-row,.agent-list>.member-row+.member-row{margin-block-start:12px}
.persona-member-ask{flex:none}
.chat-details .persona-member-ask{block-size:28px;min-block-size:28px;padding-inline:12px;font-size:.75rem}
.persona-member-purpose{flex:0 0 100%;min-width:0;margin:0;color:var(--muted);font-size:.75rem;line-height:1.4;overflow:hidden;overflow-wrap:anywhere;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:3;line-clamp:3}
.persona-member-reads{flex:0 0 100%;min-width:0;margin:0;color:var(--muted);font-size:.75rem;line-height:1.4;overflow-wrap:anywhere}
.details-agents-manage{display:inline-block;margin-block:8px 0;font-size:.8125rem;color:var(--accent);text-decoration:none}
.details-agents-manage:hover{text-decoration:underline}
.details-about-created{margin:6px 0 0;font-size:.8125rem}
.details-about-status{display:flex;align-items:center;gap:8px;margin-block-start:6px;font-size:.8125rem}
.details-manage{padding:0}
.details-group-toggle{font-size:.8125rem;font-weight:600;color:var(--muted);padding-block:12px}
.details-group-body{padding-block:0 8px}
.details-group-body[hidden]{display:none}
.details-group-body>.details-section:first-child{border-top:0}
.details-group-body .details-group{border-top:1px solid var(--line)}
.details-group-body .details-group-toggle{padding-block:8px}
.chat-details .agent-summary{display:flex;flex-direction:column;gap:8px;margin-inline-start:8px;padding:4px 0 8px;padding-inline-start:12px;border-inline-start:1px solid var(--hcm-color-border);font-size:.8125rem;line-height:1.45}
.chat-details .agent-summary :is(p,ul,ol,li,span,a,h4,h5,strong,button){font-size:.8125rem;line-height:1.45}
.chat-details .agent-summary :is(p,ul){margin:0}
.chat-details .agent-summary ul{padding-inline-start:16px;list-style:disc}
.chat-details .agent-summary :is(h4,h5,.agent-summary-label){margin:0 0 2px;font-size:.75rem;font-weight:500;color:var(--hcm-color-text-muted)}
`
