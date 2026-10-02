package chatui

// ChatlangStyles: the mark, the original beneath a translation, the
// conversation switch, the writer's language picker and the composer's line
// (CHATLANG-004). A message with a known language carries its own direction, so
// the page's plaintext rule gives way to it, and names, links, mentions and
// code keep their own order inside right-to-left text.
const ChatlangStyles = `.message-body[dir=rtl],.message-body[dir=ltr]{unicode-bidi:isolate}` +
	`.message-body :is(a,time,.mention-chip,.mention-chip-details){unicode-bidi:isolate}` +
	`.message-body :is(code,pre){direction:ltr;unicode-bidi:isolate;text-align:left}` +
	`.chatlang-mark{display:flex;flex-wrap:wrap;align-items:center;gap:0 10px;margin:2px 0 0;font-size:.75rem;line-height:1.5;color:var(--muted);min-block-size:1.5em}` +
	`.chatlang-mark-text{overflow-wrap:anywhere}` +
	`.chatlang-mark-button,.chatlang-bar-button,.chatlang-fix-option{position:relative;font:inherit;font-size:inherit;font-weight:600;color:var(--accent);background:none;border:1px solid transparent;border-radius:var(--hcm-radius-control);padding:0 4px;margin-inline-start:-4px;cursor:pointer;min-block-size:1.5em}` +
	`.chatlang-mark-button:hover,.chatlang-bar-button:hover,.chatlang-fix-option:hover{background:var(--soft)}` +
	`.chatlang-mark-button:focus-visible,.chatlang-bar-button:focus-visible,.chatlang-fix-option:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chatlang-mark-button:disabled,.chatlang-fix-option:disabled{opacity:.6;cursor:default}` +
	`.chatlang-original{margin:4px 0 0;padding-inline-start:10px;border-inline-start:2px solid var(--line);max-inline-size:90ch}` +
	`.chatlang-original-label{display:block;font-size:.75rem;font-weight:600;color:var(--muted)}` +
	`.chatlang-original-text{font-size:.9375rem;line-height:1.45;color:var(--muted);white-space:pre-wrap;overflow-wrap:anywhere;unicode-bidi:isolate;text-align:start}` +
	`.chatlang-original-text[dir=auto]{unicode-bidi:plaintext}` +
	`.chatlang-original-text :is(code,pre){direction:ltr;unicode-bidi:isolate;text-align:left}` +
	`.chatlang-bar{display:flex;flex-wrap:wrap;align-items:center;gap:4px 12px;padding:6px 16px;border-block-end:1px solid var(--line);background:var(--soft);font-size:.8125rem;color:var(--muted)}` +
	`.chatlang-bar-text{flex:1 1 auto;min-inline-size:0;overflow-wrap:anywhere}` +
	`.chatlang-bar-button{flex:none;margin-inline-start:0;padding:2px 10px;border-color:var(--line);background:var(--surface)}` +
	`.chatlang-bar-button[aria-pressed=true]{border-color:var(--accent);color:var(--ink);background:var(--surface);box-shadow:inset 0 0 0 1px var(--accent)}` +
	`.chatlang-fix{display:grid;gap:6px;margin:6px 0 0;padding:8px 10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--soft);font-size:.8125rem}` +
	`.chatlang-fix-label{font-weight:600;color:var(--ink)}` +
	`.chatlang-fix-options{display:flex;flex-wrap:wrap;gap:4px 6px}` +
	`.chatlang-fix-option{margin-inline-start:0;padding:2px 10px;border-color:var(--line);background:var(--surface)}` +
	`.chatlang-fix-option[aria-pressed=true]{border-color:var(--accent);color:var(--ink);box-shadow:inset 0 0 0 1px var(--accent)}` +
	`.chatlang-fix-status{margin:0;color:var(--muted)}.chatlang-fix-status[role=alert]{color:var(--hcm-color-danger)}` +
	`.chatlang-audience{margin:2px 0 0;padding-inline:2px;font-size:.75rem;line-height:1.4;color:var(--muted);overflow-wrap:anywhere}` +
	`@media(hover:none),(max-width:760px){.chatlang-mark-button::after,.chatlang-fix-option::after{content:"";position:absolute;inset:-12px -4px}.chatlang-bar-button{min-block-size:36px}.chatlang-fix-option{min-block-size:36px}}` +
	`@media(prefers-reduced-motion:reduce){.chatlang-bar *,.chatlang-mark *{transition:none;animation:none}}`
