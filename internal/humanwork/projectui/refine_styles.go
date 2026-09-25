package projectui

// projectUIRefinementStyles is the round-two refinement layer. It comes last
// in Styles(), so each rule here overrides the earlier layers at equal
// specificity; it deliberately keeps the earlier rules readable as the base.
const projectUIRefinementStyles = `
.projectui-breadcrumb{min-inline-size:0}
.projectui-heading .projectui-breadcrumb ol{font-size:.8125rem}

.projectui-columns{align-items:stretch}
.projectui-column{max-block-size:none}
.projectui-column-body{flex:1 1 auto;overflow:visible;min-block-size:6rem}
.projectui-columns[data-lanes="true"]{max-block-size:none;overflow-y:visible}
.projectui-column-cap{position:static;box-shadow:none;margin-block-end:.25rem}

.projectui-empty-cell{min-block-size:1.5rem;border-color:transparent}
.projectui-columns.is-dragging .projectui-empty-cell{min-block-size:2.5rem;border-color:color-mix(in srgb,var(--ink) 16%,transparent)}
.projectui-lane-tool{min-block-size:2.25rem;min-height:2.25rem}
.projectui-lane-tool,.projectui-lane-tool:hover,.projectui-lane-tool:active,.projectui-lane-tool:focus{background:var(--surface);color:var(--ink)}

a.projectui-card-title:hover{text-decoration:none}
@media (hover:hover) and (pointer:fine){.projectui-board .projectui-card[draggable="true"]:hover::before{content:"";position:absolute;inset-block:.75rem;inset-inline-start:.25rem;inline-size:.25rem;background:radial-gradient(circle,color-mix(in srgb,var(--muted) 70%,transparent) 1px,transparent 1.5px) 0 0/.25rem .3rem;opacity:.9;pointer-events:none}}
.projectui-board .projectui-priority:is([data-level="high"]) .projectui-priority-text{position:static;inline-size:auto;block-size:auto;overflow:visible;clip-path:none;white-space:nowrap;font-weight:600}
.projectui-type{font-size:.75rem}

.projectui-card-title,.projectui-card-summary,.projectui-modal-title,.projectui-modal-description,.projectui-description-text,.projectui-description-input,.projectui-comment-body p,.projectui-menu-title{unicode-bidi:plaintext}

.projectui-due-icon[data-tone="overdue"]{border-radius:50%;background:currentColor;border-color:currentColor}
.projectui-due-icon[data-tone="overdue"]::after{inset-inline-start:calc(50% - .75px);inset-block-start:.1rem;inline-size:1.5px;block-size:.22rem;background:var(--surface);box-shadow:0 .3rem 0 0 var(--surface)}
.projectui-due-none{background:transparent;color:var(--muted)}
.projectui-assignee-none .projectui-assignee-name{color:var(--muted)}

.projectui-card-menu{z-index:30}
.projectui-card:has(.projectui-card-menu[open]){z-index:45;transform:none}
.projectui-lane[data-collapsed="true"]:not(:has(.projectui-cell-tally[data-empty="false"])) .projectui-cell{display:none}
.projectui-card-menu[open]{z-index:40}
.projectui-card-menu-panel{position:absolute;inset-block-start:calc(100% + .25rem);inset-inline-end:0;display:grid;gap:.125rem;min-inline-size:14rem;max-inline-size:18rem;padding:.375rem;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-surface);background:var(--surface);box-shadow:var(--hcm-shadow-raised)}
.projectui-card-menu-panel>.projectui-card-controls{position:static;padding:0;border:0;box-shadow:none;min-inline-size:0}
.projectui-menu-title{margin:0;padding:.375rem .5rem .5rem;border-block-end:1px solid var(--pu-line);color:var(--muted);font-size:.75rem;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-inline-size:none}
.projectui-menu-item{display:flex;align-items:center;gap:.625rem;inline-size:100%;min-block-size:2.25rem;min-height:2.25rem;block-size:auto;padding:.25rem .5rem;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;font-size:.875rem;font-weight:500;text-align:start;text-decoration:none;cursor:pointer}
.projectui-menu-item:hover,.projectui-menu-item:focus-visible{background:var(--hcm-hover-surface)}
.projectui-menu-item[data-copied-state="true"]{color:var(--hcm-color-success)}
.projectui-menu-group{display:grid;gap:.25rem;padding:.375rem .5rem;border-block:1px solid var(--pu-line);margin-block:.125rem}
.projectui-menu-label{color:var(--muted);font-size:.75rem;font-weight:600}
.projectui-menu-group .projectui-card-controls{display:grid;gap:.375rem}
.projectui-menu-group .projectui-control select:not(#projectui-none){inline-size:100%;justify-content:flex-start}
.projectui-menu-icon{position:relative;flex:none;inline-size:1rem;block-size:1rem;color:var(--muted)}
.projectui-menu-icon[data-icon="open"]::before{content:"";position:absolute;inset-block-start:.45rem;inset-inline-start:.1rem;inline-size:.8rem;block-size:1.5px;background:currentColor;transform:rotate(-45deg)}
.projectui-menu-icon[data-icon="open"]::after{content:"";position:absolute;inset-block-start:.1rem;inset-inline-end:.1rem;inline-size:.45rem;block-size:.45rem;border-top:1.5px solid currentColor;border-right:1.5px solid currentColor}
[dir="rtl"] .projectui-menu-icon[data-icon="open"]{transform:scaleX(-1)}
.projectui-menu-icon[data-icon="assign"]::before{content:"";position:absolute;inset-block-start:.05rem;inset-inline-start:.3rem;inline-size:.4rem;block-size:.4rem;border:1.5px solid currentColor;border-radius:50%}
.projectui-menu-icon[data-icon="assign"]::after{content:"";position:absolute;inset-block-end:.05rem;inset-inline:.1rem;block-size:.4rem;border:1.5px solid currentColor;border-block-end:0;border-radius:.4rem .4rem 0 0}
.projectui-menu-icon[data-icon="link"]::before,.projectui-menu-icon[data-icon="link"]::after{content:"";position:absolute;inline-size:.55rem;block-size:.35rem;border:1.5px solid currentColor;border-radius:.3rem;transform:rotate(-45deg)}
.projectui-menu-icon[data-icon="link"]::before{inset-block-start:.2rem;inset-inline-start:.05rem}
.projectui-menu-icon[data-icon="link"]::after{inset-block-start:.45rem;inset-inline-start:.35rem}
@media (pointer:coarse){.projectui-card-menu>summary{inline-size:2.75rem;block-size:2.75rem;min-inline-size:44px;min-block-size:44px}.projectui-menu-item{min-block-size:2.75rem}.projectui-lane-tool,.projectui-link-button,.projectui-comment-edit>summary{min-block-size:2.75rem;display:inline-flex;align-items:center}}
@media (max-width:40rem){
.projectui-card-menu[open]>summary::before{content:"";position:fixed;inset:0;z-index:-1;background:color-mix(in srgb,#000 38%,transparent)}
.projectui-card-menu-panel{position:fixed;inset:auto 0 0 0;max-inline-size:none;min-inline-size:0;padding:.5rem .75rem max(.75rem,env(safe-area-inset-bottom));border-end-start-radius:0;border-end-end-radius:0}
.projectui-menu-item{min-block-size:2.75rem;font-size:.9375rem}
}

.projectui-modal-head{align-items:flex-start}
.projectui-modal-tools{display:flex;align-items:center;gap:.25rem;flex:none}
.projectui-modal-open{min-block-size:2.25rem;padding-inline:.625rem;border-color:transparent;background:transparent;color:var(--accent)}
.projectui-modal-open:hover{background:var(--hcm-hover-surface)}
.projectui-modal-side .projectui-field{grid-template-columns:minmax(6rem,max-content) minmax(0,1fr)}
.project-task-dialog .projectui-modal-body{border-block-end:0}

.projectui-date-field{position:relative;display:inline-flex;align-items:center;gap:.5rem;inline-size:100%;min-block-size:2.25rem;padding-inline:.5rem .625rem;border:1px solid transparent;border-radius:var(--hcm-radius-control);cursor:pointer}
.projectui-date-field:hover{border-color:var(--control-border);background:var(--surface)}
.projectui-date-field:focus-within{border-color:var(--accent);background:var(--surface);outline:2px solid var(--hcm-color-focus);outline-offset:1px}
.projectui-date-field .projectui-date-empty{color:var(--muted);font-size:.875rem}
.projectui-date-chevron{margin-inline-start:auto;inline-size:.4rem;block-size:.4rem;border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor;transform:rotate(45deg) translateY(-2px);color:var(--ink)}
.projectui-fields .projectui-date-field .projectui-field-date:not(#projectui-none){position:absolute;inset:0;inline-size:100%;block-size:100%;min-height:0;min-block-size:0;padding:0;border:0;opacity:0;cursor:pointer}
.projectui-date-field .projectui-field-date::-webkit-calendar-picker-indicator{position:absolute;inset:0;inline-size:100%;block-size:100%;margin:0;padding:0;cursor:pointer}

.projectui-title-input:not(#projectui-none){cursor:text}
.projectui-title-input:not(#projectui-none):hover{background:var(--pu-subtle)}
.projectui-description-input:not(#projectui-none){field-sizing:content;min-block-size:3rem;min-height:3rem;resize:none;padding:.375rem .5rem;margin-inline-start:-.5rem;background:transparent;border-color:transparent;cursor:text}
.projectui-description-input:not(#projectui-none):hover{background:var(--pu-subtle);border-color:transparent}
.projectui-description-input:not(#projectui-none):placeholder-shown{color:var(--muted)}
.projectui-comment-tools{align-items:center}
.projectui-comment-edit>summary{display:inline-flex;align-items:center}
.projectui-danger-link:hover{color:var(--hcm-color-danger)}
.projectui-empty-comments{justify-items:start;text-align:start;padding:1rem 1.125rem;border-style:solid;border-width:1px;background:var(--pu-subtle)}
.projectui-empty-comments .projectui-empty-comments-title{color:var(--ink);font-weight:600}
.projectui-empty-comments-icon{display:none}
@media (max-width:40rem){
}
/* Round three. */
.projectui-breadcrumb li{unicode-bidi:isolate}
.projectui-breadcrumb .projectui-crumb-key{color:var(--muted);font-variant-numeric:tabular-nums;letter-spacing:.02em}

.projectui-status-readonly{position:relative;min-block-size:1.75rem;padding-inline:.5625rem .625rem;border-radius:999px;background:var(--pu-subtle);cursor:help}
.projectui-lock-icon{position:relative;flex:none;inline-size:.625rem;block-size:.75rem;margin-inline-start:.125rem;color:var(--muted)}
.projectui-lock-icon::before{content:"";position:absolute;inset-block-start:0;inset-inline-start:.125rem;inline-size:.375rem;block-size:.375rem;box-sizing:border-box;border:1.5px solid currentColor;border-block-end:0;border-radius:.25rem .25rem 0 0}
.projectui-lock-icon::after{content:"";position:absolute;inset-block-end:0;inset-inline:0;block-size:.4375rem;border-radius:1.5px;background:currentColor}
.projectui-field:has(.projectui-status-locked-wrap){align-items:start}
.projectui-field:has(.projectui-status-locked-wrap)>.projectui-field-label{padding-block-start:.25rem}
.projectui-status-locked-wrap{display:grid;justify-items:start;gap:.3125rem;min-inline-size:0}
.projectui-lock-caption{display:none}
.projectui-status-locked-wrap>.projectui-lock-caption,.projectui-menu-locked>.projectui-lock-caption{display:block;margin:0;color:var(--muted);font-size:.8125rem;line-height:1.4;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-inline-size:100%}
.projectui-menu-locked{gap:.375rem;padding:.5rem}

.projectui-menu-group{gap:.0625rem;padding:.25rem 0;margin-block:.125rem}
.projectui-menu-group+.projectui-menu-group{border-block-start:0;margin-block-start:-.125rem}
.projectui-menu-label{padding:.25rem .5rem .125rem}
.projectui-menu-radio .projectui-status-glyph{flex:none}
.projectui-menu-radio .projectui-lane-glyph{position:relative;inset:auto;flex:none;margin-inline:.1875rem}
.projectui-menu-radio .projectui-menu-item-label{flex:1 1 auto;min-inline-size:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.projectui-menu-check{flex:none;inline-size:.375rem;block-size:.625rem;margin-inline-end:.25rem;border-right:2px solid transparent;border-bottom:2px solid transparent;transform:rotate(45deg) translate(-1px,-1px)}
.projectui-menu-radio[aria-checked="true"]{font-weight:650}
.projectui-menu-radio[aria-checked="true"] .projectui-menu-check{border-color:var(--accent)}
.projectui-menu-radio:disabled{opacity:.55;cursor:progress}
.projectui-menu-radio[aria-disabled="true"]{color:var(--muted);cursor:not-allowed}
.projectui-menu-radio[aria-disabled="true"]:hover{background:transparent}
.projectui-menu-radio[aria-disabled="true"] .projectui-status-glyph{opacity:.5}
.projectui-menu-reason{flex:none;color:var(--muted);font-size:.75rem;font-weight:500}

@media (hover:hover) and (pointer:fine){.projectui-board .projectui-card[draggable="true"]{cursor:grab;padding-inline-start:1.125rem}.projectui-board .projectui-card[draggable="true"]:hover::before{inset-block:.875rem auto;inset-inline-start:.125rem;inline-size:.5625rem;block-size:1rem;background:radial-gradient(circle,var(--muted) 1.25px,transparent 1.5px) 0 0/.28125rem .3333rem;opacity:.75}}
.projectui-board .projectui-card[draggable="true"] :is(a,button,summary){cursor:pointer}

.projectui-empty-cell{background:transparent}
.projectui-empty-cell-note{display:none}
.projectui-cell:has(>.projectui-empty-cell){background:transparent}
.projectui-columns.is-dragging .projectui-cell:has(>.projectui-empty-cell){background:var(--pu-col-bg)}
.projectui-lane-tool:hover,.projectui-lane-tool:active{background:var(--surface);border-color:color-mix(in srgb,var(--ink) 26%,var(--surface))}

.projectui-list .projectui-status-readonly{justify-self:start}
.projectui-sort{display:inline-flex;align-items:center;gap:.25rem;min-block-size:1.75rem;margin-inline:-.375rem;padding-inline:.375rem;border-radius:var(--hcm-radius-control);color:inherit;text-decoration:none}
.projectui-sort:hover{background:color-mix(in srgb,var(--ink) 6%,transparent);color:var(--ink)}
.projectui-sort[data-active="true"]{color:var(--ink)}
.projectui-sort-arrow{color:var(--accent);font-weight:700}
.projectui-sort-arrow.projectui-sort-idle{color:currentColor;font-weight:500;opacity:.35}
.projectui-sort:hover .projectui-sort-idle{opacity:1}

.projectui-modal-open .projectui-menu-icon{color:currentColor}

.projectui-select-wrap .projectui-avatar{inline-size:1.5rem;block-size:1.5rem;font-size:.75rem}
.projectui-fields .projectui-due{padding:0;border:0;background:transparent;font-size:inherit;font-weight:500}
.projectui-fields .projectui-date-field{font-size:.9375rem}

.projectui-description-section>.projectui-section-head{margin-block-end:-.25rem;justify-content:flex-start;gap:.75rem}
.projectui-edit-label{font-size:.8125rem;color:var(--accent);font-weight:600}
.projectui-description-input:not(#projectui-none):hover{border-radius:var(--hcm-radius-control)}

:root[data-hcm-color-mode="dark"] .projectui-button-primary:not(#projectui-none){border-color:transparent;background:var(--pu-primary-dark);color:#fff}
:root[data-hcm-color-mode="dark"] .projectui-button-primary:not(#projectui-none):hover{background:var(--pu-primary-dark-hover)}
@media (prefers-color-scheme:dark){:root[data-hcm-color-mode="system"] .projectui-button-primary:not(#projectui-none){border-color:transparent;background:var(--pu-primary-dark);color:#fff}:root[data-hcm-color-mode="system"] .projectui-button-primary:not(#projectui-none):hover{background:var(--pu-primary-dark-hover)}}
:root{--pu-primary-dark:color-mix(in srgb,var(--hcm-color-brand-primary) 88%,#000);--pu-primary-dark-hover:var(--hcm-color-brand-primary)}

@media (max-width:40rem){
.projectui-breadcrumb li:not(.projectui-crumb-back){display:none}
.projectui-breadcrumb .projectui-crumb-back::before{content:"";inline-size:.4rem;block-size:.4rem;border:0;border-left:1.5px solid currentColor;border-bottom:1.5px solid currentColor;transform:rotate(45deg);margin-inline-end:.25rem;opacity:1}
.projectui-breadcrumb .projectui-crumb-back a{text-decoration:none;color:var(--accent);font-weight:600}
[dir="rtl"] .projectui-breadcrumb .projectui-crumb-back::before{transform:rotate(-135deg)}
.projectui-actions:has(>.projectui-lane-tool) .project-page-segmented{flex:1 1 calc(100% - 9.5rem)}
.projectui-actions:has(>.projectui-lane-tool)>.project-page-disclosure{flex:1 1 40%}
.projectui-actions>.projectui-lane-tool{flex:none;min-block-size:2.75rem}
.projectui-lane .projectui-cell{align-self:start}
.projectui-lane .projectui-cell:has(>.projectui-empty-cell){min-block-size:1.5rem;padding-block:.375rem}
.projectui-empty-cell:has(>.projectui-empty-cell-note){min-block-size:0;border:0}
.projectui-empty-cell-note{display:block;padding:.25rem .125rem;color:var(--muted);font-size:.8125rem}
.projectui-list .projectui-due-none{display:none}
.projectui-columns:has(.projectui-card-menu[open]){-webkit-mask-image:none;mask-image:none}
.projectui-modal-head{display:grid;grid-template-columns:minmax(0,1fr) auto;grid-template-areas:"key close" "title title" "link link";gap:.375rem .5rem}
.projectui-modal-heading,.projectui-modal-tools{display:contents}
.projectui-modal-kicker{grid-area:key;align-self:center}
.projectui-modal-title{grid-area:title}
.projectui-modal-open{grid-area:link;justify-self:start;margin-inline-start:-.625rem}
.projectui-modal-close{grid-area:close}
}
`
