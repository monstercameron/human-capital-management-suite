package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// SettingsTaskGroupsProps is deliberately presentation-only. Each destination
// is supplied by the authorized route projection; unavailable controls remain
// visibly unavailable instead of pretending to persist a preference.
type SettingsTaskGroupsProps struct {
	Profile                 ViewerProfileProps
	ProfileAction           *ActionLinkProps
	ProfileActionLabel      string
	Locale                  *LocalePreferencesProps
	Accessibility           *AccessibilityPreferencesProps
	Notifications           SettingsUnavailableProps
	Navigation              SettingsNavigationProps
	Security                AccessContextProps
	SignOut                 *ActionLinkProps
	SignOutDescription      string
	Appearance              *ActionLinkProps
	AppearanceLabel         string
	Title                   string
	AccountDescription      string
	OrganizationTitle       string
	OrganizationDescription string
	PreferencesTitle        string
	Description             string
	SettingsLocale          LocaleContext
}

type SettingsUnavailableProps struct {
	Title, Description string
}

type SettingsNavigationProps struct {
	Title, Description, ExpandedLabel, CollapsedLabel, CurrentLabel string
	ExpandedHref, CollapsedHref                                     string
	Navigate                                                        func(string)
	Collapsed                                                       bool
}

func settingsNotifications(locale LocaleContext) SettingsUnavailableProps {
	switch locale.normalized().Resolved {
	case "de-DE":
		return SettingsUnavailableProps{"Benachrichtigungen", "Benachrichtigungseinstellungen sind für dieses Konto noch nicht verfügbar."}
	case "ar":
		return SettingsUnavailableProps{"الإشعارات", "إعدادات الإشعارات غير متاحة لهذا الحساب بعد."}
	default:
		return SettingsUnavailableProps{"Notifications", "Notification settings are not available for this account yet."}
	}
}

func settingsNavigationProps(view View) SettingsNavigationProps {
	switch view.Locale.normalized().Resolved {
	case "de-DE":
		return SettingsNavigationProps{"Navigation", "Wählen Sie, ob die Hauptnavigation erweitert oder kompakt geöffnet wird.", "Erweitert", "Kompakt", "Aktuell", currentPageHref(view, false), currentPageHref(view, true), view.Navigate, view.NavCollapsed}
	case "ar":
		return SettingsNavigationProps{"التنقل", "اختر ما إذا كانت القائمة الرئيسية موسعة أو مضغوطة عند فتحها.", "موسعة", "مضغوطة", "الحالية", currentPageHref(view, false), currentPageHref(view, true), view.Navigate, view.NavCollapsed}
	default:
		return SettingsNavigationProps{"Navigation", "Choose whether the main navigation opens expanded or compact.", "Expanded", "Compact", "Current", currentPageHref(view, false), currentPageHref(view, true), view.Navigate, view.NavCollapsed}
	}
}

func settingsAppearanceBoundary(locale LocaleContext) string {
	switch locale.normalized().Resolved {
	case "de-DE":
		return "Die Darstellung gehört zur Organisation und wird von autorisierten Administrierenden verwaltet."
	case "ar":
		return "المظهر تابع للمؤسسة ويديره المسؤولون المصرح لهم."
	default:
		return "Appearance belongs to the organization and is managed by authorized administrators."
	}
}

func settingsProfileAction(locale LocaleContext) string {
	switch locale.normalized().Resolved {
	case "de-DE":
		return "Beschäftigtenprofil öffnen"
	case "ar":
		return "فتح ملف الموظف"
	default:
		return "Open employee profile"
	}
}

