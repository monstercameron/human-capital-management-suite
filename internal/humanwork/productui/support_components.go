package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type HelpPageProps struct {
	Guidance QuickActionsProps
	Support  InformationalPanelProps
	// SearchDestination is the authorized knowledge-search entry point. It is
	// deliberately a destination rather than an article/result payload: Help
	// must not become a second, ungoverned knowledge index.
	SearchDestination *SupportDestinationProps
	// Escalation is copy explaining what to do when the governed service cannot
	// answer. It must not include case identifiers or imply that a request was
	// created.
	Escalation string
}

type InformationalPanelProps struct {
	Title        string
	Description  string
	Class        string
	Destinations []SupportDestinationProps
}

// SupportDestinationProps is a server-authorized route presented as a
// support option. It contains no support record or permission decision: the
// route adapter must provide only destinations already admitted for the view.
type SupportDestinationProps struct {
	Category    string
	Title       string
	Description string
	ActionLabel string
	Href        string
	Navigate    func(string)
}

type HelpHubPageProps struct {
	Title        string
	Destinations []SupportDestinationProps
	Empty        EmptyStateProps
}

type SupportRequestCategory struct {
	Value string
	Label string
}

type KnowledgeSearchPageProps struct {
	Query       string
	Action      string
	Placeholder string
	Label       string
	SubmitLabel string
	Unavailable EmptyStateProps
	Navigate    func(string)
}

type HRServiceRequestPageProps struct {
	Action              string
	CategoryLabel       string
	DetailsLabel        string
	CategoryPlaceholder string
	DetailsPlaceholder  string
	SubmitLabel         string
	SubmitDisabled      bool
	ServiceAvailable    bool
	Categories          []SupportRequestCategory
	Escalation          SupportDestinationProps
	Unavailable         string
}

type SettingsPageProps struct {
	Profile       ViewerProfileProps
	Access        AccessContextProps
	Locale        *LocalePreferencesProps
	Accessibility *AccessibilityPreferencesProps
	Preferences   EmptyStateProps
	SignOut       *ActionLinkProps
	// Group copy keeps account/security context distinct from personal
	// preferences without moving preference authority into the renderer.
	AccountGroupTitle           string
	AccountGroupDescription     string
	PreferencesGroupTitle       string
	PreferencesGroupDescription string
	SignOutDescription          string
}

type ViewerProfileProps struct {
	SectionLabel string
	Description  string
	Name         string
	Initials     string
	PhotoURL     string
	Role         string
}

type AccessContextProps struct {
	Title       string
	Description string
	Facts       []FactProps
	Callout     string
}

type StudioPageProps struct {
	Back  ActionLinkProps
	State EmptyStateProps
}

func HelpPage(props HelpPageProps) ui.Node {
	children := make([]ui.Node, 0, 4)
	if props.SearchDestination != nil {
		search := *props.SearchDestination
		children = append(children, html.Section(html.Props{Class: "surface help-search-destination", Aria: map[string]string{"labelledby": "help-search-title"}},
			html.H2(html.Props{ID: "help-search-title"}, ui.Text(search.Title)),
			html.P(html.Props{Class: "muted"}, ui.Text(search.Description)),
			softwareLink(search.Navigate, html.Props{Class: "button primary", Data: map[string]string{"hcm-help-action": "knowledge-search"}}, search.Href, ui.Text(search.ActionLabel)),
		))
	}
	if strings.TrimSpace(props.Escalation) != "" {
		children = append(children, html.P(html.Props{Class: "muted help-escalation", Raw: map[string]any{"role": "note"}}, ui.Text(props.Escalation)))
	}
	children = append(children,
		html.Div(html.Props{Class: "help-guidance-grid"}, ui.CreateElement(QuickActions, props.Guidance), ui.CreateElement(InformationalPanel, props.Support)),
	)
	return html.Div(html.Props{Class: "help-page insights-grid settings-accessibility-layout"}, children...)
}

func InformationalPanel(props InformationalPanelProps) ui.Node {
	children := []ui.Node{html.P(html.Props{Class: "muted"}, ui.Text(props.Description))}
	if len(props.Destinations) > 0 {
		children = append(children, supportDestinationList(props.Destinations))
	}
	body := html.Div(html.Props{Class: props.Class}, children...)
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: body})
}

