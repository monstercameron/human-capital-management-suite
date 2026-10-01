package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// privacyTelemetryPage is the route adapter for
// privacy-safe frontend telemetry. The governed
// telemetry service is not published to this UI yet, so
// the surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that
// instruments nothing — telemetry truth stays server
// authority. The live telemetry composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func privacyTelemetryPage(view View) ui.Node {
	if view.PrivacyTelemetry != nil && view.PrivacyTelemetry.Ready {
		return privacyTelemetryLivePage(view, *view.PrivacyTelemetry)
	}
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("privacy_telemetry.unavailable_title"),
		Description: view.Locale.Text("privacy_telemetry.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("privacy_telemetry.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
