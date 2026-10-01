package productui

import (
	"context"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// UXBLIND-122: one availability projection decides whether the Agents
// destination is advertised and what its page shows. The server composes it
// from the tenant's agents setting, the viewer's administrative authority and
// the owner-scoped agent runtime read; the browser only renders it.

// AgentsAvailabilityProjection is the server answer for one viewer. Snapshot
// is meaningful only when Enabled; a disabled projection never carries agent
// data, whatever a payload claims.
type AgentsAvailabilityProjection struct {
	Enabled       bool
	ViewerIsAdmin bool
	// ReasonKey is a catalog key explaining a disabled state; SettingsHref is
	// the administrator's route to the setting that enables agents.
	ReasonKey    string
	SettingsHref string
	Snapshot     AgentSnapshot
}

// AgentsSurfaceState is the page state the projection resolves to.
type AgentsSurfaceState string

const (
	// AgentsSurfaceUnresolved is a component preview with no server answer.
	AgentsSurfaceUnresolved AgentsSurfaceState = "unresolved"
	// AgentsSurfaceEnabled renders the viewer's own agent snapshot.
	AgentsSurfaceEnabled AgentsSurfaceState = "enabled"
	// AgentsSurfaceDisabledAdmin explains the disabled state to an
	// administrator and links to the setting.
	AgentsSurfaceDisabledAdmin AgentsSurfaceState = "disabled_admin"
	// AgentsSurfaceDisabledHidden is a regular viewer on a disabled tenant:
	// the destination is not advertised and a direct visit shows a truthful
	// unavailable state without any agent data.
	AgentsSurfaceDisabledHidden AgentsSurfaceState = "disabled_hidden"
)

// AgentsReasonTenantDisabled is the only reason a disabled projection states.
const AgentsReasonTenantDisabled = "agents.reason.tenant_disabled"

// agentsSettingAnchor identifies the setting section on the Chat settings page.
const agentsSettingAnchor = "agents-setting"

// AgentsSettingsHref is the route of the setting that enables agents.
func AgentsSettingsHref() string { return pageHref(PageChatSettings) }

// AgentsSurface is the resolved decision shared by navigation and the page.
type AgentsSurface struct {
	State        AgentsSurfaceState
	NavVisible   bool
	ReasonKey    string
	SettingsHref string
}

// ResolveAgentsSurface is the single decision for both the navigation entry
// and the page state. A nil projection is a component preview and leaves the
// registry navigation untouched; every composed view carries a projection.
func ResolveAgentsSurface(projection *AgentsAvailabilityProjection) AgentsSurface {
	switch {
	case projection == nil:
		return AgentsSurface{State: AgentsSurfaceUnresolved, NavVisible: true}
	case projection.Enabled:
		return AgentsSurface{State: AgentsSurfaceEnabled, NavVisible: true}
	case projection.ViewerIsAdmin:
		return AgentsSurface{State: AgentsSurfaceDisabledAdmin, NavVisible: true, ReasonKey: AgentsReasonTenantDisabled, SettingsHref: AgentsSettingsHref()}
	default:
		return AgentsSurface{State: AgentsSurfaceDisabledHidden}
	}
}

// NormalizeAgentsAvailability bounds an untrusted payload to what the
// projection may express: a known reason, the canonical settings route, and
// no agent data unless agents are enabled.
func NormalizeAgentsAvailability(projection AgentsAvailabilityProjection) AgentsAvailabilityProjection {
	result := AgentsAvailabilityProjection{Enabled: projection.Enabled, ViewerIsAdmin: projection.ViewerIsAdmin}
	if projection.Enabled {
		result.Snapshot = projection.Snapshot
		if result.Snapshot.Threads == nil {
			result.Snapshot.Threads = []AgentThread{}
		}
		return result
	}
	if projection.ViewerIsAdmin {
		result.ReasonKey = AgentsReasonTenantDisabled
		result.SettingsHref = AgentsSettingsHref()
	}
	return result
}

// ApplyAgentsAvailability installs the projection and re-derives navigation
// from it, so the menu, global search and the page agree.
func ApplyAgentsAvailability(view View, projection AgentsAvailabilityProjection) View {
	normalized := NormalizeAgentsAvailability(projection)
	view.AgentsProjection = &normalized
	if view.NavigationProjection != nil {
		return ApplyNavigationProjection(view, *view.NavigationProjection)
	}
	if !ResolveAgentsSurface(view.AgentsProjection).NavVisible {
		view.Navigation = withoutAgentsNavItems(view.Navigation)
	}
	return view
}

// agentsNavigationFilter removes the Agents destination from an authorized
// navigation answer when the projection says it is not advertised.
func agentsNavigationFilter(view View, projection AuthorizedNavigationProjection) AuthorizedNavigationProjection {
	if ResolveAgentsSurface(view.AgentsProjection).NavVisible {
		return projection
	}
	projection.Items = withoutAgentsAuthorizedItems(projection.Items)
	projection.Support = withoutAgentsAuthorizedItems(projection.Support)
	return projection
}

func withoutAgentsAuthorizedItems(items []AuthorizedNavigationItem) []AuthorizedNavigationItem {
	result := make([]AuthorizedNavigationItem, 0, len(items))
	for _, item := range items {
		if item.Page == PageAgents {
			continue
		}
		if len(item.Children) > 0 {
			item.Children = withoutAgentsAuthorizedItems(item.Children)
			// A group whose only remaining child is its own overview is a
			// plain destination again.
			if len(item.Children) == 1 && item.Children[0].Page == item.Page {
				item.Children = nil
			}
		}
		result = append(result, item)
	}
	return result
}

func withoutAgentsNavItems(items []NavItem) []NavItem {
	result := make([]NavItem, 0, len(items))
	for _, item := range items {
		if item.Page == PageAgents {
			continue
		}
		if len(item.Children) > 0 {
			item.Children = withoutAgentsNavItems(item.Children)
			if len(item.Children) == 1 && item.Children[0].Page == item.Page {
				item.Children = nil
			}
		}
		result = append(result, item)
	}
	return result
}

// projectedAgentClient serves the snapshot the server already composed for
// this viewer; it never reads or invents anything else.
type projectedAgentClient struct{ snapshot AgentSnapshot }

func (client projectedAgentClient) Snapshot(context.Context, AgentSnapshotRequest) (AgentSnapshot, error) {
	return client.snapshot, nil
}

// BuildAgentsSurface renders the Agents route from the availability
// projection.
func BuildAgentsSurface(view View) ui.Node {
	surface := ResolveAgentsSurface(view.AgentsProjection)
	switch surface.State {
	case AgentsSurfaceEnabled:
		return BuildAgentsPage(view, projectedAgentClient{snapshot: view.AgentsProjection.Snapshot})
	case AgentsSurfaceDisabledAdmin, AgentsSurfaceDisabledHidden:
		return agentsDisabledPage(view, surface)
	default:
		return BuildAgentsPage(view, nil)
	}
}

func agentsDisabledPage(view View, surface AgentsSurface) ui.Node {
	locale := view.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	state := EmptyStateProps{Role: "status", Class: "agents-disabled"}
	if surface.State == AgentsSurfaceDisabledAdmin {
		state.Title = locale.Text("agents.disabled_admin_title")
		state.Description = locale.Text(surface.ReasonKey)
		state.Badge = locale.Text("agents.admin_badge")
		state.Tone = "info"
		state.Action = &ActionLinkProps{Label: locale.Text("agents.open_setting"), Href: surface.SettingsHref, Class: "button primary", Navigate: view.Navigate}
	} else {
		state.Title = locale.Text("agents.disabled_title")
		state.Description = locale.Text("agents.disabled_detail")
		state.Action = &ActionLinkProps{Label: locale.Text("agents.back_to_chat"), Href: pageHref(PageChat), Class: "button secondary", Navigate: view.Navigate}
	}
	return html.Section(html.Props{Class: "agents-page agents-page-disabled", Aria: map[string]string{"labelledby": "agents-page-title"}, Raw: map[string]any{"data-agents-surface": string(surface.State)}},
		html.H1(html.Props{ID: "agents-page-title"}, ui.Text(locale.Text("agents.page_title"))),
		html.P(html.Props{Class: "agents-page-subtitle"}, ui.Text(locale.Text("agents.page_subtitle"))),
		ui.CreateElement(EmptyState, state),
	)
}

// chatSettingsWithAgents adds the tenant agents setting to the Chat settings
// page for administrators. The page itself stays unchanged for everyone else.
func chatSettingsWithAgents(view View) ui.Node {
	page := chatSettingsPage(view)
	if view.AgentsProjection == nil || !view.AgentsProjection.ViewerIsAdmin || (view.Roles != nil && !PageVisible(PageChatSettings, view.Roles)) {
		return page
	}
	return html.Div(html.Props{Class: "chat-settings-page chat-settings-stack"}, page, ui.CreateElement(AgentsSetting, AgentsSettingProps{
		I18nProps: I18nProps{Locale: view.Locale}, Enabled: view.AgentsProjection.Enabled, OnSave: view.SetAgentsEnabled,
	}))
}

// AgentsSettingProps drive the administrator's agents on/off control.
type AgentsSettingProps struct {
	I18nProps
	Enabled bool
	// OnSave persists the new value; nil renders the control read-only.
	OnSave func(enabled bool, done func(error))
}

type agentsSettingDraft struct {
	Saving bool
	Failed bool
}

// AgentsSetting is the one control that turns agents on or off for the
// tenant. The server authorizes the write again; this only presents it.
func AgentsSetting(props AgentsSettingProps) ui.Node {
	locale := props.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	state := ui.UseState(agentsSettingDraft{})
	draft := state.Get()
	status := locale.Text("agents.setting_off")
	actionKey := "agents.setting_turn_on"
	if props.Enabled {
		status = locale.Text("agents.setting_on")
		actionKey = "agents.setting_turn_off"
	}
	toggle := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnSave == nil || state.Get().Saving {
			return
		}
		state.Set(agentsSettingDraft{Saving: true})
		props.OnSave(!props.Enabled, func(err error) {
			state.Set(agentsSettingDraft{Failed: err != nil})
		})
	})
	label := locale.Text(actionKey)
	if draft.Saving {
		label = locale.Text("agents.setting_saving")
	}
	buttonClass := "button primary"
	if props.Enabled {
		buttonClass = "button secondary"
	}
	children := []ui.Node{
		html.Header(html.Props{}, html.H2(html.Props{ID: "agents-setting-title"}, ui.Text(locale.Text("agents.setting_title"))), html.P(html.Props{Class: "muted"}, ui.Text(locale.Text("agents.setting_intro")))),
		html.P(html.Props{Class: "chat-retention-current agents-setting-current", Raw: map[string]any{"aria-live": "polite", "data-agents-enabled": map[bool]string{true: "true", false: "false"}[props.Enabled]}}, ui.Text(status)),
		html.Div(html.Props{Class: "agents-setting-actions"}, html.Button(html.Props{ID: "agents-setting-toggle", Class: buttonClass, Type: "button", Disabled: props.OnSave == nil || draft.Saving, OnClick: toggle, Raw: map[string]any{"aria-describedby": "agents-setting-title"}}, ui.Text(label))),
	}
	if props.OnSave == nil {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(locale.Text("agents.setting_unavailable"))))
	}
	if draft.Failed {
		children = append(children, html.Div(html.Props{Class: "notice error", Role: "alert"}, html.P(html.Props{}, ui.Text(locale.Text("agents.setting_error")))))
	}
	return html.Section(html.Props{ID: agentsSettingAnchor, Class: "surface chat-retention-settings agents-setting", Aria: map[string]string{"labelledby": "agents-setting-title"}}, children...)
}

