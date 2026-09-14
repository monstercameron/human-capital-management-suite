package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
)

func settingsPage(view View) ui.Node {
	profile := view.Viewer
	signedInAs := valueOrUnavailableFor(view.Locale, profile.Name)
	if profile.Name == "" {
		profile.Name = view.Locale.Text("common.not_reported")
	}
	if profile.Initials == "" {
		profile.Initials = uicomponents.Initials(profile.Name)
	}
	locale := localePreferencesProps(view)
	accessibility := AccessibilityPreferencesProps{
		I18nProps: I18nProps{Locale: view.Locale}, Value: view.Accessibility,
		TextSizes: AccessibilityTextSizeOptions(), Contrasts: AccessibilityContrastOptions(),
		Motions: AccessibilityMotionOptions(), LinkStyles: AccessibilityLinkOptions(),
		OnPreview: view.PreviewAccessibility, OnSave: view.SaveAccessibility, OnReset: view.ResetAccessibility,
	}
	profileProps := ViewerProfileProps{
		SectionLabel: view.Locale.Text("settings.profile_title"), Description: view.Locale.Text("settings.profile_description"),
		Name: profile.Name, Initials: profile.Initials, PhotoURL: profile.PhotoURL, Role: valueOrUnavailableFor(view.Locale, profile.Role),
	}
	security := AccessContextProps{
		Title:       view.Locale.Text("settings.session_title"),
		Description: view.Locale.Text("settings.access_context_description"),
		Facts: []FactProps{
			{Label: view.Locale.Text("settings.organization_fact"), Value: valueOrUnavailableFor(view.Locale, view.Tenant)},
			{Label: view.Locale.Text("settings.signed_in_as"), Value: signedInAs},
		},
		Callout: view.Locale.Text("settings.access_callout"),
	}
	props := SettingsTaskGroupsProps{
		Title: view.Locale.Text("settings.account_group_title"), AccountDescription: view.Locale.Text("settings.account_group_description"), OrganizationTitle: view.Locale.Text("settings.organization_group_title"), OrganizationDescription: view.Locale.Text("settings.organization_group_description"), PreferencesTitle: view.Locale.Text("settings.preferences_group_title"), Description: view.Locale.Text("settings.preferences_group_description"),
		SettingsLocale: view.Locale,
		Profile:        profileProps, Locale: &locale, Accessibility: &accessibility, Security: security,
		Notifications:      settingsNotifications(view.Locale),
		Navigation:         settingsNavigationProps(view),
		SignOutDescription: view.Locale.Text("settings.signout_description"),
	}
	if view.Allows(PageMyself, "view") {
		props.ProfileAction = &ActionLinkProps{Label: settingsProfileAction(view.Locale), Href: statefulHref(view, PageMyself), Class: "button secondary", Navigate: view.Navigate}
	}
	if view.Allows(PageAppearance, "view") {
		props.Appearance = &ActionLinkProps{Label: view.Locale.Text("admin.appearance_action"), Href: statefulHref(view, PageAppearance), Class: "button secondary", Navigate: view.Navigate}
		props.AppearanceLabel = view.Locale.Text("page.appearance.title")
	}
	if view.LogoutHref != "" {
		props.SignOut = &ActionLinkProps{Label: view.Locale.Text("settings.signout_action"), Href: view.LogoutHref, Class: "button danger settings-signout-action", Navigate: view.Navigate}
	}
	return ui.CreateElement(SettingsTaskGroups, props)
}
