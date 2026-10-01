package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type InsightsPageProps struct {
	I18nProps
	Metrics       []MetricProps
	Attention     AttentionPanelProps
	Workforce     InsightsWorkforceProps
	Empty         *EmptyStateProps
	EvidenceTitle string
	Evidence      InsightsEvidenceProps
}

type InsightsWorkforceProps struct {
	Title           string
	UnitTitle       string
	LocationTitle   string
	UnitFacts       []FactProps
	LocationFacts   []FactProps
	ThroughputLabel string
	ThroughputValue string
	CycleTimeLabel  string
	CycleTimeValue  string
}

// BarChartProps is the shared presentation contract for a compact categorical
// bar chart. Each fact is one named segment, so the legend and the accessible
// row text stay in step with the painted bars.
type BarChartProps struct {
	Title string
	Facts []FactProps
}

// InsightsEvidenceProps keeps the boundary of a lightweight workflow
// summary visible. A metric without its period, freshness, and source is too
// easy to mistake for a complete workforce report.
type InsightsEvidenceProps struct {
	TimeRange        string
	Freshness        string
	Lineage          string
	ScopeDescription string
}

type AttentionPanelProps struct {
	Title       string
	CountLabel  string
	CountValue  string
	Description string
	Action      ActionLinkProps
}

func InsightsPage(props InsightsPageProps) ui.Node {
	// Keep the metric rail and its actionable follow-up in one grid track. If
	// these are siblings of the evidence panel, the two-column page grid makes
	// the attention card the narrow third of the row and wraps its copy one
	// character at a time at desktop widths.
	var summary []ui.Node
	if props.Empty != nil {
		summary = append(summary, ui.CreateElement(EmptyState, *props.Empty))
	} else {
		summary = append(summary, MetricGrid(props.Metrics), ui.CreateElement(AttentionPanel, props.Attention))
	}
	children := []ui.Node{html.Div(html.Props{Class: "insights-summary"}, summary...)}
	if props.Empty == nil && (len(props.Workforce.UnitFacts) > 0 || len(props.Workforce.LocationFacts) > 0) {
		workforceFacts := make([]ui.Node, 0, 3)
		if len(props.Workforce.UnitFacts) > 0 {
			workforceFacts = append(workforceFacts, ui.CreateElement(BarChart, BarChartProps{Title: props.Workforce.UnitTitle, Facts: props.Workforce.UnitFacts}))
		}
		if len(props.Workforce.LocationFacts) > 0 {
			workforceFacts = append(workforceFacts, ui.CreateElement(BarChart, BarChartProps{Title: props.Workforce.LocationTitle, Facts: props.Workforce.LocationFacts}))
		}
		workforceFacts = append(workforceFacts, html.Div(html.Props{Class: "insights-workforce-summary bars"}, FactList([]FactProps{
			{Label: props.Workforce.ThroughputLabel, Value: props.Workforce.ThroughputValue},
			{Label: props.Workforce.CycleTimeLabel, Value: props.Workforce.CycleTimeValue},
		})))
		children = append(children, ui.CreateElement(Panel, PanelProps{Title: props.Workforce.Title, Class: "insights-workforce", Body: html.Div(html.Props{Class: "insights-workforce-grid"}, workforceFacts...)}))
	}
	if strings.TrimSpace(props.Evidence.TimeRange) != "" || strings.TrimSpace(props.Evidence.Freshness) != "" || strings.TrimSpace(props.Evidence.Lineage) != "" {
		evidenceBody := []ui.Node{FactList([]FactProps{
			{Label: props.Text("insights.time_range_label"), Value: props.Evidence.TimeRange},
			{Label: props.Text("insights.freshness_label"), Value: props.Evidence.Freshness},
			{Label: props.Text("insights.source_label"), Value: props.Evidence.Lineage},
		})}
		if strings.TrimSpace(props.Evidence.ScopeDescription) != "" {
			evidenceBody = append(evidenceBody, html.P(html.Props{Class: "muted insights-evidence-note"}, ui.Text(props.Evidence.ScopeDescription)))
		}
		title := props.EvidenceTitle
		if title == "" {
			title = props.Text("insights.context_title")
		}
		children = append(children, ui.CreateElement(Panel, PanelProps{
			Title: title,
			Class: "insights-evidence",
			Body:  html.Div(html.Props{Class: "insights-evidence-body"}, evidenceBody...),
		}))
	}
	// Keep the page family hook on the component itself so its layout remains
	// stable when the route shell is rendered in isolation (for example in a
	// browser harness or a component preview).
	// Insights is intentionally stacked: a full-width evidence row keeps
	// labels readable at desktop and tablet widths, while the metric rail still
	// provides its own three-column breakdown.
	return html.Div(html.Props{Class: "insights-grid"}, children...)
}

