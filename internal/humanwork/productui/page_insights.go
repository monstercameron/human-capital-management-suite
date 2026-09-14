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
	attentionTitle := view.Locale.Text("insights.attention_title")
	if len(attentionPopulation) == 0 {
		attentionDescription = view.Locale.Text("work.action_queue_empty_detail")
	}
	if len(population) == 0 {
		notReported := view.Locale.Text("common.not_reported")
		metrics = []MetricProps{
			{Label: view.Locale.Text("insights.visible_label"), Value: notReported, Note: view.Locale.Text("insights.no_data_note")},
			{Label: view.Locale.Text("insights.in_progress_label"), Value: notReported, Note: view.Locale.Text("insights.no_data_note")},
			{Label: view.Locale.Text("insights.closed_label"), Value: notReported, Note: view.Locale.Text("insights.no_data_note")},
		}
		// A zero attention count would imply that the source was queried and
		// found no attention items. With no admitted records, that conclusion
		// is not supported by the projection.
		attention = -1
		attentionTitle = view.Locale.Text("insights.no_data_title")
		attentionDescription = view.Locale.Text("insights.no_data_description")
	}
	attentionValue := fmt.Sprint(attention)
	if attention < 0 {
		attentionValue = view.Locale.Text("common.not_reported")
	}
	return ui.CreateElement(InsightsPage, InsightsPageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Metrics:   metrics,
		Attention: AttentionPanelProps{
			Title: attentionTitle, CountLabel: view.Locale.Text("insights.needs_attention"), CountValue: attentionValue,
			Description: attentionDescription,
			Action:      action,
		},
		Evidence: InsightsEvidenceProps{
			// The current projection has no historical comparison. Saying so in
			// the period value is important: a live snapshot must not look like a
			// trend report merely because it contains numeric measures.
			TimeRange:        view.Locale.Text("insights.time_range_value"),
			Freshness:        view.Locale.Text("insights.freshness_value"),
			Lineage:          view.Locale.Text("insights.source_value"),
			ScopeDescription: view.Locale.Text("insights.attention_description"),
		},
	})
}
