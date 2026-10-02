package chatui

// ChatUX036Styles: the accent marks the one primary action of a surface and the
// selected state. Links in running text and secondary buttons use the ink
// colour (links underlined, buttons with a neutral outline); the selected
// sidebar row is a light tint with its bar; an off switch is a neutral grey
// track and an on switch uses the accent; help text is at least 12 px in the
// muted token (about 7:1 on the panel and 6.5:1 on the rail in the light mode);
// a status is a small pill with a dot (.chat-status-pill, shared with the
// details panel). Only tokens are used, so the dark mode follows the shell.
const ChatUX036Styles = `.chat-workspace .message-body a,.chat-workspace .agent-reply-answer a{color:var(--ink);text-decoration:underline;text-underline-offset:2px}` +
	`.chat-workspace :is(.agent-reply-source-link,.agent-cite){color:var(--ink)}` +
	`.chat-workspace .button.secondary{color:var(--ink);border-color:var(--hcm-color-control-border);background:var(--surface)}` +
	`.chat-workspace .agent-feedback-button:hover,.chat-workspace .agent-feedback-button:focus-visible{border-color:var(--ink);color:var(--ink)}` +
	`.chat-workspace .agent-feedback-button[aria-pressed="true"]:hover{color:var(--on-brand)}` +
	`.chat-workspace .chatsave-seg-button[aria-selected="true"]{color:var(--ink);box-shadow:inset 0 -2px 0 var(--accent)}` +
	`.chat-workspace .chat-row.selected,.chat-workspace .chat-row.selected:hover{background:color-mix(in srgb,var(--accent) 12%,var(--canvas));color:var(--ink);font-weight:600}` +
	`.chat-workspace .chat-row.selected .chat-badge{background:var(--accent);color:var(--on-brand)}` +
	`:root .chat-workspace :is(.switch,.chatmod-switch-track){border-color:var(--hcm-color-control-border);background:var(--hcm-color-control-border)}` +
	`:root .chat-workspace :is(.switch,.chatmod-switch-track)::after{background:var(--hcm-color-surface)}` +
	`:root .chat-workspace :is(.switch:checked,.chatmod-switch[aria-checked="true"] .chatmod-switch-track){border-color:var(--accent);background:var(--accent)}` +
	`:root[data-hcm-color-mode="dark"] .chat-workspace input.switch:not(:checked){border-color:var(--hcm-color-control-border);background:var(--hcm-color-control-border)}` +
	`@media(prefers-color-scheme:dark){:root[data-hcm-color-mode="system"] .chat-workspace input.switch:not(:checked){border-color:var(--hcm-color-control-border);background:var(--hcm-color-control-border)}}` +
	`.chat-workspace .message-time,.chat-workspace .menu-section-title,.chat-workspace .menu-section-hint,.chat-workspace .chatsave-note-hint,.chat-workspace .format-kind,.chat-workspace .pinned-meta,.chat-workspace .gutter-time{font-size:.75rem}` +
	`.chat-workspace .chat-status-pill{display:inline-flex;align-items:center;gap:6px;padding:1px 8px;border:1px solid var(--hcm-color-border);border-radius:999px;background:var(--surface);color:var(--ink);font-size:.75rem;font-weight:600;line-height:1.5}` +
	`.chat-workspace .chat-status-pill::before{content:"";inline-size:8px;block-size:8px;border-radius:50%;background:var(--hcm-color-success)}`
