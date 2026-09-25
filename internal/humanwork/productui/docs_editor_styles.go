package productui

// docsEditorStylesheet styles the split editor. Everything is logical
// (inline/block) so the layout mirrors in right-to-left locales; colours
// come from the theme tokens only, so light and dark follow the product
// theme. Nothing here depends on a style attribute: the content security
// policy forbids them.
func docsEditorStylesheet() string {
	return `
.docs-editor{display:grid;gap:var(--hcm-space-2);min-width:0;container:docseditor/inline-size}
.docs-editor [hidden]{display:none !important}
/* The suggest popover is position:fixed and never occupies flow space, so
   its host never needs its own grid row (and the gap that would come with
   one) between the toolbar and the panes. */
.docs-suggest-host{display:contents}
.docs-editor-head{display:flex;align-items:flex-end;justify-content:space-between;gap:var(--hcm-space-2);flex-wrap:wrap}
.docs-editor-title-field{display:grid;gap:4px;flex:1 1 20rem;min-width:0}
.docs-editor-title-field label{font-size:var(--hcm-font-size-small);font-weight:650;color:var(--muted)}
.docs-editor-title{width:100%;box-sizing:border-box;min-height:var(--hcm-control-height);padding-inline:var(--hcm-space-2);font:inherit;font-size:1.15rem;font-weight:650;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-editor-title:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:1px}
.docs-editor-view{display:inline-flex;gap:2px;padding:2px;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface)}
.docs-editor-view-option{position:relative;display:inline-flex}
.docs-editor-view-option input{position:absolute;inset:0;margin:0;opacity:0;cursor:pointer}
.docs-editor-view-option span{display:inline-flex;align-items:center;min-height:calc(var(--hcm-control-height) - 6px);padding-inline:var(--hcm-space-2);border-radius:calc(var(--hcm-radius-control) - 2px);font-size:var(--hcm-font-size-small);font-weight:600;color:var(--muted);white-space:nowrap}
.docs-editor-view-option:hover span{background:var(--hcm-hover-surface,var(--soft));color:var(--ink)}
.docs-editor-view-option input:checked+span{background:var(--hcm-color-brand-soft);color:var(--accent)}
.docs-editor-view-option input:focus-visible+span{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:1px}
/* The global paragraph measure broke this one-line hint before its last
   word ("Ctrl+S / saves.", D-22); it may use the editor's full width. */
.docs-editor .docs-editor-help{margin:0;max-width:none;color:var(--muted);font-size:var(--hcm-font-size-small)}

.docs-editor-toolbar{position:sticky;top:0;z-index:3;display:flex;flex-wrap:wrap;align-items:center;gap:2px;padding:4px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface)}
.docs-editor-tool{display:inline-grid;place-items:center;min-width:2rem;height:2rem;padding-inline:.35rem;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;cursor:pointer}
.docs-editor-tool:hover{background:var(--hcm-hover-surface,var(--soft))}
.docs-editor-tool[aria-pressed=true]{background:var(--hcm-color-brand-soft);color:var(--accent)}
.docs-editor-tool:focus-visible,.docs-editor-style-item:focus-visible,.docs-editor-link input:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:1px}
.docs-editor-icon{width:1.1rem;height:1.1rem;flex:none}
.docs-editor-flip:dir(rtl){transform:scaleX(-1)}
.docs-editor-glyph{font-size:.95rem;font-weight:750;line-height:1}
.docs-editor-glyph-italic{font-family:Georgia,serif;font-style:italic;font-weight:600}
.docs-editor-glyph-strike{text-decoration:line-through}
.docs-editor-sep{align-self:stretch;width:1px;margin:4px 6px;background:var(--line)}
.docs-editor-style{position:relative;display:inline-flex}
.docs-editor-style-trigger{display:inline-flex;align-items:center;justify-content:space-between;gap:4px;min-width:8.5rem;font-size:var(--hcm-font-size-small);font-weight:600}
.docs-editor-style-label{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.docs-editor-style-menu{position:absolute;inset-block-start:calc(100% + 4px);inset-inline-start:0;z-index:5;display:grid;gap:2px;min-width:12rem;padding:4px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);box-shadow:var(--hcm-shadow-raised)}
.docs-editor-style-item{padding:.4rem .6rem;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;text-align:start;cursor:pointer}
.docs-editor-style-item:hover{background:var(--hcm-hover-surface,var(--soft))}
.docs-editor-style-item[aria-checked=true]{background:var(--hcm-color-brand-soft);color:var(--accent)}
.docs-editor-style-h1{font-size:1.3rem;font-weight:700}.docs-editor-style-h2{font-size:1.12rem;font-weight:700}.docs-editor-style-h3{font-size:1rem;font-weight:650}

.docs-editor-link{display:flex;flex-wrap:wrap;align-items:center;gap:var(--hcm-space-1);padding:var(--hcm-space-1) var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);box-shadow:var(--hcm-shadow-raised)}
.docs-editor-link label{font-size:var(--hcm-font-size-small);font-weight:650;color:var(--muted)}
.docs-editor-link input{flex:1 1 16rem;min-width:0;box-sizing:border-box;min-height:var(--hcm-control-height);padding-inline:var(--hcm-space-1);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-editor-link input[aria-invalid=true]{border-color:var(--hcm-color-danger,var(--danger))}
.docs-editor-link-error{flex-basis:100%;margin:0;color:var(--hcm-color-danger,var(--danger));font-size:var(--hcm-font-size-small)}

.docs-editor-panes{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:var(--hcm-space-2);height:clamp(22rem,68vh,56rem)}
.docs-editor-panes.is-source,.docs-editor-panes.is-rich{grid-template-columns:minmax(0,1fr)}
.docs-editor-pane{display:grid;grid-template-rows:auto minmax(0,1fr);min-width:0;min-height:0;overflow:hidden;border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface)}
.docs-editor-pane:focus-within{border-color:var(--hcm-color-focus);box-shadow:0 0 0 calc(var(--hcm-focus-ring-width) - 1px) var(--hcm-color-focus)}
.docs-editor-pane.is-hidden{display:none}
.docs-editor-pane-head{padding:.35rem var(--hcm-space-2);border-block-end:1px solid var(--line);background:var(--surface-subtle,var(--soft));color:var(--muted);font-size:var(--hcm-font-size-small);font-weight:650}
.docs-editor-host{display:grid;min-height:0}
.docs-editor-source{box-sizing:border-box;width:100%;height:100%;margin:0;padding:var(--hcm-space-2);border:0;outline:0;resize:none;background:transparent;color:var(--ink);font-family:var(--hcm-font-mono);font-size:.9rem;line-height:1.6;tab-size:4;direction:ltr;text-align:start}
.docs-editor .docs-editor-rich{box-sizing:border-box;height:100%;max-width:none;margin:0;padding:var(--hcm-space-2) var(--hcm-space-3);overflow:auto;outline:0;color:var(--ink);caret-color:var(--accent);overflow-wrap:anywhere}
.docs-editor-rich>:first-child{margin-block-start:0}
.docs-editor-rich h1{font-size:1.6rem;line-height:1.25;margin-block:1.1em .45em}
.docs-editor-rich h2{font-size:1.35rem;line-height:1.3;margin-block:1em .4em}
.docs-editor-rich h3{font-size:1.15rem;margin-block:.9em .35em}
.docs-editor-rich h4,.docs-editor-rich h5,.docs-editor-rich h6{font-size:1rem;margin-block:.8em .3em}
.docs-editor-rich p{margin-block:.55em}
.docs-editor-rich a{color:var(--accent)}
.docs-editor-rich blockquote{margin-inline:0;margin-block:.6em;padding-inline-start:var(--hcm-space-2);border-inline-start:3px solid var(--line);color:var(--muted)}
.docs-editor-rich code{padding:.05em .3em;border-radius:var(--hcm-radius-xs,4px);background:var(--surface-subtle,var(--soft));font-size:.9em}
.docs-editor-rich pre{margin-block:.6em;padding:var(--hcm-space-1) var(--hcm-space-2);overflow:auto;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface-subtle,var(--soft));direction:ltr;text-align:start}
.docs-editor-rich pre code{padding:0;background:none;font-size:.88rem;white-space:pre}
.docs-editor-rich .docs-editor-diagram::before{content:attr(data-label);display:block;margin-block-end:.35rem;font-family:var(--hcm-font-sans);font-size:var(--hcm-font-size-small);font-weight:650;color:var(--muted)}
.docs-editor-rich .docs-editor-raw{color:var(--muted)}
.docs-editor-rich .docs-editor-image{display:inline-flex;align-items:center;gap:.3rem;padding-inline:.4rem;border:1px dashed var(--control-border);border-radius:var(--hcm-radius-control);color:var(--muted);font-size:.92em}
.docs-editor-rich .docs-editor-image::before{content:"";width:.8em;height:.65em;border:1.5px solid currentColor;border-radius:2px}
.docs-editor-rich li.docs-editor-task{list-style:none}
.docs-editor-rich li.docs-editor-task input{margin-inline:-1.3rem .45rem;accent-color:var(--accent)}
.docs-editor-rich hr{border:0;border-block-start:1px solid var(--line);margin-block:1em}
.docs-editor-rich table{border-collapse:collapse;margin-block:.6em}
.docs-editor-rich th,.docs-editor-rich td{min-width:4rem;padding:.35rem .6rem;border:1px solid var(--line);text-align:start;vertical-align:top}
.docs-editor-rich .docs-align-end{text-align:end}.docs-editor-rich .docs-align-center{text-align:center}

/* The save bar is opaque down to the viewport edge: on a phone the page
   scroller's bottom padding left a strip under the sticky bar where the
   formatted text showed through below the buttons (D-23). The solid shadow
   paints the bar's colour over that strip; the padding clears the home
   indicator. */
.docs-editor-foot{position:sticky;bottom:0;z-index:3;display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-2);flex-wrap:wrap;padding-block:var(--hcm-space-1) max(var(--hcm-space-1),env(safe-area-inset-bottom));background:var(--surface);border-block-start:1px solid var(--line)}
@media (max-width:40rem){.docs-editor-foot{box-shadow:0 var(--hcm-space-4) 0 0 var(--surface)}}
.docs-editor-meta{display:flex;align-items:center;gap:var(--hcm-space-2);flex-wrap:wrap;min-width:0;color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-editor-status{margin:0}
.docs-editor-status:empty{display:none}
.docs-editor-status.is-alert{color:var(--hcm-color-danger,var(--danger));font-weight:600}
.docs-editor-dirty{display:inline-flex;align-items:center;gap:.35rem;color:var(--ink);font-weight:600}
.docs-editor-dirty::before{content:"";width:.5rem;height:.5rem;border-radius:999px;background:var(--hcm-color-warning)}
.docs-editor-counts{font-variant-numeric:tabular-nums}
.docs-editor-actions,.docs-editor-confirm{display:flex;align-items:center;gap:var(--hcm-space-1);flex-wrap:wrap}
.docs-editor-confirm span{font-weight:600}

@container docseditor (max-width: 899px){
.docs-editor-panes,.docs-editor-panes.is-split{grid-template-columns:minmax(0,1fr);height:auto}
.docs-editor-panes .docs-editor-pane{height:clamp(20rem,65vh,40rem)}
.docs-editor-panes.is-split .docs-editor-pane{height:clamp(16rem,45vh,30rem)}
}
@container docseditor (max-width: 40rem){
/* Split cannot split at phone width; the editor opens in Formatted there
   (docsEditorNarrow), so the choice is Markdown or Formatted (D-3). */
.docs-editor-view-option:has(>#docs-editor-view-split){display:none}
}
@container docseditor (max-width: 30rem){
/* A toolbar that wraps to several rows pushes the panes down and hides
   most of the reader on first paint; one scrollable row keeps every
   tool reachable without stealing that vertical space. */
.docs-editor-toolbar{flex-wrap:nowrap;overflow-x:auto;scrollbar-width:thin}
.docs-editor-tool,.docs-editor-style,.docs-editor-sep{flex:none}
}
`
}
