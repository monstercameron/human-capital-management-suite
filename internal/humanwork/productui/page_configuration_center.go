package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// configurationCenterPage is the route adapter for the
// configuration center. The governed configuration
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that centers
// nothing — configuration truth stays server authority.
// The live center composition replaces this body once
// the governed service publishes; until then the UI will
// not simulate one.
func configurationCenterPage(view View) ui.Node {
	if view.ConfigurationCenter != nil && view.ConfigurationCenter.Ready {
		return configurationCenterLivePage(view, *view.ConfigurationCenter)
	}
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("configuration_center.unavailable_title"),
		Description: view.Locale.Text("configuration_center.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("configuration_center.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
