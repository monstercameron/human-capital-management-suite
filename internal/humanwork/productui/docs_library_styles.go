package productui

// docsLibraryStylesheet styles the Docs library and document page. The one
// loud element is the status edge: every row and the document header carry
// a stripe in the colour of who can read it (grey private, brand shared,
// info official), so a list of hundreds reads by trust at a glance.
// Layout answers the space the library actually has (container queries),
// not the window, because the product navigation can take a third of it.
func docsLibraryStylesheet() string {
	return `
.docs-hub{width:100%;display:block;padding-block:var(--hcm-space-1) var(--hcm-space-4);container:docslib/inline-size}
.docs-hub-stack{display:grid;gap:var(--hcm-space-3)}
.docs-kind-private{--docs-kind:color-mix(in srgb,var(--muted) 75%,transparent)}
.docs-kind-shared{--docs-kind:var(--accent)}
.docs-kind-team_official,.docs-kind-channel_official{--docs-kind:var(--hcm-color-info)}

.docs-library{display:grid;grid-template-columns:14.5rem minmax(0,1fr);gap:var(--hcm-space-3);align-items:start}
.docs-nav{position:sticky;top:var(--hcm-space-2);display:grid;gap:var(--hcm-space-1);min-width:0}
.docs-nav-list{list-style:none;margin:0;padding:0;display:grid;gap:2px}
.docs-nav-link{display:flex;align-items:center;gap:var(--hcm-space-1);min-height:2.25rem;padding-inline:var(--hcm-space-1);border-radius:var(--hcm-radius-control);color:var(--ink);text-decoration:none;font-weight:500}
.docs-nav-link:hover{background:var(--hcm-hover-surface,var(--soft))}
.docs-nav-link[aria-current=page]{background:var(--hcm-color-brand-soft);color:var(--accent);font-weight:650}
.docs-nav-link:focus-visible,.docs-nav-add:focus-visible,.docs-title-link:focus-visible,.docs-star:focus-visible,.docs-owner-filter:focus-visible,.docs-chip-clear:focus-visible,.docs-move-option:focus-visible,.docs-dialog-close:focus-visible,.docs-access-remove:focus-visible,.docs-pick-remove:focus-visible,.docs-bulk-clear:focus-visible,.docs-outline a:focus-visible,.docs-crumbs a:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-nav-icon{width:1.05rem;height:1.05rem;flex:none;color:var(--muted)}
.docs-nav-link[aria-current=page] .docs-nav-icon{color:currentColor}
.docs-nav-label{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.docs-nav-count{flex:none;color:var(--muted);font-size:var(--hcm-font-size-small);font-variant-numeric:tabular-nums}
/* The info icon belongs to the "Folders" label; only "+" goes to the end
   (D-9). space-between had floated the info icon mid-row. */
.docs-nav-section{display:flex;align-items:center;gap:.35rem;margin-block-start:var(--hcm-space-2);padding-inline:var(--hcm-space-1)}
.docs-nav-section>.docs-nav-add{margin-inline-start:auto}
.docs-nav-section :is(h2,h3){margin:0;font-size:var(--hcm-font-size-small);font-weight:650;color:var(--muted)}
.docs-nav-add,.docs-nav-info{display:inline-grid;place-items:center;width:1.75rem;height:1.75rem;padding:0;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--muted);cursor:pointer}
.docs-nav-add:hover,.docs-nav-info:hover{background:var(--hcm-hover-surface,var(--soft));color:var(--ink)}
.docs-nav-info{width:1.5rem;height:1.5rem}
.docs-nav-info .docs-nav-icon{width:.9rem;height:.9rem}
.docs-folder-item{position:relative;display:flex;align-items:center}
.docs-folder-item>.docs-nav-link{flex:1;min-width:0}
.docs-folder-menu{position:absolute;inset-inline-end:2px;opacity:0}
.docs-folder-item:hover .docs-folder-menu,.docs-folder-item:focus-within .docs-folder-menu,.docs-folder-menu[open]{opacity:1}
.docs-folder-item:hover .docs-nav-count,.docs-folder-item:focus-within .docs-nav-count{visibility:hidden}
.docs-folder-empty{padding:var(--hcm-space-1);color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-folder-form-row,.docs-folder-confirm{padding:var(--hcm-space-1);border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface)}
.docs-folder-form{display:grid;gap:var(--hcm-space-1)}
.docs-folder-form input{width:100%;box-sizing:border-box;min-height:2.25rem;padding-inline:var(--hcm-space-1);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-folder-form-actions{display:flex;gap:var(--hcm-space-1);flex-wrap:wrap}
.docs-folder-confirm p{margin:0 0 var(--hcm-space-1);font-size:var(--hcm-font-size-small)}
.docs-danger{color:var(--hcm-color-danger,var(--danger))}
.docs-nav-note{margin:var(--hcm-space-1) 0 0;padding-inline:var(--hcm-space-1);color:var(--muted);font-size:var(--hcm-font-size-small);line-height:1.45}

.docs-main{display:grid;gap:var(--hcm-space-2);min-width:0}
.docs-main-head{display:flex;align-items:flex-end;justify-content:space-between;gap:var(--hcm-space-2);flex-wrap:wrap}
.docs-main-title{display:flex;align-items:flex-start;gap:var(--hcm-space-2);min-width:0}
.docs-main-title-block{display:grid;gap:.2rem;min-width:0}
.docs-main-title h1{margin:0;font-size:1.5rem;font-weight:700;line-height:1.25;overflow-wrap:anywhere}
.docs-main-title .docs-total{font-size:1rem;font-weight:500;color:var(--muted)}
.docs-total{color:var(--muted);font-size:var(--hcm-font-size-small);font-variant-numeric:tabular-nums;white-space:nowrap}
.docs-main-actions{display:flex;gap:var(--hcm-space-1)}
.docs-dialog-wide{width:min(46rem,100%)}
.docs-dialog .docs-create-form{max-width:none;display:grid;grid-template-rows:minmax(0,1fr) auto;gap:0;padding:0;overflow:hidden}
.docs-create-scroll{display:grid;gap:var(--hcm-space-2);padding:var(--hcm-space-2) var(--hcm-space-3);overflow:auto;min-height:0}
.docs-create-actions{display:flex;justify-content:flex-end;gap:var(--hcm-space-1);padding:var(--hcm-space-2) var(--hcm-space-3);border-block-start:1px solid var(--line)}
.docs-dialog .docs-create-form textarea{min-height:min(16lh,38vh)}
.docs-toolbar-stack{display:grid;gap:var(--hcm-space-1)}
.docs-toolbar{display:flex;align-items:center;gap:var(--hcm-space-2);flex-wrap:wrap}
/* The search box gives way before the two selects wrap: with a 20rem basis
   the German labels pushed "Sortieren" alone onto a second row (D-16). */
.docs-find{position:relative;flex:1 1 14rem;display:flex;align-items:center;min-width:0;max-width:40rem}
.docs-find-icon{position:absolute;inset-inline-start:.7rem;width:1rem;height:1rem;color:var(--muted);pointer-events:none}
.docs-find input[type=search]{width:100%;box-sizing:border-box;min-height:var(--hcm-control-height);padding-block:0;padding-inline:2.2rem var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
/* The native clear button painted an off-token navy (#294A7E, D-12). The
   CSP allows no data: images, so the x is two gradient strokes in the
   theme's muted ink, ink on hover. */
.docs-find input[type=search]::-webkit-search-cancel-button{-webkit-appearance:none;appearance:none;width:.85rem;height:.85rem;margin-inline-start:var(--hcm-space-1);cursor:pointer;background:linear-gradient(45deg,transparent calc(50% - 1px),var(--muted) calc(50% - 1px) calc(50% + 1px),transparent calc(50% + 1px)),linear-gradient(-45deg,transparent calc(50% - 1px),var(--muted) calc(50% - 1px) calc(50% + 1px),transparent calc(50% + 1px))}
.docs-find input[type=search]::-webkit-search-cancel-button:hover{background:linear-gradient(45deg,transparent calc(50% - 1px),var(--ink) calc(50% - 1px) calc(50% + 1px),transparent calc(50% + 1px)),linear-gradient(-45deg,transparent calc(50% - 1px),var(--ink) calc(50% - 1px) calc(50% + 1px),transparent calc(50% + 1px))}
.docs-sort{display:inline-flex;align-items:center;gap:var(--hcm-space-1);color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-sort select{min-height:var(--hcm-control-height);max-width:13rem;padding-inline:var(--hcm-space-1);font:inherit;font-size:var(--hcm-font-size-body);color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);text-overflow:ellipsis}
.docs-chips{display:flex;gap:var(--hcm-space-1);flex-wrap:wrap}
.docs-chip{display:inline-flex;align-items:center;gap:.35rem;min-height:1.9rem;padding-inline:.65rem .25rem;border:1px solid var(--line);border-radius:999px;background:var(--surface);font-size:var(--hcm-font-size-small)}
.docs-chip-label{color:var(--muted)}
.docs-chip-value{font-weight:600;max-width:18rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.docs-chip-clear{display:inline-grid;place-items:center;width:1.5rem;height:1.5rem;border-radius:999px;color:var(--muted)}
.docs-chip-clear:hover{background:var(--hcm-hover-surface,var(--soft));color:var(--ink)}
.docs-chip-icon{width:.8rem;height:.8rem}
.docs-bulk{display:flex;align-items:center;gap:var(--hcm-space-1);flex-wrap:wrap;padding:var(--hcm-space-1) var(--hcm-space-2);border-radius:var(--hcm-radius-control);background:var(--hcm-color-brand-soft)}
.docs-bulk-count{font-weight:650;margin-inline-end:var(--hcm-space-1);font-variant-numeric:tabular-nums}
.docs-bulk-clear{margin-inline-start:auto;border:0;background:transparent;color:var(--accent);font:inherit;font-weight:600;cursor:pointer;padding:var(--hcm-space-1)}
.docs-button-icon{width:1rem;height:1rem;flex:none}
.button .docs-button-icon{margin-inline-end:.35rem}

.docs-table-wrap{position:relative;display:grid;gap:var(--hcm-space-2)}
.docs-table-wrap.is-refreshing .docs-table{opacity:.55;transition:opacity var(--hcm-motion-fast,.14s) linear .12s}
.docs-table-wrap.is-refreshing::before{content:"";position:absolute;inset-inline-start:0;top:0;width:40%;height:2px;z-index:2;border-radius:2px;background:linear-gradient(90deg,transparent,var(--accent),transparent);animation:docs-progress 1s linear infinite}
@keyframes docs-progress{from{transform:translateX(0)}to{transform:translateX(150%)}}
.docs-table{border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface)}
.docs-row-head{border-start-start-radius:var(--hcm-radius-surface);border-start-end-radius:var(--hcm-radius-surface)}
.docs-row:last-child{border-end-start-radius:var(--hcm-radius-surface);border-end-end-radius:var(--hcm-radius-surface)}
.docs-row{display:grid;grid-template-columns:2.5rem minmax(0,1fr) minmax(7rem,9rem) 5.75rem 5.5rem 2.75rem;align-items:center;min-height:3rem}
.docs-row+.docs-row{border-block-start:1px solid var(--line)}
.docs-sort-link{display:inline-flex;align-items:center;gap:.3rem;min-height:2rem;color:inherit;text-decoration:none;border-radius:var(--hcm-radius-control);padding-inline:.25rem;margin-inline:-.25rem}
.docs-sort-link:hover{color:var(--ink);background:color-mix(in srgb,var(--ink) 6%,transparent)}
.docs-sort-link:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:1px}
[aria-sort=ascending]>.docs-sort-link,[aria-sort=descending]>.docs-sort-link{color:var(--ink)}
.docs-sort-arrow{font-size:var(--hcm-font-size-xsmall);color:var(--accent)}
.docs-row-head{position:sticky;top:0;z-index:2;min-height:2.4rem;border-inline-start-color:transparent;background:var(--surface-subtle,var(--soft));color:var(--muted);font-size:var(--hcm-font-size-small);font-weight:600}
.docs-row:not(.docs-row-head):hover{background:var(--hcm-hover-surface,var(--soft))}
.docs-row.is-selected{background:var(--hcm-color-brand-soft)}
.docs-cell{display:flex;align-items:center;gap:var(--hcm-space-1);min-width:0;padding-inline:var(--hcm-space-1)}
.docs-cell-select{justify-content:center;padding-inline:0}
.docs-cell-select input{width:1rem;height:1rem;margin:0;accent-color:var(--accent);cursor:pointer}
.docs-cell-title{gap:.4rem}
.docs-row-phone-meta{display:none;flex-basis:100%;margin-block-start:.15rem;color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-title-stack{display:flex;align-items:center;gap:var(--hcm-space-1);min-width:0}
.docs-title-stack[data-lines="2"]{flex-direction:column;align-items:flex-start;gap:0;padding-block:.3rem}
.docs-title-line{display:flex;align-items:center;gap:var(--hcm-space-1);min-width:0;max-width:100%}
.docs-sub{display:flex;flex-wrap:wrap;gap:0 var(--hcm-space-2);color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-sub-scope{color:var(--hcm-color-info);font-weight:600}
.docs-title-link{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink);font-weight:600;text-decoration:none;padding-block:.35rem;unicode-bidi:plaintext}
.docs-title-link:hover{color:var(--accent);text-decoration:underline;text-underline-offset:.18em}
.docs-folder-tag{display:inline-flex;align-items:center;gap:.25rem;flex:none;max-width:10rem;padding:.05rem .45rem;border-radius:999px;background:var(--surface-subtle,var(--soft));color:var(--muted);font-size:var(--hcm-font-size-small);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.docs-tag-icon{width:.8rem;height:.8rem;flex:none}
.docs-star{display:inline-grid;place-items:center;flex:none;width:1.75rem;height:1.75rem;padding:0;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--muted);cursor:pointer}
.docs-star-icon{width:1rem;height:1rem}
.docs-star[aria-pressed=true]{color:var(--hcm-color-warning,var(--warning))}
.docs-star[aria-pressed=true] .docs-star-icon{fill:currentColor}
.docs-row .docs-star[aria-pressed=false]{opacity:0}
.docs-row:hover .docs-star,.docs-row:focus-within .docs-star{opacity:1}
.docs-row .docs-row-menu-trigger{opacity:0}
.docs-row:hover .docs-row-menu-trigger,.docs-row:focus-within .docs-row-menu-trigger,.docs-row .docs-row-menu[open]>.docs-row-menu-trigger{opacity:1}
.docs-star:hover{background:color-mix(in srgb,var(--ink) 7%,transparent);color:var(--ink)}
.docs-star-lg{width:2.5rem;height:2.5rem;border:1px solid var(--control-border)}
.docs-star-lg .docs-star-icon{width:1.2rem;height:1.2rem}
.docs-cell-owner .avatar,.docs-fact .avatar{flex:none;width:1.6rem;height:1.6rem;min-width:1.6rem}
.docs-owner-name{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink);font:inherit;font-size:var(--hcm-font-size-small);unicode-bidi:plaintext}
.docs-owner-filter{padding:0;border:0;background:transparent;cursor:pointer;text-align:start}
.docs-owner-filter:hover{color:var(--accent);text-decoration:underline;text-underline-offset:.18em}
.docs-title-access-icon{display:none;flex:none;width:1rem;height:1rem;color:var(--muted)}
@media(pointer:coarse){.kbd-hint{display:none}}
.docs-access{display:inline-flex;align-items:center;gap:.4rem;min-width:0;color:var(--ink);font-size:var(--hcm-font-size-small)}
.docs-access span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.docs-access-icon{width:.95rem;height:.95rem;flex:none;color:var(--docs-kind,var(--muted))}
.docs-access-private{color:var(--muted)}
.docs-cell-updated{color:var(--muted);font-size:var(--hcm-font-size-small);font-variant-numeric:tabular-nums;white-space:nowrap}
.docs-cell-actions{justify-content:center;padding-inline:0}
.docs-row-menu-trigger{display:inline-grid;place-items:center;width:2rem;height:2rem;border-radius:var(--hcm-radius-control);color:var(--muted);cursor:pointer;list-style:none}
.docs-row-menu-trigger::-webkit-details-marker{display:none}
.docs-row-menu-trigger:hover,.docs-row-menu[open]>.docs-row-menu-trigger{background:color-mix(in srgb,var(--ink) 8%,transparent);color:var(--ink)}
.docs-more-icon{width:1.1rem;height:1.1rem}
.docs-row-menu{position:relative}
.docs-row-menu-panel{position:absolute;inset-inline-end:0;top:calc(100% + 4px);z-index:30;min-width:13rem}
.docs-menu{display:grid;padding:.25rem}
.docs-menu-item{display:flex;align-items:center;gap:var(--hcm-space-1);width:100%;min-height:2.25rem;padding-inline:var(--hcm-space-1);border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;text-align:start;cursor:pointer;white-space:nowrap}
.docs-menu-item:hover,.docs-menu-item:focus-visible{background:var(--hcm-hover-surface,var(--soft));outline:none}
/* The destructive item is set apart from the everyday ones (D-18). */
.docs-menu-item.docs-menu-danger:not(:first-child){position:relative;margin-block-start:.5rem}
.docs-menu-item.docs-menu-danger:not(:first-child)::before{content:"";position:absolute;inset-inline:0;inset-block-start:-.25rem;border-block-start:1px solid var(--line);pointer-events:none}
/* A phone-width window gets the 44px targets even with a fine pointer. */
@media (max-width:40rem){.docs-menu-item{min-height:2.75rem}}
.docs-menu-icon{width:1rem;height:1rem;flex:none;color:var(--muted)}
.docs-menu-danger,.docs-menu-danger .docs-menu-icon{color:var(--hcm-color-danger,var(--danger))}
.docs-more{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-2);flex-wrap:wrap}
.docs-pages{display:flex;align-items:center;gap:2px}
.docs-page-link{display:inline-grid;place-items:center;min-width:2.25rem;height:2.25rem;padding-inline:.5rem;border-radius:var(--hcm-radius-control);color:var(--ink);text-decoration:none;font-variant-numeric:tabular-nums;font-weight:500}
.docs-page-link:hover{background:var(--hcm-hover-surface,var(--soft))}
.docs-page-link[aria-current=page]{background:var(--accent);color:var(--on-brand,#fff);font-weight:700}
.docs-page-link:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-page-step{font-size:1.2rem;line-height:1}
.docs-page-gap{min-width:1.5rem;text-align:center;color:var(--muted)}
.docs-page-size select{min-height:2.25rem}
.docs-hit{display:flex;align-items:flex-start;gap:var(--hcm-space-1);min-width:0;max-width:100%;font-size:var(--hcm-font-size-small);color:var(--muted)}
.docs-snippet-text{min-width:0;overflow:hidden;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;line-clamp:2;line-height:1.4}
.docs-title-link mark,.docs-snippet-text mark{background:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 28%,transparent);color:var(--ink);border-radius:2px;padding-inline:1px}
.docs-match{flex:none;padding:0 .4rem;border-radius:999px;border:1px solid var(--line);font-size:var(--hcm-font-size-xsmall);font-weight:650;color:var(--muted);white-space:nowrap}
.docs-match-meaning{border-color:color-mix(in srgb,var(--hcm-color-info) 50%,transparent);color:var(--hcm-color-info)}
.docs-match-fuzzy{border-color:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 50%,transparent)}
.docs-mode-notice{margin:0;flex-basis:100%;font-size:var(--hcm-font-size-small)}
.docs-more-count{color:var(--muted);font-size:var(--hcm-font-size-small);font-variant-numeric:tabular-nums}
.docs-empty{display:grid;gap:var(--hcm-space-1);justify-items:start;margin:0;padding:var(--hcm-space-4) var(--hcm-space-3);border:1px dashed var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface)}
.docs-empty p{margin:0}
.docs-empty-title{font-weight:650}
.docs-unavailable .docs-empty-title{margin:0;font-size:var(--hcm-font-size-body)}
.docs-state-title{margin:0 0 var(--hcm-space-2);font-size:clamp(1.5rem,1.2rem + 1vw,2rem);line-height:1.2}
.docs-empty-title:focus,.docs-state-title:focus{outline:none}
.docs-empty-hint{color:var(--muted);max-width:52ch}

.docs-toast{position:fixed;inset-inline:0;bottom:var(--hcm-space-3);z-index:60;width:max-content;max-width:calc(100vw - 2rem);margin-inline:auto;padding:.6rem 1rem;border-radius:var(--hcm-radius-control);background:var(--ink);color:var(--surface);box-shadow:var(--hcm-shadow-raised);font-size:var(--hcm-font-size-small);pointer-events:none;animation:docs-toast 4.2s var(--hcm-motion-easing,ease) forwards}
@keyframes docs-toast{0%{opacity:0;translate:0 .5rem}6%{opacity:1;translate:0 0}85%{opacity:1}100%{opacity:0;visibility:hidden}}
/* An actionable toast (Undo) is a real control, not a decorative echo: it
   accepts pointer/keyboard input and stays up as long as its JS lifetime
   (docsActionToastLifetime, 8s), so its fade animation matches that. */
.docs-toast-action{display:flex;align-items:center;gap:var(--hcm-space-2);pointer-events:auto;animation-duration:8s}
.docs-toast-undo{min-height:44px;padding-inline:var(--hcm-space-2);color:var(--surface);background:transparent;border:1px solid var(--surface);border-radius:var(--hcm-radius-control);font:inherit;font-weight:600;cursor:pointer}
.docs-toast-undo:hover,.docs-toast-undo:focus-visible{background:color-mix(in srgb,var(--surface) 18%,transparent)}

/* r5 D-5: dialogs anchor near the top (10vh), matching chat's dialogs and
   the Slack/Linear convention, and their buttons use body-size text. */
.docs-dialog-layer{position:fixed;inset:0;z-index:70;display:grid;place-items:start center;padding:10vh var(--hcm-space-2) var(--hcm-space-2)}
.docs-dialog .button{font-size:var(--hcm-font-size-body);font-weight:600;min-height:40px}
.docs-dialog-scrim{position:absolute;inset:0;background:color-mix(in srgb,#000 42%,transparent)}
/* In dark mode 42% black over a near-black page barely dimmed it (S-4): the
   scrim goes darker and the dialog takes the stronger control border so its
   edge still reads against the dimmed page. */
:root[data-hcm-color-mode="dark"] .docs-dialog-scrim{background:color-mix(in srgb,#000 68%,transparent)}
:root[data-hcm-color-mode="dark"] .docs-dialog{border-color:var(--control-border)}
@media (prefers-color-scheme:dark){
  :root:not([data-hcm-color-mode="light"]):not([data-hcm-color-mode="dark"]) .docs-dialog-scrim{background:color-mix(in srgb,#000 68%,transparent)}
  :root:not([data-hcm-color-mode="light"]):not([data-hcm-color-mode="dark"]) .docs-dialog{border-color:var(--control-border)}
}
.docs-dialog{position:relative;display:grid;grid-template-rows:auto minmax(0,1fr);width:min(34rem,100%);max-height:calc(90dvh - 24px);background:var(--surface);color:var(--ink);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-raised);animation:docs-dialog-in var(--hcm-motion-fast,.14s) var(--hcm-motion-easing,ease)}
@keyframes docs-dialog-in{from{opacity:0;translate:0 .4rem}}
.docs-dialog-head{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-2);padding:var(--hcm-space-2) var(--hcm-space-3);border-block-end:1px solid var(--line)}
.docs-dialog-head h2{margin:0;font-size:var(--hcm-font-size-body);font-weight:700;overflow-wrap:anywhere}
.docs-dialog-close{display:inline-grid;place-items:center;width:2rem;height:2rem;padding:0;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--muted);cursor:pointer}
.docs-dialog-close:hover{background:var(--hcm-hover-surface,var(--soft));color:var(--ink)}
.docs-dialog-body{display:grid;gap:var(--hcm-space-2);padding:var(--hcm-space-2) var(--hcm-space-3) var(--hcm-space-3);overflow:auto}
.docs-dialog-lede{margin:0 0 var(--hcm-space-1);color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-create-hint{margin:0;color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-move-list{list-style:none;margin:0;padding:0;display:grid;gap:2px;max-height:18rem;overflow:auto}
.docs-move-option{display:flex;align-items:center;gap:var(--hcm-space-1);width:100%;min-height:2.6rem;padding-inline:var(--hcm-space-1);border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;text-align:start;cursor:pointer}
.docs-move-option:hover:not(:disabled){background:var(--hcm-hover-surface,var(--soft))}
.docs-move-option:disabled{cursor:default;color:var(--muted)}
.docs-move-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.docs-move-current{font-size:var(--hcm-font-size-small);color:var(--muted)}
.docs-move-new{display:grid;gap:var(--hcm-space-1);padding-block-start:var(--hcm-space-2);border-block-start:1px solid var(--line)}
.docs-move-new label{font-weight:600;font-size:var(--hcm-font-size-small)}
.docs-move-new-row{display:flex;gap:var(--hcm-space-1)}
.docs-move-new-row input{flex:1;min-width:0;min-height:var(--hcm-control-height);padding-inline:var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}

.docs-share-form{position:relative;display:grid;gap:var(--hcm-space-1);container:docsshare/inline-size}
.docs-share-label{font-weight:600;font-size:var(--hcm-font-size-small)}
.docs-share-add-row{display:grid;grid-template-columns:minmax(9rem,1fr) auto auto;align-items:start;gap:var(--hcm-space-1)}
.docs-share-field{box-sizing:border-box;display:flex;flex-wrap:wrap;align-items:center;gap:.35rem;min-width:0;min-height:var(--hcm-control-height);padding:.25rem .6rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface)}
.docs-share-field:focus-within{border-color:var(--accent);outline:2px solid transparent;box-shadow:var(--hcm-focus-ring)}
/* The wrapper is the visible control; the input inside it is bare text (D-2).
   An ID selector, because the shell's field rules reach (0,5,1) for the
   fixed block-size and (0,2,1) for the focus ring, and either one drew a
   second box inside the wrapper. */
.docs-share-field #docs-share-people,.docs-share-field #docs-share-people:focus-visible{flex:1 1 6rem;min-width:0;block-size:auto;min-height:2rem;border:0;padding:0 .15rem;margin:0;font:inherit;color:var(--ink);background:transparent;outline:0;box-shadow:none}
@container docsshare (max-width:30rem){
  .docs-share-add-row{grid-template-columns:minmax(0,1fr) auto}
  .docs-share-field{grid-column:1/-1}
  .docs-share-add-row .docs-role select{width:100%}
}
.docs-share-add-row .docs-role select{min-height:var(--hcm-control-height)}
.docs-share-add-row .button{min-height:var(--hcm-control-height);white-space:nowrap}
.docs-pick-chip{display:inline-flex;align-items:center;gap:.35rem;padding:.15rem .2rem .15rem .25rem;border-radius:999px;background:var(--hcm-color-brand-soft);font-size:var(--hcm-font-size-small);font-weight:600}
.docs-pick-remove{display:inline-grid;place-items:center;width:1.4rem;height:1.4rem;padding:0;border:0;border-radius:999px;background:transparent;color:inherit;cursor:pointer}
.docs-pick-remove:hover{background:color-mix(in srgb,var(--ink) 10%,transparent)}
.docs-share-options{position:absolute;inset-inline:0;top:calc(1.4rem + var(--hcm-control-height) + .4rem);z-index:2;list-style:none;margin:0;padding:.25rem;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-control);box-shadow:var(--hcm-shadow-raised);max-height:16rem;overflow:auto}
.docs-share-options[hidden]{display:none}
.docs-share-option{display:flex;align-items:center;gap:var(--hcm-space-1);padding:.4rem var(--hcm-space-1);border-radius:var(--hcm-radius-control);cursor:pointer}
.docs-share-option.is-active,.docs-share-option:hover{background:var(--hcm-hover-surface,var(--soft))}
.docs-share-option-text{display:grid;min-width:0}
.docs-share-option-name{font-weight:600}
.docs-share-option-detail{color:var(--muted);font-size:var(--hcm-font-size-small);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.docs-share-none{padding:.5rem var(--hcm-space-1);color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-share-submit{display:flex;justify-content:flex-end;align-items:center;gap:var(--hcm-space-1);flex-wrap:wrap}
.docs-role select{min-height:var(--hcm-control-height);padding-inline:var(--hcm-space-1);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-share-message{margin:0;color:var(--hcm-color-success,var(--success));font-size:var(--hcm-font-size-small);font-weight:600}
.docs-access-heading{margin:var(--hcm-space-1) 0 0;font-size:var(--hcm-font-size-small);font-weight:650;color:var(--muted)}
.docs-access-list{list-style:none;margin:0;padding:0;display:grid;gap:2px}
.docs-access-row{display:flex;align-items:center;gap:var(--hcm-space-1);min-height:2.6rem}
.docs-access-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:500}
.docs-access-role{color:var(--muted);font-size:var(--hcm-font-size-small);white-space:nowrap}
.docs-access-role-edit select{min-height:2.25rem;padding-inline:.4rem;font:inherit;font-size:var(--hcm-font-size-small);color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-access-remove{min-height:2rem;padding-inline:var(--hcm-space-1);border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--hcm-color-danger,var(--danger));font:inherit;font-size:var(--hcm-font-size-small);font-weight:600;cursor:pointer}
.docs-access-remove:hover{background:color-mix(in srgb,var(--hcm-color-danger,var(--danger)) 10%,transparent)}
.docs-access-loading{color:var(--muted);font-size:var(--hcm-font-size-small);padding-block:var(--hcm-space-1)}
.docs-share-footer{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-2);flex-wrap:wrap;padding-block-start:var(--hcm-space-2);border-block-start:1px solid var(--line)}
.docs-share-footer p{margin:0;color:var(--muted);font-size:var(--hcm-font-size-small);flex:1 1 14rem}

.docs-detail{width:100%;max-width:90rem;display:grid;gap:var(--hcm-space-2);padding-block:var(--hcm-space-1) var(--hcm-space-4);container:docsdetail/inline-size}
.docs-crumbs{display:flex;align-items:center;gap:var(--hcm-space-1);flex-wrap:wrap}
.docs-back,.docs-crumb-folder{display:inline-flex;align-items:center;gap:.45rem;min-height:2.5rem;padding:0 .9rem 0 .7rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-weight:600;font-size:var(--hcm-font-size-body);text-decoration:none}
.docs-back:hover,.docs-crumb-folder:hover{background:var(--hcm-hover-surface,var(--soft));border-color:var(--accent);color:var(--accent)}
.docs-crumb-folder{border-color:transparent;background:none;color:var(--muted);font-weight:500}
.docs-back-icon{width:1.1rem;height:1.1rem;flex:none}
[dir=rtl] .docs-back-icon{transform:scaleX(-1)}
.docs-copy-text{position:absolute;inset-block-start:.75rem;inset-inline-end:.75rem;z-index:2;display:inline-flex;align-items:center;gap:.35rem;min-height:2rem;padding:0 .65rem;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--muted);font:inherit;font-size:var(--hcm-font-size-small);font-weight:600;cursor:pointer}
.docs-copy-text:hover{color:var(--ink);border-color:var(--control-border);background:var(--hcm-hover-surface,var(--soft))}
.docs-copy-text:focus-visible,.docs-back:focus-visible,.docs-crumb-folder:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
/* No coloured edge on the header: green-for-shared / grey-for-private had no
   legend, and the Access fact already says it in words (D-21). */
/* The client mounts the document after the shell's footer (GWC appends a
   late-mounted child), so the "Human Capital Management Suite / Workspace
   information" footer sat above "Back to documents" (D-20). Order the
   footer last whatever the DOM order is. */
.main:has(>:is(.docs-detail,.docs-hub)){display:flex;flex-direction:column}
.main:has(>:is(.docs-detail,.docs-hub))>.footer{order:1}
.docs-detail-header{display:grid;gap:var(--hcm-space-2)}
/* The header's display rules would otherwise defeat [hidden]: while editing,
   the actions and facts are hidden and the H1 is screen-reader only, so the
   header left an accent bar holding only star/Share/... (D-6). */
.docs-detail-actions[hidden],.docs-facts[hidden]{display:none}
.docs-detail-header.is-editing{gap:0}
.docs-detail-title-row{display:flex;align-items:flex-start;justify-content:space-between;gap:var(--hcm-space-2);flex-wrap:wrap}
.docs-detail-title-row h1{flex:1 1 20rem;min-width:0;margin:0;font-size:clamp(1.5rem,1.2rem + 1vw,2rem);line-height:1.2;overflow-wrap:anywhere}
.docs-detail-actions{display:flex;align-items:center;gap:var(--hcm-space-1);flex-wrap:wrap}
.docs-detail-more{width:2.5rem;height:2.5rem;border:1px solid var(--control-border)}
.docs-facts{display:flex;flex-wrap:wrap;gap:var(--hcm-space-1) var(--hcm-space-4);margin:0}
.docs-fact{display:grid;gap:.15rem}
.docs-fact dt{color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-fact dd{display:flex;align-items:center;gap:.4rem;margin:0;font-weight:600;font-size:var(--hcm-font-size-small)}
.docs-fact .docs-access{font-size:inherit;font-weight:inherit}
.docs-detail-layout{display:grid;grid-template-columns:minmax(0,1fr) minmax(16rem,20rem);align-items:start;gap:var(--hcm-space-3)}
.docs-detail-layout.is-editing{grid-template-columns:minmax(0,1fr)}
.docs-detail-layout.is-editing>.docs-detail-rail{display:none}
.docs-detail-content{min-width:0}
.docs-detail-rail{min-width:0;display:grid;gap:var(--hcm-space-2);position:sticky;top:var(--hcm-space-2)}
.docs-reader{min-width:0;padding:var(--hcm-space-3) clamp(1rem,3cqi,3rem);background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface)}
.docs-markdown{font-size:var(--hcm-font-size-body);line-height:1.65}
.docs-markdown h2,.docs-markdown h3,.docs-markdown h4,.docs-markdown h5{scroll-margin-top:var(--hcm-space-3)}
/* The document's own "#" title is the page H1, so its "##" sections render
   as h3: h3 is the level readers see on almost every document and carries
   the section size (21px on 16px body), not h2 (D-7). */
/* The metadata line opening a document (docsIsMetadataLead) is a caption,
   not body copy: muted and a step smaller, its labels at normal weight
   ink so they still scan (D-7). */
.docs-markdown .docs-lead{color:var(--muted);font-size:var(--hcm-font-size-small);line-height:1.5}
.docs-markdown .docs-lead strong{color:var(--ink);font-weight:600}
/* 72ch at the lead's smaller size was a narrower measure than the body's,
   which orphaned "2026" onto a second line (r5). */
.docs-markdown>.docs-lead{max-width:none}
.docs-markdown h2{font-size:1.5rem;font-weight:700;line-height:1.3;margin-block:2rem .5rem}
.docs-markdown h3{font-size:1.3125rem;font-weight:700;line-height:1.3;margin-block:2rem .5rem}
.docs-markdown h4{font-size:1.0625rem;font-weight:650;line-height:1.35;margin-block:1.5rem .4rem}
.docs-markdown h2:first-child,.docs-markdown h3:first-child{margin-block-start:0}
.docs-table-scroll{max-width:100%;overflow-x:auto;margin:0 0 var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-control)}
.docs-table-scroll:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-markdown table{width:100%;border-collapse:collapse;font-size:var(--hcm-font-size-body);line-height:1.45;font-variant-numeric:tabular-nums}
.docs-markdown th,.docs-markdown td{padding:.5rem .75rem;border-block-end:1px solid var(--line);text-align:start;vertical-align:top;min-width:8ch}
.docs-markdown thead th{background:var(--surface-subtle,var(--soft));font-weight:650;white-space:nowrap}
.docs-markdown tbody tr:last-child td{border-block-end:0}
.docs-markdown tbody tr:hover td{background:color-mix(in srgb,var(--ink) 3%,transparent)}
.docs-markdown .docs-align-end{text-align:end}.docs-markdown .docs-align-center{text-align:center}
.docs-markdown .docs-task{margin-inline-end:.4rem;accent-color:var(--accent)}
.docs-markdown li:has(>.docs-task){list-style:none;margin-inline-start:-1.2rem}
.docs-markdown s{color:var(--muted)}
.docs-markdown .docs-diagram{margin:var(--hcm-space-2) 0 var(--hcm-space-3)}
.docs-diagram-note{margin-block-start:.35rem;color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-comments{display:grid;gap:var(--hcm-space-2);padding:0;background:none;border:0;box-shadow:none}
.docs-comments-head{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-1)}
.docs-comments-head h2{display:flex;align-items:center;gap:.45rem;margin:0;font-size:var(--hcm-font-size-small);font-weight:650;color:var(--muted)}
.docs-comments-count{min-width:1.3rem;padding:0 .35rem;border-radius:999px;background:var(--surface-subtle,var(--soft));color:var(--ink);font-size:var(--hcm-font-size-xsmall);text-align:center;font-variant-numeric:tabular-nums}
.docs-comments-toggle{border:0;background:none;color:var(--accent);font:inherit;font-size:var(--hcm-font-size-small);font-weight:600;cursor:pointer;padding:.2rem .3rem;border-radius:var(--hcm-radius-control)}
.docs-comments-toggle:hover{background:var(--hcm-hover-surface,var(--soft))}
.docs-threads{list-style:none;margin:0;padding:0;display:grid;gap:var(--hcm-space-1)}
.docs-thread{display:grid;gap:.5rem;padding:.75rem .85rem;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);transition:border-color var(--hcm-motion-fast,.14s),box-shadow var(--hcm-motion-fast,.14s)}
.docs-thread:focus{outline:none}
.docs-thread.is-active,.docs-thread:focus-visible{border-color:var(--accent);box-shadow:0 0 0 1px var(--accent)}
.docs-thread.is-resolved{background:var(--surface-subtle,var(--soft))}
.docs-thread-empty{padding:var(--hcm-space-2);border:1px dashed var(--line);border-radius:var(--hcm-radius-surface);color:var(--muted);font-size:var(--hcm-font-size-small);line-height:1.45}
.docs-thread-quote{display:block;width:100%;margin:0;padding:.15rem 0 .15rem .6rem;border:0;border-inline-start:3px solid color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 70%,transparent);background:none;color:var(--muted);font:inherit;font-size:var(--hcm-font-size-small);line-height:1.45;text-align:start;cursor:pointer}
.docs-thread-quote:hover:not(:disabled){color:var(--ink)}
.docs-thread-quote:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-thread-quote.is-orphaned{border-inline-start-color:var(--line);cursor:default;text-decoration:line-through;text-decoration-color:color-mix(in srgb,var(--muted) 50%,transparent)}
.docs-thread-orphan-note{margin:0;color:var(--muted);font-size:var(--hcm-font-size-xsmall)}
.docs-comment-entry{display:grid;grid-template-columns:1.6rem minmax(0,1fr);gap:.55rem;align-items:start}
.docs-comment-entry .avatar{width:1.6rem;height:1.6rem;min-width:1.6rem}
.docs-comment-main{display:grid;gap:.15rem;min-width:0}
.docs-comment-meta{display:flex;align-items:baseline;gap:.5rem;flex-wrap:wrap;margin:0;color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-comment-author{color:var(--ink);font-weight:650;font-size:var(--hcm-font-size-small)}
.docs-comment-body{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.5}
.docs-replies{list-style:none;margin:0;padding:0 0 0 .6rem;display:grid;gap:.6rem;border-inline-start:2px solid var(--line)}
/* Resolve's icon, not its hover box, starts on the comment text's edge
   (avatar 1.6rem + gap .55rem); the .5rem button padding hangs into the
   gutter instead of pushing the icon 10px past the text (D-24). */
.docs-thread-actions{display:flex;gap:.25rem;margin-inline-start:calc(2.15rem - .5rem)}
.docs-thread-action{display:inline-flex;align-items:center;gap:.3rem;min-height:1.9rem;padding:0 .5rem;border:0;border-radius:var(--hcm-radius-control);background:none;color:var(--muted);font:inherit;font-size:var(--hcm-font-size-small);font-weight:600;cursor:pointer}
.docs-thread-action:hover{background:var(--hcm-hover-surface,var(--soft));color:var(--ink)}
.docs-thread-action:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:1px}
.docs-thread-resolve:hover{color:var(--hcm-color-success,var(--success))}
.docs-reply-form{display:grid;gap:.4rem;margin-inline-start:2.15rem}
.docs-reply-form textarea,.docs-compose textarea{width:100%;box-sizing:border-box;min-height:2.6rem;padding:.5rem .65rem;font:inherit;line-height:1.45;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);resize:vertical;field-sizing:content;max-height:14rem}
.docs-reply-actions{display:flex;justify-content:flex-end;gap:.4rem}
.docs-compose{display:grid;gap:.45rem;padding:.75rem .85rem;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface)}
.docs-compose:focus-within{border-color:color-mix(in srgb,var(--accent) 55%,var(--line))}
.docs-compose textarea{border-color:transparent;padding-inline:0;min-height:2.4rem}
.docs-compose textarea:focus-visible{outline:2px solid transparent}
.docs-compose-quote{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:.1rem .4rem;align-items:start;padding:.35rem .5rem;border-inline-start:3px solid color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 70%,transparent);background:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 8%,transparent);border-start-end-radius:var(--hcm-radius-control);border-end-end-radius:var(--hcm-radius-control)}
.docs-compose-quote-label{grid-column:1;color:var(--muted);font-size:var(--hcm-font-size-xsmall);font-weight:650}
.docs-compose-quote q{grid-column:1;font-size:var(--hcm-font-size-small);line-height:1.4}
.docs-compose-quote .docs-pick-remove{grid-column:2;grid-row:1 / span 2}
.docs-compose-foot{display:flex;align-items:center;justify-content:space-between;gap:.5rem}
.docs-compose-foot .button{flex:none;white-space:nowrap}
.docs-compose-hint{margin:0;color:var(--muted);font-size:var(--hcm-font-size-xsmall);line-height:1.35}
.docs-select-comment{position:absolute;z-index:40;display:inline-flex;align-items:center;gap:.35rem;min-height:2.1rem;padding:0 .75rem;border:0;border-radius:999px;background:var(--ink);color:var(--surface);font:inherit;font-size:var(--hcm-font-size-small);font-weight:650;box-shadow:var(--hcm-shadow-raised);translate:-50% 0;opacity:0;pointer-events:none;transition:opacity var(--hcm-motion-fast,.14s)}
.docs-select-comment.is-visible{opacity:1;pointer-events:auto}
.docs-select-comment:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-reader{position:relative}
.docs-anchor-gutter{position:absolute;inset-block:0;inset-inline-end:.35rem;width:1.6rem;pointer-events:none}
.docs-anchor-pin{position:absolute;inset-inline-end:0;display:inline-grid;place-items:center;width:1.4rem;height:1.4rem;padding:0;border:0;border-radius:999px 999px 999px 3px;background:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 30%,var(--surface));color:var(--ink);font:inherit;font-size:var(--hcm-font-size-xsmall);font-weight:750;font-variant-numeric:tabular-nums;cursor:pointer;pointer-events:auto;box-shadow:0 0 0 1px color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 55%,transparent);transition:transform var(--hcm-motion-fast,.14s),background var(--hcm-motion-fast,.14s)}
[dir=rtl] .docs-anchor-pin{border-radius:999px 999px 3px 999px}
.docs-anchor-pin.is-unplaced{visibility:hidden}
.docs-anchor-pin:hover,.docs-anchor-pin.is-linked{background:var(--hcm-color-warning,var(--warning));color:var(--ink);transform:scale(1.12)}
.docs-anchor-pin:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-markdown{padding-inline-end:1.6rem}
.docs-thread-num{display:inline-grid;place-items:center;flex:none;width:1.25rem;height:1.25rem;margin-inline-end:.4rem;border-radius:999px 999px 999px 3px;background:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 30%,var(--surface));color:var(--ink);font-size:var(--hcm-font-size-xsmall);font-weight:750;font-style:normal;vertical-align:.1em}
.docs-thread.is-linked .docs-thread-num{background:var(--hcm-color-warning,var(--warning))}
.docs-thread.is-linked{border-color:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 70%,var(--line));box-shadow:0 0 0 1px color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 70%,transparent)}
.docs-thread.is-linked .docs-thread-quote{color:var(--ink);border-inline-start-color:var(--hcm-color-warning,var(--warning))}
.docs-connector{position:fixed;inset:0;width:100vw;height:100vh;z-index:35;pointer-events:none;overflow:visible}
.docs-connector path{fill:none;stroke:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 80%,var(--ink));stroke-width:1.5;stroke-dasharray:4 3;opacity:.9}
#docs-markdown::highlight(docs-anchor-flash),#docs-markdown ::highlight(docs-anchor-flash){background-color:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 70%,transparent)}
#docs-markdown::highlight(docs-anchor),#docs-markdown ::highlight(docs-anchor){background-color:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 22%,transparent)}
#docs-markdown::highlight(docs-anchor-active),#docs-markdown ::highlight(docs-anchor-active){background-color:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 48%,transparent)}
.docs-outline{padding:var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface)}
.docs-outline h2{margin:0 0 var(--hcm-space-1);font-size:var(--hcm-font-size-small);font-weight:650;color:var(--muted)}
.docs-outline ol{list-style:none;margin:0;padding:0;display:grid;gap:2px}
.docs-outline a{display:block;padding:.25rem .5rem;border-radius:var(--hcm-radius-control);color:var(--ink);text-decoration:none;font-size:var(--hcm-font-size-small);line-height:1.35}
.docs-outline a:hover{background:var(--hcm-hover-surface,var(--soft));color:var(--accent)}
.docs-outline a.is-current{background:var(--hcm-color-brand-soft);color:var(--accent);font-weight:650}
.docs-outline-d1 a{padding-inline-start:1.1rem}
.docs-outline-d2 a{padding-inline-start:1.8rem;color:var(--muted)}

@container docslib (max-width:80rem){
  .docs-folder-tag{display:none}
}
@container docslib (max-width:62rem){
  .docs-cell-access{display:none}
  .docs-title-access-icon{display:inline-flex}
  /* D-10: at laptop width 8rem with an avatar cut owners to "Selene Nav...";
     the avatar goes and the column takes 9rem, which fits a full name. */
  .docs-row{grid-template-columns:2.5rem minmax(0,1fr) 8.5rem 4.5rem 2.75rem}
  .docs-cell-owner .avatar{display:none}
  /* r5 D-2: at laptop width most policy titles (40-60 characters) ended
     in an ellipsis; two lines beat one cut line. */
  .docs-title-link{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;line-clamp:2;white-space:normal;overflow-wrap:anywhere}
  .docs-table.docs-no-owner .docs-row{grid-template-columns:2.5rem minmax(0,1fr) 5.5rem 2.75rem}
  .docs-table.docs-no-owner .docs-cell-owner{display:none}
}
@container docslib (max-width:56rem){
  .docs-library{grid-template-columns:minmax(0,1fr)}
  /* No mask: it faded the "+" at the strip's end. The edge fades are
     painted by the strip's ::before and the sticky trailing group's
     ::before (docsVisualStylesheet, D-4). */
  .docs-nav{position:static;display:flex;align-items:center;gap:var(--hcm-space-1);overflow-x:auto;padding-block-end:.25rem;scrollbar-width:thin}
  .docs-nav-list{display:flex;gap:var(--hcm-space-1)}
  .docs-nav-link{white-space:nowrap;border:1px solid var(--line);border-radius:999px;min-height:2.25rem;padding-inline:.8rem}
  .docs-nav-section,.docs-nav-note,.docs-folder-menu,.docs-folder-empty{display:none}
  .docs-folder-item{flex:none}
}
@container docslib (max-width:40rem){
  .docs-row{grid-template-columns:2.25rem minmax(0,1fr) 6rem 5.5rem 2.75rem}
  .docs-cell-access{display:none}
  .docs-table.docs-no-owner .docs-row{grid-template-columns:2.25rem minmax(0,1fr) 5.5rem 2.75rem}
  .docs-table.docs-no-owner .docs-cell-owner{display:none}
}
@container docslib (max-width:30rem){
  .docs-row{grid-template-columns:minmax(0,1fr) 2.75rem;align-items:start;padding-block:.6rem}
  .docs-cell-select,.docs-cell-owner,.docs-cell-updated{display:none}
  /* Star, access icon and title are three grid columns, so a title that
     wraps wraps inside its own column instead of dropping below an
     orphaned icon; the owner/date line sits under the title (D-5). */
  .docs-cell-title{padding-inline-start:var(--hcm-space-2);display:grid;grid-template-columns:auto auto minmax(0,1fr);align-items:start;column-gap:.4rem;row-gap:0}
  .docs-cell-title>.docs-star,.docs-cell-title>.docs-title-access-icon{height:1.75rem;align-items:center}
  .docs-cell-title>.docs-title-stack{grid-column:3}
  .docs-folder-tag{display:none}
  /* Compact caption instead of hiding the label outright (DOCS-05): a
     bare select reading only "Smart" or "Recently updated" loses what
     it controls once its own heading is gone. */
  .docs-toolbar .docs-sort{flex-direction:column;align-items:flex-start;gap:.1rem}
  .docs-toolbar .docs-sort-label{font-size:var(--hcm-font-size-xsmall)}
  .docs-row-phone-meta{display:block;grid-column:3;margin-block-start:0}
  /* A phone-width window has no hover to reveal the row menu even with a
     fine pointer, so it is always shown here, not only under (pointer:coarse). */
  .docs-row .docs-row-menu-trigger{opacity:1}
  .docs-title-link{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;line-clamp:2;overflow:hidden;white-space:normal;overflow-wrap:anywhere}
  .docs-table.docs-no-owner .docs-row{grid-template-columns:minmax(0,1fr) 2.75rem}
  .docs-toolbar{display:grid;grid-template-columns:1fr 1fr;gap:var(--hcm-space-1)}
  .docs-toolbar .docs-find{grid-column:1 / -1}
  .docs-toolbar .docs-sort,.docs-toolbar .docs-mode{width:100%}
  .docs-toolbar .docs-sort select{max-width:none;width:100%}
  .docs-hit{flex-wrap:wrap}
  .docs-snippet-text{flex-basis:100%}
  /* D-5: the phone spent ~460px on chrome before the first row. The
     subtitle goes (a pending search's status line stays), New document
     keeps to the heading's row as the folder view already did, and the
     one-column header row ("Title" alone) goes; the Sort select above
     carries sorting. */
  .docs-main-head{flex-wrap:nowrap;align-items:center}
  .docs-main-title{flex:1 1 auto}
  .docs-main-actions{flex:none}
  .docs-main-head .subtitle:not(.docs-searching){display:none}
  .docs-row-head{display:none}
  .docs-row-head+.docs-row{border-block-start:0;border-start-start-radius:var(--hcm-radius-surface);border-start-end-radius:var(--hcm-radius-surface)}
}
@container docsdetail (max-width:56rem){
  .docs-detail-layout{grid-template-columns:minmax(0,1fr)}
  .docs-detail-rail{position:static}
}
@container docsdetail (max-width:30rem){
  /* Owner / Access / Updated collapse to one line of values, the way a
     phone reader's metadata reads elsewhere in the product (D-23). */
  /* One flex line, not inline boxes: the owner's avatar is a block-level
     box, so inside an inline dd it broke the line and left the name, the
     separator and the access label scattered over two lines (D-4). Each
     fact stays on one line and the long ones shorten with an ellipsis. */
  /* r5 D-1: a labelled two-column grid. The dot-joined single line
     dropped the labels, so "Updated" and "Reviewed" read as two bare
     dates ("Sep 20 · Sep 20"). Each value still stays on one line and
     shortens with an ellipsis. */
  .docs-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:var(--hcm-space-2) var(--hcm-space-3);min-width:0}
  .docs-fact{display:grid;gap:.1rem;min-width:0}
  .docs-fact dt{color:var(--muted);font-size:var(--hcm-font-size-small)}
  .docs-fact dd{display:flex;align-items:center;gap:.3rem;min-width:0;margin:0;white-space:nowrap}
  .docs-fact dd>span:last-child{min-width:0;overflow:hidden;text-overflow:ellipsis}
  .docs-fact .avatar{width:1.25rem;height:1.25rem;min-width:1.25rem}
  /* Title takes the full row; Actions becomes a tighter, compact strip
     beside it instead of full-size buttons wrapping onto their own row. */
  .docs-detail-title-row h1{flex-basis:100%}
  .docs-detail-actions{gap:.4rem}
  .docs-detail-actions .button{padding-inline:.6rem;font-size:var(--hcm-font-size-small)}
}
@media (pointer:coarse){
  .docs-row .docs-star[aria-pressed=false],.docs-folder-menu,.docs-row .docs-row-menu-trigger{opacity:1}
  .docs-row{min-height:3.25rem}
  .docs-star,.docs-row-menu-trigger{width:2.75rem;height:2.75rem}
}
@media (max-width:40rem){
  .docs-dialog-layer{place-items:end stretch;padding:0}
  .docs-dialog{width:100%;max-height:88dvh;border-radius:var(--hcm-radius-surface) var(--hcm-radius-surface) 0 0}
}
@media (prefers-reduced-motion:reduce){
  .docs-dialog{animation:none}
  @keyframes docs-toast{0%{opacity:0}4%{opacity:1}85%{opacity:1}100%{opacity:0;visibility:hidden}}
}
/* A 1px border reads flat against a dark canvas close to the surface
   color; a soft shadow lifts the reader card and the table the way the
   light theme's contrast already does (D-20). Keyed to the product's colour
   mode too, so an explicit Dark choice on a light OS gets it. */
:root[data-hcm-color-mode="dark"] :is(.docs-reader,.docs-table-wrap .docs-table){box-shadow:var(--hcm-shadow-raised)}
@media (prefers-color-scheme:dark){
  :root:not([data-hcm-color-mode="light"]):not([data-hcm-color-mode="dark"]) :is(.docs-reader,.docs-table-wrap .docs-table){box-shadow:var(--hcm-shadow-raised)}
}
`
}
