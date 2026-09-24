package productui

// docsDialogStylesheet styles the Docs dialogs' focus and feedback details:
// the focus ring on a dialog and a menu item reached by the arrow keys, the
// draft-discard question, the copy fallback field, the share chip's remove
// target, and the scroll cue on a long folder list.
func docsDialogStylesheet() string {
	return `
.docs-dialog:focus{outline:none}
.docs-dialog:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-menu-item:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:calc(-1 * var(--hcm-focus-ring-width))}
.docs-thread:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-pick-chip .docs-pick-remove{width:1.5rem;height:1.5rem}
.docs-move-list{background:linear-gradient(var(--surface) 30%,transparent) top/100% 1.25rem no-repeat local,linear-gradient(transparent,var(--surface) 70%) bottom/100% 1.25rem no-repeat local,linear-gradient(color-mix(in srgb,var(--ink) 16%,transparent),transparent) top/100% .5rem no-repeat scroll,linear-gradient(transparent,color-mix(in srgb,var(--ink) 16%,transparent)) bottom/100% .5rem no-repeat scroll}
.docs-create-discard{display:grid;gap:var(--hcm-space-1);margin-block-start:var(--hcm-space-1);padding:var(--hcm-space-2);border:1px solid var(--line);border-inline-start:var(--hcm-radius-xs) solid var(--hcm-color-warning);border-radius:var(--hcm-radius-control)}
.docs-create-discard p{margin:0;font-weight:600;color:var(--ink)}
.docs-create-discard-actions{display:flex;flex-wrap:wrap;justify-content:flex-end;gap:var(--hcm-space-1)}
.docs-share-copy-fallback{display:grid;gap:var(--hcm-space-1);flex:1 1 100%}
.docs-share-copy-fallback .docs-notice{margin:0;font-size:var(--hcm-font-size-small)}
.docs-share-link{width:100%;box-sizing:border-box;min-height:var(--hcm-control-height);padding-inline:var(--hcm-space-2);font:inherit;font-family:var(--hcm-font-mono);color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);text-align:start}
.docs-share-link:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:1px}
`
}
