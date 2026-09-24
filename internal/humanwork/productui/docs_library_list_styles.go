package productui

// docsLibraryListStylesheet holds the list-layout rules added after the
// review of the Docs library (stable search rows, the bulk bar over the
// toolbar, the Title header lined up with the titles, the toolbar's wrap
// points, and the owner column dropped when every row is the viewer's).
// It is appended after docsLibraryStylesheet, so equal selectors here win.
func docsLibraryListStylesheet() string {
	return `
.docs-table-wrap.is-searching .docs-row:not(.docs-row-head){min-height:5.25rem}
.docs-toolbar-stack:has(~ .docs-table-wrap.is-searching)>.docs-chips{display:flex;min-height:2.4375rem;align-items:center}

.docs-toolbar-layer{position:relative;min-width:0}
.docs-toolbar-layer>.docs-bulk{position:absolute;inset:0;z-index:3;align-content:center;padding-block:0;box-shadow:0 0 0 1px var(--line)}
.docs-toolbar-layer.has-bulk>.docs-toolbar{visibility:hidden}

.docs-row-head .docs-cell-title{padding-inline-start:calc(var(--hcm-space-1) + 2.15rem)}
@media (pointer:coarse){
  .docs-row-head .docs-cell-title{padding-inline-start:calc(var(--hcm-space-1) + 3.15rem)}
}
@container docslib (max-width:30rem){
  .docs-row-head .docs-cell-title{padding-inline-start:calc(var(--hcm-space-2) + 2.15rem)}
  @media (pointer:coarse){.docs-row-head .docs-cell-title{padding-inline-start:calc(var(--hcm-space-2) + 3.15rem)}}
}

@container docslib (width > 56rem) and (width <= 60rem){
  .docs-find{flex-basis:100%;max-width:none}
}
@container docslib (width <= 44rem){
  .docs-find{flex-basis:100%;max-width:none}
}

@container docslib (width > 62rem){
  .docs-table.docs-no-owner .docs-row{grid-template-columns:2.5rem minmax(0,1fr) minmax(8rem,10rem) 5.5rem 2.75rem}
  .docs-table.docs-no-owner .docs-cell-owner{display:none}
}
`
}
