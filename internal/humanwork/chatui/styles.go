package chatui

import "strconv"

// Stylesheet is scoped under .chat-workspace by ScopedStylesheet and included
// once by the product shell. Every color comes from the shell's live tokens
// (--surface, --canvas, --ink, --muted, --line, --accent, --soft, plus the
// --hcm-* status/shape tokens), which the shell re-declares for dark mode,
// forced colors and customer palettes; chat owns no palette of its own.
const Stylesheet = `.chat-workspace{--chat-rail:256px;--chat-details:320px;--chat-measure:720px;--chat-rail-menu-top:80px;--chat-rail-menu-left:8px;position:relative;display:flex;flex-direction:column;height:100%;min-height:0;background:var(--surface);color:var(--ink);font-size:.9375rem;line-height:1.45}` +
	// Round 3 C-13: the default rail matches RailDefault (288px) once the
	// viewport has room for it; 256px below 1280px keeps the timeline wide on
	// a laptop. A width the viewer dragged is set on the element and wins.
	`@media(min-width:1280px){.chat-workspace{--chat-rail:288px}}` +
	`.chat-workspace *{box-sizing:border-box}` +
	`.chat-workspace p{max-width:none}` +
	`.chat-workspace .message-body{max-width:90ch}` +
	`.channel-intro{padding:24px 8px 8px}.channel-intro .large-avatar{margin:0 0 10px}.channel-intro h2{font-size:1.25rem;margin:0 0 4px}.channel-intro p{margin:0;color:var(--muted);font-size:.875rem;max-width:60ch}` +
	`.chat-search-results{min-height:0;flex:1;overflow-y:auto;padding:20px clamp(16px,4vw,48px) 32px}.chat-search-results h2{font-size:1.125rem;margin:0 0 14px;overflow-wrap:anywhere}.search-status{color:var(--muted);margin:8px 0 14px}.search-error{color:var(--hcm-color-danger,var(--ink))}.search-result-group{margin:16px 0 22px}.search-result-group h3{font-size:.8125rem;color:var(--muted);margin:0 0 6px}.search-result{display:flex;flex-direction:column;align-items:flex-start;gap:2px;width:100%;padding:10px 12px;border:1px solid transparent;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;text-align:start;cursor:pointer}.search-result:hover,.search-result:focus-visible{background:var(--soft);border-color:var(--line)}.search-result strong{font-size:.9rem}.search-result span{font-size:.8125rem;color:var(--muted);overflow-wrap:anywhere}.search-message-result{margin:3px 0}.search-result-snippet{display:-webkit-box!important;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden;max-width:72ch;white-space:pre-wrap}.search-more{margin-top:8px}` +
	`.message-list.from-top::before{flex:0}` +
	`.message.search-target{outline:2px solid var(--accent);outline-offset:3px;border-radius:var(--hcm-radius-control);background:color-mix(in srgb,var(--accent) 9%,transparent);scroll-margin-block:40px}` +
	`.rail-pill{display:none;align-items:center;gap:6px;height:36px;padding:0 12px 0 10px;border:1px solid var(--line);border-radius:999px;background:var(--canvas);color:var(--ink);font:inherit;font-size:.8125rem;font-weight:600;cursor:pointer}.rail-pill .chat-icon{width:16px;height:16px}` +
	`.browse-filter{padding:0 0 10px}.browse-filter .chat-icon{inset-inline-start:10px}` +
	`.chat-workspace :focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chat-workspace button:disabled,.chat-workspace input:disabled,.chat-workspace textarea:disabled,.chat-workspace select:disabled{cursor:not-allowed;opacity:.45}` +
	`.chat-icon{width:18px;height:18px;flex:none}` +
	`.chat-layout{display:grid;grid-template-columns:minmax(220px,var(--chat-rail)) minmax(0,1fr);flex:1;min-height:0}` +
	`.chat-workspace[data-details-open="true"] .chat-layout{grid-template-columns:minmax(220px,var(--chat-rail)) minmax(0,1fr) minmax(260px,var(--chat-details))}` +
	`.chat-workspace{--chat-details:400px}` +
	// rail
	`.chat-rail{position:relative;display:flex;flex-direction:column;min-height:0;background:var(--canvas);border-inline-end:1px solid var(--line)}` +
	`.pane-handle{position:absolute;top:0;bottom:0;inset-inline-end:-4px;width:8px;cursor:col-resize;z-index:5;background:transparent;border-radius:2px;transition:background var(--hcm-motion-fast) var(--hcm-motion-easing)}` +
	`.chat-side .pane-handle{inset-inline-end:auto;inset-inline-start:-4px}` +
	`.pane-handle:hover,.pane-handle.dragging,.pane-handle:focus-visible{background:color-mix(in srgb,var(--accent) 45%,transparent);outline:0}` +
	`.chat-workspace[data-pane-dragging]{cursor:col-resize;user-select:none}` +
	`.chat-workspace[data-pane-dragging] .chat-rail,.chat-workspace[data-pane-dragging] .chat-side{pointer-events:none}` +
	`.chat-workspace[data-pane-dragging] .pane-handle{pointer-events:auto}` +
	`.rail-head{display:flex;align-items:center;justify-content:space-between;gap:8px;height:52px;padding:0 8px 0 16px;flex:none}` +
	`.rail-title{font-size:1rem;font-weight:700;margin:0}` +
	`.rail-head-actions{display:flex;gap:2px}` +
	`.rail-search{position:relative;padding:0 12px 8px;flex:none}` +
	`.rail-search .chat-icon{position:absolute;inset-inline-start:22px;top:9px;width:16px;height:16px;color:var(--muted);pointer-events:none}` +
	`.chat-search{width:100%;height:34px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:inherit;padding-inline:32px 10px;font:inherit;font-size:.875rem}` +
	`.chat-search::placeholder,.composer-input::placeholder,.chat-input::placeholder{color:var(--muted);opacity:1}` +
	`.chat-search:focus{border-color:var(--accent);outline:0;box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 22%,transparent)}` +
	// C-9: on a shorter viewport (laptop, ~780px tall) the Quiet hours footer
	// sits below the scroller and can cover the selected row and "New
	// section" at the bottom of the list. scroll-padding keeps a
	// programmatic scroll (mounting on the selected row) from landing the
	// target under the footer, and the footer's own top edge gets a
	// separator once content is scrolled under it (:has() on the sibling
	// scroller's scroll state isn't available, so this uses a static
	// border -- always-on is a safe default; a design pass can make it
	// conditional on overflow with a scroll listener if that reads as busy).
	`.rail-scroll{flex:1;min-height:0;overflow-y:auto;overscroll-behavior:contain;padding:0 8px 12px;scroll-padding-block:40px 56px;mask-image:linear-gradient(to bottom,#000 calc(100% - 14px),transparent);-webkit-mask-image:linear-gradient(to bottom,#000 calc(100% - 14px),transparent)}` +
	`.rail-footer{flex:none;padding:0 8px;border-top:1px solid var(--line);box-shadow:0 -6px 10px -8px color-mix(in srgb,var(--ink) 30%,transparent)}` +
	`.pinned-list{list-style:none;margin:6px 0 0;padding:0}` +
	`.pinned-row{border-top:1px solid var(--line)}` +
	`.pinned-link{display:flex;flex-direction:column;gap:2px;width:100%;padding:8px 0;background:none;border:0;color:var(--ink);font:inherit;font-size:.8125rem;text-align:start;cursor:pointer}` +
	`.pinned-link strong{font-size:.75rem}` +
	`.pinned-link span{color:var(--muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:100%}` +
	`.pinned-link:hover strong{color:var(--accent)}` +
	`.pin-actions{display:flex;flex-wrap:wrap;gap:4px;padding:0 0 8px}` +
	`.channel-todo-list{list-style:none;margin:8px 0;padding:0}` +
	`.channel-todo-row{display:grid;grid-template-columns:28px minmax(0,1fr) 32px;align-items:start;gap:4px;padding:6px 0;border-top:1px solid var(--line)}` +
	`.channel-todo-check{border:0;background:none;color:var(--accent);padding:2px 4px;cursor:pointer;font:inherit}` +
	`.channel-todo-text{min-width:0;overflow-wrap:anywhere;font-size:.8125rem}` +
	`.channel-todo-source{grid-column:2;grid-row:4;display:inline-flex;align-items:center;gap:4px;min-width:0;padding:2px 0;background:none;border:0;color:var(--accent);font:inherit;font-size:.75rem;text-align:start;cursor:pointer}` +
	`.channel-todo-source .chat-icon{width:13px;height:13px;flex:none}` +
	`.channel-todo-source-copy{grid-column:3;grid-row:4}` +
	`.channel-todo-row .channel-todo-completed-by{grid-column:2;grid-row:2}` +
	`.channel-todo-row .channel-todo-restriction{grid-column:2;grid-row:3}` +
	`.channel-todo-policy{grid-column:2 / -1;grid-row:5;display:flex;flex-direction:column;gap:5px;min-width:0;padding:4px 0}` +
	`.channel-todo-policy .chat-input{width:100%;min-width:0}` +
	`.channel-todo-policy-summary{grid-column:2 / -1;grid-row:5}` +
	`.channel-todo-selected{list-style:none;margin:0;padding:0}` +
	`.channel-todo-selected li{display:flex;align-items:center;justify-content:space-between;gap:4px;min-width:0}` +
	`.channel-todo-delete{grid-column:3;grid-row:1}` +
	`.channel-todo-form{display:flex;flex-direction:column;gap:6px;margin-top:8px}` +
	`.channel-todo-form .chat-input,.channel-todo-form .button{width:100%;min-width:0}` +
	`.channel-todo p{font-size:.8125rem;color:var(--muted)}` +
	`.channel-todo-trigger{display:inline-flex;align-items:center;justify-content:center;gap:6px;min-width:38px;height:34px;padding:0 9px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.8125rem;font-weight:600;cursor:pointer;white-space:nowrap}` +
	`.channel-poll-trigger{display:inline-flex;align-items:center;justify-content:center;gap:6px;min-width:38px;height:34px;padding:0 9px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.8125rem;font-weight:600;cursor:pointer;white-space:nowrap}` +
	`.channel-poll-trigger:hover,.channel-poll-trigger:focus-visible{background:var(--soft);border-color:var(--accent);color:var(--accent)}.channel-poll-trigger.active{border-color:var(--accent);background:var(--soft)}` +
	`.channel-poll-trigger .chat-icon{width:16px;height:16px;color:var(--muted)}.channel-poll-active-indicator{color:var(--accent);font-size:1rem;line-height:1}` +
	`.channel-todo-trigger:hover,.channel-todo-trigger[aria-pressed="true"]{background:var(--soft);border-color:var(--accent);color:var(--accent)}` +
	`.channel-todo-trigger .chat-icon{width:16px;height:16px;color:var(--muted)}` +
	`.channel-todo-trigger-label{overflow:hidden;text-overflow:ellipsis}` +
	`.channel-todo-count{display:inline-flex;align-items:center;justify-content:center;min-width:19px;height:19px;padding:0 5px;border-radius:999px;background:var(--soft);color:var(--muted);font-size:.6875rem;font-weight:700;line-height:1}` +
	`.channel-todo-completed-by{color:var(--muted);font-size:.75rem}` +
	`.channel-todo-restriction{color:var(--muted);font-size:.75rem}` +
	`.channel-widgets-inline{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,220px),1fr));gap:8px;margin:0 16px 6px;min-width:0}.channel-widget-inline-card{min-width:0;max-height:145px;overflow:auto;padding:8px 10px;border:1px solid var(--hcm-color-warning);border-radius:var(--hcm-radius-control);background:var(--surface-muted);box-shadow:inset 3px 0 0 var(--hcm-color-warning);font-size:.8125rem}.channel-widget-inline-card p{margin:4px 0;overflow-wrap:anywhere}.channel-widget-inline-card ul{margin:4px 0;padding-inline-start:18px}.channel-widget-list{list-style:none;margin:8px 0;padding:0}.channel-widget-roster{margin-top:10px}.channel-widget-roster summary{cursor:pointer;font-size:.8125rem;font-weight:600;color:var(--accent)}.channel-widget-row,.channel-widget-form{display:flex;flex-direction:column;gap:5px;min-width:0;padding:8px 0}.channel-widget-row{border-top:1px solid var(--line)}.channel-widget .chat-input,.channel-widget .button{width:100%;min-width:0}.channel-widget label{font-size:.75rem;color:var(--muted)}.channel-widget p{font-size:.75rem;overflow-wrap:anywhere}` +
	`.channel-poll-form{display:flex;flex-direction:column;gap:6px;margin-top:8px}.channel-poll-form label{font-size:.75rem;color:var(--muted)}.channel-poll-form .chat-input,.channel-poll-form .button{width:100%;min-width:0}.channel-poll-question{font-weight:650;overflow-wrap:anywhere}.channel-poll-list{list-style:none;margin:8px 0;padding:0}.channel-poll-option{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:4px 8px;align-items:center;padding:8px 0;border-top:1px solid var(--line)}.channel-poll-vote{grid-column:1 / -1;justify-self:start;min-height:36px}.channel-poll-option-label{min-width:0;overflow-wrap:anywhere;font-size:.8125rem;font-weight:600}.channel-poll-progress{grid-column:1 / -1;width:100%;height:8px;accent-color:var(--accent)}.channel-poll-count{color:var(--muted);font-size:.75rem;white-space:nowrap}` +
	`.browse-heading{display:flex;align-items:baseline;gap:10px;min-width:0}` +
	`.browse-count{font-size:.75rem;color:var(--muted);white-space:nowrap}` +
	`.section-controls{display:flex;align-items:center;margin:14px 0 4px}` +
	`.section-title{flex:1;display:flex;align-items:center;gap:4px;min-width:0;background:none;border:0;color:var(--muted);font:inherit;font-size:.8125rem;font-weight:600;padding:4px 4px;border-radius:var(--hcm-radius-control);cursor:pointer;text-align:start}` +
	`.section-title:hover{color:var(--ink)}` +
	`.section-title>span:not(.section-count){overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.section-title .chat-icon{width:14px;height:14px}` +
	`.section-count{margin-inline-start:4px;font-weight:500;color:var(--muted);font-size:.75rem}` +
	`.section-order{display:none;width:24px;height:24px;background:none;border:0;border-radius:var(--hcm-radius-control);color:var(--muted);align-items:center;justify-content:center;cursor:pointer}` +
	`.section-order .chat-icon{width:14px;height:14px}` +
	`.section-controls:hover .section-order,.section-controls:focus-within .section-order{display:inline-flex}` +
	// Round 3 C-4: a group's header stays pinned while its rows scroll, so a
	// rail scrolled to the selected row still says which group the rows at
	// its top belong to. The scroller's top fade was dropped for it: rows now
	// pass under an opaque header instead of fading out, and a fade would
	// have washed out the header's own first pixels.
	`.rail-scroll>.sidebar-section>.section-controls{position:sticky;top:0;z-index:2;background:var(--canvas)}` +
	// Round 3 C-5: the drawer's landing focus (mobile_rail_focus_js.go) is
	// marked quiet unless it came from the keyboard; the first key press
	// inside the rail removes the mark and the ring returns.
	`.chat-workspace .chat-row[data-quiet-focus]:focus-visible{outline:none}` +
	`.chat-row{position:relative;display:flex;align-items:center;gap:10px;width:100%;height:32px;background:none;border:0;border-radius:var(--hcm-radius-control);color:var(--muted);font:inherit;font-size:.9375rem;padding:0 8px 0 12px;cursor:pointer;text-align:start}` +
	`.chat-rail-row{display:flex;align-items:center;min-width:0}.chat-rail-row .chat-row{min-width:0;flex:1}.rail-row-more{flex:none;display:inline-flex;align-items:center;justify-content:center;width:28px;height:32px;padding:0;border:0;border-radius:var(--hcm-radius-control);background:none;color:var(--muted);cursor:pointer;opacity:0}.rail-row-more .chat-icon{width:16px;height:16px}.chat-rail-row:hover .rail-row-more,.chat-rail-row:focus-within .rail-row-more,.rail-row-more[aria-expanded="true"]{opacity:1}.rail-row-more:hover{background:var(--soft);color:var(--ink)}@media(hover:none){.rail-row-more{opacity:1}}` +
	`.rail-row-menu{position:fixed;top:var(--chat-rail-menu-top,80px);left:var(--chat-rail-menu-left,8px);z-index:1300;width:200px;max-width:calc(100vw - 16px);max-height:calc(100vh - 16px);overflow-y:auto;display:flex;flex-direction:column;padding:4px;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-control);box-shadow:var(--hcm-shadow-raised)}` +
	`.section-create{display:block;margin:8px 0 4px}.section-create summary{list-style:none}.section-create summary::-webkit-details-marker{display:none}.section-create-trigger{display:flex;align-items:center;gap:10px;width:100%;height:36px;padding:0 12px;border:1px solid transparent;border-radius:var(--hcm-radius-control);color:var(--accent);font:inherit;font-size:.875rem;font-weight:600;cursor:pointer}.section-create-trigger:hover{background:var(--soft);border-color:var(--line)}.section-create-trigger .chat-icon{width:16px;height:16px}.section-create-form{display:grid;gap:7px;margin:4px 4px 8px;padding:10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface)}.section-create-form label{color:var(--muted);font-size:.75rem;font-weight:600}.section-create-form input{width:100%}.section-create-actions{display:flex;justify-content:flex-end;gap:6px;padding-top:2px}.section-create-actions .button{min-width:70px}` +
	`.rail-menu-check{display:inline-block;width:16px;flex:none;color:var(--accent)}` +
	`.chat-row:hover{background:color-mix(in srgb,var(--ink) 8%,transparent);color:var(--ink)}` +
	`.chat-row.unread{color:var(--ink);font-weight:600}` +
	`.chat-row.muted{opacity:.6}` +
	`.chat-row.selected,.chat-row.selected:hover{background:color-mix(in srgb,var(--accent) 14%,transparent);color:var(--accent);font-weight:600;opacity:1}` +
	`.chat-row.selected::before{content:"";position:absolute;inset-inline-start:-8px;top:6px;bottom:6px;width:3px;border-radius:2px;background:var(--accent)}` +
	`.chat-row-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.kind-glyph{display:inline-flex;align-items:center;justify-content:center;width:18px;height:18px;flex:none;font-weight:600;opacity:.75}` +
	`.kind-glyph.hash{font-size:1rem}` +
	`.kind-glyph .chat-icon{width:16px;height:16px}` +
	// C-6: --hcm-color-on-brand is a fixed #fff regardless of theme; the
	// reviewed dark-mode-aware ink is --on-brand (the same token docs uses).
	`.chat-badge{background:var(--accent);color:var(--on-brand,#fff);border-radius:999px;font-size:.6875rem;font-weight:700;min-width:18px;height:18px;padding:0 6px;display:inline-flex;align-items:center;justify-content:center;flex:none}` +
	`.chat-badge.mention{background:var(--hcm-color-danger);color:#fff}` +
	// C-11: the open room is being read right now -- its own unread count
	// staying lit next to it reads as a bug, not a count.
	`.chat-row.selected .chat-badge{display:none}` +
	`.rail-link{display:flex;align-items:center;gap:10px;width:100%;height:36px;background:none;border:0;border-top:1px solid var(--line);border-radius:0;color:var(--muted);font:inherit;font-size:.875rem;padding:4px 12px 0;margin-top:0;cursor:pointer;text-align:start}` +
	`.rail-link:hover{background:color-mix(in srgb,var(--ink) 8%,transparent);color:var(--ink)}` +
	`.rail-link .chat-icon{width:16px;height:16px}` +
	`.rail-empty{color:var(--muted);font-size:.8125rem;padding:12px 8px;margin:0}` +
	`.rail-skeleton{display:grid;gap:12px;padding:14px 8px}` +
	`.rail-prefs{border-top:1px solid var(--line);padding:4px 12px 8px;font-size:.8125rem;flex:none}` +
	`.rail-prefs summary{display:flex;align-items:center;gap:8px;cursor:pointer;padding:5px 4px;color:var(--muted);list-style:none;border-radius:var(--hcm-radius-control)}` +
	`.rail-prefs summary::-webkit-details-marker{display:none}` +
	`.rail-prefs summary:hover{color:var(--ink)}` +
	`.rail-prefs summary .chat-icon{width:16px;height:16px}` +
	`.prefs-summary-text{flex:1;min-width:0}` +
	`.prefs-value{color:var(--muted);font-size:.75rem;white-space:nowrap}` +
	`.prefs-pill{font-size:.6875rem;font-weight:700;border-radius:999px;padding:1px 7px;background:color-mix(in srgb,var(--ink) 10%,transparent);color:var(--muted)}` +
	`.prefs-pill.on{background:var(--soft);color:var(--accent)}` +
	`.rail-prefs-body{display:grid;gap:8px;padding:4px 4px 6px}` +
	`.prefs-row{display:flex;align-items:center;gap:8px;color:var(--ink)}` +
	`.prefs-field{display:grid;gap:4px;font-size:.8125rem;color:var(--muted)}` +
	`.prefs-times{display:grid;grid-template-columns:1fr 1fr;gap:8px}` +
	`.chat-input{height:34px;border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;padding:0 10px;width:100%}` +
	`select.chat-input{appearance:none;-webkit-appearance:none;padding-inline-end:30px;background-image:linear-gradient(45deg,transparent 50%,currentColor 50%),linear-gradient(135deg,currentColor 50%,transparent 50%);background-position:calc(100% - 16px) 50%,calc(100% - 11px) 50%;background-size:5px 5px;background-repeat:no-repeat;cursor:pointer}` +
	`.chat-workspace[dir="rtl"] select.chat-input{padding-inline-end:30px;background-position:16px 50%,11px 50%}` +
	`.chat-input:focus{border-color:var(--accent);outline:0;box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 22%,transparent)}` +
	// main column
	`.chat-main{position:relative;display:flex;flex-direction:column;min-width:0;min-height:0;background:var(--surface)}.timeline-frame{position:relative;display:flex;flex:1;flex-direction:column;min-height:0}` +
	`.conversation-header{display:flex;align-items:center;gap:8px;height:52px;padding:0 12px 0 16px;border-bottom:1px solid var(--line);flex:none}` +
	`.conversation-header.empty{border-bottom:0;height:0;padding:0;overflow:hidden}` +
	`.state-actions{display:flex;gap:8px;justify-content:center;margin-top:6px}` +
	`.conversation-title{display:flex;align-items:center;gap:10px;min-width:0;flex:1}` +
	// C-1: the header is 52px tall; the shared 64px large-avatar overflows it,
	// so the header-scoped avatar (conversationHeaderAvatar) shrinks to fit.
	`.conversation-title .header-avatar{width:28px;height:28px;border-radius:6px;font-size:.75rem;margin:0}` +
	`.conversation-heading{min-width:0}` +
	`.conversation-heading h1{font-size:1rem;font-weight:700;margin:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}` +
	`.conversation-topic{margin:0;font-size:.75rem;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}` +
	`.conversation-actions{display:flex;gap:2px}` +
	`.icon-button{background:none;border:0;border-radius:var(--hcm-radius-control);color:var(--muted);width:34px;height:34px;display:inline-flex;align-items:center;justify-content:center;cursor:pointer;flex:none}` +
	`.icon-button:hover{background:color-mix(in srgb,var(--ink) 8%,transparent);color:var(--ink)}` +
	`.icon-button[aria-pressed="true"],.conversation-actions[aria-pressed="true"] .icon-button{background:var(--soft);color:var(--accent)}` +
	// C-10: without scroll-padding-top the message list can land its scroll
	// position with the first visible row half under the sticky-shadowed
	// header (a 6px lift the header animates in on scroll, see
	// .conversation-header's animation-timeline). Round 3 C-1: the day
	// divider stays in the flow. Every divider sits in one flat scroll
	// container, so a sticky divider never unstuck: all the passed days piled
	// into the same 4px slot and their rules struck through the row beneath.
	`.message-list{flex:1;min-height:0;overflow-y:auto;overscroll-behavior:contain;display:flex;flex-direction:column;padding:8px 20px;scroll-padding-top:8px}` +
	`.message-list::before{content:"";flex:1}` +
	`.message-list.virtualized::before{display:none}.virtual-spacer{flex:none;min-height:0}.virtual-row{flex:none;min-width:0}.virtual-prelude,.virtual-footer{flex:none}.virtual-parked{position:absolute;inset-inline-start:-10000px;inset-block-start:0;width:1px;height:1px;overflow:hidden}` +
	`.day-divider{display:flex;align-items:center;gap:12px;margin:14px 0 6px;color:var(--muted);font-size:.75rem;font-weight:600}` +
	`.day-divider::before,.day-divider::after{content:"";flex:1;border-top:1px solid color-mix(in srgb,var(--ink) 18%,transparent)}` +
	`.day-divider span{border:1px solid var(--line);border-radius:999px;padding:2px 10px;background:var(--surface)}` +
	// The public/private/DM/group intro sits above the first divider; give it
	// the same sticky-safe top clearance so it is not clipped in thread-open,
	// where the timeline column narrows and the header's shadow reaches
	// further down.
	`.channel-intro{scroll-margin-top:12px}` +
	`.load-older{align-self:center;margin:6px auto 4px}` +
	`.message{position:relative;display:grid;grid-template-columns:36px minmax(0,1fr);column-gap:10px;padding:4px 8px;margin:10px -8px 0;border-radius:var(--hcm-radius-control)}` +
	`.message.continued{margin-top:0;padding-top:2px;padding-bottom:2px}` +
	`.message:hover,.message:focus-within{background:color-mix(in srgb,var(--ink) 5%,transparent)}` +
	`.message.pinned{box-shadow:inset 3px 0 0 var(--hcm-color-warning)}` +
	`.message.thread-active{background:var(--soft)}` +
	`.avatar,.large-avatar{display:flex;align-items:center;justify-content:center;flex:none;border-radius:8px;background:var(--soft);color:var(--accent);font-weight:700}` +
	`.avatar:has(.chat-avatar-photo),.large-avatar:has(.chat-avatar-photo){position:relative}.chat-avatar-photo{position:absolute;inset:0;width:100%;height:100%;object-fit:cover;border-radius:inherit;color:transparent}` +
	`.avatar{position:relative;width:36px;height:36px;font-size:.8125rem}` +
	`.avatar.online::after{content:"";position:absolute;inset-inline-end:-2px;bottom:-2px;width:9px;height:9px;border-radius:50%;background:var(--hcm-color-success);border:2px solid var(--canvas)}` +
	`.avatar.small{width:26px;height:26px;font-size:.6875rem;border-radius:6px}` +
	`.avatar.tiny{width:18px;height:18px;font-size:.5625rem;border-radius:5px}` +
	`.large-avatar{width:64px;height:64px;border-radius:14px;font-size:1.5rem;margin:0 auto}` +
	`.gutter-time{font-size:.6875rem;color:var(--muted);text-align:end;padding-top:4px;opacity:0;white-space:nowrap;overflow:hidden}` +
	`.message:hover .gutter-time,.message:focus-within .gutter-time{opacity:1}` +
	`.message-content{min-width:0}` +
	`.message-meta{display:flex;align-items:baseline;gap:8px;flex-wrap:wrap}` +
	`.message-author{font-size:.9375rem;color:var(--ink)}` +
	`.message-time,.message-badge{font-size:.75rem;font-weight:500;color:var(--muted)}` +
	`.message.continued .message-time{display:none}` +
	`.pinned-badge{display:inline-flex;align-items:center;gap:3px;color:var(--hcm-color-warning)}` +
	`.pinned-badge .chat-icon{width:12px;height:12px}` +
	`.message-body{margin:1px 0 0;font-size:.9375rem;line-height:1.45;color:var(--ink);white-space:pre-wrap;overflow-wrap:anywhere;unicode-bidi:plaintext;text-align:start;width:fit-content;max-width:none}` +
	`.message-body>p{margin:0}.message-body>p+p{margin-top:.6em}.message-body ul,.message-body ol{margin:.35em 0;padding-inline-start:1.4em}.message-body blockquote{margin:.4em 0;padding-inline-start:.8em;border-inline-start:3px solid var(--line)}.message-body pre{max-width:100%;overflow:auto;white-space:pre}.message-body code{font-family:ui-monospace,monospace}` +
	`.chat-embed{display:grid;gap:3px;width:min(100%,520px);margin-top:8px;padding:10px 12px;border:1px solid var(--line);border-inline-start:3px solid var(--accent);border-radius:var(--hcm-radius-control);background:var(--soft);color:var(--ink);text-align:start;overflow-wrap:anywhere}.chat-embed-link{font:inherit;cursor:pointer;text-decoration:none}a.chat-embed-link:hover .chat-embed-source,a.chat-embed-link:focus-visible .chat-embed-source{text-decoration:underline}.chat-embed-link:hover,.chat-embed-link:focus-visible{border-color:var(--accent)}.chat-embed-label,.chat-embed-byline{font-size:.75rem;color:var(--muted)}.chat-embed-source{font-size:.875rem}.chat-embed-body{margin:2px 0 0;white-space:pre-wrap;max-height:8.7em;overflow:hidden;unicode-bidi:plaintext}.composer-embeds:empty{display:none}.composer-embeds{order:0;flex:0 0 100%;box-sizing:border-box;min-width:0;max-height:min(28vh,220px);overflow:auto;padding-inline:6px}.composer-embeds .chat-embed{max-height:160px;overflow:auto}` +
	`.message-footer{display:flex;gap:8px}` +
	`.reaction-row{position:relative;display:flex;flex-wrap:wrap;gap:4px;margin-top:6px}` +
	`.reaction-row.picker-only{position:absolute;inset:0;margin:0;pointer-events:none}` +
	`.reaction-row.picker-only .reaction-picker{top:24px;bottom:auto;inset-inline-start:auto;inset-inline-end:12px;max-width:calc(100% - 24px);flex-wrap:wrap;pointer-events:auto}` +
	`.reaction{display:inline-flex;align-items:center;gap:4px;height:28px;padding:0 9px;border:1px solid var(--line);border-radius:14px;background:var(--surface);color:var(--ink);font:inherit;font-size:.75rem;font-weight:600;line-height:1;cursor:pointer}` +
	`.reaction:hover{border-color:var(--accent)}` +
	`.reaction.mine{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 12%,transparent);color:var(--accent)}` +
	// Round 3 C-11: a chip's only edge was a 1px --line border (about 1.4:1
	// on white), which read as no pill at all in screenshots. A 5% ink tint
	// on the fill makes the pill visible in light and dark without
	// competing with the accent pill of your own reaction.
	`.reaction-row>.reaction:not(.mine):not(.add){background:color-mix(in srgb,var(--ink) 5%,var(--surface))}` +
	`.reaction-emoji{font-size:.875rem}` +
	`.reaction.add{color:var(--muted);padding:0 7px;gap:1px;border-style:dashed;background:transparent}` +
	`.reaction.add:hover{color:var(--accent);border-style:solid}` +
	`.reaction.add .chat-icon{width:14px;height:14px}` +
	`.reaction-picker{position:absolute;bottom:calc(100% + 4px);inset-inline-start:0;display:flex;gap:2px;padding:4px;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-control);box-shadow:var(--hcm-shadow-raised);z-index:3}` +
	`.picker-emoji{width:32px;height:32px;border:0;background:none;border-radius:6px;font-size:1.125rem;line-height:1;cursor:pointer}` +
	`.picker-emoji:hover,.picker-emoji:focus-visible{background:var(--soft)}` +
	`.message-menu{position:absolute;top:24px;inset-inline-end:12px;min-width:180px;display:flex;flex-direction:column;padding:4px;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-control);box-shadow:var(--hcm-shadow-raised);z-index:4}` +
	`.menu-item{display:flex;align-items:center;gap:8px;padding:8px 10px;border:0;background:none;color:var(--ink);font:inherit;font-size:.875rem;text-align:start;border-radius:6px;cursor:pointer}` +
	`.menu-item:hover,.menu-item:focus-visible{background:var(--soft)}` +
	`.menu-item.danger{color:var(--hcm-color-danger)}` +
	// CHAT-08: the developer-only sub-section separates "Copy API curl" from
	// the everyday actions above it, with a border and its own small heading
	// rather than looking like just another row.
	`.menu-section{display:flex;flex-direction:column;gap:1px;margin:4px 2px 0;padding:6px 8px 2px;border-top:1px solid var(--line)}` +
	`.menu-section-title{font-size:.6875rem;font-weight:700;text-transform:uppercase;letter-spacing:.03em;color:var(--muted)}` +
	`.menu-section-hint{font-size:.6875rem;color:var(--muted)}` +
	`.menu-item .chat-icon{width:16px;height:16px}` +
	`.message:has(.message-menu) .message-actions,.message:has(.reaction-picker) .message-actions{opacity:1}` +
	`.unread-divider{display:flex;align-items:center;gap:8px;margin:12px 0 2px;color:var(--hcm-color-danger);font-size:.6875rem;font-weight:700;letter-spacing:.02em}` +
	`.unread-divider::before{content:"";flex:1;border-top:1px solid currentColor}` +
	`.jump-newest{position:absolute;bottom:12px;inset-inline-end:24px;display:inline-flex;align-items:center;gap:6px;height:32px;padding:0 12px;border-radius:16px;border:1px solid var(--line);background:var(--surface);color:var(--accent);font:inherit;font-size:.8125rem;font-weight:600;box-shadow:var(--hcm-shadow-raised);cursor:pointer;opacity:0;pointer-events:none;transform:translateY(6px);transition:opacity var(--hcm-motion-fast) var(--hcm-motion-easing),transform var(--hcm-motion-fast) var(--hcm-motion-easing);z-index:2}` +
	`.timeline-frame.away .jump-newest{opacity:1;pointer-events:auto;transform:none}` +
	// C-20: reserve room for the pill while it is shown so it does not sit
	// on top of the newest message's text.
	`.timeline-frame.away .message-list{padding-bottom:52px}` +
	`.jump-newest .chat-icon{width:14px;height:14px}` +
	`.message-attachments{display:flex;flex-wrap:wrap;gap:8px;margin-top:6px}` +
	`.attachment-image{position:relative;margin:0;max-width:min(100%,360px);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);overflow:hidden;background:var(--canvas)}` +
	`.attachment-image-open{display:block;width:100%;height:100%;max-width:100%;padding:0;border:0;background:transparent;cursor:zoom-in}` +
	`.attachment-image-open:focus-visible{outline:3px solid var(--accent);outline-offset:-3px}` +
	// Round 3 C-4: alt text stays for screen readers but is never painted
	// over the tile while a thumbnail is pending or broken, and a src-less
	// <img> (fetch not started yet) is hidden so Chrome draws no broken glyph.
	`.attachment-image img{display:block;max-width:100%;max-height:320px;width:auto;height:auto;object-fit:cover;color:transparent}.attachment-image-open img:not([src]){visibility:hidden}` +
	`.attachment-image.measured .attachment-image-open img,.attachment-image.unmeasured .attachment-image-open img{width:100%;height:100%;object-fit:contain}` +
	`.attachment-image.unmeasured{width:160px;height:160px}.attachment-image.unmeasured img{width:100%;height:100%;object-fit:contain}` +
	`.attachment-image.unmeasured .attachment-pending{width:100%;height:100%}` +
	`.attachment-image.measured img{width:100%;height:100%;object-fit:contain}.attachment-image.measured .attachment-pending{width:100%;height:100%}` +
	`.attachment-pending{display:flex;align-items:center;justify-content:center;width:240px;height:160px;color:var(--muted)}` +
	`.attachment-pending:has(.attachment-download){flex-direction:column;gap:8px;text-align:center}.attachment-download{border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--accent);padding:5px 10px;font:inherit;cursor:pointer}.attachment-image>.attachment-download{position:absolute;inset-block-start:6px;inset-inline-end:6px;z-index:1;padding:3px 8px;font-size:.75rem;opacity:0;transition:opacity var(--hcm-motion-fast) var(--hcm-motion-easing)}.attachment-image:hover>.attachment-download,.attachment-image:focus-within>.attachment-download{opacity:1}@media(hover:none){.attachment-image>.attachment-download{opacity:1}}.attachment-download:disabled,.attachment-chip.unavailable:disabled{cursor:default;opacity:.6}.attachment-chip.unavailable{font:inherit;cursor:pointer}` +
	// C-2: the fallback tile stays in the tree, hidden, until
	// chatAttachmentImageErrorHandler marks the figure ".failed" on an <img>
	// load error -- swapping classes rather than replacing markup keeps this
	// safe under the reconciler.
	`.attachment-fallback{display:none}.attachment-image.failed .attachment-fallback{display:flex}.attachment-image.failed .attachment-image-open,.attachment-image.failed>.attachment-download{display:none}` +
	// Round 3 C-4 backstop, independent of the loader: an <img> that has
	// still not been given a src 20s after it mounted gets the unavailable
	// tile laid over it. The open button stays in layout underneath so the
	// loader's intersection observers can still see it; the moment a src
	// lands the :has() stops matching and the overlay goes away.
	`.attachment-image:not(.failed):has(>.attachment-image-open img:not([src]))>.attachment-fallback{display:flex;position:absolute;inset:0;z-index:1;width:auto;height:auto;padding:6px;background:var(--canvas);font-size:.75rem;visibility:hidden;animation:chat-attachment-stall 0s linear 20s forwards}` +
	`@keyframes chat-attachment-stall{to{visibility:visible}}` +
	`.attachment-badge{position:absolute;bottom:6px;inset-inline-start:6px;background:rgba(7,17,24,.7);color:#fff;font-size:.625rem;font-weight:700;letter-spacing:.04em;padding:2px 6px;border-radius:4px}` +
	`.attachment-chip{display:inline-flex;align-items:center;gap:8px;max-width:100%;padding:8px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--canvas);color:var(--ink);text-decoration:none;font-size:.875rem}` +
	`.attachment-chip:hover{border-color:var(--accent)}` +
	`.attachment-chip .chat-icon{width:16px;height:16px;color:var(--muted)}` +
	`.attachment-name{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.attachment-size{color:var(--muted);font-size:.75rem}` +
	`.attachment-chip.pending{opacity:.6}` +
	`.message-stats{background:none;border:1px solid transparent;color:var(--accent);font:inherit;font-size:.75rem;font-weight:600;padding:2px 6px;margin:4px 0 0 -6px;border-radius:var(--hcm-radius-control);cursor:pointer}` +
	`.message-stats:hover{border-color:var(--line);background:var(--surface)}` +
	`.message-actions{position:absolute;top:-12px;inset-inline-end:12px;display:flex;gap:2px;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-control);box-shadow:var(--hcm-shadow-raised);padding:2px;opacity:0;transition:opacity var(--hcm-motion-fast) var(--hcm-motion-easing);z-index:2}` +
	`.message:hover .message-actions,.message:focus-within .message-actions{opacity:1}` +
	// C-18: hover has no meaning on a touch screen, so the pointer:coarse
	// case is handled on its own rather than only at the 760px width
	// breakpoint (a touch laptop or tablet above that width was otherwise
	// stuck with a hover-only bar that never appeared). Only the "More
	// actions" trigger stays up, and it gets real button chrome -- a plain
	// muted glyph with no background is what read as a faint, easy-to-miss
	// "…" -- everything else opens through that menu instead of crowding
	// the message with row after row of tap targets.
	`@media(pointer:coarse){.message-actions{opacity:1;background:transparent;border:0;box-shadow:none}.message-actions .message-action:not([data-action="menu"]){display:none}.message-actions .message-action[data-action="menu"]{background:var(--surface);border:1px solid var(--line)}.message:focus-within .message-actions .message-action{display:inline-flex}}` +
	`.message-action{width:32px;height:32px;background:none;border:0;border-radius:var(--hcm-radius-xs);color:var(--muted);display:inline-flex;align-items:center;justify-content:center;cursor:pointer}` +
	`.message-action .chat-icon{width:16px;height:16px}` +
	// r5 C-1: the "More actions" trigger is the only way into the message
	// menu on touch, so it draws at full ink (a muted glyph measured 1.94:1),
	// and it grows to a 44px target under a coarse pointer.
	`.message-action[data-action="menu"]{color:var(--ink)}` +
	`.message-action[data-action="menu"] .chat-icon{stroke-width:3.25}` +
	`@media(pointer:coarse){.message-actions .message-action[data-action="menu"]{width:44px;height:44px}}` +
	// r5 C-2: keyboard hints are noise on touch screens.
	`@media(pointer:coarse){.kbd-hint{display:none}}` +
	`.message-action:hover{background:color-mix(in srgb,var(--ink) 8%,transparent);color:var(--ink)}` +
	`.message-action[aria-pressed="true"]{color:var(--accent)}` +
	`.message-action.danger:hover{color:var(--hcm-color-danger);background:var(--hcm-color-danger-surface)}` +
	`.message-action.danger{margin-inline-start:8px;position:relative}` +
	`.message-action.danger::before{content:"";position:absolute;inset-inline-start:-5px;top:6px;bottom:6px;width:1px;background:var(--line)}` +
	`.message-edit{display:grid;gap:8px;margin-top:4px}` +
	`.edit-input{border:1px solid var(--accent);border-radius:var(--hcm-radius-control);background:var(--surface);color:inherit;font:inherit;font-size:.9375rem;line-height:1.45;padding:8px 10px;resize:vertical;width:100%}` +
	`.edit-actions{display:flex;gap:8px}` +
	// composer
	`.chat-composer .composer-embeds{flex:none;width:100%}` +
	`.chat-composer{position:relative;flex:none;min-width:0;margin:4px 20px 20px;border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-surface);background:var(--surface);display:flex;flex-direction:column;gap:0;padding:7px 8px 6px;box-shadow:0 2px 12px color-mix(in srgb,var(--ink) 7%,transparent)}` +
	`.chat-composer:focus-within{border-color:var(--accent);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 22%,transparent)}` +
	`.composer-input{flex:1;border:0;background:transparent;color:inherit;font:inherit;font-size:.9375rem;line-height:1.45;padding:9px 6px;resize:none;min-height:36px;max-height:220px;width:100%;field-sizing:content}` +
	`.chat-composer .composer-input{display:block;flex:none;min-width:0;min-height:78px;max-height:min(220px,28vh);padding:8px 9px 10px;resize:vertical}` +
	`.composer-input:focus{outline:0}` +
	`.composer-toolbar{display:flex;align-items:center;gap:8px;padding:2px 6px 6px}` +
	`.chat-composer .composer-toolbar{min-width:0;min-height:44px;border-top:1px solid var(--line);padding:5px 2px 0}` +
	`.composer-tools{display:flex;gap:2px}` +
	`.chat-composer .composer-tools{align-items:center;min-width:0}` +
	`.emoji-control{position:relative;display:inline-flex;flex:none}` +
	`.emoji-picker{position:absolute;z-index:30;inset-block-end:calc(100% + 8px);inset-inline-start:0;display:grid;grid-template-columns:repeat(4,40px);gap:3px;padding:8px;border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface);box-shadow:var(--hcm-shadow-raised)}` +
	`.emoji-picker[hidden]{display:none}` +
	`.giphy-picker{position:absolute;z-index:31;inset-block-end:calc(100% + 8px);inset-inline-start:0;width:min(420px,calc(100vw - 32px));max-height:min(440px,70vh);overflow:auto;padding:10px;border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface);box-shadow:var(--hcm-shadow-raised)}` +
	`.giphy-picker[hidden],.giphy-more[hidden]{display:none}.giphy-query{width:100%;margin-bottom:8px}.giphy-results{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:5px}.giphy-result{min-width:0;min-height:72px;padding:0;overflow:hidden;border:0;border-radius:6px;background:var(--surface-muted);cursor:pointer}.giphy-result img{display:block;width:100%;height:100%;max-height:120px;object-fit:cover}.giphy-status{min-height:1.4em;color:var(--muted);font-size:.85rem}.giphy-more{margin-top:8px}.giphy-attribution{margin-top:8px;color:var(--muted);font-size:.75rem}.giphy-trigger-label{font-size:.7rem;font-weight:700}.giphy-unavailable{position:absolute;clip:rect(0 0 0 0);clip-path:inset(50%);height:1px;width:1px;overflow:hidden;white-space:nowrap}` +
	`.giphy-post-embed{display:flex;flex-direction:column;align-items:flex-start;width:fit-content;max-width:min(100%,480px);margin:8px 0 3px;overflow:hidden;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface-muted);color:var(--muted);text-decoration:none}` +
	`.giphy-post-embed-image{display:block;width:auto;min-width:160px;min-height:90px;max-width:100%;max-height:360px;object-fit:contain;background:var(--surface-muted)}` +
	`.giphy-post-embed-attribution{padding:4px 7px;font-size:.7rem;line-height:1.2}` +
	`.emoji-choice{display:inline-flex;align-items:center;justify-content:center;width:40px;height:40px;padding:0;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:inherit;font:inherit;font-size:1.25rem;cursor:pointer}` +
	`.emoji-choice:hover,.emoji-choice:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:-2px;background:var(--soft)}` +
	`.thread-composer .emoji-picker{inset-inline-start:0}` +
	// C-14: 32px brings the composer toolbar in line with the header's own
	// icon buttons (.icon-button is 34px; the toolbar's glyphs read small
	// and cramped at 30px next to them).
	`.tool-button{width:32px;height:32px;background:none;border:0;border-radius:var(--hcm-radius-control);color:var(--muted);display:inline-flex;align-items:center;justify-content:center;cursor:pointer}` +
	// Round 3 C-12: the GIF control is a ghost tool like its siblings; the
	// bordered surface box read as a disabled field beside them.
	`.giphy-trigger{width:auto;min-width:32px;padding:0 8px;gap:4px;border:0;background:none;color:var(--muted)}.giphy-trigger .giphy-trigger-label{font-size:.75rem;letter-spacing:.02em}` +
	`.giphy-trigger:hover:not(:disabled){color:var(--ink)}` +
	`.tool-button .chat-icon{width:16px;height:16px}` +
	`.composer-help{position:absolute;top:100%;inset-inline-end:8px;margin-top:3px;font-size:.6875rem;color:var(--muted);white-space:nowrap;opacity:0;transition:opacity var(--hcm-motion-fast) var(--hcm-motion-easing)}` +
	`.chat-composer:focus-within:has(.composer-input:placeholder-shown) .composer-help{opacity:1}` +
	`.send-button,.button{display:inline-flex;align-items:center;justify-content:center;gap:6px;background:var(--accent);color:var(--on-brand,#fff);border:1px solid transparent;border-radius:var(--hcm-radius-control);height:34px;min-height:34px;padding:0 12px;font:inherit;font-weight:600;font-size:.875rem;cursor:pointer;white-space:nowrap}` +
	`.send-button:hover,.button:hover{background:var(--accent-hover)}` +
	// C-14: a disabled Send filled like the enabled pill read as clickable
	// when it was not; it is transparent with muted ink and a hairline
	// border instead, matching how every other disabled control in this
	// stylesheet reads (opacity dims a ghost/outline state, not a solid one).
	`.send-button:disabled,.chat-composer:has(.composer-input:placeholder-shown) .send-button,.thread-composer:has(.composer-input:placeholder-shown) .send-button{background:transparent;border-color:var(--line);color:var(--muted);opacity:1}` +
	`.send-button{margin-inline-start:auto}` +
	`.send-button .chat-icon{width:16px;height:16px}` +
	`.chat-workspace[dir="rtl"] .icon-send,.chat-workspace[dir="rtl"] .icon-reply,.chat-workspace[dir="rtl"] .icon-chevron-right{transform:scaleX(-1)}` +
	`.button.secondary{background:var(--surface);color:var(--accent);border-color:var(--hcm-color-control-border)}` +
	`.button.secondary:hover{background:var(--soft)}` +
	`.button.small{height:28px;padding:0 10px;font-size:.8125rem}` +
	// side column
	`.chat-side{position:relative;display:flex;flex-direction:column;min-height:0;overflow-y:auto;overscroll-behavior:contain;border-inline-start:1px solid var(--line);background:var(--canvas);padding:0 16px 16px}` +
	`.chat-details.collapsed{display:none}` +
	`.side-heading{display:flex;align-items:center;justify-content:space-between;gap:8px;height:52px;flex:none;position:sticky;top:0;background:inherit;z-index:1}` +
	`.side-heading h2{font-size:1rem;margin:0}` +
	`.side-heading-actions{display:flex;gap:4px;align-items:center}` +
	`.details-summary{text-align:center;padding:8px 0 16px}` +
	`.details-summary h3{margin:10px 0 2px;font-size:1rem}` +
	`.details-section{border-top:1px solid var(--line);padding:12px 0}` +
	`.details-section-head{display:flex;align-items:center;justify-content:space-between}.details-section-actions{display:flex;align-items:center;gap:2px;margin-inline-start:auto}` +
	`.details-section h3{font-size:.8125rem;margin:0;color:var(--muted);font-weight:600}` +
	`.member-filter{position:relative;margin:8px 0 2px}` +
	`.member-filter .chat-icon{position:absolute;inset-inline-start:10px;top:50%;transform:translateY(-50%);width:14px;height:14px;color:var(--muted);pointer-events:none}` +
	`.member-filter .chat-search{width:100%;height:32px;padding-inline-start:30px;font-size:.8125rem}` +
	`.thread-root-body{min-width:0;flex:1}` +
	`.member-list{list-style:none;margin:6px 0 0;padding:0}` +
	`.member-row{display:flex;align-items:center;gap:8px;padding:6px 0;font-size:.875rem}` +
	`.person-avatar-button,.person-name,.conversation-person-name,.member-person-button{border:0;background:transparent;color:inherit;font:inherit;cursor:pointer;padding:0;text-align:start}` +
	`.person-avatar-button{display:inline-flex;flex:none;border-radius:50%}` +
	// Round 3 C-17: the underline is a hover cue only; on touch a tap left
	// :hover stuck on the tapped name ("Anika Desai" underlined alone).
	`@media(hover:hover){.person-name:hover,.conversation-person-name:hover,.member-person-button:hover .member-name{text-decoration:underline}}` +
	`.person-avatar-button:focus-visible,.person-name:focus-visible,.conversation-person-name:focus-visible,.member-person-button:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.member-person-button{display:flex;align-items:center;gap:8px;min-width:0;flex:1}` +
	`.conversation-person-name{font-size:inherit;font-weight:inherit;line-height:inherit}` +
	`.person-summary .large-avatar{margin-inline:auto}` +
	`.person-summary .button{margin-top:12px}` +
	`.person-detail-list{border-top:1px solid var(--line);margin:0;padding:8px 0}` +
	`.person-detail-field{padding:9px 0;border-bottom:1px solid var(--line);overflow-wrap:anywhere}` +
	`.person-detail-label{display:block;color:var(--muted);font-size:.75rem;font-weight:600}` +
	`.person-detail-value{display:block;margin-top:3px;font-size:.875rem}` +
	`.person-detail-link{display:inline-flex;align-items:center;gap:8px;min-width:0;max-width:100%;margin-top:3px;padding:0;border:0;background:none;color:var(--accent);font:inherit;text-align:start;text-decoration:underline;text-underline-offset:.14em;cursor:pointer}` +
	`.person-detail-link:focus-visible{outline:2px solid var(--hcm-color-focus,var(--accent));outline-offset:2px;border-radius:2px}` +
	`.person-detail-avatar{width:26px;height:26px;flex:none}` +
	`.person-detail-name{min-width:0;overflow-wrap:anywhere}` +
	`.person-detail-report-list{display:grid;gap:4px;list-style:none;margin:4px 0 0;padding:0}` +
	`.person-detail-report-list .person-detail-link{margin:0}` +
	`.person-detail-status{padding:8px 0;color:var(--muted);font-size:.875rem}` +
	`.person-org-chart-link{display:inline-flex;align-self:flex-start;margin:0 12px 10px;color:var(--accent);font-size:.875rem;text-decoration:underline;text-underline-offset:.14em}` +
	`.person-org-chart-link:focus-visible{outline:2px solid var(--hcm-color-focus,var(--accent));outline-offset:2px;border-radius:2px}` +
	`.member-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.member-status{font-size:.75rem;color:var(--muted)}` +
	`.member-status.online{color:var(--hcm-color-success)}` +
	`.thread-pane{padding-bottom:0}` +
	`.thread-scroll{flex:1;min-height:0;overflow-y:auto}` +
	// r5 C-4: rows scrolled under the header fade out over 16px instead of
	// being sliced mid-avatar, the thread's last reply clears the composer,
	// and the header paints its own surface so nothing shows through it.
	`.message-list{mask-image:linear-gradient(to bottom,transparent 0,#000 16px);-webkit-mask-image:linear-gradient(to bottom,transparent 0,#000 16px)}` +
	`.thread-scroll{padding-block-end:16px;scroll-padding-block-end:16px}` +
	`.conversation-header{position:relative;z-index:1;background:var(--surface)}` +
	`.thread-root{display:flex;gap:10px;padding:8px 0 12px;border-bottom:1px solid var(--line)}` +
	`.thread-replies{display:grid;gap:10px;padding-top:10px}` +
	`.thread-message{display:flex;gap:10px}` +
	`.thread-root-body,.thread-message-body{position:relative;min-width:0;flex:1}.thread-root-body .message-meta,.thread-message-body .message-meta{padding-inline-end:34px}.thread-more{position:absolute;top:0;inset-inline-end:0}.thread-root-body .message-menu,.thread-message-body .message-menu{top:30px;inset-inline-end:0}` +
	`.thread-empty{color:var(--muted);font-size:.875rem;margin:12px 0 0}` +
	`.thread-loading{display:grid;gap:8px;min-block-size:112px;margin:12px 0 0;align-content:start}` +
	`.thread-loading-skeleton{display:grid;gap:10px;padding-top:2px}` +
	`.thread-loading-row{display:flex;align-items:flex-start;gap:10px;min-block-size:42px}` +
	`.thread-loading-avatar{width:26px;height:26px;flex:none;border-radius:50%}` +
	`.thread-loading-lines{display:grid;align-content:start;gap:8px;flex:1;min-width:0;padding-top:3px}` +
	`.thread-loading-lines .chat-skeleton{display:block;width:min(100%,240px);height:9px;border-radius:999px}` +
	`.thread-loading-lines .chat-skeleton+ .chat-skeleton{width:min(72%,176px);height:11px}` +
	`.thread-composer{flex:none;margin:10px 0 12px;border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-surface);background:var(--surface);display:flex;flex-direction:column}` +
	`.thread-composer:focus-within{border-color:var(--accent);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 22%,transparent)}` +
	`.thread-composer .composer-input{font-size:.875rem;min-height:34px}` +
	`.thread-composer .composer-help{display:none}` +
	`.thread-composer .composer-toolbar{justify-content:flex-end}` +
	// states, notice, dialogs
	`.state-panel{margin:auto;width:min(100%,38ch);text-align:center;padding:24px;color:var(--muted)}` +
	`.state-panel h2{text-wrap:balance}` +
	`.message-list:has(>.state-panel)::before{flex:0}` +
	`.message-list:has(>.state-panel){justify-content:center}` +
	`.state-panel h2{font-size:1.0625rem;margin:10px 0 4px;color:var(--ink)}` +
	`.state-panel p{margin:0 0 12px;font-size:.875rem}` +
	`.empty-icon{width:48px;height:48px;border-radius:14px;background:var(--soft);color:var(--accent);display:inline-flex;align-items:center;justify-content:center}` +
	`.empty-icon .chat-icon{width:22px;height:22px}` +
	`.skeleton-lines{display:grid;gap:8px;width:220px;margin:0 auto 12px}` +
	`.chat-skeleton,.skeleton-lines span,.rail-skeleton span{position:relative;display:block;overflow:hidden;background:color-mix(in srgb,var(--ink) 8%,transparent)}` +
	`.chat-skeleton::after,.skeleton-lines span::after,.rail-skeleton span::after{position:absolute;inset:0;content:"";transform:translateX(-100%);background:linear-gradient(90deg,transparent,color-mix(in srgb,var(--surface) 82%,transparent),transparent)}` +
	`.skeleton-lines span,.rail-skeleton span{height:12px;border-radius:6px}` +
	`.skeleton-lines span:nth-child(2){width:70%}.skeleton-lines span:nth-child(3){width:85%}.rail-skeleton span:nth-child(odd){width:75%}` +
	`@keyframes chat-skeleton-shimmer{to{transform:translateX(100%)}}` +
	`@media (prefers-reduced-motion:no-preference){:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace :is(.chat-skeleton,.skeleton-lines span,.rail-skeleton span)::after{animation:chat-skeleton-shimmer 1.6s ease-in-out infinite}}` +
	`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace[dir="rtl"] :is(.chat-skeleton,.skeleton-lines span,.rail-skeleton span)::after{animation-direction:reverse}` +
	`.chat-notice{flex:none;display:flex;align-items:center;gap:8px;margin:4px 20px 0;padding:6px 6px 6px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--canvas);color:var(--ink);font-size:.8125rem}` +
	`.chat-notice>span{flex:1;min-width:0}` +
	`.chat-notice .icon-button{width:32px;height:32px}` +
	`.chat-notice .button{height:26px;padding:0 10px;font-size:.75rem}` +
	`.chat-dialog-backdrop{position:fixed;inset:0;background:rgba(7,17,24,.5);display:flex;align-items:center;justify-content:center;z-index:30;padding:16px}` +
	`.join-channel-backdrop{z-index:31}` +
	`.chat-dialog{position:static;margin:0;width:min(460px,100%);max-height:calc(100dvh - 32px);overflow:auto;background:var(--surface);color:var(--ink);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-raised);padding:0 20px 20px}` +
	`.chat-dialog::backdrop{background:transparent}` +
	`.dialog-form{display:grid;gap:12px}` +
	`.dialog-form:has(#new-chat-name:placeholder-shown) .dialog-actions .button:not(.secondary){background:color-mix(in srgb,var(--ink) 10%,transparent);color:var(--muted);opacity:.55;pointer-events:none}` +
	`.dialog-actions{display:flex;justify-content:flex-end;gap:8px;padding-top:4px}` +
	`.dialog-empty{display:grid;justify-items:center;gap:6px;text-align:center;padding:12px 8px 8px;color:var(--muted);font-size:.875rem}` +
	`.dialog-empty .button{margin-top:8px}` +
	`.browse-list{list-style:none;margin:0;padding:0;display:grid;gap:2px}` +
	`.browse-row{display:flex;align-items:center;gap:10px;padding:10px 8px;border-bottom:1px solid var(--line)}.browse-row:last-child{border-bottom:0}` +
	`.browse-row:hover{background:color-mix(in srgb,var(--ink) 6%,transparent)}` +
	`.share-dialog{display:grid;gap:12px;max-height:min(760px,calc(100dvh - 32px));overflow:auto}.share-preview{padding:12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--soft);overflow:auto;max-height:120px;overflow-wrap:anywhere}.share-preview p{margin:4px 0 0;white-space:pre-wrap}.share-caution,.share-disclosure{color:var(--muted);font-size:.8125rem}.share-error{color:var(--hcm-color-danger)}.share-list{list-style:none;margin:0;padding:0;display:grid;gap:4px;max-height:250px;overflow-y:auto}.share-choice{width:100%;display:flex;align-items:center;gap:10px;padding:10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;text-align:start;cursor:pointer}.share-choice:hover,.share-choice:focus-visible,.share-choice[aria-pressed=true]{border-color:var(--accent);background:var(--soft)}.share-choice-kind{margin-inline-start:auto;color:var(--muted);font-size:.75rem}` +
	`.browse-text{flex:1;min-width:0;display:grid}` +
	`.browse-text strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.browse-meta,.joined-label{font-size:.75rem;color:var(--muted)}` +
	`.sr-only,.chat-skip{position:absolute;clip:rect(0 0 0 0);clip-path:inset(50%);height:1px;overflow:hidden;width:1px;white-space:nowrap}` +
	`.chat-skip:focus{background:var(--surface);clip:auto;clip-path:none;height:auto;inset-inline-start:10px;padding:8px;top:10px;width:auto;z-index:10}` +
	`.mobile-chat-toggle,.mobile-chat-close,.chat-scrim{display:none}` +
	// responsive
	`@media(max-width:1180px){.composer-help{display:none}.chat-workspace{--chat-rail:232px}}` +
	`.chat-workspace{container:chat/inline-size}.chat-main{container:chatmain/inline-size}` +
	// The side column (thread, details, person) takes its own grid track so it
	// never covers the timeline. The query reads the workspace's own width, not
	// the viewport's: an expanded product nav can leave a 1440px window with an
	// 800px chat. Below 1100px of chat the conversation list steps aside while
	// a side column is open, the way Slack folds its sidebar, and the header's
	// Conversations button brings it back as a drawer.
	`@container chat (max-width:1350px){.chat-workspace[data-details-open="true"] .chat-layout{grid-template-columns:minmax(200px,var(--chat-rail)) minmax(0,1fr) minmax(280px,340px)}}` +
	`@container chat (max-width:1100px) and (min-width:761px){.chat-workspace[data-details-open="true"] .chat-layout{grid-template-columns:minmax(0,1fr) minmax(280px,360px)}.chat-workspace[data-details-open="true"] .chat-rail{display:none}.chat-workspace[data-details-open="true"] .rail-pill.mobile-chat-toggle{display:inline-flex}.chat-workspace[data-details-open="true"][data-sidebar-open="true"] .chat-rail{display:flex;position:fixed;inset:0 auto 0 0;width:min(85vw,320px);z-index:1200;box-shadow:var(--hcm-shadow-raised)}.chat-workspace[data-details-open="true"][data-sidebar-open="true"] .chat-scrim{display:block;position:fixed;inset:0;background:color-mix(in srgb,var(--ink) 55%,transparent);border:0;z-index:1199;cursor:pointer}.chat-workspace[data-details-open="true"][data-sidebar-open="true"] .mobile-chat-close{display:inline-flex}}` +
	`@container chat (max-width:760px){.chat-workspace[data-details-open="true"] .chat-layout{grid-template-columns:minmax(0,1fr)}.chat-workspace[data-details-open="true"] .chat-rail{display:none}.chat-workspace[data-details-open="true"] .rail-pill.mobile-chat-toggle{display:inline-flex}.chat-side{position:absolute;inset:0 0 0 auto;width:min(380px,100%);z-index:6;box-shadow:-12px 0 32px color-mix(in srgb,var(--ink) 22%,transparent)}}` +
	`@media(max-width:1350px){.chat-side .pane-handle{display:none}}` +
	`@media(max-width:760px){.pane-handle{display:none}}` +
	`@media(max-width:760px){.chat-layout,.chat-workspace[data-details-open="true"] .chat-layout{grid-template-columns:minmax(0,1fr)}.chat-rail{display:none}.mobile-chat-toggle,.mobile-chat-close{display:inline-flex}.rail-pill.mobile-chat-toggle{display:inline-flex}` +
	`.chat-workspace[data-sidebar-open="true"] .chat-rail{display:flex;position:fixed;inset:0 auto 0 0;width:min(85vw,320px);z-index:1200;box-shadow:var(--hcm-shadow-raised)}` +
	`.chat-workspace[data-sidebar-open="true"] .chat-scrim{display:block;position:fixed;inset:0;background:color-mix(in srgb,var(--ink) 55%,transparent);border:0;z-index:1199;cursor:pointer}` +
	// Mobile search fix: the rail's own search box lives inside the fixed
	// drawer, so a result list rendered in the main column (untouched
	// stacking order) sat visually behind it. Rather than close the drawer
	// on every keystroke -- the input closing would blur itself mid-search
	// -- results are lifted above the drawer instead, the way the finding's
	// second option suggests ("show results inside the drawer").
	`.chat-workspace[data-sidebar-open="true"] .chat-search-results{position:fixed;inset:0;z-index:1201;background:var(--surface);padding-top:calc(20px + env(safe-area-inset-top,0px))}` +
	`.chat-row,.rail-link{height:40px}.icon-button{width:40px;height:40px}.channel-todo-trigger{height:40px}.conversation-header{padding:0 8px 0 8px}.conversation-header.empty{height:52px;border-bottom:1px solid var(--line);padding:0 8px}.message{padding:6px 12px;margin-inline:-12px}.message-list{padding-inline:12px}.chat-composer{margin:4px 12px 12px}` +
	`.message-actions{position:absolute;top:2px;inset-inline-end:8px;display:flex;opacity:1;border:0;box-shadow:none;background:transparent;padding:0;justify-content:flex-end}.message-actions .message-action:not([data-action="menu"]){display:none}.message:focus-within .message-actions{background:var(--surface)}.message:focus-within .message-actions .message-action{display:inline-flex}.message-meta{padding-inline-end:44px}.message-action{width:40px;height:40px}.gutter-time{display:none}.message.continued{grid-template-columns:minmax(0,1fr);padding-inline-start:58px}.send-button{height:44px;padding:0 16px}.composer-toolbar{padding:4px 8px 8px}.tool-button{width:40px;height:40px}.message:hover{background:transparent}.message:focus-within{background:color-mix(in srgb,var(--ink) 5%,transparent)}.send-button{margin-inline-start:auto}` +
	`.chat-side{width:100%}.chat-notice{margin-inline:12px}}` +
	// Keep the active conversation title on its own row at phone widths. The
	// shortcuts wrap below it so they remain visible without squeezing the title
	// to zero or clipping the final action at 320px.
	`@media(max-width:760px){.conversation-header{height:auto;min-height:52px;flex-wrap:wrap;align-content:center;row-gap:var(--hcm-space-1);column-gap:var(--hcm-space-2);padding-block:var(--hcm-space-1)}.conversation-header.empty{height:52px;min-height:52px}.conversation-header>.mobile-chat-toggle{order:1;flex:0 0 auto}.conversation-header>.conversation-title{order:2;flex:1 1 0%;min-inline-size:0}.conversation-header>.conversation-actions{order:3;flex:0 0 100%;justify-content:flex-end;flex-wrap:wrap;gap:var(--hcm-space-1);min-inline-size:0}.conversation-header .channel-todo-trigger,.conversation-header .channel-poll-trigger{flex:none}}` +
	`@media(max-width:760px){.chat-composer{max-width:none;margin-bottom:12px;padding:5px 6px 6px}.chat-composer .composer-input{min-height:68px;max-height:min(140px,24vh);padding:9px 7px}.chat-composer .composer-toolbar{padding-top:4px}.composer-help{display:none}.chat-composer .send-button{min-width:40px;height:40px;padding:0 12px}.chat-composer .send-label{display:inline}.reaction{height:32px;padding:0 11px;font-size:.8125rem}.jump-newest{bottom:12px;inset-inline-end:12px}.message-menu{inset-inline-end:8px}}` +
	`@media(max-width:350px){.channel-todo-trigger-label,.channel-poll-trigger-label{display:none}.chat-composer .send-button{width:40px;padding:0}.chat-composer .send-label{display:none}}` +
	`@media(prefers-reduced-motion:reduce){.message-actions,.jump-newest,.composer-help{transition:none}.chat-workspace :is(.chat-skeleton,.skeleton-lines span,.rail-skeleton span)::after{animation:none;content:none}}` +
	`:root[data-hcm-motion-preference="reduce"] .chat-workspace :is(.chat-skeleton,.skeleton-lines span,.rail-skeleton span)::after,:root[data-hcm-motion-preference="limited"] .chat-workspace :is(.chat-skeleton,.skeleton-lines span,.rail-skeleton span)::after{animation:none;content:none}` +
	`@media(prefers-reduced-motion:no-preference){:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace :is(.chat-row,.rail-link,.pinned-link,.search-result,.reaction,.message-action,.tool-button,.send-button,.button,.icon-button,.menu-item,.emoji-choice,.picker-emoji,.chat-embed-link,.attachment-chip,.attachment-download,.person-avatar-button,.member-person-button){transition:background-color var(--hcm-motion-fast) var(--hcm-motion-easing),color var(--hcm-motion-fast) var(--hcm-motion-easing),border-color var(--hcm-motion-fast) var(--hcm-motion-easing),box-shadow var(--hcm-motion-fast) var(--hcm-motion-easing),transform var(--hcm-motion-fast) var(--hcm-motion-easing)}` +
	`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace :is(.rail-row-menu,.message-menu,.reaction-picker,.emoji-picker:not([hidden]),.giphy-picker:not([hidden])){animation:chat-popover-in var(--hcm-motion-fast) var(--hcm-motion-easing) both}` +
	`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace .chat-details{transition:opacity var(--hcm-motion-normal) var(--hcm-motion-easing),transform var(--hcm-motion-normal) var(--hcm-motion-easing),display var(--hcm-motion-normal) allow-discrete}` +
	`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace .chat-details.collapsed{opacity:0;transform:translateX(var(--hcm-space-2))}` +
	`@starting-style{:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace .chat-details:not(.collapsed){opacity:0;transform:translateX(var(--hcm-space-2))}}` +
	`@media(max-width:760px){:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace .chat-rail{transition:transform var(--hcm-motion-normal) var(--hcm-motion-easing),display var(--hcm-motion-normal) allow-discrete;transform:translateX(calc(0px - var(--hcm-space-2)))}:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace[data-sidebar-open="true"] .chat-rail{transform:none}:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace .chat-scrim{opacity:0;transition:opacity var(--hcm-motion-fast) var(--hcm-motion-easing)}:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace[data-sidebar-open="true"] .chat-scrim{opacity:1}}` +
	`@media(max-width:760px){@starting-style{:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .chat-workspace[data-sidebar-open="true"] .chat-rail{transform:translateX(calc(0px - var(--hcm-space-2)))}}}` +
	`@keyframes chat-popover-in{from{opacity:0;transform:translateY(var(--hcm-space-1)) scale(.99);background-color:var(--surface);border-color:var(--line);box-shadow:var(--hcm-shadow-raised)}to{opacity:1;transform:translateY(0) scale(1);background-color:var(--surface);border-color:var(--line);box-shadow:var(--hcm-shadow-raised)}}}` +
	`@media(prefers-reduced-motion:reduce){.chat-workspace :is(.chat-row,.rail-link,.pinned-link,.search-result,.reaction,.message-action,.tool-button,.send-button,.button,.icon-button,.menu-item,.emoji-choice,.picker-emoji,.chat-embed-link,.attachment-chip,.attachment-download,.person-avatar-button,.member-person-button,.chat-details,.chat-rail,.chat-scrim){transition:none;animation:none;transform:none}.chat-workspace :is(.rail-row-menu,.message-menu,.reaction-picker,.emoji-picker,.giphy-picker){animation:none}}` +
	`:root[data-hcm-motion-preference="reduce"] .chat-workspace :is(.chat-row,.rail-link,.pinned-link,.search-result,.reaction,.message-action,.message-actions,.jump-newest,.composer-help,.tool-button,.send-button,.button,.icon-button,.menu-item,.emoji-choice,.picker-emoji,.chat-embed-link,.attachment-chip,.attachment-download,.person-avatar-button,.member-person-button,.chat-details,.chat-rail,.chat-scrim),:root[data-hcm-motion-preference="limited"] .chat-workspace :is(.chat-row,.rail-link,.pinned-link,.search-result,.reaction,.message-action,.message-actions,.jump-newest,.composer-help,.tool-button,.send-button,.button,.icon-button,.menu-item,.emoji-choice,.picker-emoji,.chat-embed-link,.attachment-chip,.attachment-download,.person-avatar-button,.member-person-button,.chat-details,.chat-rail,.chat-scrim){transition:none;animation:none;transform:none}` +
	`:root[data-hcm-motion-preference="reduce"] .chat-workspace :is(.rail-row-menu,.message-menu,.reaction-picker,.emoji-picker,.giphy-picker),:root[data-hcm-motion-preference="limited"] .chat-workspace :is(.rail-row-menu,.message-menu,.reaction-picker,.emoji-picker,.giphy-picker){animation:none}` +
	composerPolishStyles + surfaceStyles + projectEmbedStyles + journeyEmbedStyles

func paneStyle(p PaneSizes) map[string]string {
	out := map[string]string{}
	if p.Rail > 0 {
		out["--chat-rail"] = strconv.Itoa(p.Rail) + "px"
	}
	if p.Details > 0 {
		out["--chat-details"] = strconv.Itoa(p.Details) + "px"
	}
	return out
}
