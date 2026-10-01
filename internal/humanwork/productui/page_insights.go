package productui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func insightsPage(view View) ui.Node {
	if view.LoadError != "" {
		// The shell owns recovery; an empty read is not evidence of zero activity.
		return ui.Fragment()
	}
	if !insightsHasContent(view) {
		// Navigation filtering is not a route guard: an employee can still open
		// a bookmarked Insights URL. Keep that route honest when neither work nor
		// authorized workforce facts are available.
		return unavailablePanel(view.Locale.Text("shell.page_unavailable"), view.Locale.Text("shell.page_recovery"))
	}
	// UXLIVE-027: the totals are the server's one authorized summary over
	// the same journeys Journeys and Home show, never a page recount.
	totals, fromServer := journeyTotals(view)
	workforce := insightsWorkforce(view, admittedWork(view))
	// Needs attention is an assignment measure, not a count of every visible
	// journey in review. Use the same viewer-scoped projection as My Work so
	// the Insights action and its destination cannot disagree.
	pending := PendingWork(view)
	attention := len(pending)
	action := ActionLinkProps{}
	if view.Allows(PageWork, "view") {
		action = ActionLinkProps{Label: view.Locale.Text("insights.open_work"), Href: statefulHref(view, PageWork), Class: "button secondary", Navigate: view.Navigate}
	}
	metrics := []MetricProps{
		{Label: view.Locale.Text("insights.visible_label"), Value: view.Locale.FormatNumber(fmt.Sprint(totals.Total), 0), Note: view.Locale.Text("insights.visible_note")},
		{Label: view.Locale.Text("insights.in_progress_label"), Value: view.Locale.FormatNumber(fmt.Sprint(totals.Active), 0), Note: view.Locale.Text("insights.in_progress_note")},
		{Label: view.Locale.Text("insights.closed_label"), Value: view.Locale.FormatNumber(fmt.Sprint(totals.Closed), 0), Note: view.Locale.Text("insights.closed_note")},
	}
	// The queue describes the queue. The scope limitation is the evidence
	// card's job, and printing it in both places said the same sentence
	// twice on one screen (UXLIVE-015).
	attentionDescription := view.Locale.Text("insights.attention_queue_detail")
	if len(pending) == 0 {
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
	// UXLIVE-027: the server summary carries when the counted journeys last
	// changed; show it rather than claiming the time is unavailable.
	if fromServer && !totals.LatestUpdate.IsZero() {
		evidence.Freshness = formatInstantLabel(view.Locale, totals.LatestUpdate)
	}
	// Empty copy is only ever the authorized source's own answer: a
	// non-empty summary can never render "no journeys".
	if totals.Total == 0 {
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
			Workforce:     workforce,
			EvidenceTitle: view.Locale.Text("insights.empty_context_title"), Evidence: evidence,
		})
	}
	return ui.CreateElement(InsightsPage, InsightsPageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Metrics:   metrics,
		Workforce: workforce,
		Attention: AttentionPanelProps{
			Title: view.Locale.Text("insights.attention_title"), CountLabel: view.Locale.Text("insights.needs_attention"), CountValue: view.Locale.FormatNumber(fmt.Sprint(attention), 0),
			Description: attentionDescription,
			Action:      action,
		},
		Evidence: evidence,
	})
}

func insightsWorkforce(view View, work []WorkItem) InsightsWorkforceProps {
	units := make(map[string]int)
	locations := make(map[string]int)
	for _, person := range insightsWorkforcePeople(view) {
		unit, location := strings.TrimSpace(person.Team), strings.TrimSpace(person.Location)
		if unit != "" {
			units[unit]++
		}
		if location != "" {
			locations[location]++
		}
	}
	return InsightsWorkforceProps{
		Title:           view.Locale.Text("insights.workforce_title"),
		UnitTitle:       view.Locale.Text("insights.headcount_by_unit"),
		LocationTitle:   view.Locale.Text("insights.headcount_by_location"),
		UnitFacts:       insightDimensionFacts(view.Locale, units),
		LocationFacts:   insightDimensionFacts(view.Locale, locations),
		ThroughputLabel: view.Locale.Text("insights.promotion_throughput"),
		ThroughputValue: view.Locale.FormatNumber(fmt.Sprint(len(work)), 0),
		CycleTimeLabel:  view.Locale.Text("insights.promotion_cycle_time"),
		CycleTimeValue:  insightsAverageCycleTime(view.Locale, work),
	}
}

// insightsWorkforcePeople follows Organization's admitted workforce and
// canonical graph projection. The graph owns unit/location labels whenever
// it is supplied; the person fields remain the authorized fallback for older
// projections that do not carry a graph snapshot.
func insightsWorkforcePeople(view View) []Person {
	people := visibleWorkforcePeople(view)
	if view.OrganizationGraph == nil {
		return people
	}
	graph := readOrganizationGraph(view)
	if graph.Err != nil {
		return nil
	}
	for index := range people {
		unit, ok := graph.UnitByID[view.OrganizationMemberships[people[index].ID]]
		if !ok {
			people[index].Team = ""
			people[index].Location = ""
			continue
		}
		people[index].Team = unit.Name
		people[index].Location = graph.LocationByUnit[unit.ID]
	}
	return people
}

func insightDimensionFacts(locale LocaleContext, counts map[string]int) []FactProps {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	facts := make([]FactProps, 0, len(keys))
	for _, key := range keys {
		facts = append(facts, FactProps{Label: key, Value: locale.FormatNumber(fmt.Sprint(counts[key]), 0)})
	}
	return facts
}

func insightsAverageCycleTime(locale LocaleContext, work []WorkItem) string {
	var total time.Duration
	count := 0
	for _, item := range work {
		if !item.Terminal || strings.TrimSpace(item.EffectiveDate) == "" {
			continue
		}
		start, err := time.Parse(time.DateOnly, strings.TrimSpace(item.EffectiveDate))
		if err != nil {
			continue
		}
		completed, ok := parseWorkInstant(item.CompletedAt)
		if !ok {
			completed, err = time.Parse(time.RFC3339, strings.TrimSpace(item.CompletedAt))
			if err != nil {
				continue
			}
		}
		if completed.Before(start) {
			continue
		}
		total += completed.Sub(start)
		count++
	}
	if count == 0 {
		return locale.Text("insights.cycle_time_unavailable")
	}
	days := int(math.Round(total.Hours() / 24 / float64(count)))
	if days < 1 {
		days = 1
	}
	return locale.Text("insights.cycle_time_days", map[string]string{"days": locale.FormatNumber(fmt.Sprint(days), 0)})
}
