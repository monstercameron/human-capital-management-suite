package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// timeExceptionsPage is the route adapter for the
// time-exception workbench. The governed time service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that resolves nothing — resolution
// stays server authority. The live workbench composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func timeExceptionsPage(view View) ui.Node {
	if view.TimeExceptionsProjection.State == TimeSurfaceReady {
		return timeExceptionsSurface(view, view.TimeExceptionsProjection, false)
	}
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("time_exceptions.unavailable_title"),
		Description: view.Locale.Text("time_exceptions.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("time_exceptions.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
