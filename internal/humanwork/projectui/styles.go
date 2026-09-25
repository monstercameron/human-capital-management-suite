package projectui

// Styles returns the responsive stylesheet required by these component class
// names. Integrations may append it to their existing stylesheet bundle.
//
// Surfaces and text use the shell's theme-aware variables (--surface,
// --canvas, --ink, --muted, --accent), which the product redefines for dark
// mode; status hues use the --hcm-color-* status tokens, which it also
// redefines. The only literal hues are the review violet (with a dark
// variant) and the avatar hue buckets, which are mixed with --surface and
// --ink so they follow the theme. Logical properties keep every rule
// direction-agnostic for RTL locales.
func Styles() string {
	return projectUIStyles + projectUIInteractionStyles + projectUIRefinementStyles + projectUIBoardStyles + projectUIShareStyles + projectUIScrollStyles
}

const projectUIStyles = `
.projectui-board,.projectui-list,.projectui-detail,.project-task-dialog,.projectui-taskpage{--pu-bleed:0px;--pu-drop-h:4.5rem;--pu-col-w:17rem;--pu-col-max:22rem;--pu-board-offset:15.5rem;--pu-violet:#6941c6;--pu-line:color-mix(in srgb,var(--ink) 11%,var(--surface));--pu-col-bg:color-mix(in srgb,var(--ink) 4.5%,var(--canvas));--pu-subtle:color-mix(in srgb,var(--ink) 5%,var(--surface));--pu-lift:0 1px 2px color-mix(in srgb,var(--ink) 8%,transparent),0 8px 20px -10px color-mix(in srgb,var(--ink) 32%,transparent)}
:root[data-hcm-color-mode="dark"] :is(.projectui-board,.projectui-list,.projectui-detail,.project-task-dialog,.projectui-taskpage){--pu-violet:#b69cf5}
@media (prefers-color-scheme:dark){:root:not([data-hcm-color-mode="light"]):not([data-hcm-color-mode="dark"]) :is(.projectui-board,.projectui-list,.projectui-detail,.project-task-dialog,.projectui-taskpage){--pu-violet:#b69cf5}}
.projectui-sr{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
.projectui-board,.projectui-list{display:grid;gap:var(--hcm-space-2);min-inline-size:0;color:var(--ink)}
.projectui-header{display:flex;flex-wrap:wrap;align-items:flex-end;justify-content:space-between;gap:var(--hcm-space-2) var(--hcm-space-3);min-inline-size:0}
.projectui-heading{flex:1 1 20rem;min-inline-size:0;display:grid;gap:.375rem}
.projectui-heading h1{margin:0;color:var(--ink);font-size:clamp(1.5rem,1.15rem + 1.1vw,2rem);line-height:1.15;letter-spacing:-.02em;max-inline-size:none}
.projectui-heading .projectui-description{margin:0;color:var(--muted);font-size:.9375rem;line-height:1.5;max-inline-size:68ch}
.projectui-actions{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem;min-inline-size:0}

.projectui-status-glyph{--tone:var(--muted);position:relative;display:inline-block;flex:none;inline-size:.875rem;block-size:.875rem;box-sizing:border-box;border:1.5px solid var(--tone);border-radius:50%}
.projectui-status-glyph[data-tone="planned"]{border-style:dashed}
.projectui-status-glyph[data-tone="active"]{--tone:var(--hcm-color-info);padding:1.5px;background:conic-gradient(var(--tone) 0 50%,transparent 0) content-box}
.projectui-status-glyph[data-tone="review"]{--tone:var(--pu-violet);padding:1.5px;background:conic-gradient(var(--tone) 0 75%,transparent 0) content-box}
.projectui-status-glyph:is([data-tone="done"],[data-tone="blocked"],[data-tone="cancelled"]){background:var(--tone)}
.projectui-status-glyph[data-tone="done"]{--tone:var(--hcm-color-success)}
.projectui-status-glyph[data-tone="blocked"]{--tone:var(--hcm-color-danger)}
.projectui-status-glyph[data-tone="done"]::after{content:"";position:absolute;inset-inline-start:.21rem;inset-block-start:.08rem;inline-size:.19rem;block-size:.38rem;border:solid var(--surface);border-width:0 1.5px 1.5px 0;transform:rotate(45deg)}
.projectui-status-glyph:is([data-tone="blocked"],[data-tone="cancelled"])::after{content:"";position:absolute;inset:0;margin:auto;inline-size:.4rem;block-size:1.5px;background:var(--surface)}

.projectui-columns{display:flex;align-items:flex-start;gap:.75rem;min-inline-size:0;overflow-x:auto;overscroll-behavior-inline:contain;scroll-snap-type:x proximity;scroll-padding-inline:var(--pu-bleed);margin-inline:calc(var(--pu-bleed) * -1);padding-inline:var(--pu-bleed);padding-block:.125rem .75rem;scrollbar-width:thin;scrollbar-color:var(--hcm-scrollbar-thumb) transparent}
@property --pu-fade-start{syntax:"<length>";inherits:false;initial-value:0px}
@property --pu-fade-end{syntax:"<length>";inherits:false;initial-value:0px}
@supports (animation-timeline:scroll()){.projectui-columns{-webkit-mask-image:linear-gradient(to right,transparent 0,#000 var(--pu-fade-start),#000 calc(100% - var(--pu-fade-end)),transparent 100%);mask-image:linear-gradient(to right,transparent 0,#000 var(--pu-fade-start),#000 calc(100% - var(--pu-fade-end)),transparent 100%);animation:projectui-edge-fade linear both;animation-timeline:scroll(self inline)}[dir="rtl"] .projectui-columns{-webkit-mask-image:linear-gradient(to left,transparent 0,#000 var(--pu-fade-start),#000 calc(100% - var(--pu-fade-end)),transparent 100%);mask-image:linear-gradient(to left,transparent 0,#000 var(--pu-fade-start),#000 calc(100% - var(--pu-fade-end)),transparent 100%)}}
@keyframes projectui-edge-fade{0%{--pu-fade-start:0px;--pu-fade-end:3rem}8%{--pu-fade-start:3rem;--pu-fade-end:3rem}92%{--pu-fade-start:3rem;--pu-fade-end:3rem}100%{--pu-fade-start:3rem;--pu-fade-end:0px}}
.projectui-columns:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:-2px;border-radius:var(--hcm-radius-surface)}
.projectui-column{flex:1 0 var(--pu-col-w);max-inline-size:var(--pu-col-max);min-inline-size:0;display:flex;flex-direction:column;max-block-size:calc(100dvh - var(--pu-board-offset));background:var(--pu-col-bg);border-radius:var(--hcm-radius-surface);scroll-snap-align:start}
.projectui-column-head{display:flex;align-items:center;gap:.5rem;padding:.75rem .875rem .5rem;min-inline-size:0}
.projectui-column-title{display:flex;align-items:center;gap:.5rem;min-inline-size:0;margin:0;font-size:.875rem;font-weight:650;line-height:1.3;letter-spacing:0;max-inline-size:none}
.projectui-column-label{min-inline-size:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.projectui-count{flex:none;min-inline-size:1.375rem;padding-inline:.4rem;border-radius:999px;background:color-mix(in srgb,var(--ink) 8%,transparent);color:var(--muted);font-size:.75rem;font-weight:650;line-height:1.375rem;text-align:center;font-variant-numeric:tabular-nums}
.projectui-column-body{display:flex;flex-direction:column;gap:.5rem;min-block-size:0;overflow-y:auto;padding:.25rem .5rem .5rem;scrollbar-width:thin;scrollbar-color:var(--hcm-scrollbar-thumb) transparent}
.projectui-empty{margin:0;color:var(--muted);font-size:.875rem}
.projectui-empty-column{display:grid;justify-items:center;gap:.5rem;padding:1.5rem .75rem;border:1.5px dashed color-mix(in srgb,var(--ink) 16%,transparent);border-radius:calc(var(--hcm-radius-control) + 2px);text-align:center}
.projectui-empty-column p{margin:0;font-size:.8125rem;max-inline-size:none}

.projectui-card{position:relative;display:block;margin:0;padding:.75rem .8125rem .6875rem;background:var(--surface);border:1px solid var(--pu-line);border-radius:calc(var(--hcm-radius-control) + 2px);box-shadow:var(--hcm-shadow-resting);overflow-wrap:anywhere;transition:border-color var(--hcm-motion-fast) var(--hcm-motion-easing),box-shadow var(--hcm-motion-normal) var(--hcm-motion-easing),transform var(--hcm-motion-normal) var(--hcm-motion-easing)}
.projectui-board .projectui-card:hover{border-color:color-mix(in srgb,var(--ink) 24%,var(--surface));box-shadow:var(--pu-lift);transform:translateY(-1px)}
.projectui-card[data-selected="true"]{border-color:var(--accent);box-shadow:0 0 0 1px var(--accent),var(--hcm-shadow-resting)}
.projectui-list .projectui-card[data-selected="true"]{box-shadow:inset 3px 0 0 var(--accent);background:var(--hcm-hover-surface)}
[dir="rtl"] .projectui-list .projectui-card[data-selected="true"]{box-shadow:inset -3px 0 0 var(--accent)}
.projectui-card-content{display:grid;gap:.5rem;min-inline-size:0}
.projectui-card-main{display:grid;gap:.25rem;min-inline-size:0}
.projectui-card-title{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden;margin:0;color:var(--ink);font-size:.9375rem;font-weight:600;line-height:1.35;letter-spacing:-.005em;text-decoration:none;max-inline-size:none}
a.projectui-card-title::after{content:"";position:absolute;inset:0;border-radius:inherit}
.projectui-board .projectui-card:is([data-tone="done"],[data-tone="cancelled"]){background:color-mix(in srgb,var(--surface) 70%,var(--canvas));box-shadow:none}
.projectui-board .projectui-card:is([data-tone="done"],[data-tone="cancelled"]) .projectui-card-title{color:var(--muted);font-weight:500}
a.projectui-card-title:hover{text-decoration:underline;text-decoration-thickness:1px;text-underline-offset:.15em}
/* The title link stretches over the card; its focus ring is drawn on that overlay so it outlines the whole card. */
a.projectui-card-title:focus-visible{outline:2px solid transparent}
a.projectui-card-title:focus-visible::after{outline:2px solid var(--hcm-color-focus);outline-offset:2px}
.projectui-card-summary{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden;margin:0;color:var(--muted);font-size:.8125rem;line-height:1.45;max-inline-size:none}
.projectui-card-meta-row{display:flex;flex-wrap:wrap;align-items:center;gap:.375rem .5rem;min-inline-size:0;font-size:.75rem;color:var(--muted)}
.projectui-chip{display:inline-flex;align-items:center;gap:.3rem;min-block-size:1.375rem;padding-inline:.4375rem;border-radius:var(--hcm-radius-control);background:var(--pu-subtle);color:var(--muted);font-size:.75rem;font-weight:550;line-height:1.2;white-space:nowrap}
.projectui-type{border:1px solid var(--pu-line);background:transparent}
.projectui-due-icon{position:relative;inline-size:.6875rem;block-size:.6875rem;border:1.5px solid currentColor;border-radius:50%;box-sizing:border-box;flex:none}
.projectui-due-icon::after{content:"";position:absolute;inset-inline-start:calc(50% - .75px);inset-block-start:1px;inline-size:1.5px;block-size:.19rem;background:currentColor;box-shadow:.12rem .16rem 0 -.02rem currentColor}
.projectui-due[data-tone="overdue"]{background:var(--hcm-color-danger-surface);color:var(--hcm-color-danger)}
.projectui-due[data-tone="today"]{background:var(--hcm-color-warning-surface);color:var(--hcm-color-warning)}
.projectui-due[data-tone="soon"]{color:var(--hcm-color-warning)}
.projectui-priority{display:inline-flex;align-items:center;gap:.3rem;min-block-size:1.375rem;color:var(--muted)}
.projectui-priority-bars{display:inline-flex;align-items:flex-end;gap:1.5px;block-size:.75rem}
.projectui-priority-bars>span{inline-size:3px;border-radius:1px;background:color-mix(in srgb,currentColor 24%,transparent)}
.projectui-priority-bars>span:nth-child(1){block-size:40%}
.projectui-priority-bars>span:nth-child(2){block-size:70%}
.projectui-priority-bars>span:nth-child(3){block-size:100%}
.projectui-priority:is([data-level="low"],[data-level="normal"],[data-level="high"],[data-level="urgent"]) .projectui-priority-bars>span:nth-child(1),.projectui-priority:is([data-level="normal"],[data-level="high"],[data-level="urgent"]) .projectui-priority-bars>span:nth-child(2),.projectui-priority:is([data-level="high"],[data-level="urgent"]) .projectui-priority-bars>span:nth-child(3){background:currentColor}
.projectui-priority[data-level="high"]{color:var(--hcm-color-warning)}
.projectui-priority[data-level="urgent"]{color:var(--hcm-color-danger);font-weight:650}
.projectui-board .projectui-priority:not([data-level="urgent"],[data-level="unknown"]) .projectui-priority-text{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
.projectui-assignee{display:inline-flex;align-items:center;gap:.375rem;min-inline-size:0;margin-inline-start:auto;color:var(--ink)}
.projectui-board .projectui-assignee-name{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
.projectui-avatar{--pu-h:210;display:inline-grid;place-items:center;flex:none;inline-size:1.625rem;block-size:1.625rem;border-radius:50%;background:color-mix(in srgb,hsl(var(--pu-h) 62% 52%) 20%,var(--surface));box-shadow:inset 0 0 0 1px color-mix(in srgb,hsl(var(--pu-h) 55% 45%) 35%,transparent);color:color-mix(in srgb,hsl(var(--pu-h) 65% 42%) 42%,var(--ink));font-size:.75rem;font-weight:700;letter-spacing:.01em;line-height:1}
.projectui-avatar[data-hue="0"]{--pu-h:4}.projectui-avatar[data-hue="1"]{--pu-h:24}.projectui-avatar[data-hue="2"]{--pu-h:42}.projectui-avatar[data-hue="3"]{--pu-h:88}.projectui-avatar[data-hue="4"]{--pu-h:140}.projectui-avatar[data-hue="5"]{--pu-h:168}.projectui-avatar[data-hue="6"]{--pu-h:190}.projectui-avatar[data-hue="7"]{--pu-h:208}.projectui-avatar[data-hue="8"]{--pu-h:228}.projectui-avatar[data-hue="9"]{--pu-h:258}.projectui-avatar[data-hue="10"]{--pu-h:286}.projectui-avatar[data-hue="11"]{--pu-h:326}

/* :not(#projectui-none) lifts these selects above the shell control rule :is(input:not(...)x5,select), whose specificity is (0,5,1). */
.projectui-card-controls{position:relative;z-index:1;display:flex;flex-wrap:wrap;align-items:center;gap:.375rem;min-inline-size:0}
.projectui-control{position:relative;display:inline-flex;align-items:center;min-inline-size:0;max-inline-size:100%}
.projectui-control>.projectui-status-glyph,.projectui-lane-glyph{position:absolute;inset-inline-start:.5625rem;pointer-events:none}
.projectui-lane-glyph{inline-size:.625rem;block-size:.5rem;border-block:1.5px solid var(--muted);box-sizing:border-box}
.projectui-card-controls .projectui-control select:not(#projectui-none){appearance:none;-webkit-appearance:none;block-size:1.75rem;min-block-size:1.75rem;min-height:1.75rem;max-inline-size:100%;margin:0;padding-block:0;padding-inline:1.625rem 1.5rem;border:1px solid transparent;border-radius:999px;background-color:var(--pu-subtle);background-image:linear-gradient(45deg,transparent 50%,currentColor 50%),linear-gradient(135deg,currentColor 50%,transparent 50%);background-position:calc(100% - .75rem) 52%,calc(100% - .5rem) 52%;background-size:.25rem .25rem,.25rem .25rem;background-repeat:no-repeat;color:var(--ink);font-size:.8125rem;font-weight:550;line-height:normal;text-overflow:ellipsis;cursor:pointer;box-shadow:none;transition:border-color var(--hcm-motion-fast) var(--hcm-motion-easing),background-color var(--hcm-motion-fast) var(--hcm-motion-easing)}
[dir="rtl"] .projectui-card-controls .projectui-control select:not(#projectui-none){background-position:.5rem 52%,.75rem 52%}
.projectui-card-controls .projectui-control select:not(#projectui-none):hover{border-color:var(--control-border);background-color:var(--surface)}
.projectui-card-controls .projectui-control select:not(#projectui-none):focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px;border-color:var(--control-border);box-shadow:none}
.projectui-card-controls .projectui-control select:not(#projectui-none):disabled{opacity:.6;cursor:progress}
.projectui-status-readonly{display:inline-flex;align-items:center;gap:.375rem;font-size:.8125rem;font-weight:550;color:var(--ink)}
.projectui-readonly-lane{font-size:.8125rem;color:var(--muted)}
@media (pointer:coarse){.projectui-card-controls .projectui-control select:not(#projectui-none){block-size:2.25rem;min-block-size:2.25rem;min-height:2.25rem;font-size:.875rem}}

.projectui-card[data-state="pending"]{overflow:hidden}
.projectui-card[data-state="pending"]::before{content:"";position:absolute;inset-inline:0;inset-block-start:0;block-size:2px;background:linear-gradient(90deg,transparent,var(--hcm-color-info),transparent);background-size:45% 100%;background-repeat:no-repeat;animation:projectui-pending 1.2s linear infinite}
.projectui-card[data-state="pending"] :is(.projectui-card-main,.projectui-card-meta-row){opacity:.62}
@keyframes projectui-pending{from{background-position:-50% 0}to{background-position:150% 0}}
.projectui-pending{display:flex;align-items:center;gap:.4rem;margin:0;color:var(--hcm-color-info);font-size:.75rem;font-weight:600}
.projectui-pending::before{content:"";inline-size:.4rem;block-size:.4rem;border-radius:50%;background:currentColor;animation:projectui-pulse 1s ease-in-out infinite alternate}
@keyframes projectui-pulse{from{opacity:.35}to{opacity:1}}
.projectui-card[data-state="conflict"]{border-color:color-mix(in srgb,var(--hcm-color-danger) 45%,var(--surface));box-shadow:inset 3px 0 0 var(--hcm-color-danger)}
[dir="rtl"] .projectui-card[data-state="conflict"]{box-shadow:inset -3px 0 0 var(--hcm-color-danger)}
.projectui-conflict{position:relative;z-index:1;margin:0;padding:.5rem .625rem;border-radius:var(--hcm-radius-control);background:var(--hcm-color-danger-surface);color:var(--hcm-color-danger);font-size:.8125rem;font-weight:550;line-height:1.4;max-inline-size:none}
.projectui-conflict:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}

.projectui-columns[data-lanes="true"]{display:grid;grid-template-columns:repeat(var(--pu-cols,3),minmax(var(--pu-col-w),var(--pu-col-max)));column-gap:.75rem;row-gap:0;align-items:stretch;max-block-size:calc(100dvh - var(--pu-board-offset));overflow:auto;padding-block-end:.75rem}
.projectui-columns[data-columns="1"]{--pu-cols:1}.projectui-columns[data-columns="2"]{--pu-cols:2}.projectui-columns[data-columns="3"]{--pu-cols:3}.projectui-columns[data-columns="4"]{--pu-cols:4}.projectui-columns[data-columns="5"]{--pu-cols:5}.projectui-columns[data-columns="6"]{--pu-cols:6}.projectui-columns[data-columns="7"]{--pu-cols:7}.projectui-columns[data-columns="8"]{--pu-cols:8}.projectui-columns[data-columns="9"]{--pu-cols:9}.projectui-columns[data-columns="10"]{--pu-cols:10}.projectui-columns[data-columns="11"]{--pu-cols:11}.projectui-columns[data-columns="12"]{--pu-cols:12}
.projectui-column-cap{grid-row:1;position:sticky;inset-block-start:0;z-index:2;max-inline-size:none;max-block-size:none;margin-block-end:.25rem;background:linear-gradient(var(--pu-col-bg),var(--pu-col-bg)),var(--canvas)}
.projectui-lane{grid-column:1/-1;display:grid;grid-template-columns:subgrid;row-gap:0;align-items:start;min-inline-size:0}
.projectui-lane+.projectui-lane{border-block-start:1px solid var(--pu-line);margin-block-start:.5rem}
.projectui-lane-head{grid-column:1/-1;position:sticky;inset-inline-start:0;z-index:1;display:flex;align-items:center;gap:.5rem;justify-self:start;margin:0;padding:.875rem .25rem .5rem;font-size:.8125rem;font-weight:650;line-height:1.3;max-inline-size:none;background:var(--canvas)}
.projectui-lane-head::before{content:"";inline-size:.25rem;block-size:1rem;border-radius:2px;background:color-mix(in srgb,var(--ink) 30%,transparent)}
.projectui-lane+.projectui-lane .projectui-lane-head{border-block-start:0}
.projectui-cell{display:flex;flex-direction:column;gap:.5rem;min-inline-size:0;padding:.5rem;background:var(--pu-col-bg);border-radius:var(--hcm-radius-surface)}
.projectui-empty-cell{min-block-size:2.5rem;border:1.5px dashed color-mix(in srgb,var(--ink) 12%,transparent);border-radius:calc(var(--hcm-radius-control) + 2px)}

.projectui-pagination{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem 1rem;margin:0;color:var(--muted);font-size:.8125rem}
.projectui-pagination:empty{display:none}
.projectui-page-link{display:inline-flex;align-items:center;min-block-size:2.25rem;padding-inline:.75rem;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-weight:600;text-decoration:none}
.projectui-more-in-lane{color:var(--muted)}

.projectui-list{--pu-list-cols:minmax(0,1fr) minmax(9rem,11rem) 6.5rem minmax(8rem,11rem) 6.5rem}
.projectui-list-frame{min-inline-size:0;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-surface);background:var(--surface);overflow:hidden;box-shadow:var(--hcm-shadow-resting)}
.projectui-list-head{display:grid;grid-template-columns:var(--pu-list-cols);gap:1rem;padding:.5625rem 1rem;border-block-end:1px solid var(--pu-line);background:var(--pu-subtle);color:var(--muted);font-size:.75rem;font-weight:650}
.projectui-list-items{display:grid;min-inline-size:0}
.projectui-list .projectui-card{padding:.625rem 1rem;border:0;border-block-end:1px solid var(--pu-line);border-radius:0;box-shadow:none;transition:background-color var(--hcm-motion-fast) var(--hcm-motion-easing)}
.projectui-list .projectui-card:last-child{border-block-end:0}
.projectui-list .projectui-card:hover{background:var(--hcm-hover-surface)}
.projectui-list a.projectui-card-title:focus-visible::after{outline-offset:-3px}
.projectui-list .projectui-card-content{grid-template-columns:var(--pu-list-cols);grid-template-areas:"main status prio who due";align-items:center;column-gap:1rem;row-gap:.375rem}
.projectui-list .projectui-card-main{grid-area:main}
.projectui-list .projectui-card-title{-webkit-line-clamp:1;font-size:.875rem}
.projectui-list .projectui-card-summary{-webkit-line-clamp:1;font-size:.75rem}
.projectui-list .projectui-card-meta-row{display:contents}
.projectui-list .projectui-priority{grid-area:prio}
.projectui-list .projectui-due{grid-area:due;justify-self:start}
.projectui-list .projectui-type{grid-area:main;justify-self:end;align-self:start}
.projectui-list .projectui-assignee{grid-area:who;margin:0;font-size:.8125rem;min-inline-size:0}
.projectui-list .projectui-assignee-name{min-inline-size:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.projectui-list .projectui-card-controls{grid-area:status}
.projectui-list :is(.projectui-pending,.projectui-conflict){grid-column:1/-1}
.projectui-list .projectui-card-main:has(+.projectui-card-meta-row .projectui-type){padding-inline-end:5.5rem}

.projectui-detail{display:grid;gap:1.25rem;min-inline-size:0;padding:1.25rem;border:1px solid var(--pu-line);border-radius:var(--hcm-radius-surface);background:var(--surface);box-shadow:var(--hcm-shadow-resting);color:var(--ink)}
.projectui-detail-head{display:grid;gap:.5rem}
.projectui-detail-title{margin:0;color:var(--ink);font-size:1.25rem;line-height:1.3;letter-spacing:-.01em;max-inline-size:none}
.projectui-detail-description{margin:0;color:var(--muted);font-size:.9375rem;line-height:1.55;max-inline-size:65ch}
.projectui-facts{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(7.25rem,100%),1fr));gap:.75rem 1rem;margin:0;padding:.875rem 1rem;border-radius:var(--hcm-radius-control);background:var(--pu-subtle)}
.projectui-fact{display:grid;gap:.125rem;min-inline-size:0}
.projectui-facts dt{color:var(--muted);font-size:.75rem;font-weight:600;max-inline-size:none}
.projectui-facts dd{margin:0;font-size:.9375rem;font-weight:550;overflow-wrap:anywhere;max-inline-size:none}
.projectui-detail-section{display:grid;gap:.625rem;padding-block-start:1rem;border-block-start:1px solid var(--pu-line)}
.projectui-detail-section>h3{margin:0;font-size:.875rem;font-weight:650;max-inline-size:none}
.projectui-references,.projectui-comments,.projectui-activity{display:grid;gap:.5rem;margin:0;padding:0;list-style:none}
.projectui-reference{display:flex;align-items:center;gap:.5rem;min-inline-size:0;font-size:.875rem;max-inline-size:none}
.projectui-reference a{color:var(--accent);font-weight:600;text-decoration:none}
.projectui-reference a:hover{text-decoration:underline}
.projectui-reference-kind{display:inline-grid;place-items:center;flex:none;inline-size:1.5rem;block-size:1.5rem;border-radius:var(--hcm-radius-control);background:var(--pu-subtle);color:var(--muted);font-size:.75rem;font-weight:700}
.projectui-reference-kind::before{content:"\2022"}
.projectui-reference-kind[data-kind="chat"]::before{content:"#"}
.projectui-reference-kind[data-kind="docs"]::before{content:"";inline-size:.55rem;block-size:.7rem;border:1.5px solid currentColor;border-radius:1px}
.projectui-reference-neutral{color:var(--muted)}
.projectui-reference-state{font-size:.8125rem}
.projectui-reference-state::before{content:"\2014";margin-inline-end:.375rem}
.projectui-comment{display:grid;grid-template-columns:auto minmax(0,1fr);gap:.625rem;align-items:start;max-inline-size:none}
.projectui-comment-body{display:grid;gap:.25rem;padding:.5rem .75rem;border-radius:var(--hcm-radius-control);background:var(--pu-subtle)}
.projectui-comment-byline{display:flex;flex-wrap:wrap;align-items:baseline;gap:.5rem;font-size:.8125rem}
.projectui-comment-byline time{color:var(--muted);font-size:.75rem}
.projectui-comment-body p{margin:0;font-size:.875rem;line-height:1.5;max-inline-size:none}
.projectui-activity{gap:0;padding-inline-start:.3125rem}
.projectui-activity-item{position:relative;display:grid;gap:.125rem;padding-block:0 .875rem;padding-inline-start:1.125rem;border-inline-start:1.5px solid var(--pu-line);font-size:.8125rem;max-inline-size:none}
.projectui-activity-item:last-child{border-inline-start-color:transparent;padding-block-end:0}
.projectui-activity-item::before{content:"";position:absolute;inset-inline-start:-.3125rem;inset-block-start:.3rem;inline-size:.5rem;block-size:.5rem;border-radius:50%;background:var(--surface);box-shadow:0 0 0 1.5px var(--muted)}
.projectui-activity-item:first-child::before{background:var(--accent);box-shadow:0 0 0 1.5px var(--accent)}
.projectui-activity-item time{color:var(--muted);font-size:.75rem}
.projectui-more{justify-self:start;color:var(--accent);font-size:.8125rem;font-weight:600}
.projectui-board :focus-visible,.projectui-list :focus-visible,.projectui-detail :focus-visible{outline-color:var(--hcm-color-focus)}

@media (max-width:60rem){.projectui-list{--pu-list-cols:minmax(0,1fr) minmax(8.5rem,10rem) 6rem}.projectui-list .projectui-card-content{grid-template-areas:"main status due" "main prio who"}.projectui-list-head>span:nth-child(3),.projectui-list-head>span:nth-child(4){display:none}.projectui-list-head{grid-template-columns:minmax(0,1fr) minmax(8.5rem,10rem) 6rem}.projectui-list-head>span:nth-child(5){grid-column:3}}
@media (max-width:40rem){
.projectui-columns{scroll-snap-type:x mandatory;gap:.625rem}
.projectui-column{flex:0 0 min(84vw,22rem);max-inline-size:none;max-block-size:none}
.projectui-column-body{overflow:visible}
.projectui-columns[data-lanes="true"]{grid-template-columns:repeat(var(--pu-cols,3),min(84vw,22rem));max-block-size:calc(100dvh - 11rem)}
.projectui-list-head{display:none}
.projectui-list .projectui-card{padding:.75rem .875rem}
.projectui-list .projectui-card-content{grid-template-columns:minmax(0,1fr);grid-template-areas:none}
.projectui-list .projectui-card-main{padding-inline-end:0!important}
.projectui-list .projectui-card-title{-webkit-line-clamp:2}
.projectui-list .projectui-card-meta-row{display:flex}
.projectui-list .projectui-card-content>*,.projectui-list :is(.projectui-priority,.projectui-due,.projectui-type,.projectui-assignee,.projectui-card-controls){grid-area:auto}
.projectui-list .projectui-assignee{margin-inline-start:auto}
.projectui-list .projectui-board .projectui-assignee-name{display:none}
.projectui-pagination>*{min-block-size:2.5rem;display:inline-flex;align-items:center}
.projectui-detail{padding:1rem}
}
@media (prefers-reduced-motion:reduce){.projectui-board *,.projectui-list *,.projectui-detail *{scroll-behavior:auto!important;transition:none!important;animation:none!important}.projectui-board .projectui-card:hover{transform:none}}
@media (forced-colors:active){.projectui-card,.projectui-column,.projectui-cell,.projectui-detail{border:1px solid CanvasText}.projectui-status-glyph,.projectui-avatar,.projectui-chip{forced-color-adjust:auto;border:1px solid CanvasText}}
`