var agentsAvailabilityCopy = map[string]map[string]string{
	DefaultProductLocale: {
		"agents.disabled_admin_title": "Agents are turned off for your organization",
		AgentsReasonTenantDisabled:    "An administrator has not turned on agents for this organization, so people cannot see or use this page. Turn agents on in Chat settings.",
		"agents.admin_badge":          "Only administrators see this page",
		"agents.open_setting":         "Open agent settings",
		"agents.disabled_title":       "Agents are not available",
		"agents.disabled_detail":      "Your organization has not turned on agents. Ask an administrator if you need them.",
		"agents.back_to_chat":         "Back to Chat",
		"agents.setting_title":        "Agents",
		"agents.setting_intro":        "Let people in this organization start agents that work on their behalf from Chat. Agents only use the access each person already has.",
		"agents.setting_on":           "Agents are on for this organization.",
		"agents.setting_off":          "Agents are off for this organization. People do not see Agents in the menu.",
		"agents.setting_turn_on":      "Turn on agents",
		"agents.setting_turn_off":     "Turn off agents",
		"agents.setting_saving":       "Saving…",
		"agents.setting_error":        "The agent setting could not be saved. Try again.",
		"agents.setting_unavailable":  "This setting cannot be changed from this session.",
	},
	"de-DE": {
		"agents.disabled_admin_title": "Agenten sind für Ihre Organisation ausgeschaltet",
		AgentsReasonTenantDisabled:    "Ein Administrator hat Agenten für diese Organisation nicht eingeschaltet, daher können Mitarbeitende diese Seite weder sehen noch nutzen. Schalten Sie Agenten in den Chat-Einstellungen ein.",
		"agents.admin_badge":          "Nur Administratoren sehen diese Seite",
		"agents.open_setting":         "Agenteneinstellungen öffnen",
		"agents.disabled_title":       "Agenten sind nicht verfügbar",
		"agents.disabled_detail":      "Ihre Organisation hat Agenten nicht eingeschaltet. Wenden Sie sich an einen Administrator, wenn Sie sie benötigen.",
		"agents.back_to_chat":         "Zurück zum Chat",
		"agents.setting_title":        "Agenten",
		"agents.setting_intro":        "Erlauben Sie Personen in dieser Organisation, im Chat Agenten zu starten, die in ihrem Auftrag arbeiten. Agenten nutzen nur die Berechtigungen, die die jeweilige Person bereits hat.",
		"agents.setting_on":           "Agenten sind für diese Organisation eingeschaltet.",
		"agents.setting_off":          "Agenten sind für diese Organisation ausgeschaltet. Personen sehen Agenten nicht im Menü.",
		"agents.setting_turn_on":      "Agenten einschalten",
		"agents.setting_turn_off":     "Agenten ausschalten",
		"agents.setting_saving":       "Wird gespeichert…",
		"agents.setting_error":        "Die Agenteneinstellung konnte nicht gespeichert werden. Versuchen Sie es erneut.",
		"agents.setting_unavailable":  "Diese Einstellung kann in dieser Sitzung nicht geändert werden.",
	},
	"ar": {
		"agents.disabled_admin_title": "الوكلاء متوقفون لمؤسستك",
		AgentsReasonTenantDisabled:    "لم يفعّل أي مسؤول الوكلاء لهذه المؤسسة، لذلك لا يمكن للموظفين رؤية هذه الصفحة أو استخدامها. فعّل الوكلاء من إعدادات الدردشة.",
		"agents.admin_badge":          "يرى المسؤولون فقط هذه الصفحة",
		"agents.open_setting":         "فتح إعدادات الوكلاء",
		"agents.disabled_title":       "الوكلاء غير متاحين",
		"agents.disabled_detail":      "لم تفعّل مؤسستك الوكلاء. اطلب ذلك من أحد المسؤولين إذا كنت بحاجة إليهم.",
		"agents.back_to_chat":         "العودة إلى الدردشة",
		"agents.setting_title":        "الوكلاء",
		"agents.setting_intro":        "اسمح للأشخاص في هذه المؤسسة ببدء وكلاء يعملون نيابةً عنهم من الدردشة. لا يستخدم الوكلاء إلا الصلاحيات التي يملكها كل شخص حاليًا.",
		"agents.setting_on":           "الوكلاء مفعّلون لهذه المؤسسة.",
		"agents.setting_off":          "الوكلاء متوقفون لهذه المؤسسة. لا يرى الأشخاص الوكلاء في القائمة.",
		"agents.setting_turn_on":      "تفعيل الوكلاء",
		"agents.setting_turn_off":     "إيقاف الوكلاء",
		"agents.setting_saving":       "جارٍ الحفظ…",
		"agents.setting_error":        "تعذّر حفظ إعداد الوكلاء. حاول مرة أخرى.",
		"agents.setting_unavailable":  "لا يمكن تغيير هذا الإعداد من هذه الجلسة.",
	},
}

// agentsAvailabilityMessages is the English catalog for this surface.
func agentsAvailabilityMessages() map[string]localize.Message {
	result := make(map[string]localize.Message, len(agentsAvailabilityCopy[DefaultProductLocale]))
	for key, text := range agentsAvailabilityCopy[DefaultProductLocale] {
		result[key] = localize.Message{Text: text}
	}
	return result
}

// agentsAvailabilityTranslations is the reviewed German and Arabic copy.
func agentsAvailabilityTranslations(locale string) map[string]string {
	if strings.HasPrefix(locale, "ar") {
		return agentsAvailabilityCopy["ar"]
	}
	if locale == "de-DE" {
		return agentsAvailabilityCopy["de-DE"]
	}
	return nil
}
