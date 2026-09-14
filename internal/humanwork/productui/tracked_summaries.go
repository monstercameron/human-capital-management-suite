package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TrackedSummary is the derived-only tracking projection for
// one admitted work item: exactly the fields the UF-005
// status summary needs. Openness derives from terminality so
// counts reconcile with the attention (WEB-098) and
// recent-work (WEB-099) lists. No dates are interpreted and
// no new truth is added.
type TrackedSummary struct {
	ID     string
	Status string
	Due    string
	Open   bool
}

// TrackedRequestProps is the render-safe row for a journey being followed.
// Open is derived from the admitted item and never interpreted as an action
// permission.
type TrackedRequestProps struct {
	ID       string
	Title    string
	Person   string
	Status   string
	Due      string
	Open     bool
	Href     string
	Navigate func(string)
}

type TrackedRequestsProps struct {
	I18nProps
	Title       string
	Description string
	Items       []TrackedRequestProps
	EmptyTitle  string
	EmptyDetail string
	More        ActionLinkProps
}

// TrackedRequests renders status-only continuity. It deliberately has no
// primary action button: a tracked journey or passive wait belongs to the
// detail route, not the viewer's action queue.
func TrackedRequests(props TrackedRequestsProps) ui.Node {
	rows := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
		identityText := item.Title
		if identityText == "" {
			identityText = item.ID
		}
		identity := html.Strong(html.Props{}, ui.Text(identityText))
		if item.Href != "" {
			identity = softwareLink(item.Navigate, html.Props{}, item.Href, ui.Text(identityText))
		}
		detail := item.Status
		if !item.Open {
			detail += " · " + props.Text("home.tracked_closed")
		}
		if item.Person != "" {
			detail = item.Person + " · " + detail
		}
		rows = append(rows, html.Li(html.Props{Class: "activity"},
			html.Span(html.Props{Class: "row-main"},
				identity,
				html.Small(html.Props{}, ui.Text(detail)),
			),
			html.Tag("time", html.Props{}, ui.Text(item.Due)),
		))
	}
	if len(rows) == 0 {
		rows = append(rows, html.Li(html.Props{Class: "collection-empty"},
			html.Strong(html.Props{}, ui.Text(props.EmptyTitle)),
			html.Small(html.Props{}, ui.Text(props.EmptyDetail)),
		))
	}
	body := []ui.Node{}
	if len(props.Items) > 0 && props.Description != "" {
		body = append(body, html.P(html.Props{Class: "muted"}, ui.Text(props.Description)))
	}
	body = append(body, html.Ul(html.Props{Class: "recent tracked-requests", Raw: map[string]any{"role": "list"}}, rows...))
	if props.More.Href != "" {
		body = append(body, html.Div(html.Props{Class: "panel-foot"}, ui.CreateElement(ActionLink, props.More)))
	}
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Class: "tracked-requests-panel", Body: html.Div(html.Props{}, body...)})
}

// SummarizeTracked projects one admitted work stream to its
// tracked-request summaries in admission order. Fields pass
// through untouched.
func SummarizeTracked(items []WorkItem) []TrackedSummary {
	summaries := make([]TrackedSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, TrackedSummary{
			ID:     item.ID,
			Status: item.Status,
			Due:    item.Due,
			Open:   !item.Terminal,
		})
	}
	return summaries
}
