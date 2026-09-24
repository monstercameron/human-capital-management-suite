package productui

// docsVisualStylesheet holds the Docs visual-quality rules from the docs
// review (M7, M10, M11, L1, L5): the reading measure, touch targets, the
// phone folder strip and reduced motion. It is appended after the library,
// editor and diagram sheets, so equal-specificity rules here win; it only
// adds rules, and the rules it corrects in those sheets were changed in place.
func docsVisualStylesheet() string {
	return `
:root{--hcm-font-size-xsmall:calc(var(--hcm-font-size-small) - .0625rem)}
.docs-markdown>:is(p,ul,ol,dl,h1,h2,h3,h4,h5,h6,blockquote,pre,hr){max-width:72ch}
.docs-markdown>:is(.docs-diagram,.docs-table-scroll){max-width:calc(100% + 2rem);margin-inline-end:-2rem}
[dir=rtl] .docs-table-wrap.is-refreshing::before{animation-name:docs-progress-rtl}
@keyframes docs-progress-rtl{from{transform:translateX(0)}to{transform:translateX(-150%)}}
.docs-cell-select{align-self:stretch}
.docs-select-hit{display:grid;place-items:center;align-self:stretch;width:100%;min-height:2.5rem;cursor:pointer}
.docs-owner-filter{min-height:1.5rem}
@container docslib (max-width:56rem){
  .docs-nav>*{order:1}
  .docs-nav-section{display:flex;order:2;flex:none;margin:0;padding:0}
  .docs-nav-section :is(h2,h3){position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
  .docs-nav-add{width:2.25rem;height:2.25rem;border:1px solid var(--line);border-radius:999px}
  .docs-nav::after{content:"";order:3;position:sticky;inset-inline-end:0;flex:0 0 2.5rem;align-self:stretch;background:linear-gradient(to left,var(--canvas),transparent);pointer-events:none}
  [dir=rtl] .docs-nav::after{background:linear-gradient(to right,var(--canvas),transparent)}
  .docs-folder-item{border:1px solid var(--line);border-radius:999px}
  .docs-folder-item>.docs-nav-link{border:0;padding-inline-end:.25rem}
  .docs-folder-item>.docs-nav-link[aria-current=page]{border-start-end-radius:0;border-end-end-radius:0}
  .docs-folder-menu{display:block;position:static;opacity:1}
  .docs-folder-menu .docs-row-menu-trigger{width:2.25rem;height:2.25rem;border-radius:999px}
  @supports (anchor-name:--a){
    .docs-folder-menu[open]>.docs-row-menu-trigger{anchor-name:--docs-folder-menu}
    .docs-folder-menu .docs-row-menu-panel{position:fixed;inset:auto;position-anchor:--docs-folder-menu;position-area:block-end span-inline-end;position-try-fallbacks:flip-inline;margin-block-start:4px}
  }
  .docs-folder-item:hover .docs-nav-count,.docs-folder-item:focus-within .docs-nav-count{visibility:visible}
}
@media (pointer:coarse){
  .docs-menu-item,.docs-nav-link,.docs-sort-link,.docs-thread-action,.docs-comments-toggle,.docs-copy-text,.docs-move-option,.docs-access-remove,.docs-outline a,.docs-owner-filter,.docs-folder-item{min-height:2.75rem}
  .docs-comments-toggle,.docs-outline a{display:flex;align-items:center}
  .docs-thread-quote{min-height:2.75rem}
  .docs-page-link{min-width:2.75rem;height:2.75rem}
  .docs-nav-add,.docs-dialog-close{width:2.75rem;height:2.75rem}
  .docs-chip-clear,.docs-pick-remove{position:relative}
  .docs-chip-clear::after,.docs-pick-remove::after{content:"";position:absolute;inset:-.7rem}
  .docs-folder-menu{position:static}
  .docs-folder-menu .docs-row-menu-trigger{width:2.25rem;height:2.25rem}
  .docs-folder-item:hover .docs-nav-count,.docs-folder-item:focus-within .docs-nav-count{visibility:visible}
}
@media (prefers-reduced-motion:reduce){
  .docs-table-wrap.is-refreshing::before{animation:none;width:100%;opacity:.7}
  .docs-thread,.docs-table-wrap.is-refreshing .docs-table{transition:none}
}
`
}
