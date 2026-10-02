package chatui

// ChatEmojiStyles styles the one emoji picker (CHATEMOJI-002). Colours and radii
// come only from the --hcm-* tokens, every property that has a direction is
// logical so the picker mirrors in Arabic, and nothing animates. The row heights
// here (36px cells and 28px headings, 44px and 32px on touch) are the numbers
// chatemoji_layout.go lays the grid out with; change them together.
const ChatEmojiStyles = `.emoji-pop{position:fixed;inset:auto;z-index:1400;box-sizing:border-box;display:flex;flex-direction:column;gap:6px;inline-size:340px;block-size:392px;max-inline-size:calc(100vw - 16px);max-block-size:calc(100dvh - 16px);margin:0;padding:8px;overflow:hidden;background:var(--hcm-color-surface);color:var(--hcm-color-text);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-raised);font-size:.875rem;line-height:1.3}` +
	`.emoji-pop:not(:popover-open){display:none}` +
	`.emoji-pop-search{flex:none}` +
	`.emoji-pop-input{box-sizing:border-box;inline-size:100%;min-block-size:36px;padding:6px 10px;border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:inherit;font:inherit}` +
	`.emoji-pop-input:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:1px}` +
	`.emoji-pop-tabs{flex:none;display:flex;gap:2px;padding-block-end:2px;border-block-end:1px solid var(--hcm-color-border)}` +
	`.emoji-pop .emoji-pop-tab{flex:1 1 0;min-inline-size:28px;min-block-size:38px;padding:0;border:0;border-block-end:3px solid transparent;border-radius:var(--hcm-radius-control) var(--hcm-radius-control) 0 0;background:transparent;color:inherit;font-size:1.5rem;line-height:1;cursor:pointer;opacity:.7}` +
	`.emoji-pop-tab:hover:not(:disabled){background:color-mix(in srgb,var(--hcm-color-text) 8%,transparent);opacity:1}` +
	`.emoji-pop .emoji-pop-tab.current{opacity:1;border-block-end-color:var(--hcm-color-brand-primary);background:color-mix(in srgb,var(--hcm-color-brand-primary) 14%,transparent)}` +
	`.emoji-pop-tab:disabled{opacity:.4;cursor:default}` +
	`.emoji-pop-tab:focus-visible,.emoji-pop-tone:focus-visible,.emoji-pop-swatch:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:-2px}` +
	`.emoji-pop-scroll{position:relative;flex:1 1 auto;min-block-size:0;overflow-y:auto;overscroll-behavior:contain;scrollbar-width:thin}` +
	`.emoji-pop-sticky{position:sticky;inset-block-start:0;z-index:2;block-size:0;overflow:visible;pointer-events:none}` +
	`.emoji-pop-sticky>span,.emoji-pop-head{display:block;box-sizing:border-box;padding:6px 4px 0;background:var(--hcm-color-surface);color:var(--hcm-color-text-muted);font-size:.75rem;font-weight:600;letter-spacing:.02em;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}` +
	`.emoji-pop-sticky>span{block-size:28px}` +
	`.emoji-pop-headrow{box-sizing:border-box;block-size:28px;overflow:hidden}` +
	`.emoji-pop-head{block-size:100%}` +
	`.emoji-pop-spacer{block-size:0}` +
	`.emoji-pop-row{display:grid;grid-template-columns:repeat(8,36px);justify-content:space-between;block-size:36px}` +
	`.emoji-pop-cell{display:flex;min-inline-size:0}` +
	`.emoji-pop-btn{display:flex;align-items:center;justify-content:center;box-sizing:border-box;inline-size:100%;block-size:100%;min-block-size:0;min-inline-size:0;padding:0;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:inherit;font-size:1.375rem;line-height:1;cursor:pointer}` +
	`.emoji-pop-btn:hover{background:color-mix(in srgb,var(--hcm-color-text) 8%,transparent)}` +
	`.emoji-pop-cell.active .emoji-pop-btn{background:color-mix(in srgb,var(--hcm-color-brand-primary) 16%,transparent);box-shadow:inset 0 0 0 2px var(--hcm-color-focus)}` +
	`.emoji-pop-note{margin:12px 4px;color:var(--hcm-color-text-muted);font-size:.8125rem}` +
	`.emoji-pop-foot{flex:none;display:flex;align-items:center;justify-content:space-between;gap:8px;min-block-size:48px;padding-block-start:6px;border-block-start:1px solid var(--hcm-color-border)}` +
	`.emoji-pop-preview{display:flex;flex:1 1 auto;align-items:center;gap:10px;min-inline-size:0}` +
	`.emoji-pop-big{flex:none;font-size:1.75rem;line-height:1}` +
	`.emoji-pop-names{display:flex;flex-direction:column;min-inline-size:0}` +
	`.emoji-pop-name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:600}` +
	`.emoji-pop-code{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--hcm-color-text-muted);font-size:.75rem}` +
	`.emoji-pop-preview.hint .emoji-pop-name{font-size:.75rem;font-weight:400;color:var(--hcm-color-text-muted);white-space:normal}` +
	`.emoji-pop-tone{flex:none;box-sizing:border-box;inline-size:36px;block-size:36px;padding:0;border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-control);background:transparent;color:inherit;font-size:1.25rem;line-height:1;cursor:pointer}` +
	`.emoji-pop-tone:hover,.emoji-pop-swatch:hover{background:color-mix(in srgb,var(--hcm-color-text) 8%,transparent)}` +
	`.emoji-pop-swatches{display:flex;flex:1 1 auto;gap:2px;min-inline-size:0}` +
	`.emoji-pop-swatch{flex:none;box-sizing:border-box;inline-size:34px;block-size:34px;padding:0;border:2px solid transparent;border-radius:var(--hcm-radius-control);background:transparent;color:inherit;font-size:1.125rem;line-height:1;cursor:pointer}` +
	`.emoji-pop-swatch.current{border-color:var(--hcm-color-brand-primary)}` +
	`.emoji-pop-btn,.emoji-pop-big,.emoji-pop-tab,.emoji-pop-tone,.emoji-pop-swatch{font-family:"Segoe UI Emoji","Apple Color Emoji","Noto Color Emoji",sans-serif}` +
	`.emoji-pop[data-touch="true"] .emoji-pop-row{grid-template-columns:repeat(8,minmax(0,1fr));block-size:44px}` +
	`.emoji-pop[data-touch="true"] .emoji-pop-headrow,.emoji-pop[data-touch="true"] .emoji-pop-sticky>span{block-size:32px}` +
	`.emoji-pop[data-touch="true"] .emoji-pop-tab,.emoji-pop[data-touch="true"] .emoji-pop-tone,.emoji-pop[data-touch="true"] .emoji-pop-input{min-block-size:44px}` +
	`.emoji-pop[data-sheet="true"]{inset:auto 0 0 0!important;inline-size:auto!important;max-inline-size:none!important;block-size:min(440px,72dvh)!important;max-block-size:72dvh!important;border-radius:var(--hcm-radius-surface) var(--hcm-radius-surface) 0 0;border-inline-width:0;border-block-end-width:0;padding-block-end:calc(8px + env(safe-area-inset-bottom,0px))}` +
	`.emoji-completion-option .mention-emoji{flex:none;inline-size:1.75rem;text-align:center;font-size:1.25rem;line-height:1;font-family:"Segoe UI Emoji","Apple Color Emoji","Noto Color Emoji",sans-serif}` +
	`.emoji-flag-chip{display:inline-flex;align-items:center;box-sizing:border-box;padding:0 .4em;min-block-size:1.35em;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text);font-size:.75em;font-weight:600;letter-spacing:.04em;line-height:1.35;vertical-align:baseline;white-space:nowrap}` +
	`@media(forced-colors:active){.emoji-pop-cell.active .emoji-pop-btn,.emoji-pop-tab.current{outline:2px solid Highlight}}`