func supportDestinationList(destinations []SupportDestinationProps) ui.Node {
	items := make([]ui.Node, 0, len(destinations))
	for _, destination := range destinations {
		body := []ui.Node{}
		if destination.Category != "" {
			body = append(body, html.Span(html.Props{Class: "eyebrow"}, ui.Text(destination.Category)))
		}
		body = append(body,
			html.Strong(html.Props{}, ui.Text(destination.Title)),
			html.Small(html.Props{}, ui.Text(destination.Description)),
		)
		if destination.Href != "" {
			body = append(body, softwareLink(destination.Navigate, html.Props{Class: "button secondary compact"}, destination.Href, ui.Text(destination.ActionLabel)))
		}
		items = append(items, html.Li(html.Props{Class: "support-destination"}, html.Div(html.Props{Class: "support-destination-copy"}, body...)))
	}
	return html.Ul(html.Props{Class: "quick-actions support-destinations", Raw: map[string]any{"role": "list"}}, items...)
}

func HelpHubPage(props HelpHubPageProps) ui.Node {
	if len(props.Destinations) == 0 {
		return ui.CreateElement(EmptyState, props.Empty)
	}
	return html.Div(html.Props{Class: "insights-grid settings-accessibility-layout"},
		ui.CreateElement(Panel, PanelProps{
			Title: props.Title,
			Body:  html.Div(html.Props{Class: "support-destination-grid"}, supportDestinationList(props.Destinations)),
		}),
	)
}

func KnowledgeSearchPage(props KnowledgeSearchPageProps) ui.Node {
	query := props.Query
	input := html.Props{ID: "knowledge-search-query", Name: "q", Value: query,
		Raw: map[string]any{"type": "search", "placeholder": props.Placeholder, "aria-label": props.Label}}
	form := html.Props{Class: "support-form support-search", Action: props.Action, Method: "get", Raw: map[string]any{"role": "search"}}
	if props.Navigate != nil {
		input.OnInput = ui.UseEvent(func(event ui.InputEvent) { query = event.GetValue() })
		navigate := props.Navigate
		action := props.Action
		form.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			if strings.TrimSpace(query) != "" {
				navigate(withExplicitQueryValue(action, []string{"q"}, strings.TrimSpace(query)))
			}
		})
	}
	return html.Div(html.Props{Class: "support-search-page"},
		html.Form(form,
			html.Label(html.Props{For: input.ID}, ui.Text(props.Label)),
			html.Div(html.Props{Class: "support-search-controls"}, html.Tag("input", input), html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(props.SubmitLabel))),
		),
		ui.CreateElement(EmptyState, props.Unavailable),
	)
}

func HRServiceRequestPage(props HRServiceRequestPageProps) ui.Node {
	categoryOptions := []ui.Node{html.Option(html.Props{Value: ""}, ui.Text(props.CategoryPlaceholder))}
	for _, option := range props.Categories {
		categoryOptions = append(categoryOptions, html.Option(html.Props{Value: option.Value}, ui.Text(option.Label)))
	}
	category := html.Select(html.Props{ID: "support-request-category", Name: "category", Raw: map[string]any{"aria-label": props.CategoryLabel}}, categoryOptions...)
	details := html.Tag("textarea", html.Props{ID: "support-request-details", Name: "details", Raw: map[string]any{"placeholder": props.DetailsPlaceholder, "aria-label": props.DetailsLabel}})
	// Request details must never travel in a query string or browser history.
	// The route is a progressive-enhancement target for the governed service;
	// until that service is available the status copy makes the boundary clear.
	form := html.Form(html.Props{Class: "support-request support-form", Action: props.Action, Method: "post"},
		html.Label(html.Props{For: "support-request-category"}, ui.Text(props.CategoryLabel)), category,
		html.Label(html.Props{For: "support-request-details"}, ui.Text(props.DetailsLabel)), details,
		html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: props.SubmitDisabled || !props.ServiceAvailable}, ui.Text(props.SubmitLabel)),
	)
	children := []ui.Node{form}
	if props.Unavailable != "" {
		children = append(children, html.P(html.Props{Class: "muted", Raw: map[string]any{"role": "status"}}, ui.Text(props.Unavailable)))
	}
	if props.Escalation.Href != "" {
		children = append(children, html.Div(html.Props{Class: "support-escalation"},
			html.Strong(html.Props{}, ui.Text(props.Escalation.Category)),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Escalation.Description)),
			softwareLink(props.Escalation.Navigate, html.Props{Class: "button secondary"}, props.Escalation.Href, ui.Text(props.Escalation.Title)),
		))
	}
	return html.Div(html.Props{Class: "support-request-page"}, children...)
}

