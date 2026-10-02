package chatui

// ChatUX008Styles finishes the thread pane header and the parent message: the
// channel link under "Thread", the bell toggle, and the reply count as the
// divider between the parent and the replies.
const ChatUX008Styles = `
.thread-heading{height:auto;min-height:56px;padding-block:6px}
.thread-heading-text{display:flex;flex-direction:column;min-width:0;gap:1px}
.thread-heading-text h2{line-height:1.2}
.thread-channel-link{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted);font-size:.8125rem;text-decoration:none}
a.thread-channel-link:hover,a.thread-channel-link:focus-visible{color:var(--accent);text-decoration:underline}
.thread-notify[aria-pressed="true"] .chat-icon{fill:currentColor}
.thread-root{margin-block-start:8px;padding:12px;border-radius:var(--hcm-radius-control);background:var(--soft)}
.thread-count{justify-content:center;margin:14px 0 8px;color:var(--ink);font-size:.8125rem}
.thread-count::before{content:"";flex:1;border-top:1px solid var(--line)}
`