func SettingsTaskGroups(props SettingsTaskGroupsProps) ui.Node {
	profile := ui.Node(ui.CreateElement(ViewerProfileCard, props.Profile))
	if props.ProfileAction != nil {
		profile = html.Section(html.Props{ID: "user-profile", Class: "surface settings-task-card viewer-profile-card", Data: map[string]string{"hcm-setting-group": "profile"}, Raw: map[string]any{"aria-labelledby": "user-profile-name"}},
			// Keep the canonical profile anchors on the actionable composition so
			// consumers can address the viewer profile regardless of authorization.
			html.Div(html.Props{Class: "settings-profile-content"},
				html.Div(html.Props{Class: "settings-task-card-head"},
					html.H2(html.Props{ID: "settings-profile-title"}, ui.Text(props.Profile.SectionLabel)),
					html.P(html.Props{Class: "muted"}, ui.Text(props.Profile.Description))),
				html.Div(html.Props{Class: "settings-task-card-body settings-profile-identity"},
					personAvatar(props.Profile.Name, props.Profile.Initials, props.Profile.PhotoURL, "profile viewer-profile-photo"),
					html.Div(html.Props{Class: "viewer-profile-copy"}, html.Strong(html.Props{ID: "user-profile-name"}, ui.Text(props.Profile.Name)), html.P(html.Props{}, ui.Text(props.Profile.Role)))),
			),
			ui.CreateElement(ActionLink, ActionLinkProps{Label: props.ProfileAction.Label, Href: props.ProfileAction.Href, Class: props.ProfileAction.Class + " settings-profile-action", Navigate: props.ProfileAction.Navigate}),
		)
	}
	preferences := make([]ui.Node, 0, 5)
	if props.Locale != nil {
		preferences = append(preferences, ui.CreateElement(LocalePreferencesPanel, *props.Locale))
	}
	if props.Accessibility != nil {
		preferences = append(preferences, ui.CreateElement(AccessibilityPreferencesPanel, *props.Accessibility))
	}
	preferences = append(preferences, settingsUnavailable(props.Notifications, "notifications"), settingsNavigation(props.Navigation))
	account := []ui.Node{ui.CreateElement(AccessContext, props.Security)}
	var organization ui.Node
	if props.Appearance != nil {
		appearance := html.Section(html.Props{Class: "surface settings-task-card", Data: map[string]string{"hcm-setting-group": "tenant-appearance"}, Raw: map[string]any{"aria-labelledby": "settings-appearance-title"}},
			html.H3(html.Props{ID: "settings-appearance-title"}, ui.Text(props.AppearanceLabel)),
			html.P(html.Props{Class: "muted"}, ui.Text(settingsAppearanceBoundary(props.SettingsLocale))),
			ui.CreateElement(ActionLink, *props.Appearance))
		organization = html.Div(html.Props{Class: "settings-group settings-organization-group", Data: map[string]string{"hcm-setting-group": "organization-configuration"}},
			html.H2(html.Props{}, ui.Text(props.OrganizationTitle)),
			html.P(html.Props{Class: "muted settings-group-description"}, ui.Text(props.OrganizationDescription)),
			html.Div(html.Props{Class: "settings-group-content"}, appearance))
	}
	if props.SignOut != nil {
		children := []ui.Node{html.H3(html.Props{ID: "settings-signout-title"}, ui.Text(props.SignOut.Label))}
		if strings.TrimSpace(props.SignOutDescription) != "" {
			children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(props.SignOutDescription)))
		}
		children = append(children, ui.CreateElement(ActionLink, *props.SignOut))
		account = append(account, html.Section(html.Props{Class: "surface settings-signout", Data: map[string]string{"hcm-setting-group": "signout"}, Raw: map[string]any{"aria-labelledby": "settings-signout-title"}}, children...))
	}
	accountGroup := []ui.Node{
		html.H2(html.Props{}, ui.Text(props.Title)),
		html.P(html.Props{Class: "muted settings-group-description"}, ui.Text(props.AccountDescription)),
		html.Div(html.Props{Class: "settings-group-content"}, account...),
	}
	preferencesGroup := []ui.Node{
		html.H2(html.Props{}, ui.Text(props.PreferencesTitle)),
		html.P(html.Props{Class: "muted settings-group-description"}, ui.Text(props.Description)),
		html.Div(html.Props{Class: "settings-group-content"}, preferences...),
	}
	return html.Div(html.Props{Class: "settings-page-stack", Data: map[string]string{"hcm-settings-scope": "personal"}},
		profile,
		html.Div(html.Props{Class: "settings-task-groups"},
			html.Div(html.Props{Class: "settings-overview-grid"},
				html.Div(html.Props{Class: "settings-group settings-account-group", Data: map[string]string{"hcm-setting-group": "account-security"}}, accountGroup...),
				organization,
			),
			html.Div(html.Props{Class: "settings-group settings-preferences-group", Data: map[string]string{"hcm-setting-group": "personal-preferences"}}, preferencesGroup...),
		),
	)
}

func settingsUnavailable(props SettingsUnavailableProps, group string) ui.Node {
	return html.Section(html.Props{Class: "surface settings-task-card settings-unavailable", Data: map[string]string{"hcm-setting-group": group}, Raw: map[string]any{"aria-labelledby": "settings-" + group + "-title"}},
		html.H3(html.Props{ID: "settings-" + group + "-title"}, ui.Text(props.Title)), html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
	)
}

func settingsNavigation(props SettingsNavigationProps) ui.Node {
	choices := []ui.Node{settingsNavigationChoice(props.ExpandedLabel, props.ExpandedHref, !props.Collapsed, props)}
	choices = append(choices, settingsNavigationChoice(props.CollapsedLabel, props.CollapsedHref, props.Collapsed, props))
	return html.Section(html.Props{Class: "surface settings-task-card", Data: map[string]string{"hcm-setting-group": "navigation"}, Raw: map[string]any{"aria-labelledby": "settings-navigation-title"}},
		html.H3(html.Props{ID: "settings-navigation-title"}, ui.Text(props.Title)), html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
		html.Div(html.Props{Class: "settings-navigation-choices", Raw: map[string]any{"role": "group", "aria-label": props.Title}}, choices...))
}

func settingsNavigationChoice(label, href string, current bool, props SettingsNavigationProps) ui.Node {
	linkProps := html.Props{Class: "button secondary"}
	content := []ui.Node{ui.Text(label)}
	if current {
		linkProps.Class += " current"
		linkProps.Aria = map[string]string{"current": "true"}
		content = append(content, html.Span(html.Props{Class: "settings-navigation-current"}, ui.Text(props.CurrentLabel)))
	}
	return softwareLink(props.Navigate, linkProps, href, content...)
}
