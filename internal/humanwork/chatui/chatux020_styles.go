package chatui

// ChatUX020Styles is the sidebar's fixed Saved and Moderation rows, the opaque
// section heading, the draft pencil on a row and the row menu's headed group.
const ChatUX020Styles = `
.rail-fixed{flex:none;padding:0 8px}
.rail-fixed:empty{display:none}
.rail-scroll>.sidebar-section>.section-controls{margin:0;padding-block:14px 4px;background:var(--canvas)}
.chat-row-draft{flex:none;display:inline-flex;align-items:center;color:var(--hcm-color-text-muted)}
.chat-row-draft .chat-icon{inline-size:13px;block-size:13px}
.rail-row-menu .menu-group{display:flex;flex-direction:column;border-block:1px solid var(--hcm-color-border);margin-block:4px;padding-block:2px}
.rail-row-menu .menu-heading{padding:6px 12px 2px;font-size:.6875rem;font-weight:600;letter-spacing:.04em;text-transform:uppercase;color:var(--hcm-color-text-muted)}
.rail-row-menu .menu-item.danger{color:var(--hcm-color-danger)}
.rail-row-menu .menu-reorder{display:flex;flex-direction:column}
`
