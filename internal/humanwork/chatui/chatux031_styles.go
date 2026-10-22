package chatui

// ChatUX031Styles draws Chat preferences as one settings list. Every row is the
// same component: at least 44 px tall with a 12 px inline padding, its label first (14 px,
// weight 500), its value or its one control at the end, and a hairline between
// rows. Help text sits under its row at 12 px, read-only facts are 13 px text,
// the saved tick is inline at the end of the select's row and fades after two
// seconds, and More options is a disclosure row. The popover respects the
// sidebar placement width down to its 220 px floor; row contents wrap when the
// narrower rail cannot hold them on one line. It scrolls inside itself only
// when the viewport is shorter.
// The last rules are CHATUX-039: sources a message copies as text are chips,
// a linked-document card is neutral (the accent rule means an agent answer) and
// keeps a floor width so a narrow message column cannot squeeze it to letters.
const ChatUX031Styles = `.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"]{padding:0;min-inline-size:min(220px,calc(100vw - 16px));overflow-x:hidden;overflow-y:auto;overscroll-behavior:contain;container-type:inline-size}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] .chat-prefs-section{display:grid;gap:0;padding:0}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] .chat-prefs-section+.chat-prefs-section,.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] .chatrender-personal-host>.chat-prefs-section,.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] .chat-prefs-section+.chatrender-personal-host>.chat-prefs-section,.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] .chatrender-personal-host+.chat-prefs-section{padding-block-start:0}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"]>.chat-prefs-section:not(:first-child),.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"]>.chatrender-personal-host:not(:first-child)>.chat-prefs-section{border-block-start:1px solid var(--line)}` +
	`.chat-workspace .chat-prefs-head{display:flex;flex-wrap:wrap;align-items:center;gap:8px;box-sizing:border-box;min-block-size:44px;padding:0 12px}` +
	`.chat-workspace .chat-prefs-title{flex:1 1 0;min-inline-size:0;margin:0;font-size:.875rem;font-weight:500;line-height:20px;color:var(--ink);white-space:normal;overflow-wrap:anywhere}` +
	`.chat-workspace .chat-prefs-value{flex:0 1 auto;max-inline-size:55%;color:var(--muted);font-size:.875rem;font-weight:400;line-height:20px;text-align:end;overflow-wrap:anywhere}` +
	`.chat-workspace .chat-prefs-head>.switch{flex:none;margin:0}` +
	`.chat-workspace .chatux002-layer .rail-prefs-body{display:grid;gap:8px;padding:0 12px 12px}` +
	`.chat-workspace .chat-prefs-note,.chat-workspace .chatux031-status{margin:0;font-size:.75rem;line-height:16px;color:var(--muted)}` +
	`.chat-workspace .chatux031-status{padding:0 12px 8px}` +
	`.chat-workspace .chatux031-status[data-state="saved"]{position:absolute;inline-size:1px;block-size:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}` +
	`.chat-workspace .chatux031-status[data-state="error"]{color:var(--hcm-color-danger)}` +
	`.chat-workspace .chat-prefs-section>p[role="status"]{margin:0;padding:0 12px 12px;font-size:.8125rem;line-height:20px;color:var(--muted)}` +
	`.chat-workspace .chatrender-personal .chatrender-settings{display:grid;gap:0;padding:0;background:none;border-radius:0}` +
	`.chat-workspace .chatrender-personal .chatrender-settings select.chatux031-select{inline-size:auto;min-block-size:36px;block-size:36px}` +
	`.chat-workspace .chatux031-row{gap:8px}` +
	`.chat-workspace .chatux031-select{flex:0 1 auto;margin-inline-start:auto;max-inline-size:60%;min-inline-size:0;box-sizing:border-box;padding:0 8px;border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-size:.875rem}` +
	`@container(max-width:250px){.chat-workspace .chat-prefs-head .chat-prefs-title{flex:1 0 100%}.chat-workspace .chat-prefs-head>.chatux031-select{margin-inline-start:0}}` +
	`.chat-workspace .chatux031-end{flex:none;display:inline-flex;align-items:center;justify-content:center;inline-size:16px}` +
	`.chat-workspace .chatux031-tick{font-size:.875rem;font-weight:700;line-height:1;color:var(--ink);animation:chatux031-tick calc(var(--hcm-motion-slow)*8) ease forwards}` +
	`@keyframes chatux031-tick{0%,83%{opacity:1}100%{opacity:0}}` +
	`@media(prefers-reduced-motion:reduce){.chat-workspace .chatux031-tick{animation:none}}` +
	`.chat-workspace .chatrender-check.chatrender-switch{box-sizing:border-box;min-block-size:44px;padding:0 12px;gap:8px;border-block-start:1px solid var(--line);font-size:.875rem;font-weight:500;line-height:20px;color:var(--ink)}` +
	`.chat-workspace .chatrender-check.chatrender-switch>span{flex:1 1 0;min-inline-size:0}` +
	`.chat-workspace .chatrender-more{display:grid;gap:0;border-block-start:1px solid var(--line)}` +
	`.chat-workspace .chatrender-more-toggle{display:flex;align-items:center;justify-content:space-between;gap:8px;inline-size:100%;box-sizing:border-box;min-block-size:44px;padding:0 12px;border:0;border-radius:0;background:transparent;color:var(--ink);font-size:.875rem;font-weight:500;line-height:20px;text-align:start;cursor:pointer}` +
	`.chat-workspace .chatrender-more-toggle:hover{background:var(--soft)}` +
	`.chat-workspace .chatrender-more-toggle>.chat-icon{flex:none;color:var(--muted)}` +
	`.chat-workspace .chatrender-more-body{display:grid;gap:12px;padding:4px 12px 12px}` +
	`.chat-workspace .chatrender-chips>legend{margin-block-end:6px;font-size:.8125rem;color:var(--muted)}` +
	`.chat-workspace .chatrender-indicator{display:grid;gap:4px;padding:12px;border-block-start:1px solid var(--line);font-size:.8125rem;line-height:20px}` +
	`.chat-workspace .chatrender-indicator>h3{margin:0;font-size:.875rem;font-weight:500;line-height:20px;color:var(--ink)}` +
	`.chat-workspace .chatrender-indicator>p,.chat-workspace .chatrender-indicator .chatrender-language-count{margin:0;border:0;padding:0;font-size:.8125rem;font-weight:400;color:var(--muted)}` +
	`.chat-workspace .chat-prefs-swatches{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:4px;padding:0 12px 12px}` +
	`.chat-workspace .chat-prefs-swatch{inline-size:100%;min-inline-size:0;block-size:40px}` +
	`@media(pointer:coarse){.chat-workspace .chat-prefs-swatch{block-size:44px}}` +
	`.chat-workspace .message-body a:not([class]){display:inline;min-width:0;min-height:0}` +
	`.chat-workspace .md-source-chips{display:flex;flex-wrap:wrap;gap:6px;margin:6px 0 0;padding:0;list-style:none;min-width:0}` +
	`.chat-workspace .md-source-chips>.agent-reply-source{display:inline-flex;align-items:center;gap:6px;max-inline-size:100%;min-inline-size:0;margin:0;padding:2px 10px;border:1px solid var(--line);border-radius:999px;background:var(--surface);font-size:.8125rem;line-height:20px}` +
	`.chat-workspace .md-source-chips .chat-icon{inline-size:14px;block-size:14px;flex:none;color:var(--muted)}` +
	`.chat-workspace .md-source-chips a{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink);text-decoration:none}` +
	`.chat-workspace .md-source-chips a:hover{text-decoration:underline}` +
	`.chat-workspace .chat-doc-embed,.chat-workspace .chat-doc-embed-wrap>.chat-embed-more{border-inline-start:1px solid var(--line);background:var(--surface)}` +
	`.chat-workspace :is(.chat-doc-embed-wrap,.chat-doc-embed){min-inline-size:min(240px,calc(100vw - 88px))}`
