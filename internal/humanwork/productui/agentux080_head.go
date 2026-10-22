package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-080: the Agents page head. The Ask / Setup / Operations switch at the
// right of the title is the one navigation. "My agent access" is not a fourth
// place to go but a second view of Ask, so it is a quiet text link at the end of
// the intro line rather than a second tab strip that repeats the word "Ask".
// Inside the access view the same slot holds a way back.

func agentUX080Text(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"back_to_agents": {"Back to Agents", "Zurück zu Agenten", "العودة إلى الوكلاء"},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	return values[agentRPLocaleIndex(locale)]
}

// agentsIntroLine is the line under the page title on the Ask view: what the
// page does, with the link to the person's own agent access at its end.
func agentsIntroLine(view View, locale LocaleContext) ui.Node {
	return html.Div(html.Props{Class: "agents-view-line"},
		html.P(html.Props{Class: "agents-page-subtitle"}, ui.Text(locale.Text("agents.page_subtitle"))),
		html.Nav(html.Props{Class: "agents-view-link", Aria: map[string]string{"label": agentAccessText(locale, "tabs_label")}},
			ui.CreateElement(ActionLink, ActionLinkProps{Label: agentAccessText(locale, "tab_access"), Href: statefulHref(view, PageAgents, "view", "access"), Class: "agents-access-link", Navigate: view.Navigate})))
}

// agentsBackLine is the same slot inside the access view: one link back to the
// Ask view, never a sibling tab.
func agentsBackLine(view View, locale LocaleContext) ui.Node {
	return html.Div(html.Props{Class: "agents-view-line agents-view-line-back"},
		html.Nav(html.Props{Class: "agents-view-link", Aria: map[string]string{"label": agentAccessText(locale, "tabs_label")}},
			ui.CreateElement(ActionLink, ActionLinkProps{Label: agentUX080Text(locale, "back_to_agents"), Href: statefulHref(view, PageAgents), Class: "agents-access-link", Navigate: view.Navigate})))
}

// agentUX080Styles gives the Agents pages one link style and lets each button
// of the Ask form carry its explanation on one line when there is room.
//
// Links in the content of an agent page (people, conversations, documents, "Ask
// it a question") are the accent colour and underlined: the colour says it is a
// link and the underline keeps it one inside a sentence. The page's own
// controls, the breadcrumb and the task rows keep their own styles.
const agentUX080Styles = `
.agent-page-frame :is(.persona-admin-page,.agents-page,.agent-operations-page,.agent-access-page,.agent-admin-access-page) a:not(.button,.agents-page-nav-item,.agents-task-link,.agents-task-filter,.agents-manage-link,.agents-back-link,.persona-admin-instruction-chip,.agent-operations-tabs a){color:var(--hcm-color-brand-primary,var(--accent));text-decoration:underline;text-underline-offset:.15em;text-decoration-thickness:1px}
.agent-page-frame :is(.persona-admin-page,.agents-page,.agent-operations-page,.agent-access-page,.agent-admin-access-page) a:not(.button,.agents-page-nav-item,.agents-task-link,.agents-task-filter,.agents-manage-link,.agents-back-link,.persona-admin-instruction-chip,.agent-operations-tabs a):hover{text-decoration-thickness:2px}
.agents-view-line{display:flex;flex-wrap:wrap;align-items:baseline;justify-content:space-between;gap:.25rem 1rem;inline-size:100%}
.agents-view-line>.agents-page-subtitle{inline-size:auto;flex:0 1 auto;margin:0}
.agents-view-line-back{justify-content:flex-start}
.agents-view-link{flex:none}
.persona-admin-reactions{display:grid;gap:.25rem}
.persona-admin-reactions>small{display:block}
.agents-composer-action{flex:0 1 auto}
.agents-composer-action-hint{max-inline-size:none}
`
