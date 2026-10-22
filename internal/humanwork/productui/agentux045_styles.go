package productui

// agentUX045Styles is the Agents pages on a phone.
//
// The two tab strips (Ask, Setup, Operations under the title; Activity,
// Rollout, Move and the rest under it) are one row that scrolls sideways when
// it is wider than the screen. Each fades at the edge that has more to scroll
// to, shown only while there is something to scroll, and opens with the chosen
// tab in view: the chosen tab is the strip's one snap point, so the browser
// lays the strip out already scrolled to it. The title row gives the Ask, Setup
// and Operations strip a line of its own instead of squeezing it beside the
// title.
//
// The cost tables become one card per row on a phone: each cell names its
// column in front of its value, so nothing needs sideways scrolling.
const agentUX045Styles = `
@property --agents-fade-start{syntax:"<length>";inherits:false;initial-value:0px}
@property --agents-fade-end{syntax:"<length>";inherits:false;initial-value:0px}
@keyframes agents-fade-start{from{--agents-fade-start:0px}to{--agents-fade-start:var(--hcm-space-3)}}
@keyframes agents-fade-end{from{--agents-fade-end:var(--hcm-space-3)}to{--agents-fade-end:0px}}
:is(.agents-page-nav,.agent-operations-tabs){max-inline-size:100%;min-inline-size:0;box-sizing:border-box;overflow-x:auto;overscroll-behavior-x:contain;scrollbar-width:none;scroll-snap-type:inline proximity;scroll-padding-inline:var(--hcm-space-3);mask-image:linear-gradient(90deg,transparent,#000 var(--agents-fade-start),#000 calc(100% - var(--agents-fade-end)),transparent);animation:agents-fade-start linear both,agents-fade-end linear both;animation-timeline:scroll(self inline),scroll(self inline);animation-range:0 var(--hcm-space-3),calc(100% - var(--hcm-space-3)) 100%}
:is(.agents-page-nav,.agent-operations-tabs):dir(rtl){mask-image:linear-gradient(270deg,transparent,#000 var(--agents-fade-start),#000 calc(100% - var(--agents-fade-end)),transparent)}
:is(.agents-page-nav,.agent-operations-tabs)::-webkit-scrollbar{display:none}
:is(.agents-page-nav,.agent-operations-tabs)>:is([aria-current="page"],[aria-selected="true"]){scroll-snap-align:center}
:is(.agents-page-nav,.agent-operations-tabs)>*{flex:0 0 auto}
.product-page-frame-actions:has(>.agents-page-nav){flex:0 1 max-content;min-inline-size:0;max-inline-size:100%}
@media(max-width:560px){
.agent-cost-table table,.agent-cost-table tbody,.agent-cost-table tr,.agent-cost-table td{display:block;inline-size:100%;box-sizing:border-box}
.agent-cost-table thead{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
.agent-cost-table tbody{display:grid;gap:var(--hcm-space-1)}
.agent-cost-table tr{display:grid;gap:var(--hcm-space-1);padding:var(--hcm-space-1) var(--hcm-space-2);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}
.agent-cost-table td{display:flex;align-items:baseline;justify-content:space-between;gap:var(--hcm-space-2);padding:0;border:0;text-align:start;min-inline-size:0;overflow-wrap:anywhere}
.agent-cost-table td::before{content:attr(data-label);flex:none;color:var(--hcm-color-text-muted);font-weight:600}
.agent-cost-table td.numeric{text-align:end}
}
`
