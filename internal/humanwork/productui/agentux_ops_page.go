package productui

import (
	"net/url"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageAgentOperations is the owner-only administration surface for running
// agents, version rollout, and portable agent files.
const PageAgentOperations PageID = "agent-operations"

type agentOperationsPageModuleRenderer struct{}

func (agentOperationsPageModuleRenderer) Render(view View) ui.Node {
	return BuildAgentOperationsPage(view)
}

func (agentOperationsPageModuleRenderer) PageFeatures() []FeatureDefinition {
	return []FeatureDefinition{
		feature("agent_owner_controls", "Running agent controls", "Pause, resume, or stop authorized agents", true, false, true, false),
		feature("agent_rollout", "Agent rollout", "Preview and advance authorized agent versions", true, true, true, false),
		feature("agent_portability", "Portable agent files", "Export or import authorized agent files", true, true, false, false),
	}
}

// BuildAgentOperationsPage uses the same server-owned administration signal
// as the Agents page's Manage agents link. A denied projection never receives
// browser mounts, so it cannot discover owner data through the APIs.
func BuildAgentOperationsPage(view View) ui.Node {
	locale := view.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	if view.AgentsProjection == nil || !view.AgentsProjection.ViewerIsAdmin {
		return agentOperationsDenied(view, locale)
	}
	selectedTab := agentOperationsSelectedTab(view.Query)
	return ProductPageFrame(ProductPageFrameProps{
		Class: "agent-page-frame", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "agent-operations-title"}, Raw: map[string]any{"data-agent-operations-state": "ready"},
		Breadcrumbs: personaAdminBreadcrumb(locale), Title: agentOperationsText(locale, "title"), TitleID: "agent-operations-title", Actions: []ui.Node{AgentPageNavigation(view, AgentPageOperations)},
		Body: []ui.Node{html.Div(html.Props{Class: "persona-admin-page agent-operations-page"},
			agentOperationsTabs(view, locale),
			AgentControlsMountForTab(locale, selectedTab == "running"),
			AgentRolloutPortableMountForTab(locale, AgentRolloutSnapshot{Loading: true}, AgentPortableSnapshot{}, selectedTab),
			AgentAnnouncementsMountForTab(locale, selectedTab == "announcements"),
		)},
	})
}

func agentOperationsSelectedTab(raw string) string {
	if query, err := url.ParseQuery(raw); err == nil && query.Get("tab") != "" {
		raw = query.Get("tab")
	}
	switch raw {
	case "rollout", "move", "announcements":
		return raw
	default:
		return "running"
	}
}

func agentOperationsPageNav(view View, locale LocaleContext) ui.Node {
	items := []struct {
		id, label string
		page      PageID
	}{
		{"ask", agentOperationsText(locale, "nav_ask"), PageAgents},
		{"setup", agentOperationsText(locale, "nav_setup"), PagePersonaAdmin},
		{"operations", agentOperationsText(locale, "nav_operations"), PageAgentOperations},
	}
	nodes := make([]ui.Node, 0, len(items))
	for _, item := range items {
		class := "agent-operations-page-nav-link"
		if item.id == "operations" {
			nodes = append(nodes, html.Span(html.Props{Class: class + " selected", Raw: map[string]any{"aria-current": "page"}}, ui.Text(item.label)))
			continue
		}
		nodes = append(nodes, ui.CreateElement(ActionLink, ActionLinkProps{Label: item.label, Href: statefulHref(view, item.page), Class: class, Navigate: view.Navigate}))
	}
	return html.Nav(html.Props{Class: "agent-operations-page-nav", Aria: map[string]string{"label": agentOperationsText(locale, "page_nav_label")}}, nodes...)
}

