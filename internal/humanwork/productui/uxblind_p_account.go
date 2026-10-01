package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
)

// accountMenu is the shell's account boundary. The trigger remains a compact
// avatar, while the disclosure gives the signed-in person a predictable place
// for identity, profile, settings, and sign-out destinations.
func accountMenu(view View) ui.Node {
	profile := view.Viewer
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = view.Locale.Text("common.not_reported")
	}
	if strings.TrimSpace(profile.Initials) == "" {
		profile.Initials = uicomponents.Initials(name)
	}
	companyName := strings.TrimSpace(view.Tenant)
	if companyName != "" {
		companyName, _ = HeaderBrandIdentity(NormalizeCustomerTheme(view.Appearance), companyName)
	}
	triggerLabel := view.Locale.Text("shell.account_menu")
	children := []ui.Node{
		html.H2(html.Props{ID: "account-menu-title"}, ui.Text(name)),
		html.P(html.Props{Class: "muted account-menu-company"}, ui.Text(valueOrUnavailableFor(view.Locale, companyName))),
	}
	items := make([]ui.Node, 0, 3)
	if navigationDestinationAuthorized(view, PageMyself) {
		items = append(items, html.Li(html.Props{}, appLink(view, html.Props{Class: "account-menu-link"}, statefulHref(view, PageMyself), ui.Text(view.Locale.Text("page.myself.label")))))
	}
	if navigationDestinationAuthorized(view, PageSettings) {
		items = append(items, html.Li(html.Props{}, appLink(view, html.Props{Class: "account-menu-link"}, statefulHref(view, PageSettings), ui.Text(view.Locale.Text("page.settings.label")))))
	}
	if href := strings.TrimSpace(view.LogoutHref); href != "" {
		items = append(items, html.Li(html.Props{}, softwareLink(view.Navigate, html.Props{Class: "account-menu-link account-menu-signout"}, href, ui.Text(view.Locale.Text("settings.signout_action")))))
	}
	if len(items) > 0 {
		children = append(children, html.Nav(html.Props{Aria: map[string]string{"label": triggerLabel}}, html.Ul(html.Props{Class: "account-menu-links"}, items...)))
	}
	return ui.CreateElement(TransientPopover, TransientPopoverProps{
		Kind: "account", Class: "account-menu network-slot network-slot-ready", TriggerClass: "viewer-profile-link",
		Label: triggerLabel, Title: triggerLabel,
		Trigger:    []ui.Node{personAvatar(name, profile.Initials, profile.PhotoURL, "viewer")},
		PanelClass: "popover account-popover", Children: children,
	})
}
