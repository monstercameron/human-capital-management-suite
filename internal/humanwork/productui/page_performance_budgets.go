package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// performanceBudgetsPage is the route adapter for
// frontend performance budgets. The governed performance
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that enforces
// nothing — budget truth stays server authority. The
// live budget composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func performanceBudgetsPage(view View) ui.Node {
	if view.PerformanceBudgets != nil && view.PerformanceBudgets.Ready {
		return performanceBudgetsLivePage(view, *view.PerformanceBudgets)
	}
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("performance_budgets.unavailable_title"),
		Description: view.Locale.Text("performance_budgets.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("performance_budgets.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