func agentOperationsTabs(view View, locale LocaleContext) ui.Node {
	tabs := []struct {
		id, label, short, panel string
	}{
		{"running", agentOperationsText(locale, "tab_running"), agentOperationsText(locale, "tab_running"), "agent-controls"},
		{"rollout", agentOperationsText(locale, "tab_rollout"), agentOperationsText(locale, "tab_rollout"), "agent-rollout-panel"},
		{"move", agentOperationsText(locale, "tab_portable"), agentOperationsText(locale, "tab_portable_short"), "agent-portable-panel"},
		{"announcements", agentOperationsText(locale, "tab_announcements"), agentOperationsText(locale, "tab_announcements"), "agent-announcements"},
	}
	items := make([]ui.Node, 0, len(tabs))
	selectedTab := agentOperationsSelectedTab(view.Query)
	for _, tab := range tabs {
		selected := tab.id == selectedTab
		items = append(items, html.A(html.Props{ID: "agent-operations-tab-" + tab.id, Href: statefulHref(view, PageAgentOperations, "tab", tab.id), Raw: map[string]any{
			"role": "tab", "aria-selected": map[bool]string{true: "true", false: "false"}[selected], "aria-controls": tab.panel,
			"tabindex": map[bool]string{true: "0", false: "-1"}[selected], "data-agent-operations-tab": tab.id,
		}}, html.Span(html.Props{Class: "agent-operations-tab-label-long"}, ui.Text(tab.label)), html.Span(html.Props{Class: "agent-operations-tab-label-short", Aria: map[string]string{"hidden": "true"}}, ui.Text(tab.short))))
	}
	return html.Nav(html.Props{Class: "agent-operations-tabs", Aria: map[string]string{"label": agentOperationsText(locale, "tabs_label")}, Raw: map[string]any{"role": "tablist"}}, items...)
}

func agentOperationsDenied(view View, locale LocaleContext) ui.Node {
	title, id := agentOperationsText(locale, "title"), "agent-operations-title"
	if view.Page == PagePersonaAdmin {
		title, id = personaAdminText(locale, "title"), "persona-admin-title"
	}
	return ProductPageFrame(ProductPageFrameProps{
		Class: "agent-page-frame", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": id}, Raw: map[string]any{"data-agent-operations-state": "denied"},
		Title: title, TitleID: id,
		Body: []ui.Node{html.Div(html.Props{Class: "persona-admin-page agent-operations-page"},
			html.Section(html.Props{Class: "surface empty-state persona-admin-unavailable", Role: "status", Raw: map[string]any{"aria-atomic": "true"}},
				html.H2(html.Props{}, ui.Text(agentOperationsText(locale, "denied_title"))),
				html.P(html.Props{Class: "muted"}, ui.Text(agentUXR7Text(locale, "owner_page"))),
				html.Div(html.Props{Class: "agent-operations-denied-actions"},
					html.A(html.Props{Class: "button primary", Href: Path(PageAgents)}, ui.Text(agentOperationsText(locale, "go_agents"))),
				),
			),
		)},
	})
}

func agentOperationsText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"title":              {"Agent operations", "Agentenbetrieb", "عمليات الوكلاء"},
		"page_nav_label":     {"Agent pages", "Agentenseiten", "صفحات الوكلاء"},
		"nav_ask":            {"Ask", "Fragen", "اسأل"},
		"nav_setup":          {"Setup", "Einrichtung", "الإعداد"},
		"nav_operations":     {"Operations", "Betrieb", "العمليات"},
		"denied_title":       {"Agent operations are unavailable", "Agentenbetrieb ist nicht verfügbar", "عمليات الوكلاء غير متاحة"},
		"denied_help":        {"You need agent owner permission to use these controls. Ask an administrator for access.", "Sie benötigen die Berechtigung als Agentenverantwortliche, um diese Steuerelemente zu verwenden. Bitten Sie eine Administration um Zugriff.", "تحتاج إلى صلاحية مالك الوكيل لاستخدام عناصر التحكم هذه. اطلب الوصول من مسؤول."},
		"go_agents":          {"Go to Agents", "Zu Agenten", "الانتقال إلى الوكلاء"},
		"back":               {"Back", "Zurück", "رجوع"},
		"tabs_label":         {"Agent operations sections", "Bereiche des Agentenbetriebs", "أقسام عمليات الوكلاء"},
		"tab_running":        {"Activity", "Aktivität", "النشاط"},
		"tab_rollout":        {"Rollout", "Versionswechsel", "الطرح"},
		"tab_portable":       {"Move between workspaces", "Zwischen Arbeitsbereichen verschieben", "النقل بين مساحات العمل"},
		"tab_portable_short": {"Move", "Verschieben", "نقل"},
		"tab_announcements":  {"Announcements", "Ankündigungen", "الإعلانات"},
	}
	values, ok := copy[key]
	if !ok {
		return ""
	}
	return values[agentRPLocaleIndex(locale)]
}
