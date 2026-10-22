package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-077: the Agents pages' first impression. Two links run together under
// the title, the breadcrumb has no spacing and names the wrong page, the chosen
// agent is marked by a dot, and one paragraph explains two buttons. These pieces
// give each its own element and the sheet below gives them room.

// agentUX077Text is the copy this file adds, in the three languages the pages
// carry. The explanation of each button is one short line, so the person reads
// it with the button it is about.
func agentUX077Text(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"ask_hint":     {"Get an answer here, usually within a minute.", "Die Antwort erscheint hier, meist innerhalb einer Minute.", "احصل على إجابة هنا، عادةً خلال دقيقة."},
		"plan_hint":    {"See a plan to approve first. It then works in the background.", "Sie sehen zuerst einen Plan zur Freigabe. Danach läuft die Arbeit im Hintergrund.", "تعرض الخطة أولاً لتوافق عليها ثم يعمل في الخلفية."},
		"posts_once":   {"Posts once", "Wird einmal veröffentlicht", "يُنشر مرة واحدة"},
		"already_done": {"already posted", "bereits veröffentlicht", "تم النشر بالفعل"},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	return values[agentRPLocaleIndex(locale)]
}

// agentUX077ComposerActions is the row of the two ways to start work. Each
// button sits above its own one-line explanation, so neither is a paragraph the
// reader has to match back to a button.
func agentUX077ComposerActions(locale LocaleContext, available bool) ui.Node {
	return html.Div(html.Props{Class: "agents-composer-actions"},
		html.Div(html.Props{Class: "agents-composer-action"},
			html.Button(html.Props{Class: "button primary", Type: "button", Disabled: !available, Raw: map[string]any{"data-agent-action": "quick-answer", "aria-describedby": "agents-ask-hint"}}, ui.Text(locale.Text("agents.quick_answer"))),
			html.P(html.Props{ID: "agents-ask-hint", Class: "agents-composer-action-hint muted"}, ui.Text(agentUX077Text(locale, "ask_hint"))),
		),
		html.Div(html.Props{Class: "agents-composer-action"},
			html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: !available, Raw: map[string]any{"data-agent-action": "long-task", "aria-describedby": "agents-plan-hint"}}, ui.Text(locale.Text("agents.start_task"))),
			html.P(html.Props{ID: "agents-plan-hint", Class: "agents-composer-action-hint muted"}, ui.Text(agentUX077Text(locale, "plan_hint"))),
		),
	)
}

// agentUX077Breadcrumb names the page the reader is on: "Admin / Agent setup" or
// "Admin / Agent operations", not the section's name for both.
func agentUX077Breadcrumb(locale LocaleContext, leaf string) ui.Node {
	return html.Nav(html.Props{Class: "persona-admin-breadcrumb", Aria: map[string]string{"label": personaAdminText(locale, "breadcrumb")}},
		html.A(html.Props{Href: Path(PageAdmin)}, ui.Text(personaAdminText(locale, "breadcrumb_admin"))),
		html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, ui.Text(personaAdminPathSeparator)),
		html.Span(html.Props{Aria: map[string]string{"current": "page"}}, ui.Text(leaf)),
	)
}

const agentUX077Styles = `
.persona-admin-breadcrumb{display:flex;align-items:center;flex-wrap:wrap;gap:.5rem;font-size:.875rem;color:var(--hcm-color-text-muted)}
.persona-admin-breadcrumb a{color:inherit}.persona-admin-breadcrumb [aria-current=page]{color:var(--hcm-color-text)}
.agents-view-tabs{justify-self:start;overflow-x:auto}
.agents-view-tabs .agents-page-nav-item{min-height:40px}
.agents-agent-choice{position:relative}
.agents-agent-choice:has(input:checked){border-color:var(--hcm-color-brand-primary);background:color-mix(in srgb,var(--hcm-color-brand-primary) 7%,var(--hcm-color-surface,transparent));box-shadow:0 0 0 1px var(--hcm-color-brand-primary)}
.agents-agent-choice:has(input:checked)::after{content:"\2713";position:absolute;inset-block-start:.5rem;inset-inline-end:.75rem;color:var(--hcm-color-brand-primary);font-weight:700;line-height:1}
.agents-composer-actions{align-items:flex-start;gap:var(--hcm-space-4,1rem)}
.agents-composer-action{display:grid;justify-items:start;gap:.375rem;flex:0 1 22rem;min-width:0}
.agents-composer-action-hint{margin:0;font-size:.875rem;line-height:1.4;max-inline-size:22rem}
@media(max-width:390px){.agents-composer-action{flex:1 1 100%}.agents-composer-action .button{width:100%}}
.agent-announcement-list{gap:0}
.agent-announcement-row{border:0;border-radius:0;box-shadow:none;background:transparent;border-block-start:1px solid var(--hcm-color-border);padding-inline:0;padding-block:var(--hcm-space-4,1rem)}
@media(min-width:900px){.agent-announcement-row-main{grid-template-columns:minmax(0,3fr) minmax(0,2fr);column-gap:var(--hcm-space-3);align-items:start}.agent-announcement-row-main>:where(h3),.agent-announcement-row-main>:is(.agent-announcement-owner,.agent-announcement-instruction){grid-column:1}.agent-announcement-row-main>dl.agent-announcement-facts{grid-column:2;grid-row:1/span 3}.agent-announcement-row-main>.agent-announcement-history{grid-column:2}}
.agent-announcement-row .button.danger{color:var(--hcm-color-danger);border-color:var(--hcm-color-danger);background:transparent}
.agent-announcement-row .button.danger:hover{background:color-mix(in srgb,var(--hcm-color-danger) 10%,transparent)}
.agent-announcement-history ul{margin-block-end:0}
@media(min-width:761px){
:root .primary-nav{scroll-snap-type:y proximity}
:root .primary-nav .nav-entry{scroll-snap-align:start}
:root .primary-nav::after{box-shadow:none;block-size:1px;background:var(--hcm-color-border,var(--line))}
}
`
