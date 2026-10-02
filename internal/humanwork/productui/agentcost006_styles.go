package productui

// agentCost006Styles lays out the spend limits card and the cost panel on
// Agent operations. It uses the same tokens as the agent pages around it:
// labels sit above their fields, the figures wrap into a grid, and the tables
// scroll sideways inside their own box rather than widening the page.
const agentCost006Styles = `
.agent-spend-limits{display:grid;gap:.75rem;max-inline-size:36rem}
.agent-spend-limits .field{display:grid;gap:.25rem}
.agent-spend-limits .field label{font-weight:600}
.agent-spend-limits .action-row{display:flex;flex-wrap:wrap;align-items:center;gap:var(--hcm-space-2,.5rem)}
.agent-spend-reached{margin:0;padding:.5rem .75rem;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}
.agent-cost-panel{display:grid;gap:.75rem}
.agent-cost-figures{display:grid;grid-template-columns:repeat(auto-fit,minmax(11rem,1fr));gap:.5rem;margin:0;padding:0;list-style:none}
.agent-cost-figure{display:grid;gap:.125rem;padding:.5rem .75rem;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);max-inline-size:none}
.agent-cost-figure strong{font-size:1.25rem}
.agent-cost-table{display:grid;gap:.375rem;overflow-x:auto}
.agent-cost-table td.numeric,.agent-cost-table th:last-child{text-align:end;font-variant-numeric:tabular-nums}
`
