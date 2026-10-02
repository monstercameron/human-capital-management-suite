package chatui

// ChatMod003Styles styles the administrator's filter panel: the built-in lists
// with their switches, the filters of a channel, the editor and its Try box. It
// uses only the --hcm-* tokens, no style element and no style attribute. The
// panel is laid out in flowing rows, so it holds from a wide column down to a
// 390 px screen, and the switch shows its state in words as well as in colour.
const ChatMod003Styles = `.chatmod{display:grid;gap:16px;min-width:0}` +
	`.chatmod [hidden]{display:none!important}` +
	`.chatmod h4,.chatmod h5{margin:0;overflow-wrap:anywhere}.chatmod h4{font-size:1rem}.chatmod h5{font-size:.875rem;color:var(--hcm-color-text-muted)}` +
	`.chatmod-intro,.chatmod-hint,.chatmod-state,.chatmod-desc{margin:0;font-size:.8125rem;color:var(--hcm-color-text-muted);overflow-wrap:anywhere}` +
	`.chatmod-status{margin:0;font-size:.875rem;overflow-wrap:anywhere}.chatmod-status:not(:empty){padding:8px 12px;border-inline-start:3px solid var(--hcm-color-brand-primary);border-radius:var(--hcm-radius-control);background:var(--hcm-color-brand-soft)}` +
	`.chatmod-status[data-tone="error"]:not(:empty){border-inline-start-color:var(--hcm-color-danger);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-danger)}` +
	`.chatmod-section,.chatmod-group{display:grid;gap:8px;min-width:0}` +
	`.chatmod-list{list-style:none;margin:0;padding:0;display:grid;gap:4px}` +
	`.chatmod-row{display:grid;gap:6px;padding:8px 0;border-block-end:1px solid var(--hcm-color-border);min-width:0}.chatmod-row:last-child{border-block-end:0}` +
	`.chatmod-row-main{display:flex;flex-wrap:wrap;align-items:center;gap:6px 12px;min-width:0}` +
	`.chatmod-name{flex:1 1 140px;font-weight:600;overflow-wrap:anywhere}` +
	`.chatmod-row-more{display:flex;flex-wrap:wrap;align-items:center;gap:6px 12px}` +
	`.chatfilter-panel .chatmod-switch{display:inline-flex;align-items:center;gap:8px;min-width:44px;min-height:44px;padding:0 4px;border:0;background:transparent;color:inherit;cursor:pointer}` +
	`.chatmod-switch-track{position:relative;flex:none;width:40px;height:24px;border:2px solid var(--hcm-color-text-muted);border-radius:999px;background:var(--hcm-color-surface)}` +
	`.chatmod-switch-track::after{content:"";position:absolute;top:2px;inset-inline-start:2px;width:16px;height:16px;border-radius:50%;background:var(--hcm-color-text-muted)}` +
	`.chatmod-switch[aria-checked="true"] .chatmod-switch-track{border-color:var(--hcm-color-brand-primary);background:var(--hcm-color-brand-primary)}` +
	`.chatmod-switch[aria-checked="true"] .chatmod-switch-track::after{inset-inline-start:18px;background:var(--hcm-color-on-brand)}` +
	`.chatmod-switch-text{min-width:2.5em;font-weight:700;font-size:.875rem}` +
	`.chatmod-switch[disabled]{opacity:.6;cursor:default}` +
	`.chatfilter-panel .chatmod-switch:focus-visible .chatmod-switch-track{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chatmod-field{display:grid;gap:4px;min-width:0}.chatmod-field label,.chatmod-field legend,.chatmod-label{font-weight:600;font-size:.875rem}` +
	`.chatmod-field fieldset{margin:0;padding:0;border:0;min-width:0}` +
	`.chatfilter-panel .chatmod-link{min-height:44px;border:0;background:transparent;color:var(--hcm-color-brand-primary);text-decoration:underline;padding:0 4px;cursor:pointer}` +
	`.chatfilter-panel .chatmod-select{width:auto;max-width:100%}` +
	`.chatmod-editor{display:grid;gap:12px;min-width:0}` +
	`.chatmod-error{margin:0;font-size:.8125rem;color:var(--hcm-color-danger);overflow-wrap:anywhere}` +
	`.chatfilter-panel [aria-invalid="true"]{border-color:var(--hcm-color-danger)}` +
	`.chatmod-channels{display:grid;gap:2px;max-height:12rem;overflow:auto}` +
	`.chatmod-channels label{display:flex;align-items:center;gap:8px;min-height:44px}.chatmod-channels input,.chatmod-editor .chatfilter-check input{width:24px;min-height:24px;flex:none}` +
	`.chatmod-details summary{min-height:44px;display:flex;align-items:center;cursor:pointer;font-weight:600;font-size:.875rem}` +
	`.chatmod-terms{margin:0;padding-inline-start:20px;overflow-wrap:anywhere}` +
	`.chatmod-try{display:grid;gap:8px;padding:12px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);min-width:0}` +
	`.chatmod-outcome{margin:0;padding:8px 12px;border-inline-start:3px solid var(--hcm-color-brand-primary);border-radius:var(--hcm-radius-control);background:var(--hcm-color-brand-soft);overflow-wrap:anywhere}` +
	`.chatmod-actions{display:flex;flex-wrap:wrap;gap:8px}` +
	`.chatfilter-panel .chatmod-primary{background:var(--hcm-color-brand-primary);color:var(--hcm-color-on-brand);border-color:var(--hcm-color-brand-primary)}` +
	`.chatmod button[disabled]{opacity:.6}` +
	`@media(max-width:390px){.chatmod-row-main{display:grid;grid-template-columns:minmax(0,1fr)}.chatmod-actions{display:grid}}` +
	chatmod003HitsStyles
