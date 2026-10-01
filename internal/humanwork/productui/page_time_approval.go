package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// timeApprovalPage is the route adapter for manager time
// approval. The governed time service is not published to
// this UI yet, so the surface keeps the journeys fallback
// contract: an honest empty state with a recovery link
// that approves nothing — approval stays server
// authority. The live approval composition replaces this
// body once the governed service publishes; until then
// the UI will not simulate one.
func timeApprovalPage(view View) ui.Node {
	if view.TimeReviewProjection.State == TimeSurfaceReady {
		return timeReviewPage(view, view.TimeReviewProjection, false)
	}
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("time_approval.unavailable_title"),
		Description: view.Locale.Text("time_approval.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("time_approval.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
