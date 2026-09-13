package journey

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// pageHeaderProps is the shared heading contract for the journey overview
// and focused workflow pages. Feature views supply content and optional
// actions; this component owns hierarchy and spacing.
type pageHeaderProps struct {
	Class   string
	Eyebrow string
	Title   string
	Lead    string
	Actions []ui.Node
	// HeadingID, when set, names the <h1> and makes it a script-focusable
	// (tabindex="-1") landmark: PROMOUX-010's residual is that a live
	// client swapping Page.List for Page.Proposal wholesale (Start) drops
	// focus to <body> with nothing scrolled into view, so proposalView
	// sets this and a mount-only effect (proposal_view_focus_wasm.go)
	// moves focus here the moment the page transition lands. Every other
	// caller leaves this empty and gets the exact heading it always has.
	HeadingID string
}

func pageHeader(props pageHeaderProps) ui.Node {
	class := strings.TrimSpace("jn-pagehead " + props.Class)
	headingProps := html.Props{}
	if props.HeadingID != "" {
		headingProps.ID = props.HeadingID
		headingProps.TabIndex = -1
	}
	return html.Div(html.Props{Class: class},
		htmlIf(props.Eyebrow != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-eyebrow"}, html.Text(props.Eyebrow))
		}),
		html.H1(headingProps, html.Text(props.Title)),
		htmlIf(props.Lead != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-lead"}, html.Text(props.Lead))
		}),
		htmlIf(len(props.Actions) > 0, func() ui.Node {
			return html.Div(html.Props{Class: "jn-context-actions"}, html.Fragment(props.Actions...))
		}),
	)
}

// engineUnavailableCallout is shared by every page that can offer an
// execution-backed action. Feature views decide availability; the callout
// owns one consistent explanation and accessible structure.
func engineUnavailableCallout(available bool, notice string) ui.Node {
	if available || notice == "" {
		return nil
	}
	return html.Div(html.Props{Class: "jn-callout"},
		iconInfo("jn-callout-icon"),
		html.Div(html.Props{},
			html.P(html.Props{Class: "jn-callout-title"}, html.Text("Execution is not composed on this cell")),
			html.P(html.Props{Class: "jn-callout-detail"}, html.Text(notice)),
		),
	)
}

// loadingPanel is the shared stable placeholder for a page region whose
// facts are in flight. It deliberately contains no disabled copy of the
// eventual controls.
func loadingPanel(title, detail string) ui.Node {
	return html.Section(html.Props{Class: "jn-panel jn-loading", Raw: map[string]any{"aria-busy": "true"}},
		html.H2(html.Props{}, html.Text(title)),
		htmlIf(detail != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-muted"}, html.Text(detail))
		}),
	)
}

// networkPending recognizes only the client-owned in-flight notice. Ordinary
// informational notices remain fully interactive; a server refusal can never
// accidentally turn the page into a loading state.
func networkPending(p Page) bool {
	return p.Notice != nil && p.Notice.Title == "Working…"
}

// networkAwarePageBody retains enough of the previous surface to preserve
// spatial context while making every stale control inert and covering the
// unresolved region with a component-shaped proxy. The focused proposal has
// its own narrower loadingPanel and does not need a second proxy.
func networkAwarePageBody(p Page, body ui.Node) ui.Node {
	if !networkPending(p) || p.Proposal != nil && p.Proposal.Loading {
		return body
	}
	return html.Div(html.Props{Class: "jn-network-stage"},
		html.Div(html.Props{Class: "jn-network-stale", Raw: map[string]any{"aria-hidden": "true", "inert": true}}, body),
		journeyLoadingProxy(),
	)
}

func journeyLoadingProxy() ui.Node {
	rows := make([]ui.Node, 0, 5)
	for index := 0; index < 5; index++ {
		rows = append(rows, html.Div(html.Props{Class: "jn-proxy-row"},
			html.Span(html.Props{Class: "jn-proxy-block jn-proxy-avatar"}),
			html.Div(html.Props{Class: "jn-proxy-copy"},
				html.Span(html.Props{Class: "jn-proxy-block jn-proxy-line"}),
				html.Span(html.Props{Class: "jn-proxy-block jn-proxy-line jn-proxy-line-short"}),
			),
			html.Span(html.Props{Class: "jn-proxy-block jn-proxy-chip"}),
		))
	}
	return html.Div(html.Props{Class: "jn-panel jn-network-proxy", Raw: map[string]any{"aria-hidden": "true"}},
		html.Div(html.Props{Class: "jn-proxy-toolbar"},
			html.Span(html.Props{Class: "jn-proxy-block jn-proxy-heading"}),
			html.Span(html.Props{Class: "jn-proxy-block jn-proxy-control"}),
		),
		html.Div(html.Props{Class: "jn-proxy-rows"}, rows...),
	)
}
