package productui

// agentUX073Stylesheet carries every rule the agent pages used to ship as a
// <style> element inside the page body. The product document's content
// security policy admits one stylesheet, by hash, so those elements were
// refused by the browser and the pages rendered with none of these rules:
// names touching their buttons, stacked full-width filters, text held to the
// narrow default measure. Joined to the platform sheet they are covered by its
// hash. Every selector here is scoped to an agent page's own classes.
func agentUX073Stylesheet() string {
	return agentUXR7Stylesheet() + AgentAnnouncementsStyles + agentAnswerDocumentStyles() + agentUX073LayoutStyles + agentUX074LayoutStyles + agentUX039PickerStyles + agentUX042ProblemStyles + agentCost006Styles + AgentLoadingStyles
}

// agentUX073LayoutStyles lays out Activity and Announcements: agents as rows,
// the run filters on one line, each announcement as a card, and prose held to
// a readable measure inside a wide card instead of the narrow default. The
// shell gives every paragraph, list item and definition the prose measure;
// on these pages list items and definitions are rows of a layout, so they take
// the width of their container and only explanatory sentences keep a measure.
const agentUX073LayoutStyles = `
.agent-page-frame :is(li,dd,dt){max-inline-size:none}
.agent-page-frame :is(.agents-page-subtitle,.agents-composer-help,.agents-composer-actions-help,.agents-general-agent,.agents-chat-history-note,.agent-operations-region-header p,.agent-operations-empty p,.agent-announcements-header p,.persona-admin-hero p){max-inline-size:72ch}
.agent-activity-heading{margin-block-start:.5rem;font-size:1.0625rem}
.agent-activity-agents{display:grid;gap:0;margin:0;padding:0;list-style:none;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}
.agent-activity-agents .agent-owner-pause-row{display:grid;grid-template-columns:auto minmax(0,1fr) auto auto;align-items:center;gap:.75rem;padding:.625rem .75rem;border-block-start:1px solid var(--hcm-color-border)}
.agent-activity-agents .agent-owner-pause-row:first-child{border-block-start:0}
.agent-activity-icon{display:inline-flex;inline-size:2rem;block-size:2rem;flex:none}.agent-activity-icon svg{inline-size:100%;block-size:100%}
.agent-activity-identity{display:grid;gap:.125rem;min-width:0}.agent-activity-identity strong{overflow-wrap:anywhere}.agent-activity-identity small{color:var(--hcm-color-text-muted)}
.agent-activity-state{white-space:nowrap}.agent-activity-state[data-tone=success]{color:var(--hcm-color-success)}.agent-activity-state[data-tone=warning]{color:var(--hcm-color-text-muted)}
.agent-activity-agents .persona-admin-pause-unavailable{display:grid;gap:.25rem;justify-items:end;max-inline-size:22rem;text-align:end}
.agent-run-history .agent-operations-recent-title{margin-block:.75rem 0;font-size:1.0625rem}
.agent-history-filters{display:flex;flex-wrap:wrap;align-items:end;gap:.75rem}
.agent-history-filters .persona-admin-editor-field{flex:0 1 14rem;min-width:10rem}
.agent-announcement-list{gap:var(--hcm-space-4,1rem)}
.agent-announcement-row{grid-template-columns:minmax(0,1fr);gap:var(--hcm-space-3,.75rem);background:var(--hcm-color-surface)}
.agent-announcement-row-main{display:grid;gap:var(--hcm-space-2,.5rem)}
.agent-announcement-row h3{margin:0;font-size:1.0625rem;overflow-wrap:anywhere}
.agent-announcement-row .agent-announcement-owner{margin:0;font-size:.875rem}
.agent-announcement-row .agent-announcement-instruction{margin:0;max-inline-size:72ch;line-height:1.5}
.agent-announcement-row dl.agent-announcement-facts{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:.375rem 1rem;margin:0}
.agent-announcement-row dl.agent-announcement-facts div{display:contents}
.agent-announcement-row dl.agent-announcement-facts dt{color:var(--hcm-color-text-muted)}
.agent-announcement-row dl.agent-announcement-facts dd{margin:0;min-width:0;overflow-wrap:anywhere}
.agent-announcement-history summary{cursor:pointer;inline-size:fit-content}
.agent-announcement-row-actions{align-items:center;gap:var(--hcm-space-2,.5rem);padding-block-start:var(--hcm-space-3,.75rem);border-block-start:1px solid var(--hcm-color-border)}
.agent-announcement-row-actions .agent-announcement-delete-action{margin-inline-start:auto}
.agent-announcement-delete{display:flex;flex-wrap:wrap;align-items:center;gap:var(--hcm-space-2,.5rem)}.agent-announcement-delete p{flex:1 1 16rem;margin:0}
.agent-announcements-header{align-items:center;gap:var(--hcm-space-4,1rem)}.agent-announcements-header h2,.agent-announcements-header p{margin:0}.agent-announcements-header>div{display:grid;gap:var(--hcm-space-2,.5rem);flex:1 1 20rem;min-width:0}
@media(max-width:599px){.agent-activity-agents .agent-owner-pause-row{grid-template-columns:auto minmax(0,1fr) auto}.agent-activity-agents .agent-owner-pause-row>:is(.button,.persona-admin-pause-unavailable){grid-column:1/-1;justify-self:start;justify-items:start;text-align:start}.agent-history-filters .persona-admin-editor-field{flex:1 1 100%}.agent-announcement-row dl.agent-announcement-facts{grid-template-columns:minmax(0,1fr)}.agent-announcement-row-actions .agent-announcement-delete-action{margin-inline-start:0}}
`
