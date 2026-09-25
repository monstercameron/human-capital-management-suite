package projectui

// projectUIInteractionStyles covers the Jira-style interactions: collapsible
// swimlanes, drag-and-drop states, the card move menu, the ticket modal and
// the task page. Like projectUIStyles it uses only the shell's theme-aware
// variables and logical properties.
const projectUIInteractionStyles = `
.projectui-lanes-frame{display:grid;gap:.5rem;min-inline-size:0}
.projectui-lane-tools{display:flex;flex-wrap:wrap;justify-content:flex-end;gap:.375rem}
.projectui-lane-tool{display:inline-flex;align-items:center;gap:.4rem;min-block-size:2rem;min-height:2rem;block-size:auto;padding-inline:.625rem;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--muted);font-size:.8125rem;font-weight:600;cursor:pointer}
.projectui-lane-tool:hover{color:var(--ink);background:var(--hcm-hover-surface)}
.projectui-lane-tool-icon{position:relative;inline-size:.75rem;block-size:.75rem}
.projectui-lane-tool-icon::before{content:"";position:absolute;inset:0;margin:auto;inline-size:.4rem;block-size:.4rem;border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor}
.projectui-lane-tool-icon[data-dir="collapse"]::before{transform:translateY(.1rem) rotate(-135deg)}
.projectui-lane-tool-icon[data-dir="expand"]::before{transform:translateY(-.1rem) rotate(45deg)}
.projectui-lane-head{padding:.625rem 0 .375rem}
.projectui-lane-head::before{content:none}
.projectui-lane-toggle{display:inline-flex;align-items:center;gap:.5rem;min-block-size:2rem;min-height:2rem;block-size:auto;margin:0;padding:.125rem .5rem .125rem .25rem;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;font-size:.8125rem;font-weight:650;cursor:pointer}
.projectui-lane-toggle:hover{background:var(--hcm-hover-surface)}
.projectui-lane-toggle:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}
.projectui-lane-chevron{inline-size:.45rem;block-size:.45rem;margin-inline:.3rem .15rem;border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor;transform:rotate(45deg) translate(-1px,-1px);transition:transform var(--hcm-motion-fast) var(--hcm-motion-easing)}
.projectui-lane[data-collapsed="true"] .projectui-lane-chevron{transform:rotate(-45deg)}
[dir="rtl"] .projectui-lane[data-collapsed="true"] .projectui-lane-chevron{transform:rotate(135deg)}
.projectui-cell-tally{display:none}
.projectui-lane[data-collapsed="true"] .projectui-cell{flex-direction:row;align-items:center;min-block-size:0;padding:.3125rem .625rem}
.projectui-lane[data-collapsed="true"] .projectui-cell>:not(.projectui-cell-tally){display:none}
.projectui-lane[data-collapsed="true"] .projectui-cell-tally{display:inline-flex;align-items:center;gap:.35rem;color:var(--muted);font-size:.75rem;font-weight:650;font-variant-numeric:tabular-nums}
.projectui-lane[data-collapsed="true"] .projectui-cell-tally::before{content:"";inline-size:.4rem;block-size:.4rem;border-radius:50%;background:currentColor;opacity:.55}
.projectui-lane[data-collapsed="true"] .projectui-cell-tally[data-empty="true"]{opacity:.45}

.projectui-card[draggable="true"]{cursor:grab}
.projectui-card[draggable="true"]:active{cursor:grabbing}
.projectui-card.is-drag-source{opacity:.4}
.projectui-card.is-drag-source:hover{transform:none;box-shadow:none}
.projectui-columns.is-dragging [data-drop-status]{transition:background-color var(--hcm-motion-fast) var(--hcm-motion-easing),box-shadow var(--hcm-motion-fast) var(--hcm-motion-easing)}
.projectui-columns.is-dragging [data-drop-state="invalid"]{opacity:.5}
.projectui-column-cap{box-shadow:0 -.75rem 0 var(--canvas),0 8px 10px -8px color-mix(in srgb,var(--ink) 30%,transparent)}
.projectui-columns[data-lanes="true"]{padding-block-start:0}
.projectui-columns.is-dragging [data-drop-state="valid"]{box-shadow:inset 0 0 0 1.5px color-mix(in srgb,var(--accent) 35%,transparent);border-radius:var(--hcm-radius-surface)}
.projectui-columns.is-dragging .is-drop-target{background:color-mix(in srgb,var(--accent) 16%,var(--canvas));box-shadow:inset 0 0 0 2px var(--accent)}
.projectui-columns.is-dragging .is-drop-target::before{content:"";display:block;flex:none;order:-1;block-size:var(--pu-drop-h);border:2px dashed color-mix(in srgb,var(--accent) 70%,transparent);border-radius:calc(var(--hcm-radius-control) + 2px);background:color-mix(in srgb,var(--accent) 8%,var(--surface))}
.projectui-columns.is-dragging .is-drop-target .projectui-empty-column,.projectui-columns.is-dragging .is-drop-target .projectui-empty-cell{display:none}

.projectui-card-menu{position:absolute;inset-block-start:.375rem;inset-inline-end:.375rem;z-index:2}
.projectui-card-menu>summary{display:grid;place-items:center;inline-size:1.75rem;block-size:1.75rem;border-radius:var(--hcm-radius-control);color:var(--muted);list-style:none;cursor:pointer;opacity:0;transition:opacity var(--hcm-motion-fast) var(--hcm-motion-easing),background-color var(--hcm-motion-fast) var(--hcm-motion-easing)}
.projectui-card-menu>summary::-webkit-details-marker{display:none}
.projectui-card:hover .projectui-card-menu>summary,.projectui-card:focus-within .projectui-card-menu>summary,.projectui-card-menu[open]>summary{opacity:1}
.projectui-card-menu>summary:hover,.projectui-card-menu[open]>summary{background:var(--pu-subtle);color:var(--ink)}
.projectui-card-menu>summary:focus-visible{opacity:1;outline:2px solid var(--hcm-color-focus);outline-offset:1px}
.projectui-card-menu-dots,.projectui-card-menu-dots::before,.projectui-card-menu-dots::after{display:block;inline-size:3.5px;block-size:3.5px;border-radius:50%;background:currentColor}
.projectui-card-menu-dots{position:relative}
.projectui-card-menu-dots::before,.projectui-card-menu-dots::after{content:"";position:absolute;inset-block-start:0}
.projectui-card-menu-dots::before{inset-inline-start:-6px}
.projectui-card-menu-dots::after{inset-inline-start:6px}
.projectui-card-menu>.projectui-card-controls{position:absolute;inset-block-start:calc(100% + .25rem);inset-inline-end:0;display:grid;gap:.375rem;min-inline-size:11rem;padding:.5rem;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-surface);background:var(--surface);box-shadow:var(--hcm-shadow-raised)}
.projectui-card-menu .projectui-control select:not(#projectui-none){inline-size:100%}
.projectui-board .projectui-card-main{padding-inline-end:1.5rem}
@media (pointer:coarse){.projectui-card-menu>summary{opacity:1;inline-size:2.25rem;block-size:2.25rem}}

.project-task-dialog{inline-size:min(56rem,calc(100vw - 2rem));max-inline-size:none;max-block-size:min(88dvh,52rem);margin:auto;padding:0;border:1px solid var(--pu-line,var(--line));border-radius:calc(var(--hcm-radius-surface) + 4px);background:var(--surface);color:var(--ink);box-shadow:0 24px 64px -12px rgba(0,0,0,.35);overflow:hidden}
.project-task-dialog[open]{display:flex;flex-direction:column;animation:projectui-dialog-in var(--hcm-motion-normal) var(--hcm-motion-easing)}
.project-task-dialog::backdrop{background:color-mix(in srgb,#0b1118 48%,transparent)}
@keyframes projectui-dialog-in{from{opacity:0;transform:translateY(8px) scale(.99)}to{opacity:1;transform:none}}
.projectui-modal{display:flex;flex-direction:column;min-block-size:0;max-block-size:inherit}
.projectui-modal-head{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem;padding:1.125rem 1.25rem .875rem;border-block-end:1px solid var(--pu-line,var(--line))}
.projectui-modal-heading{display:grid;gap:.5rem;min-inline-size:0}
.projectui-modal-kicker{display:flex;flex-wrap:wrap;align-items:center;gap:.625rem}
.projectui-task-key{color:var(--muted);font-size:.8125rem;font-weight:650;letter-spacing:.01em;font-variant-numeric:tabular-nums}
.projectui-modal-title{margin:0;font-size:1.25rem;line-height:1.3;letter-spacing:-.01em;color:var(--ink);max-inline-size:none}
.projectui-modal-close{display:grid;place-items:center;flex:none;inline-size:2.25rem;block-size:2.25rem;min-height:2.25rem;padding:0;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--muted);cursor:pointer}
.projectui-modal-close:hover{background:var(--hcm-hover-surface);color:var(--ink)}
.projectui-modal-close-icon{position:relative;inline-size:.875rem;block-size:.875rem}
.projectui-modal-close-icon::before,.projectui-modal-close-icon::after{content:"";position:absolute;inset:0;margin:auto;inline-size:100%;block-size:1.5px;background:currentColor;transform:rotate(45deg)}
.projectui-modal-close-icon::after{transform:rotate(-45deg)}
.projectui-modal-body{display:grid;grid-template-columns:minmax(0,1fr) minmax(17rem,21rem);gap:0;min-block-size:0;overflow:auto}
.projectui-modal-main{display:grid;gap:1.25rem;align-content:start;padding:1.125rem 1.25rem 1.25rem;min-inline-size:0}
.projectui-modal-side{display:grid;gap:.625rem;align-content:start;padding:1.125rem 1.25rem;border-inline-start:1px solid var(--pu-line,var(--line));background:var(--pu-subtle,var(--surface))}
.projectui-modal-section{display:grid;gap:.5rem}
.projectui-modal-section h3,.projectui-side-title{margin:0;color:var(--muted);font-size:.8125rem;font-weight:650;max-inline-size:none}
.projectui-modal-description{margin:0;font-size:.9375rem;line-height:1.55;white-space:pre-wrap;max-inline-size:65ch}
.projectui-muted{color:var(--muted)}
.projectui-section-head{display:flex;align-items:baseline;justify-content:space-between;gap:.75rem}
.projectui-inline-link{color:var(--accent);font-size:.8125rem;font-weight:600;text-decoration:none}
.projectui-inline-link:hover{text-decoration:underline}
.projectui-modal-foot{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.75rem;padding:.875rem 1.25rem;border-block-start:1px solid var(--pu-line,var(--line))}
.projectui-touch-hint{margin:0;color:var(--muted);font-size:.8125rem;max-inline-size:none}
.projectui-touch-hint:empty{display:none}
@media (pointer:fine){.projectui-touch-hint{visibility:hidden}}
.projectui-modal-actions{display:flex;gap:.5rem;margin-inline-start:auto}
.projectui-button{display:inline-flex;align-items:center;justify-content:center;gap:.4rem;min-block-size:2.25rem;min-height:2.25rem;block-size:auto;padding-inline:.875rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;font-weight:600;text-decoration:none;cursor:pointer;white-space:nowrap}
.projectui-button:hover{background:var(--hcm-hover-surface)}
.projectui-button-primary{border-color:transparent;background:var(--accent);color:var(--on-brand)}
.projectui-button-primary:hover{background:var(--accent-hover)}
.projectui-arrow{inline-size:.4rem;block-size:.4rem;border-right:1.5px solid currentColor;border-top:1.5px solid currentColor;transform:rotate(45deg)}
[dir="rtl"] .projectui-arrow{transform:rotate(-135deg)}
.projectui-link-button{min-height:auto;block-size:auto;padding:0;border:0;background:none;color:var(--muted);font:inherit;font-size:.8125rem;font-weight:600;cursor:pointer}
.projectui-link-button:hover{color:var(--ink);text-decoration:underline}

.projectui-status-pill{display:inline-flex;align-items:center;gap:.375rem;min-block-size:1.5rem;padding:.125rem .5rem .125rem .4375rem;border-radius:999px;background:var(--pu-subtle,var(--surface));color:var(--ink);font-size:.8125rem;font-weight:600;white-space:nowrap}
.projectui-status-pill[data-tone="active"]{background:var(--hcm-color-info-surface)}
.projectui-status-pill[data-tone="done"]{background:var(--hcm-color-success-surface)}
.projectui-status-pill[data-tone="blocked"]{background:var(--hcm-color-danger-surface)}
.projectui-fields{display:grid;gap:.125rem}
.projectui-field{display:grid;grid-template-columns:6.5rem minmax(0,1fr);align-items:center;gap:.25rem .75rem;min-block-size:2.5rem;padding-block:.25rem}
.projectui-field-label{color:var(--muted);font-size:.8125rem;font-weight:600;max-inline-size:none}
.projectui-field-value{display:grid;justify-items:start;gap:.125rem;min-inline-size:0}
.projectui-field-value>.projectui-select-wrap,.projectui-field-value>select,.projectui-field-value>input{justify-self:stretch}
.projectui-modal-side .projectui-field{grid-template-columns:5.25rem minmax(0,1fr)}
.projectui-fields .projectui-field-select:not(#projectui-none){text-overflow:ellipsis}
.projectui-field-text{font-size:.875rem;font-weight:550}
.projectui-select-wrap{position:relative;display:flex;align-items:center;min-inline-size:0}
.projectui-select-wrap>:first-child:not(select){position:absolute;inset-inline-start:.5rem;pointer-events:none;z-index:1}
.projectui-select-wrap .projectui-avatar{inline-size:1.375rem;block-size:1.375rem;font-size:.625rem}
.projectui-fields .projectui-field-select:not(#projectui-none),.projectui-fields .projectui-field-date:not(#projectui-none){inline-size:100%;block-size:2.25rem;min-block-size:2.25rem;min-height:2.25rem;padding-block:0;padding-inline:.625rem 1.75rem;border:1px solid transparent;border-radius:var(--hcm-radius-control);background-color:transparent;color:var(--ink);font-size:.875rem;font-weight:550;box-shadow:none;cursor:pointer}
.projectui-fields .projectui-select-wrap .projectui-field-select:not(#projectui-none){padding-inline-start:2.125rem}
.projectui-fields .projectui-field-date:not(#projectui-none){padding-inline-end:.5rem}
.projectui-fields .projectui-field-select:not(#projectui-none):hover,.projectui-fields .projectui-field-date:not(#projectui-none):hover{border-color:var(--control-border);background-color:var(--surface)}
.projectui-fields .projectui-field-select:not(#projectui-none):focus-visible,.projectui-fields .projectui-field-date:not(#projectui-none):focus-visible{border-color:var(--accent);background-color:var(--surface);outline:2px solid var(--hcm-color-focus);outline-offset:1px;box-shadow:none}
.projectui-avatar-empty{border:1.5px dashed var(--control-border);background:transparent;box-shadow:none}
.projectui-field-status{min-block-size:0;font-size:.75rem;font-weight:600;color:var(--muted)}
.projectui-field-status:empty{display:none}
.projectui-field-status[data-state="saving"]{color:var(--hcm-color-info)}
.projectui-field-status[data-state="saved"]{color:var(--hcm-color-success)}
.projectui-field-status[data-state="error"]{color:var(--hcm-color-danger)}
.projectui-field-status[data-state="saving"]::before{content:"";display:inline-block;inline-size:.4rem;block-size:.4rem;margin-inline-end:.3rem;border-radius:50%;background:currentColor;animation:projectui-pulse 1s ease-in-out infinite alternate}
.projectui-field[data-state="error"] :is(select,input){border-color:var(--hcm-color-danger)!important}
.projectui-person{display:inline-flex;align-items:center;gap:.5rem;font-size:.875rem;font-weight:550}
.projectui-avatar{position:relative;overflow:hidden}
.projectui-avatar-photo{position:absolute;inset:0;inline-size:100%;block-size:100%;object-fit:cover;border-radius:50%}

.project-page-task{min-inline-size:0}
.projectui-taskpage{display:grid;gap:1rem;min-inline-size:0;color:var(--ink)}
.projectui-breadcrumb ol{display:flex;flex-wrap:wrap;align-items:center;gap:.375rem;margin:0;padding:0;list-style:none;font-size:.8125rem;color:var(--muted)}
.projectui-breadcrumb li{display:inline-flex;align-items:center;gap:.375rem;max-inline-size:none}
.projectui-breadcrumb li+li::before{content:"";inline-size:.35rem;block-size:.35rem;border-right:1.5px solid currentColor;border-top:1.5px solid currentColor;transform:rotate(45deg);opacity:.6;margin-inline-end:.2rem}
[dir="rtl"] .projectui-breadcrumb li+li::before{transform:rotate(-135deg)}
.projectui-breadcrumb a{color:var(--muted);font-weight:600;text-decoration:none}
.projectui-breadcrumb a:hover{color:var(--accent);text-decoration:underline}
.projectui-breadcrumb [aria-current]{color:var(--ink);font-weight:650}
.projectui-taskpage-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(17rem,21rem);gap:1.5rem 2rem;align-items:start}
.projectui-taskpage-main{display:grid;gap:1.75rem;min-inline-size:0}
.projectui-taskpage-side{display:grid;gap:1rem;position:sticky;inset-block-start:1rem}
.projectui-side-card{display:grid;gap:.5rem;padding:1rem 1rem .75rem;border:1px solid var(--pu-line,var(--line));border-radius:var(--hcm-radius-surface);background:var(--surface);box-shadow:var(--hcm-shadow-resting)}
.projectui-side-card-head{display:flex;align-items:center;justify-content:space-between;gap:.5rem}
.projectui-inline-editor{display:grid;gap:.5rem}
.projectui-editor-actions{display:none;align-items:center;gap:.5rem}
.projectui-inline-editor:focus-within .projectui-editor-actions,.projectui-inline-editor:has(.projectui-field-status:not(:empty)) .projectui-editor-actions{display:flex}
.projectui-inline-editor:not(:focus-within) .projectui-editor-actions>button{display:none}
.projectui-title-input:not(#projectui-none){field-sizing:content;resize:none;overflow:hidden;font-family:inherit;inline-size:100%;block-size:auto;min-block-size:0;min-height:0;margin:0;padding:.25rem .5rem;margin-inline-start:-.5rem;border:1px solid transparent;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font-size:clamp(1.375rem,1.1rem + .9vw,1.75rem);font-weight:700;line-height:1.25;letter-spacing:-.015em;box-shadow:none}
.projectui-title-input:not(#projectui-none):hover{background:var(--hcm-hover-surface)}
.projectui-title-input:not(#projectui-none):focus-visible,.projectui-title-input:not(#projectui-none):focus{border-color:var(--accent);background:var(--surface);outline:2px solid var(--hcm-color-focus);outline-offset:1px;box-shadow:none}
.projectui-page-section{display:grid;gap:.75rem;min-inline-size:0}
.projectui-page-section>h2,.projectui-section-head>h2{margin:0;font-size:1rem;font-weight:650;color:var(--ink);max-inline-size:none}
.projectui-description-input:not(#projectui-none){inline-size:100%;min-block-size:6rem;min-height:6rem;padding:.625rem .75rem;border:1px solid transparent;border-radius:var(--hcm-radius-control);background:var(--pu-subtle,var(--surface));color:var(--ink);font:inherit;font-size:.9375rem;line-height:1.55;resize:vertical;box-shadow:none}
.projectui-description-input:not(#projectui-none):hover{border-color:var(--pu-line,var(--line))}
.projectui-description-input:not(#projectui-none):focus{border-color:var(--accent);background:var(--surface);outline:2px solid var(--hcm-color-focus);outline-offset:1px}
.projectui-description-text{margin:0;font-size:.9375rem;line-height:1.55;white-space:pre-wrap}
.projectui-comment-composer{display:grid;gap:.5rem}
.projectui-comment-composer textarea:not(#projectui-none){inline-size:100%;min-block-size:4.5rem;min-height:4.5rem;padding:.625rem .75rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.9375rem;line-height:1.5;resize:vertical}
.projectui-comment-composer .projectui-editor-actions{display:flex}
.projectui-comment-composer:not(:focus-within) .projectui-editor-actions>button{display:inline-flex}
.projectui-comment-thread{gap:.875rem}
.projectui-comment-tools{display:flex;flex-wrap:wrap;align-items:flex-start;gap:.25rem .875rem;margin-block-start:.125rem}
.projectui-comment-edit>summary{color:var(--muted);font-size:.8125rem;font-weight:600;list-style:none;cursor:pointer}
.projectui-comment-edit>summary::-webkit-details-marker{display:none}
.projectui-comment-edit>summary:hover{color:var(--accent);text-decoration:underline}
.projectui-comment-edit[open]{flex:1 1 100%}
.projectui-comment-edit[open]>summary{margin-block-end:.375rem}
.projectui-comment-edit-form{display:grid;gap:.5rem}
.projectui-comment-edit-form textarea:not(#projectui-none){inline-size:100%;min-block-size:4rem;min-height:4rem;padding:.5rem .625rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem}
.projectui-comment-edit-form .projectui-editor-actions{display:flex}
.projectui-comment-edit-form:not(:focus-within) .projectui-editor-actions>button{display:inline-flex}
.projectui-comment-delete{display:inline}
.projectui-comment-edited{color:var(--muted);font-size:.75rem}
.projectui-empty-comments{display:grid;justify-items:center;gap:.5rem;padding:1.5rem 1rem;border:1.5px dashed var(--pu-line,var(--line));border-radius:var(--hcm-radius-surface);color:var(--muted);text-align:center}
.projectui-empty-comments p{margin:0;font-size:.875rem;max-inline-size:none}
.projectui-empty-comments-icon{inline-size:1.5rem;block-size:1.125rem;border:1.5px solid currentColor;border-radius:.4rem .4rem .4rem 0;opacity:.6}
.projectui-empty-note{margin:0;font-size:.875rem}
.projectui-taskpage .projectui-activity-item span{font-weight:550}

@media (max-width:60rem){.projectui-taskpage-grid{grid-template-columns:minmax(0,1fr)}.projectui-taskpage-main{display:contents}.projectui-taskpage-grid>.projectui-taskpage-side{position:static}.projectui-title-editor,.projectui-page-title{order:1}.projectui-description-section{order:2}.projectui-taskpage-side{order:3}.projectui-taskpage-main>.projectui-page-section:not(.projectui-description-section){order:4}}
@media (max-width:40rem){
.project-task-dialog{inline-size:100vw;max-inline-size:100vw;max-block-size:92dvh;margin:auto 0 0;border-end-start-radius:0;border-end-end-radius:0;border-inline:0;border-block-end:0}
.project-task-dialog[open]{animation-name:projectui-sheet-in}
.projectui-modal-body{grid-template-columns:minmax(0,1fr)}
.projectui-modal-side{order:-1;border-inline-start:0;border-block-end:1px solid var(--pu-line,var(--line))}
.projectui-modal-head,.projectui-modal-main,.projectui-modal-side,.projectui-modal-foot{padding-inline:1rem}
.projectui-modal-foot{padding-block-end:max(.875rem,env(safe-area-inset-bottom))}
.projectui-modal-actions{inline-size:100%}
.projectui-modal-actions .projectui-button{flex:1}
.projectui-field{grid-template-columns:5.5rem minmax(0,1fr)}
}
@keyframes projectui-sheet-in{from{transform:translateY(24px);opacity:.5}to{transform:none;opacity:1}}
@media (prefers-reduced-motion:reduce){.project-task-dialog[open]{animation:none}.projectui-lane-chevron{transition:none}}
`
