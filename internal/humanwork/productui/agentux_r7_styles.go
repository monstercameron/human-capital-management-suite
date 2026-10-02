package productui

// Keep this CSS free of HTML text escapes so inline SSR and browser renders agree.
func agentUXR7Stylesheet() string {
	return `
.agent-page-frame,.agent-page-frame *,.agent-operations-region-content,.agent-run-history{min-width:0;max-width:100%;box-sizing:border-box}
.agent-operations-page :is(.card,.agent-operations-region,.agent-running-card,.persona-admin-editor-field){min-width:0;max-width:100%;box-sizing:border-box}.agent-operations-page :is(input,select,textarea){min-width:0;max-width:100%;box-sizing:border-box}
.agent-page-frame p,.agent-page-frame label{max-inline-size:none;overflow-wrap:anywhere}
.agents-tasks .agents-task-list,.agents-tasks .agents-task-row,.agents-task-listing{width:100%;max-width:none;min-width:0;justify-self:stretch}
.agents-task-source-meta{display:flex;align-items:center;flex-wrap:wrap;gap:.35rem .75rem;min-width:0}
.agents-task-source-meta .agents-task-documents{font-size:.875rem}
.agents-task-times{align-self:start;white-space:nowrap;grid-column:3;grid-row:1}
.agents-task-chevron{grid-column:4;grid-row:1}
.agents-task-request{text-align:start;grid-column:2;grid-row:1;line-height:1.5;min-width:0;min-inline-size:0;overflow-wrap:anywhere}
.agents-detail-close-desktop{display:inline}.agents-detail-back-mobile{display:none}
.agents-back-link{justify-self:end}.agents-task-filter{padding-inline:12px}
.agents-agent-choice input[type=radio]{appearance:none;flex:0 0 18px;inline-size:18px;block-size:18px;padding:0;min-height:0;border:1.5px solid var(--hcm-color-border);border-radius:50%;background:transparent}
.agents-agent-choice input[type=radio]:checked{border-color:var(--hcm-color-brand-primary);background:radial-gradient(circle,var(--hcm-color-brand-primary) 0 4px,transparent 5px)}
.agents-agent-choice span{flex:1;min-width:0;align-items:start;justify-items:start}.agents-agent-choice :is(strong,small){width:100%;text-align:inherit}
[dir=rtl] .agents-agent-choice,[dir=rtl] .agents-task-request,[dir=rtl] .persona-admin-purpose{text-align:start}
.agents-document-picker-panel input:focus-visible{border-color:var(--hcm-color-focus);outline:2px solid var(--hcm-color-focus);outline-offset:2px;box-shadow:none}
.persona-admin-page .persona-admin-card-title h3{font-size:1.25rem;font-weight:600}
.persona-admin-page .persona-admin-preview-heading h2{font-size:1.0625rem}
.persona-admin-page .persona-admin-facts .persona-admin-fact:last-child{grid-column:2}
.persona-admin-page .persona-admin-skill-list{grid-template-columns:repeat(2,minmax(0,1fr));padding:0;list-style:none;max-width:none;width:100%}
.persona-admin-page .persona-admin-installation-list{width:100%;max-width:none;padding:0;list-style:none;box-sizing:border-box}
.persona-admin-page .persona-admin-placement-main{flex-wrap:wrap;min-width:0}.persona-admin-page .persona-admin-placement-detail{white-space:normal;overflow-wrap:anywhere}
.persona-admin-page .persona-admin-purpose{display:block;text-align:start}.persona-admin-page .persona-admin-person-chip{display:inline-flex;align-items:center;gap:.5rem;text-decoration:none}
.persona-admin-page .persona-admin-document-warning{flex-basis:100%}
.persona-admin-page .persona-admin-reference-documents li{display:flex;align-items:baseline;gap:.35rem;flex-wrap:wrap;min-width:0}
.persona-admin-page .persona-admin-placement-document-titles{padding-inline-start:1.25rem;max-width:none;text-align:start}.persona-admin-page .persona-admin-placement-document-titles li{direction:inherit;text-align:inherit}
.persona-admin-page .persona-admin-published-approval{display:grid;gap:.35rem;line-height:1.5}
.persona-admin-page .persona-admin-instructions p{display:block;max-width:none}.persona-admin-page .persona-admin-instruction-chip{display:inline-flex;vertical-align:baseline}
.persona-admin-page textarea[data-agent-purpose-autosize]{min-height:4.5rem;max-height:10rem;field-sizing:content;overflow:auto}
.persona-admin-page :is(input,select)[aria-invalid=true]{border-color:var(--hcm-color-danger);outline:1px solid var(--hcm-color-danger)}
.persona-admin-page .persona-admin-confirm summary{list-style:none}.persona-admin-page .persona-admin-confirm summary::-webkit-details-marker{display:none}
.persona-admin-page .persona-admin-secondary-actions:popover-open,.persona-admin-page .persona-admin-add-placement-form:popover-open{position:fixed;inset:auto;inset-block-start:anchor(bottom);inset-inline-end:anchor(end);margin:.25rem 0 0;position-try-fallbacks:flip-inline,flip-block;max-width:calc(100vw - 56px);width:min(26rem,calc(100vw - 56px));max-height:calc(100vh - 56px);overflow:auto}
.persona-admin-page [popover]:not(:popover-open){display:none}
.agent-version-table-wrap{overflow:auto;max-width:100%}.persona-admin-version-history{width:100%;border-collapse:collapse;table-layout:fixed}.persona-admin-version-history :is(th,td){padding:.5rem;text-align:start;vertical-align:top;border-block-end:1px solid var(--hcm-color-border);overflow-wrap:anywhere}.persona-admin-version-actions{display:grid;gap:.5rem}
.agent-operations-tabs{min-width:0;max-width:100%;overflow-x:auto;flex-wrap:nowrap;scrollbar-width:thin}.agent-operations-tabs a{flex:0 0 auto;white-space:nowrap}
.agent-run-warning{display:flex;flex-wrap:wrap;align-items:center;gap:.75rem;padding:.75rem;border:1px solid var(--hcm-color-danger);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}.agent-run-warning p{flex:1 1 100%;margin:0}
.agent-owner-pause-row{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.75rem;min-width:0}
.agent-history-filters{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:.75rem;min-width:0}.agent-run-history{display:grid;gap:.75rem}
.agent-history-desktop{min-width:0}.agent-history-table{width:100%;table-layout:fixed;border-collapse:collapse}.agent-history-table :is(th,td){height:44px;padding:.5rem;text-align:start;vertical-align:top;border-block-end:1px solid var(--hcm-color-border);overflow-wrap:anywhere;font-size:.875rem}.agent-history-table th{font-weight:600}.agent-history-table td:nth-child(1){width:18%}.agent-history-table td:nth-child(2){width:23%}
.agent-run-details summary{cursor:pointer;list-style:none;min-height:28px}.agent-run-details summary::before{content:attr(data-chevron);display:inline-block;margin-inline-end:.35rem}.agent-run-details[open] summary::before{transform:rotate(90deg)}.agent-run-details .agent-operations-technical{margin-block-start:.5rem}
.agent-run-outcome-cell{display:grid;gap:.25rem}.agent-run-outcome[data-tone=danger],.agent-run-failure{color:var(--hcm-color-danger)}.agent-run-outcome[data-tone=success]{color:var(--hcm-color-success)}
.agent-history-mobile{display:none}.agent-history-pagination{display:flex;align-items:center;justify-content:space-between;gap:.5rem;flex-wrap:wrap}
@media(min-width:1024px){.agents-tasks[data-has-task-detail=true]{grid-template-columns:420px minmax(0,1fr)}}
@media(max-width:1023px){.agents-detail-close-desktop{display:none}.agents-detail-back-mobile{display:inline}.agents-back-link{justify-self:start}}
@media(max-width:800px){body:has(.agent-page-frame) .topbar .locale-menu{flex:0 0 44px;width:44px;min-width:44px}body:has(.agent-page-frame) .topbar .header-navigation-tools{flex:1;min-width:0}.persona-admin-page .persona-admin-facts .persona-admin-fact:last-child{grid-column:auto}}
@media(min-width:761px) and (max-width:800px){body:has(.agent-page-frame) .topbar{grid-template-columns:minmax(0,120px) minmax(0,1fr) 44px auto auto}body:has(.agent-page-frame) .topbar .brand-cluster{grid-column:1;grid-row:1}body:has(.agent-page-frame) .topbar .header-navigation-tools{grid-column:2;grid-row:1;padding:0}body:has(.agent-page-frame) .topbar .locale-menu{grid-column:3;grid-row:1}body:has(.agent-page-frame) .header-navigation-tools .global-search{flex:1 1 0;width:auto;min-width:0}body:has(.agent-page-frame) .global-search .global-search-input{width:100%;padding-inline:42px 14px;color:var(--hcm-color-text);cursor:text}body:has(.agent-page-frame) .global-search .global-search-input::placeholder{color:var(--hcm-color-text-muted)}body:has(.agent-page-frame) .header-navigation-tools .global-search:focus-within{position:relative;inset:auto;width:auto}body:has(.agent-page-frame) .header-navigation-tools .history-navigation{display:none}}
@media(max-width:599px){.agent-history-desktop{display:none}.agent-history-mobile{display:grid;gap:.75rem}.agent-history-filters{grid-template-columns:minmax(0,1fr)}.persona-admin-page .persona-admin-skill-list{grid-template-columns:minmax(0,1fr)}.persona-admin-version-history{min-width:32rem}.agent-operations-tabs{padding-inline-end:24px;mask-image:linear-gradient(to right,var(--hcm-color-text) calc(100% - 12px),transparent)}[dir=rtl] .agent-operations-tabs{mask-image:linear-gradient(to left,var(--hcm-color-text) calc(100% - 12px),transparent)}}
@media(max-width:390px){.agents-task-row-heading{grid-template-columns:auto minmax(0,1fr) auto auto;gap:6px}.agents-task-time{font-size:.75rem}.agents-task-filter{padding-inline:12px;min-width:max-content}.agents-task-filters{display:flex;overflow-x:auto;gap:6px}.agents-task-filter{flex:1 0 auto}.persona-admin-page .persona-admin-step-label{font-size:.875rem}.agent-operations-tabs a{padding-inline:10px}}
@media(max-width:359px){.persona-admin-page .persona-admin-card-controls{grid-template-columns:minmax(0,1fr)}.persona-admin-page .persona-admin-card-controls .button{width:100%;white-space:normal;height:auto;min-height:44px}.product-page-frame-title-row{flex-wrap:wrap}.agent-page-frame .product-page-frame-actions{width:100%}.agent-page-frame .agents-page-nav{width:100%;justify-content:space-between}}
@media(max-width:480px){.persona-admin-page .persona-admin-lifecycle-progress .persona-admin-step-label{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}.persona-admin-page .persona-admin-step-content{display:flex;align-items:center;justify-content:center;gap:.3rem}.persona-admin-page .persona-admin-mobile-progress{display:block;font-size:.875rem;font-weight:600;overflow-wrap:anywhere}}
.agent-operations-technical summary{list-style:none;cursor:pointer}.agent-operations-technical summary::-webkit-details-marker{display:none}.agent-operations-technical summary::before{content:attr(data-chevron);display:inline-block;margin-inline-end:.35rem}.agent-operations-technical[open] summary::before{transform:rotate(90deg)}
`
}