func SettingsPage(props SettingsPageProps) ui.Node {
	preferencePanel := ui.CreateElement(EmptyState, props.Preferences)
	if props.Accessibility != nil {
		preferencePanel = ui.CreateElement(AccessibilityPreferencesPanel, *props.Accessibility)
	}
	accountChildren := []ui.Node{ui.CreateElement(AccessContext, props.Access)}
	if props.SignOut != nil {
		signOut := []ui.Node{ui.CreateElement(ActionLink, *props.SignOut)}
		if strings.TrimSpace(props.SignOutDescription) != "" {
			signOut = append([]ui.Node{html.P(html.Props{Class: "muted settings-signout-description"}, ui.Text(props.SignOutDescription))}, signOut...)
		}
		signOutChildren := append([]ui.Node{html.H3(html.Props{ID: "settings-signout-title"}, ui.Text(props.SignOut.Label))}, signOut...)
		accountChildren = append(accountChildren, html.Div(html.Props{Class: "surface settings-signout", Aria: map[string]string{"labelledby": "settings-signout-title"}}, signOutChildren...))
	}
	accountOverview := ui.Node(html.Div(html.Props{Class: "settings-group settings-account-group", Aria: map[string]string{"labelledby": "settings-account-group-title"}},
		html.H2(html.Props{ID: "settings-account-group-title"}, ui.Text(props.AccountGroupTitle)),
		html.P(html.Props{Class: "muted settings-group-description"}, ui.Text(props.AccountGroupDescription)),
		html.Div(html.Props{Class: "settings-group-content"}, accountChildren...),
	))
	if props.AccountGroupTitle == "" {
		accountOverview = html.Div(html.Props{}, accountChildren...)
	}
	overview := []ui.Node{accountOverview}
	personalChildren := make([]ui.Node, 0, 2)
	if props.Locale != nil {
		personalChildren = append(personalChildren, ui.CreateElement(LocalePreferencesPanel, *props.Locale))
	}
	personalChildren = append(personalChildren, preferencePanel)
	children := make([]ui.Node, 0, 4)
	if strings.TrimSpace(props.Profile.Name) != "" {
		children = append(children, ui.CreateElement(ViewerProfileCard, props.Profile))
	}
	if props.AccountGroupTitle == "" && props.PreferencesGroupTitle == "" {
		if props.Locale != nil {
			overview = append(overview, ui.CreateElement(LocalePreferencesPanel, *props.Locale))
		}
		children = append(children, html.Div(html.Props{Class: "settings-overview-grid"}, overview...), preferencePanel)
	} else {
		children = append(children, html.Div(html.Props{Class: "settings-group-stack"},
			html.Div(html.Props{Class: "settings-overview-grid"}, overview...),
			html.Div(html.Props{Class: "settings-group settings-preferences-group", Aria: map[string]string{"labelledby": "settings-preferences-group-title"}},
				html.H2(html.Props{ID: "settings-preferences-group-title"}, ui.Text(props.PreferencesGroupTitle)),
				html.P(html.Props{Class: "muted settings-group-description"}, ui.Text(props.PreferencesGroupDescription)),
				html.Div(html.Props{Class: "settings-group-content"}, personalChildren...),
			),
		))
	}
	return html.Div(html.Props{Class: "settings-page-stack", Data: map[string]string{"hcm-settings-scope": "personal"}}, children...)
}

func ViewerProfileCard(props ViewerProfileProps) ui.Node {
	return html.Section(html.Props{ID: "user-profile", Class: "surface viewer-profile-card", Aria: map[string]string{"labelledby": "user-profile-name"}},
		personAvatar(props.Name, props.Initials, props.PhotoURL, "profile viewer-profile-photo"),
		html.Div(html.Props{Class: "viewer-profile-copy"},
			html.Span(html.Props{Class: "eyebrow"}, ui.Text(props.SectionLabel)),
			html.H2(html.Props{ID: "user-profile-name"}, ui.Text(props.Name)),
			html.P(html.Props{}, ui.Text(props.Role)),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
		),
	)
}

func AccessContext(props AccessContextProps) ui.Node {
	return html.Aside(html.Props{Class: "settings-context", Data: map[string]string{"hcm-setting-group": "account-security"}, Aria: map[string]string{"labelledby": "settings-access-title"}},
		html.H3(html.Props{ID: "settings-access-title"}, ui.Text(props.Title)),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
		FactList(props.Facts),
		html.P(html.Props{Class: "callout"}, ui.Text(props.Callout)),
	)
}

func StudioPage(props StudioPageProps) ui.Node {
	return html.Div(html.Props{Class: "studio-page"},
		ui.CreateElement(ActionLink, props.Back),
		ui.CreateElement(EmptyState, props.State),
	)
}
