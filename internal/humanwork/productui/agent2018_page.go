package productui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Where the access, console and cost pieces sit in the Agents area:
//   - "My agent access" is the Agents page's second view (?view=access), for
//     every signed-in person.
//   - The administrator's connection console and the owner's cost figures are
//     two more tabs of Agent operations (?tab=connections and ?tab=cost).
//   - Each agent's spend limits sit on its Agent setup card.
//
// Each piece is a placeholder the page's script fills from the server; until it
// does, and when it cannot, the placeholder says so.

// AgentAccessViewRequested reports whether the Agents page was asked for the
// person's own access view.
func AgentAccessViewRequested(rawQuery string) bool {
	values, err := url.ParseQuery(strings.TrimPrefix(rawQuery, "?"))
	return err == nil && values.Get("view") == "access"
}

func agentAccessContainer(embedded bool, props html.Props, children ...ui.Node) ui.Node {
	if embedded {
		return html.Div(props, children...)
	}
	return html.Main(props, children...)
}

func agentAccessHeading(embedded bool, props html.Props, text string) ui.Node {
	if embedded {
		return html.H2(props, ui.Text(text))
	}
	return html.H1(props, ui.Text(text))
}

// agentsViewTabs is the Ask / My agent access switch every person sees on the
// Agents page. The current view is text, not a link.
func agentsViewTabs(view View, locale LocaleContext, current string) ui.Node {
	items := make([]ui.Node, 0, 2)
	for _, item := range []struct{ id, key, href string }{
		{"ask", "tab_ask", statefulHref(view, PageAgents)},
		{"access", "tab_access", statefulHref(view, PageAgents, "view", "access")},
	} {
		label := agentAccessText(locale, item.key)
		if item.id == current {
			items = append(items, html.Span(html.Props{Class: "agents-view-tab", Raw: map[string]any{"aria-current": "page"}}, ui.Text(label)))
			continue
		}
		items = append(items, ui.CreateElement(ActionLink, ActionLinkProps{Label: label, Href: item.href, Class: "agents-view-tab", Navigate: view.Navigate}))
	}
	return html.Nav(html.Props{Class: "agents-view-tabs", Aria: map[string]string{"label": agentAccessText(locale, "tabs_label")}}, items...)
}

// BuildAgentAccessPage is the Agents page's access view: the frame, the view
// switch and the placeholder the script fills with the person's own access.
func BuildAgentAccessPage(view View) ui.Node {
	locale := view.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	actions := []ui.Node(nil)
	if view.AgentsProjection != nil && view.AgentsProjection.ViewerIsAdmin {
		actions = append(actions, AgentPageNavigation(view, AgentPageAsk))
	}
	return ProductPageFrame(ProductPageFrameProps{
		Class: "agent-page-frame", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "agents-page-title"}, Raw: map[string]any{"data-agent-access-view": "true"},
		Breadcrumbs: agentUXR7Breadcrumb(locale), Title: locale.Text("agents.page_title"), TitleID: "agents-page-title", Actions: actions,
		Body: []ui.Node{html.Div(html.Props{Class: "agents-page agent-access-view"}, agentsViewTabs(view, locale, "access"), AgentAccessMount(locale))},
	})
}

// AgentAccessMount is the placeholder of the person's own access page.
func AgentAccessMount(locale LocaleContext) ui.Node {
	return html.Section(html.Props{ID: "agent-access-mount", Class: "agent-access-mount", Dir: string(locale.Direction), Raw: map[string]any{"data-locale": locale.Resolved}, Aria: map[string]string{"labelledby": "agent-access-title", "busy": "true"}},
		html.H2(html.Props{ID: "agent-access-title"}, ui.Text(agentAccessText(locale, "title"))),
		html.P(html.Props{Role: "status", Class: "muted", Aria: map[string]string{"live": "polite"}}, ui.Text(agentAccessText(locale, "loading"))),
	)
}

// AgentAdminAccessMount is the placeholder of the connection console, a tab
// panel of Agent operations.
func AgentAdminAccessMount(locale LocaleContext, active bool) ui.Node {
	return html.Section(html.Props{ID: "agent-admin-access", Class: "persona-admin-editor agent-operations-region", Dir: string(locale.Direction), Hidden: !active,
		Raw:  map[string]any{"role": "tabpanel", "data-locale": locale.Resolved, "data-agent-operations-panel": "connections"},
		Aria: map[string]string{"labelledby": "agent-operations-tab-connections", "busy": "true"}},
		html.H2(html.Props{ID: "agent-admin-access-heading"}, ui.Text(agentAdminText(locale, "title"))),
		html.P(html.Props{Role: "status", Class: "muted", Aria: map[string]string{"live": "polite"}}, ui.Text(agentAdminText(locale, "loading"))),
	)
}

// AgentCostMount is the placeholder of the owner's cost figures and limits, a
// tab panel of Agent operations.
func AgentCostMount(locale LocaleContext, active bool) ui.Node {
	return html.Section(html.Props{ID: "agent-cost", Class: "persona-admin-editor agent-operations-region", Dir: string(locale.Direction), Hidden: !active,
		Raw:  map[string]any{"role": "tabpanel", "data-locale": locale.Resolved, "data-agent-operations-panel": "cost"},
		Aria: map[string]string{"labelledby": "agent-operations-tab-cost", "busy": "true"}},
		html.H2(html.Props{ID: "agent-cost-heading"}, ui.Text(agentCostText(locale, "cost_title"))),
		html.P(html.Props{Role: "status", Class: "muted", Aria: map[string]string{"live": "polite"}}, ui.Text(agentCostText(locale, "loading"))),
	)
}

// AgentSpendLimitsMount is the placeholder of one agent's spend limits card on
// its Agent setup card. It replaces the old "No limits set" sentence: the card
// shows the real limit, or says there is none, and lets the owner set one.
func AgentSpendLimitsMount(locale LocaleContext, agentID, agentName string) ui.Node {
	return html.Div(html.Props{Class: "agent-spend-mount", Dir: string(locale.Direction), Raw: map[string]any{"data-agent-spend-mount": agentID, "data-agent-name": agentName, "data-locale": locale.Resolved}},
		html.P(html.Props{Role: "status", Class: "muted", Aria: map[string]string{"live": "polite"}}, ui.Text(agentCostText(locale, "loading_limits"))))
}

// AgentSpendLimitsFailed is what an agent's limits card says when its limits
// could not be read: a sentence, and a Try again that reads them again.
func AgentSpendLimitsFailed(locale LocaleContext) ui.Node {
	return html.Div(html.Props{Class: "agent-spend-failed", Role: "alert"},
		html.P(html.Props{Class: "muted"}, ui.Text(agentCostText(locale, "limits_failed"))),
		html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"data-agent-spend-action": "retry"}}, ui.Text(agentCostText(locale, "try_again"))))
}
