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
  /* D-4: "?" and "+" are a fixed trailing group: the section sticks to the
     strip's end on an opaque canvas, so the chips scroll under it and the
     end fade sits on the chips (its ::before), never on the buttons. The
     old whole-strip mask faded the "+" itself. The start fade (the strip's
     ::before) only appears once the strip is scrolled, so a chip cut at
     the start edge fades out instead of being hard-clipped at x=0. */
  .docs-nav>*{order:1}
  /* A chip focused or scrolled into view stops clear of the trailing
     group (2 x 2.75rem + gaps) and its fade, not underneath them (r5). */
  .docs-nav{scroll-padding-inline:2.5rem 9rem}
  .docs-nav-section{display:flex;order:2;flex:none;align-items:center;gap:.25rem;margin:0;padding:0;padding-inline-start:.25rem;position:sticky;inset-inline-end:0;z-index:1;background:var(--canvas)}
  .docs-nav-section::before{content:"";position:absolute;inset-block:0;inset-inline-end:100%;width:2.5rem;background:linear-gradient(to left,var(--canvas),transparent);pointer-events:none}
  [dir=rtl] .docs-nav-section::before{background:linear-gradient(to right,var(--canvas),transparent)}
  .docs-nav-section :is(h2,h3){position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
  /* 44px targets whose visible ring is the whole target, the same height
     as the folder chips beside them; "?" gets the ring too, so it reads as
     a control rather than a bare glyph (r4 D-2). */
  .docs-nav-info,.docs-nav-add{width:2.75rem;height:2.75rem;border:0;border-radius:999px;box-shadow:inset 0 0 0 1px var(--line)}
  .docs-nav::before{content:"";order:0;position:sticky;inset-inline-start:0;z-index:1;flex:0 0 2rem;margin-inline-end:calc(-2rem - var(--hcm-space-1));align-self:stretch;background:linear-gradient(to right,var(--canvas),transparent);pointer-events:none;opacity:0}
  [dir=rtl] .docs-nav::before{background:linear-gradient(to left,var(--canvas),transparent)}
  @supports (animation-timeline:scroll()){
    .docs-nav::before{animation:docs-strip-start linear both;animation-timeline:scroll(nearest inline)}
    .docs-nav-section::before{animation:docs-strip-end linear both;animation-timeline:scroll(nearest inline)}
  }
  .docs-folder-item{border:1px solid var(--line);border-radius:999px}
  .docs-folder-item>.docs-nav-link{border:0;padding-inline-end:.25rem}
  /* The current folder's fill belongs to the whole pill (link and its
     "..." menu), not a square-cornered box inside it that stopped before
     the menu (D-4). */
  .docs-folder-item:has(>.docs-nav-link[aria-current=page]){background:var(--hcm-color-brand-soft)}
  .docs-folder-item>.docs-nav-link[aria-current=page]{background:transparent}
  .docs-folder-menu{display:block;position:static;opacity:1}
  .docs-folder-menu .docs-row-menu-trigger{width:2.25rem;height:2.25rem;border-radius:999px}
  @supports (anchor-name:--a){
    .docs-folder-menu[open]>.docs-row-menu-trigger{anchor-name:--docs-folder-menu}
    .docs-folder-menu .docs-row-menu-panel{position:fixed;inset:auto;position-anchor:--docs-folder-menu;position-area:block-end span-inline-end;position-try-fallbacks:flip-inline;margin-block-start:4px}
  }
  .docs-folder-item:hover .docs-nav-count,.docs-folder-item:focus-within .docs-nav-count{visibility:visible}
}
@keyframes docs-strip-start{from{opacity:0}4%{opacity:1}to{opacity:1}}
@keyframes docs-strip-end{from{opacity:1}96%{opacity:1}to{opacity:0}}
@media (pointer:coarse){
  .docs-menu-item,.docs-nav-link,.docs-sort-link,.docs-thread-action,.docs-comments-toggle,.docs-copy-text,.docs-move-option,.docs-access-remove,.docs-outline a,.docs-owner-filter,.docs-folder-item{min-height:2.75rem}
  .docs-nav-add,.docs-nav-info,.docs-folder-menu .docs-row-menu-trigger{width:2.75rem;height:2.75rem}
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
