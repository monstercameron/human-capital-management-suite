package projectui

// projectUIBoardStyles is the round-four layer: the quick-filter bar, the
// first-run board, the shortcut sheet, the drop settle, the modal title
// editor, and the round-four fixes. It comes last in Styles().
const projectUIBoardStyles = `
/* B-41: one card inset in every column; only the grip is draggable-specific. */
@media (hover:hover) and (pointer:fine){.projectui-board .projectui-card,.projectui-board .projectui-card[draggable="true"]{padding-inline-start:1.125rem}}

/* T-29: the locked caption wraps instead of truncating. */
.projectui-status-locked-wrap>.projectui-lock-caption{white-space:normal;overflow:visible;text-overflow:clip;text-wrap:balance}

/* T-30, T-31: no hover fills on touch screens, where :hover sticks after a tap. */
@media (hover:none){
.projectui-title-input:not(#projectui-none):hover,.projectui-description-input:not(#projectui-none):hover{background:transparent;border-color:transparent}
.projectui-date-field:hover{border-color:transparent;background:transparent}
.projectui-fields .projectui-field-select:not(#projectui-none):hover,.projectui-fields .projectui-field-date:not(#projectui-none):hover{border-color:transparent;background-color:transparent}
}

/* Quick filters. */
.projectui-filters{display:flex;flex-direction:column;gap:.5rem;min-inline-size:0}
.projectui-filterbar{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem .75rem;min-inline-size:0}
.projectui-filter-toggle{display:none;align-items:center;gap:.4rem;align-self:flex-start;min-block-size:2.25rem;padding-inline:.75rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;font-weight:600;cursor:pointer}
.projectui-filter-toggle:hover{border-color:color-mix(in srgb,var(--ink) 30%,var(--surface))}
.projectui-filter-toggle-icon{position:relative;inline-size:.875rem;block-size:.75rem;background:linear-gradient(currentColor,currentColor) 0 0/100% 1.5px no-repeat,linear-gradient(currentColor,currentColor) 50% 50%/65% 1.5px no-repeat,linear-gradient(currentColor,currentColor) 50% 100%/30% 1.5px no-repeat}
.projectui-filter-count{display:inline-grid;place-items:center;min-inline-size:1.25rem;block-size:1.25rem;padding-inline:.3rem;border-radius:999px;background:var(--accent);color:var(--on-brand,#fff);font-size:.75rem;font-weight:700;font-variant-numeric:tabular-nums}
.projectui-filters[data-filters-toggled="true"] .projectui-filterbar{display:none}
.projectui-filters[data-filters-toggled="true"] .projectui-filter-toggle{display:inline-flex}
.projectui-filter-search{position:relative;flex:0 1 15rem;min-inline-size:10rem}
.projectui-filter-search input:not(#projectui-none){inline-size:100%;min-block-size:2.25rem;min-height:2.25rem;padding-inline:2rem .75rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem}
.projectui-filter-search input:not(#projectui-none):focus-visible{border-color:var(--accent);outline:2px solid var(--hcm-color-focus);outline-offset:1px}
.projectui-filter-search-icon{position:absolute;inset-inline-start:.7rem;inset-block-start:50%;inline-size:.7rem;block-size:.7rem;margin-block-start:-.4rem;border:1.5px solid var(--muted);border-radius:50%;pointer-events:none}
.projectui-filter-search-icon::after{content:"";position:absolute;inset-block-end:-.3rem;inset-inline-end:-.3rem;inline-size:.35rem;block-size:1.5px;background:var(--muted);transform:rotate(45deg)}
.projectui-filter-group{display:flex;flex-wrap:wrap;align-items:center;gap:.25rem}
.projectui-filter-people{gap:0;padding-inline-start:.25rem}
.projectui-filter-person{position:relative;display:inline-grid;place-items:center;inline-size:2rem;block-size:2rem;margin-inline-start:-.25rem;border-radius:50%;text-decoration:none;transition:transform var(--hcm-motion-fast) var(--hcm-motion-easing)}
.projectui-filter-person .projectui-avatar{inline-size:1.75rem;block-size:1.75rem;box-shadow:0 0 0 2px var(--canvas,var(--surface))}
.projectui-filter-person:hover{z-index:1;transform:translateY(-1px)}
.projectui-filter-person[data-active="true"]{z-index:2}
.projectui-filter-person[data-active="true"] .projectui-avatar{box-shadow:0 0 0 2px var(--canvas,var(--surface)),0 0 0 4px var(--accent)}
.projectui-filters[data-active="true"] .projectui-filter-person[data-active="false"] .projectui-avatar{opacity:.55}
.projectui-filter-person:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:3px}
.projectui-filter-chip{display:inline-flex;align-items:center;gap:.375rem;min-block-size:2rem;padding-inline:.5rem .625rem;border:1px solid var(--control-border);border-radius:999px;background:var(--surface);color:var(--ink);font-size:.8125rem;font-weight:500;text-decoration:none;white-space:nowrap}
.projectui-filter-chip:hover{border-color:color-mix(in srgb,var(--ink) 30%,var(--surface))}
.projectui-filter-chip[data-active="true"]{border-color:color-mix(in srgb,var(--accent) 55%,var(--surface));background:color-mix(in srgb,var(--accent) 12%,var(--surface));font-weight:600}
.projectui-filter-chip .projectui-priority{display:inline-flex}
.projectui-filter-clear{margin-inline-start:.25rem;color:var(--accent);font-size:.8125rem;font-weight:600;text-decoration:none}
.projectui-filter-clear:hover{text-decoration:underline}
.projectui-filter-done{display:none}
.projectui-count-filtered{padding-inline:.4rem;background:color-mix(in srgb,var(--accent) 14%,var(--surface));color:var(--ink);font-variant-numeric:tabular-nums;white-space:nowrap}
.projectui-board[data-projectui="true"] .projectui-filters+.projectui-columns,.projectui-filters+.projectui-lanes-frame{margin-block-start:0}

/* First-run board. */
.projectui-firstrun{display:grid;justify-items:center;gap:.75rem;margin-block-start:.5rem;padding:3rem 1.5rem;border:1.5px dashed color-mix(in srgb,var(--ink) 14%,transparent);border-radius:var(--hcm-radius-surface);background:color-mix(in srgb,var(--surface) 70%,transparent);text-align:center}
.projectui-firstrun-art{display:flex;gap:.375rem;margin-block-end:.25rem}
.projectui-firstrun-art span{display:block;inline-size:2.5rem;block-size:3.25rem;border-radius:.5rem;background:var(--pu-col-bg,color-mix(in srgb,var(--ink) 6%,var(--surface)))}
.projectui-firstrun-art span:first-child{background:linear-gradient(var(--surface),var(--surface)) 50% .45rem/78% .8rem no-repeat,var(--pu-col-bg,color-mix(in srgb,var(--ink) 6%,var(--surface)));box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--accent) 35%,transparent)}
.projectui-firstrun-title{margin:0;font-size:1.125rem;font-weight:650;color:var(--ink)}
.projectui-firstrun-body{margin:0;max-inline-size:34rem;color:var(--muted);font-size:.9375rem;line-height:1.5}

/* Shortcut sheet. */
.projectui-shortcuts{position:fixed;z-index:60;inset-block-end:1.25rem;inset-inline-end:1.25rem;inline-size:min(22rem,calc(100vw - 2rem));padding:1rem 1.125rem;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-surface);background:var(--surface);box-shadow:var(--hcm-shadow-raised);color:var(--ink)}
.projectui-shortcuts[hidden]{display:none}
.projectui-shortcuts-head{display:flex;align-items:center;justify-content:space-between;gap:.75rem;margin-block-end:.5rem}
.projectui-shortcuts-head h2{margin:0;font-size:1rem;font-weight:650}
.projectui-shortcuts dl{display:grid;gap:.375rem;margin:0}
.projectui-shortcut{display:flex;align-items:center;justify-content:space-between;gap:1rem;font-size:.875rem}
.projectui-shortcut dt{color:var(--ink)}
.projectui-shortcut dd{display:flex;gap:.25rem;margin:0}
.projectui-shortcut kbd{display:inline-grid;place-items:center;min-inline-size:1.5rem;block-size:1.5rem;padding-inline:.375rem;border:1px solid var(--control-border);border-block-end-width:2px;border-radius:.3rem;background:var(--pu-subtle,var(--surface));font:inherit;font-size:.75rem;font-weight:600}

/* Keyboard card focus. */
.projectui-card:has(.projectui-card-title:focus-visible){border-color:var(--accent);box-shadow:0 0 0 2px color-mix(in srgb,var(--accent) 30%,transparent)}

/* Drop settle and rollback. */
@keyframes projectui-settle{0%{transform:scale(.965);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 40%,transparent)}60%{transform:scale(1.01)}100%{transform:none;box-shadow:var(--hcm-shadow-resting)}}
@keyframes projectui-highlight{0%,40%{background:color-mix(in srgb,var(--accent) 12%,var(--surface))}100%{background:var(--surface)}}
@keyframes projectui-return{0%{transform:translateX(0)}25%{transform:translateX(-5px)}50%{transform:translateX(4px)}75%{transform:translateX(-2px)}100%{transform:none}}
.projectui-card.is-settling{animation:projectui-settle 180ms var(--hcm-motion-easing,ease-out),projectui-highlight 900ms ease-out}
.projectui-card.is-returning{animation:projectui-return 260ms ease-out}
@media (prefers-reduced-motion:reduce){.projectui-card.is-settling{animation:projectui-highlight 900ms step-end}.projectui-card.is-returning{animation:none}}

/* Modal title edits in place. */
.projectui-modal-title-editor{margin:0}
.projectui-modal-title-editor .projectui-title-input:not(#projectui-none){font-size:1.25rem;font-weight:650;line-height:1.3;letter-spacing:-.01em}
.projectui-modal-heading{min-inline-size:0;flex:1 1 auto}

/* B-43: the sticky settings footer casts an edge while content scrolls under it. */
.project-page-board-settings .project-page-form-actions{box-shadow:0 -10px 12px -10px color-mix(in srgb,var(--ink) 22%,transparent)}

@media (max-width:40rem){
/* B-42, B-44 */
.projectui-columns{align-items:flex-start}
.projectui-column:not(.projectui-column-cap){min-block-size:12rem}
.projectui-lane .projectui-cell:has(.projectui-empty-cell){min-block-size:0;padding-block:.25rem}
/* B-46: the view switch spans the width; the lane toggle is an icon beside the other tools. */
.projectui-actions:has(>.projectui-lane-tool) .project-page-segmented{flex:1 1 100%;order:-1}
.projectui-actions:has(>.projectui-lane-tool)>.project-page-disclosure{flex:1 1 0}
.projectui-actions>.projectui-lane-tool{flex:none;inline-size:2.75rem;min-block-size:2.75rem;justify-content:center;padding:0}
.projectui-actions>.projectui-lane-tool .projectui-lane-tool-label{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
/* Filters as a bottom sheet. */
.projectui-filter-toggle{display:inline-flex}
.projectui-filterbar{display:none}
.projectui-filters[data-filters-toggled="true"] .projectui-filterbar{position:fixed;z-index:70;inset:auto 0 0 0;display:flex;flex-direction:column;align-items:stretch;gap:.875rem;max-block-size:80dvh;overflow:auto;padding:1rem 1rem max(1rem,env(safe-area-inset-bottom));border-radius:var(--hcm-radius-surface) var(--hcm-radius-surface) 0 0;background:var(--surface);box-shadow:var(--hcm-shadow-raised)}
.projectui-filters[data-filters-toggled="true"]::before{content:"";position:fixed;z-index:69;inset:0;background:color-mix(in srgb,#000 38%,transparent)}
.projectui-filters[data-filters-toggled="true"] .projectui-filter-toggle{display:inline-flex}
.projectui-filter-search{flex:none;inline-size:100%}
.projectui-filter-search input:not(#projectui-none){min-block-size:2.75rem}
.projectui-filter-person{inline-size:2.75rem;block-size:2.75rem}
.projectui-filter-chip{min-block-size:2.75rem}
.projectui-filterbar>.projectui-filter-chip,.projectui-filterbar>.projectui-filter-clear{align-self:flex-start}
.projectui-filter-done{display:inline-flex;justify-content:center;min-block-size:2.75rem}
.projectui-shortcuts{inset-inline:1rem;inline-size:auto}
.projectui-modal-title-editor{grid-area:title}
}
`
