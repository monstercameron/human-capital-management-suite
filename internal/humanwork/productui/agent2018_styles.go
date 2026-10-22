package productui

// agentAccessStyles lays out My agent access, the administrator's connection
// console, the cost panel and the spend-limits card. Reviewed on the committed
// build at 1280 and 390 px, in English and Arabic, these were unstyled beyond a
// border: labels ran into their fields, help text sat beside the input, cards
// had no padding, and the console printed codes at the reader. Each rule uses
// the tokens the agent pages around them use, and logical properties so the
// Arabic page mirrors.
const agentAccessStyles = `
.agent-access-page,.agent-admin-access-page,.agent-cost-panel{display:grid;gap:var(--hcm-space-4,1rem);min-width:0}
.agent-access-hero p,.agent-access-hero h2{margin:0}.agent-access-hero{display:grid;gap:.25rem}
.agent-access-page :is(.section-head,.agent-access-subheading),.agent-admin-access-page .section-head{display:grid;gap:.25rem}
.agent-access-page :is(.section-head,.agent-access-subheading) :is(h2,h3,h4,p),.agent-admin-access-page .section-head :is(h2,p){margin:0}
.agent-access-connection-list,.agent-admin-revision-list{display:grid;gap:var(--hcm-space-3,.75rem)}
.agent-access-connection,.agent-admin-revision,.agent-admin-form,.agent-delegations,.agent-cost-panel,.agent-spend-limits{padding:var(--hcm-space-4,1rem);box-sizing:border-box}
.agent-access-connection,.agent-admin-revision{display:grid;gap:var(--hcm-space-3,.75rem)}
.agent-access-connection-heading,.agent-admin-revision-heading{display:flex;flex-wrap:wrap;align-items:center;gap:.25rem var(--hcm-space-3,.75rem)}
.agent-access-connection-heading>*,.agent-admin-revision-heading>*{margin:0}.agent-admin-revision-heading>div{flex:1 1 12rem;display:grid;gap:.125rem}
.agent-access-connection-heading p:empty,.agent-access-skill small:empty{display:none}
.agent-access-page ul,.agent-admin-access-page ul,.agent-cost-notices{list-style:none;margin:0;padding:0;display:grid;gap:.5rem}
.agent-access-skill,.agent-admin-grant,.agent-admin-grant-row,.agent-delegation-grant{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.5rem var(--hcm-space-3,.75rem);padding:.5rem 0;border-block-start:1px solid var(--hcm-color-border)}
.agent-access-skill>div,.agent-admin-grant>div:first-child,.agent-admin-grant-row>div,.agent-delegation-grant>div{display:grid;gap:.125rem;min-width:0;flex:1 1 14rem;overflow-wrap:anywhere}
.agent-admin-grant-badges,.agent-delegation-expiry{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem}
.agent-admin-tier{display:flex;align-items:center;gap:.5rem;flex-wrap:wrap}
.agent-access-actions,.agent-admin-actions,.agent-admin-access-page .action-row{display:flex;flex-wrap:wrap;align-items:center;gap:var(--hcm-space-2,.5rem)}
.agent-admin-access-page .field,.agent-admin-grant-form .field{display:grid;gap:.25rem;max-inline-size:34rem}
.agent-admin-access-page .field label{font-weight:600}
.agent-admin-access-page :is(input:not([type=checkbox]):not([type=radio]),select,textarea){inline-size:100%;box-sizing:border-box;font:inherit;padding:.5rem .625rem;border:1px solid var(--hcm-color-control-border,var(--hcm-color-border));border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text)}
.agent-admin-access-page :is(input:not([type=checkbox]):not([type=radio]),select,textarea):disabled{background:var(--hcm-color-canvas)}
.agent-admin-access-page .agent-admin-tier select{inline-size:auto}
.agent-admin-form,.agent-admin-grant-form,.agent-admin-import-form{display:grid;gap:var(--hcm-space-3,.75rem)}
.agent-admin-form h3,.agent-admin-form p,.agent-admin-revision h4,.agent-admin-revision p{margin:0}
.agent-admin-grant-form fieldset{display:grid;gap:.375rem;margin:0;padding:.5rem .75rem;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}
.agent-admin-skill-choice{display:flex;align-items:center;gap:.5rem}
.agent-admin-warning{padding:.625rem .75rem;border:1px solid var(--hcm-color-border);border-inline-start:4px solid var(--hcm-color-warning,var(--hcm-color-brand-primary));border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}
.agent-admin-warning :is(strong,p){display:block;margin:0}
.agent-cost-panel{border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface)}
.agent-cost-panel h3,.agent-cost-panel h4,.agent-cost-panel>p{margin:0}
.agent-cost-notices li{padding:.5rem .75rem;border:1px solid var(--hcm-color-border);border-inline-start:4px solid var(--hcm-color-warning,var(--hcm-color-brand-primary));border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}
.agent-cost-table th:not(:first-child):last-child,.agent-cost-table th.numeric,.agent-cost-table td.numeric{text-align:end}
.agent-spend-limits{border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface)}
.agent-spend-limits :is(h3,p){margin:0}
.agent-spend-limits input{inline-size:100%;box-sizing:border-box;font:inherit;padding:.5rem .625rem;border:1px solid var(--hcm-color-control-border,var(--hcm-color-border));border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text)}
.agent-spend-limits input:disabled{background:var(--hcm-color-canvas)}
.agent-spend-mount{display:grid;gap:var(--hcm-space-3,.75rem)}
@media(max-width:600px){.agent-access-connection,.agent-admin-revision,.agent-admin-form,.agent-delegations,.agent-cost-panel,.agent-spend-limits{padding:var(--hcm-space-3,.75rem)}.agent-admin-actions .button,.agent-access-actions .button{flex:1 1 100%}}
`
