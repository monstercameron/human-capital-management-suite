package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// integrationOperationsPage is the route adapter for
// integration operations. The governed integration
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that operates
// nothing — operation truth stays server authority. The
// live operations composition replaces this body once
// the governed service publishes; until then the UI will
// not simulate one.
func integrationOperationsPage(view View) ui.Node {
	if view.IntegrationOperations != nil && view.IntegrationOperations.Ready {
		return integrationOperationsLivePage(view, *view.IntegrationOperations)
	}
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("integration_operations.unavailable_title"),
		Description: view.Locale.Text("integration_operations.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("integration_operations.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
