package chatui

// issue3854ModerationStyles holds two layout repairs for the Moderation page.
//
// #3856: the muted reset zeroed the side margins of the no-results line and the
// Permissions explanation, so they sat against the edge while the tabs, search
// and cards kept the page's 16px inset. Every child of the page but its heading
// gets the inset again, with logical properties so right-to-left reads the same.
// The doubled class outranks the muted reset; a :root prefix cannot be used
// because this whole sheet sits inside @scope (.chat-workspace), where :root
// never matches (the running app showed the text at 0px inset).
//
// #3854: the permission matrix is five columns. Words are no longer broken into
// characters; the wide table keeps a readable width inside its scroll region,
// and on a phone each role becomes a card with the permission named beside each
// switch (the label comes from the cell's data-label, in the reader's language).
const issue3854ModerationStyles = `
.chatremove.chatmod005-page.chatmod005-page>:not(.chatmod005-heading){margin-inline:16px}
.chatremove .chatmod005-perm-table{min-inline-size:36rem}
.chatremove .chatmod005-perm-table :is(th,td){overflow-wrap:normal;word-break:normal}
@media(max-width:640px){.chatremove .chatmod005-perm-table{min-inline-size:0;display:block}
.chatremove .chatmod005-perm-table thead{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
.chatremove .chatmod005-perm-table tbody{display:grid;gap:12px;padding:8px}
.chatremove .chatmod005-perm-table tr{display:block;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}
.chatremove .chatmod005-perm-table tbody tr:last-child>*{border-block-end:1px solid var(--hcm-color-border)}
.chatremove .chatmod005-perm-table tbody th{display:block;font-size:1rem;background:var(--hcm-color-surface)}
.chatremove .chatmod005-perm-table td{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:4px 12px}
.chatremove .chatmod005-perm-table tbody td:last-child{border-block-end:0}
.chatremove .chatmod005-perm-table td::before{content:attr(data-label);font-weight:600}
.chatremove .chatmod005-perm-table td>.chatmod005-perm-source{grid-column:1/-1;margin:0}}/*end-3854*/
`
