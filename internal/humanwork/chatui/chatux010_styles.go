package chatui

// ChatUX010Styles is the shortcut label drawn inside the sidebar search box.
const ChatUX010Styles = `
.rail-search .chat-search-shortcut{position:absolute;inset-inline-end:22px;top:calc(50% - 4px);transform:translateY(-50%);padding:1px 6px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-canvas);color:var(--hcm-color-text-muted);font:inherit;font-size:.6875rem;line-height:1.4;pointer-events:none;white-space:nowrap;unicode-bidi:isolate}
.rail-search:has(.chat-search-shortcut) .chat-search{padding-inline-end:64px}
`
