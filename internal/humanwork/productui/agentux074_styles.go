package productui

// agentUX074LayoutStyles lets the Ask form and the task list use the page: a
// task is its question and time on one line and who answered with the answer
// excerpt on the next, and people on Agent setup are drawn at the size their
// names are read at.
const agentUX074LayoutStyles = `
.agents-task-summary{display:flex;align-items:center;gap:.5rem;margin:0;min-width:0;padding-inline-start:calc(1.5rem + 12px);font-size:.9375rem}
.agents-task-summary .agents-task-agent{display:inline-flex;align-items:center;gap:.375rem;flex:0 1 auto;min-width:0;max-inline-size:24rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.agents-task-summary .agents-task-agent .agent-icon{inline-size:1.25rem;block-size:1.25rem;flex:none}
.agents-task-summary :is(.agents-task-preview,.agents-task-failure-reason){flex:1 1 0;min-width:0;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.agents-task-summary .agents-task-preview::before,.agents-task-summary .agents-task-failure-reason::before{content:"·";margin-inline-end:.5rem;color:var(--hcm-color-text-muted)}
.agents-task-row[data-task-category=failed] .agents-task-summary{flex-wrap:wrap}
.agents-task-row[data-task-category=failed] .agents-task-summary :is(.agents-task-preview,.agents-task-failure-reason){flex:0 1 auto;white-space:normal;overflow:visible}
.agents-task-row[data-task-category=failed] .agents-task-summary .agents-task-failure-reason::before{content:none}
.agents-task-row-meta{padding-inline-start:calc(1.5rem + 24px)}
.agents-agent-choice .agent-icon{flex:none}
.persona-admin-page .persona-admin-card-identity{flex:1 1 auto;flex-wrap:nowrap;min-width:0}
.persona-admin-page .persona-admin-card-title{display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;column-gap:.75rem;row-gap:.125rem;flex:1 1 auto;min-width:0}
.persona-admin-page .persona-admin-card-title>.agent-icon{grid-row:1 / span 2;align-self:start;inline-size:2.25rem;block-size:2.25rem}
.persona-admin-page .persona-admin-card-name{display:flex;flex-wrap:wrap;align-items:center;gap:.25rem .75rem;min-width:0}
.persona-admin-page .persona-admin-card-name h3{margin:0;overflow-wrap:anywhere}
.persona-admin-page .persona-admin-card-title>small{grid-column:2}
.persona-admin-page .persona-admin-person-chip .avatar.tiny{inline-size:1.5rem;block-size:1.5rem;border-radius:50%;object-fit:cover}
@media(max-width:599px){.agents-task-summary{flex-wrap:wrap;padding-inline-start:0}.agents-task-summary .agents-task-agent{max-inline-size:none;white-space:normal}.agents-task-summary :is(.agents-task-preview,.agents-task-failure-reason){flex-basis:100%;white-space:normal;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2}.agents-task-summary .agents-task-preview::before,.agents-task-summary .agents-task-failure-reason::before{content:none}.agents-task-row-meta{padding-inline-start:12px}}
`
