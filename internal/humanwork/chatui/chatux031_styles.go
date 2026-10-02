package chatui

// ChatUX031Styles draws Chat preferences as one settings list. A row is 44 px
// with its label first, its current value and its one control at the end; the
// Reading language row holds a select where a switch row holds its switch, and
// a brief tick after a save. The read-only language counts are plain text, the
// six skin tones are one row, and the status line is spoken text unless there
// is something to say (loading, or a failed save).
const ChatUX031Styles = `.chat-workspace .chat-prefs-head{min-block-size:44px}` +
	`.chat-workspace .chat-prefs-title{font-size:.875rem;font-weight:500;color:var(--ink)}` +
	`.chat-workspace .chat-prefs-value{color:var(--muted);font-size:.8125rem}` +
	`.chat-workspace .chatrender-personal .chatrender-settings{display:grid;gap:0}` +
	`.chat-workspace .chatux031-row{display:flex;align-items:center;gap:8px}` +
	`.chat-workspace .chatux031-select{flex:0 1 auto;margin-inline-start:auto;max-inline-size:55%;min-block-size:36px;padding:0 8px;border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-size:.8125rem}` +
	`.chat-workspace .chatux031-tick{flex:none;font-weight:700;color:var(--ink);animation:chatux031-tick 2.4s ease forwards}` +
	`@keyframes chatux031-tick{0%,70%{opacity:1}100%{opacity:0}}` +
	`@media(prefers-reduced-motion:reduce){.chat-workspace .chatux031-tick{animation:none}}` +
	`.chat-workspace .chatux031-status{margin:0;font-size:.75rem;line-height:1.4;color:var(--muted)}` +
	`.chat-workspace .chatux031-status[data-state="saved"]{position:absolute;inline-size:1px;block-size:1px;margin:-1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}` +
	`.chat-workspace .chatux031-status[data-state="error"]{color:var(--hcm-color-danger)}` +
	`.chat-workspace .chatrender-check.chatrender-switch{min-block-size:44px;font-size:.875rem;color:var(--ink)}` +
	`.chat-workspace .chatrender-more-toggle{min-block-size:44px;font-size:.875rem;color:var(--ink)}` +
	`.chat-workspace .chatrender-indicator{display:flex;flex-wrap:wrap;align-items:baseline;gap:2px 8px;padding-block:8px}` +
	`.chat-workspace .chatrender-indicator>h3{margin:0;font-size:.75rem;font-weight:500;color:var(--muted)}` +
	`.chat-workspace .chatrender-indicator .chatrender-language-count{border:0;padding:0;font-size:.8125rem;color:var(--ink)}` +
	`.chat-workspace .chat-prefs-swatches{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:4px}` +
	`.chat-workspace .chat-prefs-swatch{inline-size:100%;min-inline-size:0;block-size:40px}` +
	`@media(pointer:coarse){.chat-workspace .chat-prefs-swatch{block-size:44px}}`
