package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentPageNavigationCurrent identifies the one non-clickable destination in
// the administrator's shared agent-page navigation.
type AgentPageNavigationCurrent string

const (
	AgentPageAsk        AgentPageNavigationCurrent = "ask"
	AgentPageSetup      AgentPageNavigationCurrent = "setup"
	AgentPageOperations AgentPageNavigationCurrent = "operations"
)

// AgentPageNavigation keeps the three administrator surfaces in one stable
// place and makes the current destination text instead of a misleading link.
func AgentPageNavigation(view View, current AgentPageNavigationCurrent) ui.Node {
	type destination struct {
		id    AgentPageNavigationCurrent
		label string
		href  string
	}
	destinations := []destination{
		{id: AgentPageAsk, label: view.Locale.Text("agents.nav.ask"), href: statefulHrefAtRoute(view, "/workspace/app/chat/agents")},
		{id: AgentPageSetup, label: view.Locale.Text("agents.nav.setup"), href: statefulHrefAtRoute(view, "/workspace/app/admin/personas")},
		{id: AgentPageOperations, label: view.Locale.Text("agents.nav.operations"), href: statefulHrefAtRoute(view, "/workspace/app/admin/agents")},
	}
	items := make([]ui.Node, 0, len(destinations))
	for _, item := range destinations {
		props := html.Props{Class: "agents-page-nav-item"}
		if item.id == current {
			props.Raw = map[string]any{"aria-current": "page"}
			items = append(items, html.Span(props, ui.Text(item.label)))
			continue
		}
		items = append(items, ui.CreateElement(ActionLink, ActionLinkProps{Label: item.label, Href: item.href, Class: props.Class, Navigate: view.Navigate}))
	}
	return html.Nav(html.Props{Class: "agents-page-nav", Aria: map[string]string{"label": view.Locale.Text("agents.admin_navigation")}}, items...)
}
