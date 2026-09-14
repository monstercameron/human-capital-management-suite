package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func insightsPage(view View) ui.Node {
	if view.LoadError != "" {
		// The shell owns recovery; an empty read is not evidence of zero activity.
		return ui.Fragment()
	}
	// Derived counts draw from the admitted population, so denied
	// journeys keep no share of any metric.
	population := admittedWork(view)
	active, terminal := journeyCounts(population)
	// Needs attention is an assignment measure, not a count of every visible
	// journey in review. Use the same viewer-scoped projection as My Work so
	// the Insights action and its destination cannot disagree.
	attentionPopulation := MyWorkItems(population, view.Viewer)
	attention := 0
	for _, item := range attentionPopulation {
		// PROMOUX-012: every approval decision (not only the literal
		// "Awaiting approval" label) and every blocked proposal, by the
		// server's next-step code.
		if WorkNeedsViewerAction(item) && (WorkAwaitsDecision(item) || item.NextStep == "correct_proposal") {
			attention++
		}
	}
	action := ActionLinkProps{}
	if view.Allows(PageWork, "view") {
		action = ActionLinkProps{Label: view.Locale.Text("insights.open_work"), Href: statefulHref(view, PageWork), Class: "button secondary", Navigate: view.Navigate}
	}
	metrics := []MetricProps{
		{Label: view.Locale.Text("insights.visible_label"), Value: fmt.Sprint(len(population)), Note: view.Locale.Text("insights.visible_note")},
		{Label: view.Locale.Text("insights.in_progress_label"), Value: fmt.Sprint(active), Note: view.Locale.Text("insights.in_progress_note")},
		{Label: view.Locale.Text("insights.closed_label"), Value: fmt.Sprint(terminal), Note: view.Locale.Text("insights.closed_note")},
	}
	attentionDescription := view.Locale.Text("insights.attention_description")
	if len(attentionPopulation) == 0 {
		attentionDescription = view.Locale.Text("work.action_queue_empty_detail")
	}
	evidence := InsightsEvidenceProps{
		// This is a current authorized snapshot, not a historical trend or
		// an organization-wide population. No source timestamp is supplied.
		TimeRange:        view.Locale.Text("insights.time_range_value"),
		Freshness:        view.Locale.Text("insights.freshness_value"),
		Lineage:          view.Locale.Text("insights.source_value"),
		ScopeDescription: view.Locale.Text("insights.attention_description"),
	}
	if len(population) == 0 {
		empty := &EmptyStateProps{
			Title:       view.Locale.Text("insights.no_data_title"),
			Description: view.Locale.Text("insights.no_data_description"),
			Class:       "insights-empty",
		}
		// Only the server's semantic-action projection may offer a start.
		// Page CRUD alone is not promotion authority.
		if globalSearchCanStartPromotion(view) {
			empty.Action = &ActionLinkProps{Label: view.Locale.Text("insights.start_promotion"), Href: statefulHref(view, PagePeople, "eligible", "1"), Class: "button primary", Navigate: view.Navigate}
		}
		return ui.CreateElement(InsightsPage, InsightsPageProps{
			I18nProps: I18nProps{Locale: view.Locale}, Empty: empty,
			EvidenceTitle: view.Locale.Text("insights.empty_context_title"), Evidence: evidence,
		})
	}
	return ui.CreateElement(InsightsPage, InsightsPageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Metrics:   metrics,
		Attention: AttentionPanelProps{
			Title: view.Locale.Text("insights.attention_title"), CountLabel: view.Locale.Text("insights.needs_attention"), CountValue: fmt.Sprint(attention),
			Description: attentionDescription,
			Action:      action,
		},
		Evidence: evidence,
	})
}
