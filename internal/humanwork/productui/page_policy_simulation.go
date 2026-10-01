package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// policySimulationPage is the route adapter for
// authorization-policy simulation. The governed policy
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that simulates
// nothing — simulation truth stays server authority.
// The live simulation composition replaces this body
// once the governed service publishes; until then the UI
// will not simulate one.
func policySimulationPage(view View) ui.Node {
	if view.PolicySimulation != nil && !signedOutState(view) && strings.TrimSpace(view.PolicySimulation.Subject) != "" {
		return policySimulationLivePage(view)
	}
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("policy_simulation.unavailable_title"),
		Description: view.Locale.Text("policy_simulation.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("policy_simulation.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