func insightsWorkforceBreakdown(title string, facts []FactProps) ui.Node {
	return ui.CreateElement(BarChart, BarChartProps{Title: title, Facts: facts})
}

// BarChart renders a label-safe chart row, a named legend, and a text
// equivalent for assistive technology. The painted segment also carries its
// count as a native tooltip, so the value is available without estimating the
// bar width.
func BarChart(props BarChartProps) ui.Node {
	rows := make([]ui.Node, 0, len(props.Facts))
	maximum := 1
	values := make([]int, len(props.Facts))
	for index, fact := range props.Facts {
		values[index] = insightsWorkforceFactCount(fact.Value)
		if values[index] > maximum {
			maximum = values[index]
		}
	}
	legend := make([]ui.Node, 0, len(props.Facts))
	for index, fact := range props.Facts {
		label := insightsWorkforceDisplayLabel(fact.Label)
		tooltip := label + ": " + fact.Value
		tone := strconv.Itoa(index % 3)
		fillClass := "bar-fill workforce-bar-fill workforce-bar-fill-" + tone
		rows = append(rows, html.Div(html.Props{Class: "workforce-bar-row", Aria: map[string]string{"label": tooltip}},
			html.Span(html.Props{Class: "workforce-bar-label", Raw: map[string]any{"title": label}}, ui.Text(label)),
			html.Div(html.Props{Class: "bar-track", Raw: map[string]any{"aria-hidden": "true", "title": tooltip}}, html.Span(html.Props{Class: fillClass, Raw: map[string]any{"title": tooltip}, Style: map[string]string{"width": insightsWorkforceBarWidth(values[index], maximum)}}, ui.Text(""))),
			html.Strong(html.Props{Class: "workforce-bar-value"}, ui.Text(fact.Value)),
		))
		legend = append(legend, html.Li(html.Props{Class: "workforce-bar-legend-item"},
			html.Span(html.Props{Class: "workforce-bar-swatch workforce-bar-swatch-" + tone, Raw: map[string]any{"aria-hidden": "true"}}, ui.Text("")),
			html.Span(html.Props{Class: "workforce-bar-legend-copy"}, ui.Text(tooltip)),
		))
	}
	return html.Div(html.Props{Class: "insights-breakdown"},
		html.H3(html.Props{}, ui.Text(props.Title)),
		html.Div(html.Props{Class: "workforce-bar-legend", Aria: map[string]string{"label": props.Title + " legend"}},
			html.Ul(html.Props{}, legend...)),
		html.Div(html.Props{Class: "bars"}, rows...),
	)
}

func insightsWorkforceFactCount(value string) int {
	value = strings.NewReplacer(",", "", ".", "").Replace(strings.TrimSpace(value))
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func insightsWorkforceBarWidth(value, maximum int) string {
	if value <= 0 || maximum <= 0 {
		return "0%"
	}
	width := value * 100 / maximum
	if width < 8 {
		width = 8
	}
	return strconv.Itoa(width) + "%"
}

func insightsWorkforceDisplayLabel(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "safety quality":
		return "Safety & Quality"
	case "warranty service":
		return "Warranty & Service"
	case "quality safety":
		return "Quality & Safety"
	default:
		return label
	}
}

func AttentionPanel(props AttentionPanelProps) ui.Node {
	children := []ui.Node{
		FactList([]FactProps{{Label: props.CountLabel, Value: props.CountValue}}),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
	}
	if props.Action.Href != "" {
		children = append(children, ui.CreateElement(ActionLink, props.Action))
	}
	body := html.Div(html.Props{Class: "coverage-strip"}, children...)
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: body})
}
