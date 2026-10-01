package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TimeExceptionFilter is one way to narrow the queue, such as a kind of
// problem. The service supplies the destination so filtering stays a link.
type TimeExceptionFilter struct {
	Label   string
	Count   string
	Href    string
	Current bool
}

// TimeExceptionRow is one attendance problem waiting for a supervisor.
type TimeExceptionRow struct {
	ID        string
	Worker    string
	DateLabel string
	KindLabel string
	KindTone  string
	Detail    string
	AgeLabel  string
	Open      *ActionLinkProps
	Resolve   *ActionLinkProps
}

// TimeExceptionsProjection is the supervisor's queue of attendance problems.
type TimeExceptionsProjection struct {
	State       TimeSurfaceState
	PeriodLabel string
	Filters     []TimeExceptionFilter
	Rows        []TimeExceptionRow
	Error       string
}

// TimeExceptionsPage lists what is wrong, who it belongs to and the one action
// that fixes it, oldest problem first as the service orders it.
func TimeExceptionsPage(view View, projection TimeExceptionsProjection) ui.Node {
	return timeExceptionsSurface(view, projection, true)
}

// timeExceptionsSurface renders the queue; the registered route passes
// withHead=false because the shell already prints the page heading.
func timeExceptionsSurface(view View, projection TimeExceptionsProjection, withHead bool) ui.Node {
	locale := view.Locale
	title := timeText(locale, "exceptions.title")
	if projection.State != TimeSurfaceReady {
		return timeUnavailable(view, PageTimeExceptions, "exceptions-title", title, timeText(locale, "exceptions.intro"), timeText(locale, "exceptions.unavailable"), timeText(locale, "exceptions.unavailable_help"))
	}
	children := timeNodes(timeNotice("error", projection.Error))
	if withHead {
		children = append([]ui.Node{timeHead(PageTimeExceptions, "exceptions-title", title, timeText(locale, "exceptions.intro"))}, children...)
	}
	if filters := timeExceptionFilters(view, projection.Filters); filters != nil {
		children = append(children, filters)
	}
	if len(projection.Rows) == 0 {
		children = append(children, html.Div(html.Props{Class: "surface time-empty"},
			html.H2(html.Props{}, ui.Text(timeText(locale, "exceptions.empty"))),
			html.P(html.Props{Class: "muted"}, ui.Text(timeText(locale, "exceptions.empty_help"))),
		))
		return html.Section(html.Props{Class: "time-page exceptions-page", Raw: map[string]any{"aria-labelledby": "exceptions-title"}}, children...)
	}
	rows := make([]ui.Node, 0, len(projection.Rows))
	for _, row := range projection.Rows {
		rows = append(rows, html.WithKey(timeExceptionRow(view, row), "exception-"+safeID(row.ID)))
	}
	children = append(children, html.Ul(html.Props{Class: "surface exception-rows", Raw: map[string]any{"aria-label": title}}, rows...))
	return html.Section(html.Props{Class: "time-page exceptions-page", Raw: map[string]any{"aria-labelledby": "exceptions-title"}}, children...)
}

func timeExceptionFilters(view View, filters []TimeExceptionFilter) ui.Node {
	if len(filters) == 0 {
		return nil
	}
	items := make([]ui.Node, 0, len(filters))
	for _, filter := range filters {
		if !validTimeHref(filter.Href) {
			continue
		}
		class := "exception-filter"
		raw := map[string]any{}
		if filter.Current {
			class += " exception-filter-current"
			raw["aria-current"] = "true"
		}
		label := filter.Label
		children := []ui.Node{html.Span(html.Props{}, ui.Text(label))}
		if filter.Count != "" {
			children = append(children, html.Span(html.Props{Class: "exception-filter-count"}, ui.Text(filter.Count)))
		}
		items = append(items, html.Li(html.Props{}, softwareLink(view.Navigate, html.Props{Class: class, Raw: raw}, filter.Href, children...)))
	}
	if len(items) == 0 {
		return nil
	}
	return html.Nav(html.Props{Class: "exception-filters", Raw: map[string]any{"aria-label": timeText(view.Locale, "exceptions.filters")}}, html.Ul(html.Props{}, items...))
}

func timeExceptionRow(view View, row TimeExceptionRow) ui.Node {
	return html.Li(html.Props{Class: "exception-row"},
		html.Div(html.Props{Class: "exception-main"},
			html.Strong(html.Props{Class: "exception-worker"}, ui.Text(row.Worker)),
			html.Div(html.Props{Class: "exception-kind"}, timePill(row.KindTone, row.KindLabel)),
			html.P(html.Props{Class: "exception-detail"}, ui.Text(row.Detail)),
		),
		html.Div(html.Props{Class: "exception-when"},
			html.Span(html.Props{}, ui.Text(row.DateLabel)),
			html.Span(html.Props{Class: "muted"}, ui.Text(row.AgeLabel)),
		),
		html.Div(html.Props{Class: "exception-actions"}, timeNodes(
			timeLinkFor(view, row.Resolve, "button primary", row.Worker),
			timeLinkFor(view, row.Open, "button secondary", row.Worker),
		)...),
	)
}
