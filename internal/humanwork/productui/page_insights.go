package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func insightsPage(view View) ui.Node {
	// Derived counts draw from the admitted population, so denied
	// journeys keep no share of any metric.
	population := admittedWork(view)
	active, terminal := journeyCounts(population)
	attention := 0
	for _, item := range population {
		// PROMOUX-012: every approval decision (not only the literal
		// "Awaiting approval" label) and every blocked proposal, by the
		// server's next-step code.
		if !item.Terminal && (WorkAwaitsDecision(item) || item.NextStep == "correct_proposal") {
			attention++
		}
	}
	action := ActionLinkProps{}
	if view.Allows(PageWork, "view") {
		action = ActionLinkProps{Label: "Open live work", Href: statefulHref(view, PageWork), Class: "button secondary", Navigate: view.Navigate}
	}
	return ui.CreateElement(InsightsPage, InsightsPageProps{
		Metrics: []MetricProps{
			{Label: "Visible workflows", Value: fmt.Sprint(len(population)), Note: "Current workflows in your authorized scope"},
			{Label: "In progress", Value: fmt.Sprint(active), Note: "Workflows awaiting a next step"},
			{Label: "Completed or closed", Value: fmt.Sprint(terminal), Note: "Workflows with a final outcome"},
		},
		Attention: AttentionPanelProps{
			Title: "Operational attention", CountLabel: "Needs attention", CountValue: fmt.Sprint(attention),
			Description: view.Locale.Text("insights.attention_description"),
			Action:      action,
		},
	})
}
