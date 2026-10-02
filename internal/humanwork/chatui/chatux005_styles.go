package chatui

// ChatUX005Styles finishes the reordered Conversation details panel: the agent
// rows with their description and Ask button, the Agents and People subheadings,
// the About lines and the collapsed Manage channel group.
const ChatUX005Styles = `
.details-subheading{margin:12px 0 2px;font-size:.75rem;font-weight:600;color:var(--muted)}
#chat-agents-here:focus-visible,#chat-details-members:focus-visible,#chat-details-pinned:focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:var(--hcm-radius-control)}
.persona-member-row{flex-wrap:wrap}
.persona-member-ask{flex:none}
.persona-member-purpose{flex:0 0 100%;min-width:0;margin:0;color:var(--muted);font-size:.8125rem;line-height:1.4;overflow:hidden;overflow-wrap:anywhere;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;line-clamp:2}
.details-agents-manage{display:inline-block;margin-block:2px 4px;font-size:.8125rem}
.details-about-created{margin:6px 0 0;font-size:.8125rem}
.details-about-status{display:flex;align-items:center;gap:8px;margin-block-start:6px;font-size:.8125rem}
.details-manage{padding:0}
.details-group-toggle{font-size:.8125rem;font-weight:600;color:var(--muted);padding-block:12px}
.details-group-body{padding-block:0 8px}
.details-group-body[hidden]{display:none}
.details-group-body>.details-section:first-child{border-top:0}
.details-group-body .details-group{border-top:1px solid var(--line)}
.details-group-body .details-group-toggle{padding-block:8px}
`
