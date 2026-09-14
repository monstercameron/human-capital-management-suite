package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ActionLinkProps is the shared navigation contract used by feature
// components. Navigate upgrades same-application links to software navigation;
// cross-application links remain progressively enhanced browser links.
type ActionLinkProps struct {
	Label    string
	Href     string
	Class    string
	Navigate func(string)
}

// FactProps is an already-formatted label/value pair.
type FactProps struct {
	Label string
	Value string
}

// MetricProps is a single operational measure and its provenance note.
type MetricProps struct {
	Label string
	Value string
	Note  string
}

// ActivityProps is one compact timeline entry.
type ActivityProps struct {
	Title  string
	Detail string
	Status string
}

// PanelProps is the common titled-surface composition primitive. Body is a
// deliberate slot: low-level layout accepts children while feature props stay
// transport-neutral and contain presentation data only.
type PanelProps struct {
	Title string
	Class string
	Body  ui.Node
}

// EmptyStateProps standardizes honest empty and unavailable states.
type EmptyStateProps struct {
	Title       string
	Description string
	Badge       string
	Tone        string
	Class       string
	Role        string
	Action      *ActionLinkProps
}

func ActionLink(props ActionLinkProps) ui.Node {
	return softwareLink(props.Navigate, html.Props{Class: props.Class}, props.Href, ui.Text(props.Label))
}

func Panel(props PanelProps) ui.Node {
	class := "surface panel"
	if props.Class != "" {
		class += " " + props.Class
	}
	return html.Section(html.Props{Class: class},
		html.Div(html.Props{Class: "section-head"}, html.H2(html.Props{}, ui.Text(props.Title))),
		props.Body,
	)
}

func EmptyState(props EmptyStateProps) ui.Node {
	class := "surface empty-state"
	if props.Class != "" {
		class += " " + props.Class
	}
	raw := map[string]any{}
	if props.Role != "" {
		raw["role"] = props.Role
		raw["aria-atomic"] = "true"
	}
	children := make([]ui.Node, 0, 4)
	if props.Badge != "" {
		tone := props.Tone
		if tone == "" {
			tone = "warning"
		}
		children = append(children, html.Span(html.Props{Class: "status " + tone}, ui.Text(props.Badge)))
	}
	children = append(children,
		html.H2(html.Props{}, ui.Text(props.Title)),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
	)
	if props.Action != nil {
		children = append(children, ui.CreateElement(ActionLink, *props.Action))
	}
	return html.Section(html.Props{Class: class, Raw: raw}, children...)
}

func FactList(facts []FactProps) ui.Node {
	return html.Tag("dl", html.Props{Class: "facts"}, factRows(facts)...)
}

func factRows(facts []FactProps) []ui.Node {
	children := make([]ui.Node, 0, len(facts))
	for _, item := range facts {
		children = append(children, html.Div(html.Props{},
			html.Tag("dt", html.Props{}, ui.Text(item.Label)),
			html.Tag("dd", html.Props{}, ui.Text(item.Value)),
		))
	}
	return children
}

// TechnicalDetailItem is one raw identifier in PROMOUX-008's authorized
// diagnostics disclosure: a label and its full, uncensored value. The
// component renders the value redacted (see maskIdentifier) and copies the
// full value to the clipboard on request, so an authorized viewer can act
// on the identifier without a shoulder-surfer or a screen share reading it
// off the page.
type TechnicalDetailItem struct {
	Label string
	Value string
}

// TechnicalDetailsProps is the whole disclosure. Available is the single
// gate: it must come from a server-derived authorization decision (see
// View.Can(PageJourneyDiagnostics, "view")), never from whether Items
// happens to be empty, so an unauthorized viewer's page and an authorized
// viewer's page whose journey has nothing to disclose yet cannot be told
// apart by the disclosure's presence, count or layout -- only Available
// distinguishes "not shown because not authorized" from "shown, but there
// is nothing to list."
type TechnicalDetailsProps struct {
	I18nProps
	Available bool
	Items     []TechnicalDetailItem
}

