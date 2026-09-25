package projectui

// projectUIShareStyles covers sharing, labels and story points, the
// restructured task page and the cross-project tickets list.
const projectUIShareStyles = `
.projectui-tickets,.projectui-share-host{--pu-line:color-mix(in srgb,var(--ink) 11%,var(--surface));--pu-subtle:color-mix(in srgb,var(--ink) 5%,var(--surface));--pu-col-bg:color-mix(in srgb,var(--ink) 4.5%,var(--canvas))}

/* Share menu. */
.projectui-share{position:relative;z-index:31}
.projectui-share[open]{z-index:41}
.projectui-share>summary{display:inline-flex;align-items:center;gap:.45rem;min-block-size:2.25rem;padding-inline:.75rem .875rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-size:.875rem;font-weight:600;list-style:none;cursor:pointer;white-space:nowrap}
.projectui-share>summary::-webkit-details-marker{display:none}
.projectui-share>summary:hover{background:var(--hcm-hover-surface)}
.projectui-share[open]>summary{border-color:var(--accent)}
.projectui-share-icon{position:relative;inline-size:.875rem;block-size:.875rem}
.projectui-share-icon::before{content:"";position:absolute;inset:.3rem 0 0;border:1.5px solid currentColor;border-block-start:0;border-radius:0 0 .2rem .2rem}
.projectui-share-icon::after{content:"";position:absolute;inset-block-start:0;inset-inline-start:calc(50% - .75px);inline-size:1.5px;block-size:.6rem;background:currentColor;box-shadow:-.18rem .12rem 0 -.02rem transparent}
.projectui-share-panel{inset-inline-end:0;min-inline-size:13rem}
.projectui-menu-icon[data-icon="chat"]::before{content:"";position:absolute;inset:.1rem .05rem .25rem;border:1.5px solid currentColor;border-radius:.3rem}
.projectui-menu-icon[data-icon="chat"]::after{content:"";position:absolute;inset-block-end:.05rem;inset-inline-start:.25rem;inline-size:.3rem;block-size:.3rem;border-inline-start:1.5px solid currentColor;border-block-end:1.5px solid currentColor;transform:skewY(-30deg)}
.projectui-menu-icon[data-icon="docs"]::before{content:"";position:absolute;inset:.05rem .15rem;border:1.5px solid currentColor;border-radius:.15rem}
.projectui-menu-icon[data-icon="docs"]::after{content:"";position:absolute;inset-inline:.35rem;inset-block-start:.35rem;block-size:.3rem;border-block:1.5px solid currentColor}
.projectui-menu-share{border-block-end:0}
.projectui-menu-item[data-copied-state="true"] .projectui-menu-icon{color:var(--hcm-color-success)}

/* Send-to-chat dialog and toast. */
.projectui-share-dialog{inline-size:min(28rem,calc(100vw - 2rem));max-inline-size:none;padding:0;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-surface);background:var(--surface);color:var(--ink);box-shadow:var(--hcm-shadow-raised)}
.projectui-share-dialog::backdrop{background:color-mix(in srgb,#000 40%,transparent)}
.projectui-share-form{display:grid;gap:.5rem;padding:1.125rem 1.25rem 1.25rem}
.projectui-share-head{display:flex;align-items:center;justify-content:space-between;gap:.75rem}
.projectui-share-head h2{margin:0;font-size:1.0625rem;font-weight:650}
.projectui-share-item{display:flex;align-items:center;gap:.5rem;margin:0 0 .25rem;padding:.625rem .75rem;border-radius:var(--hcm-radius-control);background:var(--pu-subtle);font-size:.875rem;font-weight:600;overflow-wrap:anywhere}
.projectui-share-item:empty{display:none}
.projectui-share-label{margin-block-start:.25rem;font-size:.8125rem;font-weight:600;color:var(--ink)}
.projectui-share-form :is(select,textarea):not(#projectui-none){inline-size:100%;min-block-size:2.5rem;padding:.5rem .75rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.9375rem}
.projectui-share-form textarea:not(#projectui-none){min-block-size:4.5rem;resize:vertical}
.projectui-share-error{margin:0;padding:.5rem .75rem;border-radius:var(--hcm-radius-control);background:var(--hcm-color-danger-surface);color:var(--hcm-color-danger);font-size:.8125rem}
.projectui-share-error[hidden]{display:none}
.projectui-share-actions{display:flex;justify-content:flex-end;gap:.5rem;margin-block-start:.5rem}
.projectui-share-form[aria-busy="true"] button[type="submit"]{opacity:.6;cursor:progress}
.projectui-toast{position:fixed;z-index:80;inset-block-end:1.25rem;inset-inline-start:50%;display:flex;align-items:center;gap:1rem;padding:.75rem 1rem;border-radius:var(--hcm-radius-surface);background:var(--ink);color:var(--surface);box-shadow:var(--hcm-shadow-raised);font-size:.875rem;transform:translateX(-50%)}
[dir="rtl"] .projectui-toast{transform:translateX(50%)}
.projectui-toast[hidden]{display:none}
.projectui-toast-link{color:inherit;font-weight:700;text-decoration:underline;text-underline-offset:2px}

/* Labels and story points. */
.projectui-labels{display:inline-flex;flex-wrap:wrap;align-items:center;gap:.25rem;min-inline-size:0}
.projectui-label{display:inline-flex;align-items:center;gap:.25rem;max-inline-size:10rem;min-block-size:1.25rem;padding-inline:.4375rem;border-radius:999px;background:color-mix(in srgb,var(--pu-label-color,var(--muted)) 14%,var(--surface));color:color-mix(in srgb,var(--pu-label-color,var(--muted)) 70%,var(--ink));font-size:.75rem;font-weight:600;line-height:1.1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.projectui-label[data-hue="0"],.projectui-label[data-hue="6"]{--pu-label-color:#2563eb}
.projectui-label[data-hue="1"],.projectui-label[data-hue="7"]{--pu-label-color:#0f766e}
.projectui-label[data-hue="2"],.projectui-label[data-hue="8"]{--pu-label-color:#a16207}
.projectui-label[data-hue="3"],.projectui-label[data-hue="9"]{--pu-label-color:#be185d}
.projectui-label[data-hue="4"],.projectui-label[data-hue="10"]{--pu-label-color:#7c3aed}
.projectui-label[data-hue="5"],.projectui-label[data-hue="11"]{--pu-label-color:#c2410c}
.projectui-label-more{--pu-label-color:var(--muted)}
.projectui-points{display:inline-grid;place-items:center;min-inline-size:1.375rem;block-size:1.25rem;padding-inline:.3rem;border-radius:.375rem;background:var(--pu-subtle);box-shadow:inset 0 0 0 1px var(--pu-line);color:var(--ink);font-size:.75rem;font-weight:700;font-variant-numeric:tabular-nums}
.projectui-card-meta-row .projectui-labels{flex:0 1 auto;overflow:hidden}
.projectui-label-editor{display:flex;flex-wrap:wrap;align-items:center;gap:.375rem;inline-size:100%;min-block-size:2.25rem;padding:.25rem .375rem;border:1px solid transparent;border-radius:var(--hcm-radius-control)}
.projectui-label-editor:hover,.projectui-label-editor:focus-within{border-color:var(--control-border);background:var(--surface)}
.projectui-label-editor:focus-within{border-color:var(--accent);outline:2px solid var(--hcm-color-focus);outline-offset:1px}
.projectui-label-editor .projectui-label{min-block-size:1.5rem;max-inline-size:12rem;padding-inline-end:.125rem;font-size:.8125rem}
.projectui-label-remove{display:inline-grid;place-items:center;inline-size:1.125rem;block-size:1.125rem;padding:0;border:0;border-radius:50%;background:transparent;color:inherit;font:inherit;font-size:.875rem;line-height:1;cursor:pointer;opacity:.7}
.projectui-label-remove:hover{opacity:1;background:color-mix(in srgb,currentColor 16%,transparent)}
.projectui-label-input:not(#projectui-none){flex:1 1 6rem;min-inline-size:6rem;min-block-size:1.75rem;min-height:1.75rem;padding:.125rem .25rem;border:0;background:transparent;color:var(--ink);font:inherit;font-size:.875rem;box-shadow:none;outline:none}
.projectui-fields .projectui-field-number:not(#projectui-none){inline-size:9rem;min-block-size:2.25rem;min-height:2.25rem;padding-inline:.5rem;border:1px solid transparent;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;font-size:.9375rem;font-variant-numeric:tabular-nums}
.projectui-fields .projectui-field-number:not(#projectui-none):focus-visible{border-color:var(--accent);background:var(--surface);outline:2px solid var(--hcm-color-focus);outline-offset:1px}
@media (hover:hover){.projectui-fields .projectui-field-number:not(#projectui-none):hover{border-color:var(--control-border);background:var(--surface)}}
.projectui-date-text{font-size:.9375rem}
.projectui-date-line{margin:0;color:var(--muted);font-size:.875rem}
.projectui-date-line+.projectui-date-line{margin-block-start:.25rem}

/* Task page structure. */
.projectui-taskpage-head{display:flex;flex-wrap:wrap;align-items:flex-start;justify-content:space-between;gap:.75rem 1.5rem;min-inline-size:0}
.projectui-taskpage-headline{display:grid;gap:.25rem;flex:1 1 28rem;min-inline-size:0}
.projectui-taskpage-headline .projectui-task-key{font-size:.8125rem}
.projectui-taskpage-tools{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem;flex:none}
.projectui-taskpage-status{display:flex;flex-direction:column;align-items:flex-end;gap:.25rem;min-inline-size:11rem}
.projectui-taskpage-status .projectui-status-wrap .projectui-field-select:not(#projectui-none){min-block-size:2.25rem;font-weight:600}
.projectui-taskpage-status .projectui-status-wrap{min-inline-size:10rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface)}
.projectui-taskpage-status .projectui-status-wrap[data-tone="done"]{border-color:color-mix(in srgb,var(--hcm-color-success) 45%,var(--surface));background:color-mix(in srgb,var(--hcm-color-success) 8%,var(--surface))}
.projectui-taskpage-status .projectui-status-wrap[data-tone="active"]{border-color:color-mix(in srgb,var(--accent) 45%,var(--surface))}
.projectui-taskpage-status .projectui-status-locked-wrap{justify-items:end}
.projectui-taskpage-status .projectui-lock-caption{display:none}
.projectui-taskpage-status .projectui-field-status:empty{display:none}
.projectui-details-block .projectui-fields{gap:.125rem}
.projectui-details-block .projectui-field{grid-template-columns:7rem minmax(0,1fr)}
.projectui-thread{gap:.75rem}
.projectui-thread-tabs{display:inline-flex;align-self:start;justify-self:start;gap:2px;padding:2px;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-control);background:var(--pu-subtle)}
.projectui-thread-radio{position:absolute;opacity:0;pointer-events:none;inline-size:1px;block-size:1px}
.projectui-thread-tab{display:inline-flex;align-items:center;gap:.4rem;min-block-size:2.125rem;padding-inline:.875rem;border-radius:calc(var(--hcm-radius-control) - 2px);color:var(--muted);font-size:.875rem;font-weight:600;cursor:pointer}
.projectui-thread-radio:checked+.projectui-thread-tab{background:var(--surface);color:var(--ink);box-shadow:0 1px 2px color-mix(in srgb,var(--ink) 16%,transparent)}
.projectui-thread-radio:focus-visible+.projectui-thread-tab{outline:2px solid var(--hcm-color-focus);outline-offset:1px}
.projectui-thread-panel{display:grid;gap:.75rem}
.projectui-thread:has(#projectui-thread-comments:checked) .projectui-thread-panel[data-panel="activity"],.projectui-thread:has(#projectui-thread-activity:checked) .projectui-thread-panel[data-panel="comments"]{display:none}
.projectui-thread-panel .projectui-section-head{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%)}
.projectui-thread{position:relative}
.projectui-side-card .projectui-fields{gap:.125rem}
.projectui-taskpage-side{display:grid;gap:1rem}

/* Projects home tabs. */
.project-page-tabs{display:flex;flex-wrap:wrap;gap:.25rem;margin-block-start:-.25rem;border-block-end:1px solid color-mix(in srgb,var(--ink) 11%,var(--surface))}
.project-page-tab{position:relative;display:inline-flex;align-items:center;gap:.4rem;min-block-size:2.5rem;padding-inline:.875rem;color:var(--muted);font-size:.9375rem;font-weight:600;text-decoration:none}
.project-page-tab:hover{color:var(--ink)}
.project-page-tab[aria-current="page"]{color:var(--ink)}
.project-page-tab[aria-current="page"]::after{content:"";position:absolute;inset-inline:.5rem;inset-block-end:-1px;block-size:2px;border-radius:2px;background:var(--accent)}
.project-page-tab:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:-2px;border-radius:var(--hcm-radius-control)}

.project-page-tab-count{display:inline-grid;place-items:center;min-inline-size:1.5rem;block-size:1.375rem;padding-inline:.4rem;border-radius:999px;background:color-mix(in srgb,var(--ink) 7%,var(--surface));color:var(--muted);font-size:.75rem;font-weight:700;font-variant-numeric:tabular-nums}
.project-page-tab[aria-selected="true"] .project-page-tab-count{background:color-mix(in srgb,var(--accent) 16%,var(--surface));color:var(--ink)}

/* My tickets / All tickets. */
.project-tickets-section-title{display:flex;align-items:center;gap:.5rem;margin:0 0 .5rem;font-size:1rem;font-weight:650;color:var(--ink)}
.project-tickets-section-title .projectui-count,.project-my-tickets-group-title .projectui-count{font-size:.75rem}
.project-my-tickets{display:grid;gap:.25rem}
.project-my-tickets-toggle{display:inline-flex;align-items:center;gap:.5rem;min-block-size:2.25rem;margin-inline-start:-.5rem;padding-inline:.5rem;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:inherit;font:inherit;font-weight:650;cursor:pointer}
.project-my-tickets-toggle:hover{background:var(--hcm-hover-surface)}
.project-my-tickets-toggle:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}
.project-my-tickets-chevron{inline-size:.45rem;block-size:.45rem;border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor;transform:rotate(45deg) translate(-1px,-1px);transition:transform var(--hcm-motion-fast) var(--hcm-motion-easing)}
.project-my-tickets[data-open="false"] .project-my-tickets-chevron{transform:rotate(-45deg)}
[dir="rtl"] .project-my-tickets[data-open="false"] .project-my-tickets-chevron{transform:rotate(135deg)}
.project-my-tickets-body[hidden]{display:none}
.project-my-tickets-group+.project-my-tickets-group{border-block-start:1px solid var(--pu-line)}
.project-my-tickets .projectui-tickets-td[data-col="assignee"]>*{visibility:hidden}
.project-tickets-section-title,.project-my-tickets-group-title{max-inline-size:none}
.project-my-tickets-group-title{display:flex;align-items:center;gap:.5rem;margin:0;padding:.5rem 1rem;background:var(--pu-subtle);color:var(--muted);font-size:.75rem;font-weight:700;letter-spacing:.02em;text-transform:uppercase}
.project-my-tickets-group[data-urgency="0"] .project-my-tickets-group-title{color:var(--hcm-color-danger)}
.project-my-tickets-group[data-urgency="1"] .project-my-tickets-group-title{color:var(--hcm-color-warning)}
.project-my-tickets-empty{display:grid;gap:.25rem;padding:1.25rem 1rem;color:var(--muted)}
.project-my-tickets-empty p{margin:0}
.project-my-tickets-empty-title{color:var(--ink);font-weight:600}
.project-all-tickets{display:grid;gap:.25rem}
.projectui-tickets .projectui-tickets-search{flex:0 1 18rem}
.projectui-tickets-toolbar .projectui-tickets-filters{flex:1 1 20rem}

/* Table footers: range, rows per page, pages. */
.projectui-pager-footer{display:flex;flex-wrap:wrap;align-items:center;justify-content:flex-end;gap:.5rem 1.25rem;padding:.625rem 1rem;color:var(--muted);font-size:.875rem}
.projectui-list-frame .projectui-pager-footer{border-block-start:1px solid var(--pu-line)}
.projectui-pager-range{margin:0;margin-inline-end:auto;font-variant-numeric:tabular-nums}
.projectui-page-size{display:inline-flex;align-items:center;gap:.5rem;white-space:nowrap}
.projectui-page-size select:not(#projectui-none){min-block-size:2.25rem;min-height:2.25rem;padding-inline:.5rem 1.75rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background-color:var(--surface);color:var(--ink);font:inherit;font-size:.875rem}
.projectui-pager-pages{display:inline-flex;align-items:center;gap:.5rem}
.projectui-pager-pages .projectui-button[aria-disabled="true"]{opacity:.45;pointer-events:none}
.project-my-tickets-more{display:flex;justify-content:center;padding:.5rem;border-block-start:1px solid var(--pu-line)}

/* Linked workflows. */
.projectui-flow-chip{display:inline-flex;align-items:center;gap:.25rem;min-block-size:1.25rem;padding-inline:.375rem .4375rem;border-radius:999px;background:color-mix(in srgb,var(--accent) 12%,var(--surface));color:color-mix(in srgb,var(--accent) 75%,var(--ink));font-size:.75rem;font-weight:650;white-space:nowrap}
.projectui-flow-icon{position:relative;display:inline-block;flex:none;inline-size:.75rem;block-size:.75rem}
.projectui-flow-icon::before{content:"";position:absolute;inset-block-start:0;inset-inline-start:0;inline-size:.3rem;block-size:.3rem;border:1.5px solid currentColor;border-radius:50%;box-shadow:.42rem .42rem 0 -1.5px var(--surface),.42rem .42rem 0 0 currentColor}
.projectui-flow-icon::after{content:"";position:absolute;inset-block-start:.28rem;inset-inline-start:.28rem;inline-size:.25rem;block-size:.25rem;border-inline-end:1.5px solid currentColor;border-block-end:1.5px solid currentColor;border-end-end-radius:.2rem}
.projectui-tickets-chips{display:inline-flex;flex-wrap:wrap;align-items:center;gap:.25rem}
.projectui-workflows-head{justify-content:flex-start;gap:.625rem}
.projectui-workflows-head .projectui-workflow-add{margin-inline-start:auto;color:var(--accent);font-weight:600}
.projectui-workflows{display:grid;gap:.375rem;margin:0;padding:0;list-style:none}
.projectui-workflow{display:flex;align-items:center;gap:.75rem;min-block-size:3rem;padding:.5rem .75rem;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-control);background:var(--surface);max-inline-size:none}
.projectui-workflow-glyph{color:var(--accent);inline-size:1rem;block-size:1rem}
.projectui-workflow[data-state="restricted"] .projectui-workflow-glyph,.projectui-workflow[data-state="unavailable"] .projectui-workflow-glyph{color:var(--muted)}
.projectui-workflow-main{display:grid;gap:.25rem;flex:1 1 auto;min-inline-size:0}
.projectui-workflow-title{font-weight:600;font-size:.9375rem;color:var(--ink);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.projectui-workflow-meta{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem;font-size:.8125rem;color:var(--muted)}
.projectui-workflow-meta .projectui-avatar{inline-size:1.25rem;block-size:1.25rem;font-size:.6875rem}
.projectui-workflow-open{flex:none;min-block-size:2rem;padding-inline:.75rem;color:var(--accent)}
.projectui-workflow-unlink{display:grid;place-items:center;flex:none;inline-size:1.75rem;block-size:1.75rem;padding:0;border:0;border-radius:50%;background:transparent;color:var(--muted);font:inherit;font-size:1rem;cursor:pointer}
.projectui-workflow-unlink:hover{background:var(--hcm-hover-surface);color:var(--hcm-color-danger)}
.projectui-workflows-empty{margin:0}
.projectui-workflow-picker{inline-size:min(34rem,calc(100vw - 2rem))}
.projectui-picker-body{gap:.75rem}
.projectui-picker-tools{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem}
.projectui-picker-tools .projectui-filter-search{flex:1 1 14rem}
.projectui-picker-status .projectui-thread-tab[aria-checked="true"]{background:var(--surface);color:var(--ink);box-shadow:0 1px 2px color-mix(in srgb,var(--ink) 16%,transparent)}
.projectui-picker-groups{display:grid;gap:.75rem;max-block-size:min(55dvh,26rem);margin:0;padding:0;overflow:auto;list-style:none}
.projectui-picker-group{margin:0;max-inline-size:none}
.projectui-picker-group[hidden],.projectui-picker-item[hidden]{display:none}
.projectui-picker-group-title{display:flex;align-items:center;gap:.4rem;margin:0 0 .375rem;color:var(--muted);font-size:.75rem;font-weight:700;letter-spacing:.02em;text-transform:uppercase}
.projectui-picker-list{display:grid;gap:.25rem;margin:0;padding:0;list-style:none}
.projectui-picker-item{margin:0;max-inline-size:none}
.projectui-picker-row{display:grid;gap:.25rem;inline-size:100%;padding:.5rem .75rem;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-control);background:var(--surface);color:inherit;font:inherit;text-align:start;cursor:pointer}
.projectui-picker-row:hover{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 6%,var(--surface))}
.projectui-picker-row:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:1px}
.projectui-picker-row:disabled{cursor:default;opacity:.7}
.projectui-picker-empty,.projectui-picker-nomatch{margin:0}

.projectui-workflow-change{font-variant-numeric:tabular-nums;color:var(--ink);font-weight:500}
.projectui-board-workflows-panel{min-inline-size:min(26rem,calc(100vw - 2rem));max-block-size:min(70dvh,32rem);overflow:auto;padding:.5rem}
.projectui-board-workflows-panel .projectui-menu-title{white-space:normal;font-weight:500}
.projectui-board-workflows>summary .projectui-filter-count{margin-inline-start:.125rem}
.jn-detail-toolbar{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.75rem}
.projectui-journey-actions{display:inline-flex;flex-wrap:wrap;align-items:center;gap:.5rem}
.projectui-journey-link{display:inline-flex;align-items:center;gap:.45rem;min-block-size:2.25rem;padding-inline:.75rem .875rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;font-weight:600;cursor:pointer}
.projectui-journey-link:hover{background:var(--hcm-hover-surface)}
.projectui-journey-actions .projectui-share-panel{inset-inline-end:0}
.projectui-ticket-linker .projectui-share-select{inline-size:100%;min-block-size:2.5rem;padding:.5rem .75rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit}
.projectui-ticket-linker .projectui-filter-search input{inline-size:100%;min-block-size:2.25rem;padding-inline:2rem .75rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit}
.projectui-linker-list{max-block-size:min(45dvh,22rem);overflow:auto}
.projectui-linker-list .projectui-picker-row{display:flex;align-items:center;justify-content:space-between;gap:.75rem}
.projectui-status-pill[data-tone="blocked"]{background:color-mix(in srgb,var(--hcm-color-danger) 12%,var(--surface));color:var(--hcm-color-danger)}

/* Board edge scroll buttons. */
.projectui-columns-frame{position:relative;min-inline-size:0}
.projectui-scroll-btn{position:absolute;z-index:5;inset-block-start:min(40%,14rem);display:none;place-items:center;inline-size:2.5rem;block-size:2.5rem;padding:0;border:1px solid var(--control-border);border-radius:50%;background:var(--surface);color:var(--ink);box-shadow:var(--hcm-shadow-raised);cursor:pointer}
.projectui-scroll-btn:hover{background:var(--hcm-hover-surface)}
.projectui-scroll-btn:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}
.projectui-scroll-btn[data-dir="prev"]{inset-inline-start:-.75rem}
.projectui-scroll-btn[data-dir="next"]{inset-inline-end:-.75rem}
.projectui-columns-frame[data-can-prev="true"] .projectui-scroll-btn[data-dir="prev"],.projectui-columns-frame[data-can-next="true"] .projectui-scroll-btn[data-dir="next"]{display:grid}
.projectui-scroll-chevron{inline-size:.55rem;block-size:.55rem;border-right:2px solid currentColor;border-top:2px solid currentColor;transform:translateX(-2px) rotate(45deg)}
.projectui-scroll-btn[data-dir="prev"] .projectui-scroll-chevron{transform:translateX(2px) rotate(-135deg)}
[dir="rtl"] .projectui-scroll-btn[data-dir="next"] .projectui-scroll-chevron{transform:translateX(2px) rotate(-135deg)}
[dir="rtl"] .projectui-scroll-btn[data-dir="prev"] .projectui-scroll-chevron{transform:translateX(-2px) rotate(45deg)}
@media (max-width:40rem){.projectui-scroll-btn{display:none!important}}
.projectui-tickets .projectui-filterbar{display:flex}
@media (max-width:40rem){.projectui-tickets .projectui-tickets-filters{flex-wrap:nowrap;overflow-x:auto;padding-block-end:.25rem;inline-size:100%}}

/* Tickets list. */
.projectui-tickets{display:grid;gap:.75rem;min-inline-size:0;color:var(--ink)}
.projectui-tickets-toolbar{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.5rem 1rem}
.projectui-tickets-filters{flex:1 1 auto}
.projectui-tickets-count{margin:0;color:var(--muted);font-size:.875rem;white-space:nowrap}
.projectui-tickets-filter{position:relative;z-index:31}
.projectui-tickets-filter[open]{z-index:41}
.projectui-tickets-filter>summary{list-style:none;cursor:pointer}
.projectui-tickets-filter>summary::-webkit-details-marker{display:none}
.projectui-tickets-filter[data-active="true"]>summary{border-color:color-mix(in srgb,var(--accent) 55%,var(--surface));background:color-mix(in srgb,var(--accent) 12%,var(--surface))}
.projectui-tickets-chevron{inline-size:.35rem;block-size:.35rem;margin-inline-start:.125rem;border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor;transform:translateY(-.1rem) rotate(45deg)}
.projectui-tickets-panel{inset-inline-start:0;inset-inline-end:auto;max-block-size:18rem;overflow:auto}
.projectui-tickets-panel .projectui-menu-radio[aria-checked="true"] .projectui-menu-check{border-color:var(--accent)}
.projectui-tickets-mine .projectui-menu-icon{inline-size:.875rem;block-size:.875rem}
.projectui-tickets-table{border:1px solid var(--pu-line);border-radius:var(--hcm-radius-surface);background:var(--surface);overflow:hidden}
.projectui-tickets-head,.projectui-tickets-row{display:grid;grid-template-columns:6.5rem minmax(12rem,2.4fr) minmax(8rem,1.1fr) 8.5rem minmax(8rem,1fr) 6rem 6.5rem 6.5rem;align-items:center;column-gap:.875rem;padding-inline:1rem}
.projectui-tickets-head{min-block-size:2.5rem;border-block-end:1px solid var(--pu-line);background:var(--pu-subtle);color:var(--muted);font-size:.75rem;font-weight:650}
.projectui-tickets-th{min-inline-size:0}
.projectui-tickets-rows{margin:0;padding:0;list-style:none}
.projectui-tickets-item{margin:0;max-inline-size:none}
.projectui-tickets-item+.projectui-tickets-item{border-block-start:1px solid var(--pu-line)}
.projectui-tickets-row{min-block-size:3.25rem;padding-block:.5rem;color:var(--ink);text-decoration:none}
.projectui-tickets-row:hover{background:var(--hcm-hover-surface)}
.projectui-tickets-row:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:-2px}
.projectui-tickets-td{display:flex;align-items:center;gap:.375rem;min-inline-size:0;font-size:.875rem}
.projectui-tickets-td[data-col="title"]{flex-direction:column;align-items:flex-start;gap:.25rem}
.projectui-tickets-title-text{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-inline-size:100%}
.projectui-tickets-project{color:var(--muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.projectui-tickets-td .projectui-assignee-name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.projectui-tickets-empty{display:grid;justify-items:center;gap:.5rem;padding:2.5rem 1rem;color:var(--muted);text-align:center}
.projectui-tickets-empty p{margin:0}
.projectui-tickets-empty-title{color:var(--ink);font-weight:650}
.projectui-tickets-shimmer{display:grid;grid-template-columns:5rem 1fr 8rem;gap:1rem;padding:1rem;opacity:.7}
.projectui-tickets-shimmer span{block-size:.75rem;border-radius:999px;background:var(--pu-subtle)}
.projectui-tickets-pager{display:flex;align-items:center;justify-content:flex-end;gap:.75rem}
.projectui-tickets-page{color:var(--muted);font-size:.875rem}
@media (max-width:75rem){.projectui-tickets-head,.projectui-tickets-row{grid-template-columns:6rem minmax(10rem,2fr) minmax(7rem,1fr) 8rem minmax(7rem,1fr) 6rem}.projectui-tickets-th:is([data-col="priority"],[data-col="updated"]),.projectui-tickets-td:is([data-col="priority"],[data-col="updated"]){display:none}}
@media (max-width:48rem){
.projectui-tickets-head{display:none}
.projectui-tickets-row{grid-template-columns:minmax(0,1fr) auto;grid-template-areas:"key status" "title title" "meta meta";row-gap:.375rem;padding-block:.75rem}
.projectui-tickets-td[data-col="key"]{grid-area:key}
.projectui-tickets-td[data-col="status"]{grid-area:status;justify-content:flex-end}
.projectui-tickets-td[data-col="title"]{grid-area:title}
.projectui-tickets-td:is([data-col="project"],[data-col="assignee"],[data-col="due"]){grid-row:3;display:inline-flex}
.projectui-tickets-row{grid-template-areas:"key status" "title title" "project due"}
.projectui-tickets-td[data-col="project"]{grid-area:project}
.projectui-tickets-td[data-col="due"]{grid-area:due;justify-content:flex-end}
.projectui-tickets-td:is([data-col="assignee"],[data-col="priority"],[data-col="updated"]){display:none}
.projectui-tickets-title-text{white-space:normal}
.projectui-tickets-count{inline-size:100%}
.project-page-tab{flex:1 1 0;justify-content:center}
.projectui-taskpage-status{align-items:flex-start}
.projectui-share[open]>summary::before,.projectui-tickets-filter[open]>summary::before{content:"";position:fixed;inset:0;z-index:-1;background:color-mix(in srgb,#000 38%,transparent)}
}
`
