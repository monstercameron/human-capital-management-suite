package chatui

// ChattoneMenuStyles draws the Rewrite control: a tool button that carries its
// word beside the icon, and the menu of styles it opens. The rows reuse the
// shared .menu-item row of the message and sidebar menus; the rules here only
// lay out what a style row holds (name, one line of description, the shortcut)
// and win over ".chattone button:not(.tool-button)" in chattone_styles.go, which
// gives the panel's own buttons their bordered look. Colours are the shell's
// tokens, so dark mode and right-to-left follow without a rule of their own.
const ChattoneMenuStyles = `.chattone .tool-button.chattone-trigger{inline-size:auto;gap:6px;padding-inline:8px;font-size:.8125rem;font-weight:500;line-height:1;white-space:nowrap}` +
	`.chattone-trigger-busy{display:none}.chattone[aria-busy="true"] .chattone-trigger-label{display:none}.chattone[aria-busy="true"] .chattone-trigger-busy{display:inline}` +
	`.chattone .chattone-menu{padding:6px;inline-size:min(320px,calc(100vw - 16px))}` +
	`.chattone-menu-list{display:flex;flex-direction:column;gap:2px}` +
	`.chattone .menu-item.chattone-row{align-items:flex-start;justify-content:space-between;gap:12px;min-block-size:48px;padding:8px 10px;border:0;background:transparent;color:var(--ink);font-weight:400}` +
	`.chattone .menu-item.chattone-row:hover,.chattone .menu-item.chattone-row:focus-visible{background:var(--soft)}` +
	`.chattone-row-text{display:flex;flex-direction:column;flex:1 1 auto;gap:2px;min-width:0}` +
	`.chattone-row-head{display:flex;flex-wrap:wrap;align-items:center;gap:6px}` +
	`.chattone-row-name{color:var(--ink);font-size:.875rem;font-weight:400}` +
	`.chattone-row-detail{display:flex;flex-direction:column;gap:2px}` +
	`.chattone-row-note,.chattone-row-reason{color:var(--muted);font-size:.75rem;line-height:1.35}` +
	`.chattone-row-key{flex:none;padding-block-start:2px;color:var(--muted);font-size:.75rem;line-height:1.35;white-space:nowrap}` +
	`.chattone-menu .chattone-suggested{display:inline-flex;min-height:0;padding:1px 6px;border-radius:var(--hcm-radius-control);background:color-mix(in srgb,var(--accent) 14%,transparent);color:var(--accent);font-size:.6875rem;font-weight:600;line-height:1.4}` +
	`.chattone-menu-notice{margin:6px 0 0;padding:8px 10px 4px;border-block-start:1px solid var(--line);color:var(--muted);font-size:.75rem;line-height:1.4;overflow-wrap:anywhere}` +
	// A touch screen has no Alt key: the shortcuts are not offered there.
	`@media(pointer:coarse){.chattone-row-key{display:none}}` +
	// In the narrow thread pane the Enter hint is a clipped, empty-looking box laid
	// over the last tools; it took the presses meant for Rewrite. It is text, not a
	// control, so presses go through it.
	`.chat-workspace .thread-composer .composer-help{pointer-events:none}` +
	// What a rewrite leaves above the composer's text: the status line and the
	// "Rewritten: Professional · Undo · See changes" line. Quiet text, no borders;
	// the slot takes no room while both are empty.
	`.chattone-chip-slot{display:flex;flex-direction:column;gap:2px;min-width:0;padding:2px 8px 0}` +
	`.chattone-chip-slot:not(:has(.chattone-status:not(:empty),.chattone-chip)){display:none}` +
	`.chattone-chip-slot .chattone-status{margin:0;block-size:auto;color:var(--ink);font-size:.8125rem;line-height:1.4}` +
	`.chattone-chip{display:flex;flex-wrap:wrap;align-items:center;gap:2px 10px;min-width:0;color:var(--muted);font-size:.8125rem;line-height:1.4}` +
	`.chattone-chip-label{display:inline-flex;flex-wrap:wrap;align-items:center;gap:4px;min-width:0}` +
	`.chattone-chip .chat-icon{flex:none;inline-size:14px;block-size:14px;color:var(--accent)}` +
	`.chattone-chip-style{color:var(--ink);font-weight:600}` +
	`.chattone-chip .chattone-chip-action{min-block-size:24px;padding:2px 6px;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--accent);font:inherit;cursor:pointer}` +
	`.chattone-chip .chattone-chip-action:hover,.chattone-chip .chattone-chip-action[aria-expanded="true"]{background:color-mix(in srgb,var(--accent) 12%,transparent)}` +
	`.chattone-options .chattone-diff p{margin:6px 0 2px;color:var(--muted);font-size:.75rem}` +
	`.chattone-options .chattone-diff del,.chattone-options .chattone-diff ins{color:var(--ink);font-size:.875rem;text-decoration:none}` +
	`.chattone-options .chattone-diff del{color:var(--muted)}`
