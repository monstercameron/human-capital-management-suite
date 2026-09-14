package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type InsightsPageProps struct {
	I18nProps
	Metrics       []MetricProps
	Attention     AttentionPanelProps
	Empty         *EmptyStateProps
	EvidenceTitle string
	Evidence      InsightsEvidenceProps
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
