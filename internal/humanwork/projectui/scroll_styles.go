package projectui

// projectUIScrollStyles makes a wide or tall board navigable the way Trello
// and Jira boards are: the board scrolls sideways with a visible scrollbar,
// and on a wide screen each column scrolls on its own inside the viewport
// under its header, so a long column never pushes the others off screen.
// Shadows at a column's top and bottom edges appear only while there is
// more to scroll that way (background-attachment: local covers them at
// rest), which answers the "tasks silently clipped" objection that turned
// column scrolling off in an earlier round. Swimlane boards keep their own
// grid scroller and are untouched.
const projectUIScrollStyles = `
.projectui-board{--pu-board-offset:18.25rem}
.projectui-board .projectui-columns:not([data-lanes="true"]){scrollbar-width:auto;scrollbar-color:color-mix(in srgb,var(--ink) 28%,transparent) color-mix(in srgb,var(--ink) 5%,transparent);padding-block-end:.875rem}
.projectui-board .projectui-columns:not([data-lanes="true"])::-webkit-scrollbar{block-size:.625rem}
.projectui-board .projectui-columns:not([data-lanes="true"])::-webkit-scrollbar-thumb{border-radius:999px;background:color-mix(in srgb,var(--ink) 28%,transparent)}
.projectui-board .projectui-columns:not([data-lanes="true"])::-webkit-scrollbar-track{border-radius:999px;background:color-mix(in srgb,var(--ink) 5%,transparent)}
@media (min-width:40.01rem){
.projectui-board .projectui-columns:not([data-lanes="true"]){align-items:flex-start}
.projectui-board .projectui-columns:not([data-lanes="true"])>.projectui-column{max-block-size:calc(100dvh - var(--pu-board-offset))}
.projectui-board .projectui-columns:not([data-lanes="true"])>.projectui-column>.projectui-column-body{flex:1 1 auto;min-block-size:6rem;overflow-y:auto;overscroll-behavior-block:contain;scrollbar-width:thin;scrollbar-gutter:stable;
background:linear-gradient(var(--pu-col-bg) 30%,transparent) top/100% 2.5rem no-repeat local,linear-gradient(transparent,var(--pu-col-bg) 70%) bottom/100% 2.5rem no-repeat local,radial-gradient(farthest-side at 50% 0,color-mix(in srgb,var(--ink) 16%,transparent),transparent) top/100% .75rem no-repeat scroll,radial-gradient(farthest-side at 50% 100%,color-mix(in srgb,var(--ink) 16%,transparent),transparent) bottom/100% .75rem no-repeat scroll}
.projectui-board .projectui-columns:not([data-lanes="true"])>.projectui-column>.projectui-column-head{position:sticky;inset-block-start:0;z-index:1;background:var(--pu-col-bg);border-start-start-radius:var(--hcm-radius-surface);border-start-end-radius:var(--hcm-radius-surface)}
}
`
