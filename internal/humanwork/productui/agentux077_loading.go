package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentUX077LoadingBody is what Agents, Agent setup and Agent operations draw
// while their data is read (AGENTUX-077). The frame is the loaded page's own:
// breadcrumb, title and the page's switches are on screen at once, so the page
// reads as the page it is, and only the region below them is placeholder rows,
// drawn by the same component the regions use for their own reads. Nothing here
// is a link a person can follow: the proxy is inert until the page replaces it.
func agentUX077LoadingBody(page PageID, locale LocaleContext) ui.Node {
	view := ApplyLocale(NewView(page, "", "", ""), locale)
	loading := func(shape AgentLoadingShape, rows int) ui.Node {
		return AgentLoadingFrame(AgentLoadingProps{Locale: locale, Shape: shape, Rows: rows, Status: locale.Text("shell.loading_authorized")})
	}
	props := ProductPageFrameProps{Class: "agent-page-frame", Dir: string(locale.Direction)}
	switch page {
	case PageAgentOperations:
		props.Breadcrumbs = agentUX077Breadcrumb(locale, agentOperationsText(locale, "title"))
		props.Title = agentOperationsText(locale, "title")
		props.Actions = []ui.Node{AgentPageNavigation(view, AgentPageOperations)}
		props.Body = []ui.Node{html.Div(html.Props{Class: "agent-operations-page"}, agentOperationsTabs(view, locale), loading(AgentLoadingRows, 4))}
	case PagePersonaAdmin:
		props.Breadcrumbs = personaAdminBreadcrumb(locale)
		props.Title = personaAdminText(locale, "title")
		props.Actions = []ui.Node{AgentPageNavigation(view, AgentPageSetup)}
		props.Body = []ui.Node{html.Div(html.Props{Class: "persona-admin-page"}, loading(AgentLoadingCards, 2))}
	default:
		props.Breadcrumbs = agentUXR7Breadcrumb(locale)
		props.Title = locale.Text("agents.page_title")
		props.Body = []ui.Node{html.Div(html.Props{Class: "agents-page"}, agentsIntroLine(view, locale), loading(AgentLoadingRows, 4))}
	}
	return ProductPageFrame(props)
}

// agentUX077LoadingStyles gives the placeholder bars enough contrast to read as
// loading rather than as an empty page: the platform's muted surface is close to
// the page's own.
const agentUX077LoadingStyles = `
.chatux037-bar{background:color-mix(in srgb,var(--hcm-color-text,#1f2328) 11%,var(--hcm-color-surface,transparent))}
`
