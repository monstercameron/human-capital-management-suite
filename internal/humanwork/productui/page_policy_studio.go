package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// policyStudioPage is the route adapter for the Policy
// Studio. The governed policy service is not published
// to this UI yet, so the surface keeps the journeys
// fallback contract: an honest empty state with a
// recovery link that authors nothing — policy truth
// stays server authority. The live studio composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func policyStudioPage(view View) ui.Node {
	if view.PolicyStudio != nil && view.PolicyStudio.Ready {
		return policyStudioLivePage(view, *view.PolicyStudio)
	}
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("policy_studio.unavailable_title"),
		Description: view.Locale.Text("policy_studio.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("policy_studio.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