// maskIdentifier redacts a raw identifier for on-screen display: everything
// but its last four characters becomes a bullet. The full value still
// travels to the copy control, which is the authorized viewer's actual
// entitlement; masking only reduces what a passerby reads off the screen.
func maskIdentifier(value string) string {
	const visible = 4
	if len(value) <= visible {
		return strings.Repeat("•", len(value))
	}
	return "••••" + value[len(value)-visible:]
}

// TechnicalDetails renders GREEN's authorized Technical details disclosure
// as a collapsed <details>, present at all only when Available -- the
// presence-channel closure PROMOUX-008 requires. Diagnostics.Available
// false renders the same empty, hidden placeholder
// PromotionValidationDiagnosticsPanel uses for the same reason: an absent
// section that still occupies no visible or structural difference a viewer
// could read as "this journey has something to disclose."
func TechnicalDetails(props TechnicalDetailsProps) ui.Node {
	if !props.Available {
		return html.Div(html.Props{Hidden: true, Class: "technical-details-empty"})
	}
	rows := make([]ui.Node, 0, len(props.Items))
	for i, item := range props.Items {
		if strings.TrimSpace(item.Value) == "" {
			continue
		}
		valueID := "technical-detail-value-" + strconv.Itoa(i)
		rows = append(rows, html.Div(html.Props{Class: "technical-detail-row"},
			html.Span(html.Props{Class: "technical-detail-label"}, ui.Text(item.Label)),
			html.Input(html.Props{ID: valueID, Type: "text", ReadOnly: true, Class: "technical-detail-value", Value: maskIdentifier(item.Value)}),
			html.Button(html.Props{
				Type:  "button",
				Class: "technical-detail-copy",
				Aria:  map[string]string{"label": props.Text("work.copy_value") + ": " + item.Label},
				OnClick: ui.UseEvent(func(ui.MouseEvent) {
					copyToClipboard(item.Value)
				}),
			}, ui.Text(props.Text("work.copy_value"))),
		))
	}
	if len(rows) == 0 {
		return html.Div(html.Props{Hidden: true, Class: "technical-details-empty"})
	}
	return html.Details(html.Props{Class: "technical-details", Dir: string(props.Locale.Direction)},
		html.Summary(html.Props{}, ui.Text(props.Text("work.technical_details"))),
		html.Div(html.Props{Class: "technical-details-body"}, rows...),
	)
}

func MetricGrid(metrics []MetricProps) ui.Node {
	children := make([]ui.Node, 0, len(metrics))
	for _, item := range metrics {
		children = append(children, html.Li(html.Props{Class: "metric"},
			html.Span(html.Props{Class: "muted"}, ui.Text(item.Label)),
			html.Strong(html.Props{}, ui.Text(item.Value)),
			html.Small(html.Props{}, ui.Text(item.Note)),
		))
	}
	return html.Ul(html.Props{Class: "metrics", Raw: map[string]any{"role": "list"}}, children...)
}

func ActivityList(items []ActivityProps, emptyTitle, emptyDescription string) ui.Node {
	children := make([]ui.Node, 0, len(items))
	for _, item := range items {
		main := []ui.Node{html.Strong(html.Props{}, ui.Text(item.Title))}
		if item.Detail != "" {
			main = append(main, html.Small(html.Props{}, ui.Text(item.Detail)))
		}
		children = append(children, html.Li(html.Props{Class: "activity"},
			html.Span(html.Props{Class: "check"}, productIcon("check", "activity-check-glyph")),
			html.Span(html.Props{Class: "row-main"}, main...),
			html.Small(html.Props{}, ui.Text(item.Status)),
		))
	}
	if len(children) == 0 {
		children = append(children, html.Li(html.Props{Class: "collection-empty"},
			html.Strong(html.Props{}, ui.Text(emptyTitle)),
			html.Small(html.Props{}, ui.Text(emptyDescription)),
		))
	}
	return html.Ul(html.Props{Class: "recent", Raw: map[string]any{"role": "list"}}, children...)
}
