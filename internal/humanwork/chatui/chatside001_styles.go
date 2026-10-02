package chatui

// ChatSide001Styles is the sidebar's own sections: the heading's three dots and
// unread total, the name field, the star in the conversation header and the
// highlight a section takes while a row is dragged over it. Every colour is a
// token the stylesheet already uses.
const ChatSide001Styles = `
.section-controls .rail-row-more{inline-size:24px;block-size:24px}
.section-controls:hover .rail-row-more,.section-controls:focus-within .rail-row-more,.section-controls .rail-row-more[aria-expanded="true"]{opacity:1}
@media(hover:none){.section-controls .rail-row-more{opacity:1}}
.section-title .chat-badge.section-unread{margin-inline-start:auto;padding:0 4px;border-radius:0;background:none;color:var(--ink);font-size:.75rem;font-weight:600;font-variant-numeric:tabular-nums}
.section-title .chat-badge.section-unread.mention{color:var(--accent);font-weight:700}
.section-title .chat-badge.section-unread.mention::before{content:"@";font-weight:600}
.section-controls>.section-create-form{flex:1 1 auto;min-inline-size:0;margin:0}
.section-name-error{margin:0;color:var(--hcm-color-danger);font-size:.75rem}
.section-new-prompt{padding:8px 8px 0}
.section-new-prompt .section-create-form{margin:4px 0 4px}
.section-new-title{margin:0;color:var(--muted);font-size:.75rem;font-weight:600}
.chat-favorite-toggle[aria-pressed="true"]{color:var(--accent)}
.chat-favorite-toggle .icon-star-filled path{fill:currentColor}
.chat-rail-row.side-dragging{opacity:.5}
.sidebar-section.side-drop{border-radius:var(--hcm-radius-control);background:color-mix(in srgb,var(--accent) 12%,transparent);outline:2px solid var(--accent);outline-offset:-2px}
.chat-workspace[data-side-dragging]{cursor:grabbing;-webkit-user-select:none;user-select:none}
@media(hover:none){.chat-rail-row{-webkit-touch-callout:none;-webkit-user-select:none;user-select:none}}
` + ChatBug090Styles + ChatBug091Styles + ChatUX032Styles
