package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// reconciliationWorkbenchPage is the route adapter for
// the reconciliation and repair workbench. The governed
// repair service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that repairs
// nothing — repair truth stays server authority. The
// live workbench composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func reconciliationWorkbenchPage(view View) ui.Node {
	if view.ReconciliationWorkbench != nil && view.ReconciliationWorkbench.Ready {
		return reconciliationWorkbenchLivePage(view, *view.ReconciliationWorkbench)
	}
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("reconciliation_workbench.unavailable_title"),
		Description: view.Locale.Text("reconciliation_workbench.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("reconciliation_workbench.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
