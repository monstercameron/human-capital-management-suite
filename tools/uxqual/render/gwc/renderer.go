// Package gwc is the GoWebComponents renderer for the Promotion workspace
// contract (tools/uxqual/contract). It builds the same component tree for
// both targets GWC supports:
//
//   - GOOS=js GOARCH=wasm: Mount (mount_wasm.go) renders the tree live into
//     the DOM via ui.Render, exactly like any other GWC page.
//   - every other platform (including this repository's native
//     windows/arm64 toolchain): RenderToString renders the identical tree
//     to an HTML string via GWC's own native SSR path
//     (github.com/monstercameron/GoWebComponents/v5/ui.RenderToString),
//     which is what UX-QUAL-001's qualification fixture
//     (tools/uxqual/qual) scores -- it is not a third renderer, it is the
//     wasm renderer's own component tree observed without a browser.
//
// Structure mirrors tools/uxqual/render/ssr/ssr.go: same landmarks, same
// field order (DOM order drives keyboard tab order), same live region for
// simulation status, and the same shared tokens.WorkspaceCSS stylesheet.
package gwc

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/contract"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/page"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
)

// Build constructs the Promotion workspace component tree for the given
// contract. It does NOT include the stylesheet: GWC has no raw-text sink on
// the node/render path by design (see html/raw_html.go's doc comment), so
// any ui.Node text content -- including a <style> child holding a literal
// CSS blob -- is HTML-escaped on render, which would corrupt selectors like
// `[data-status="ready"]`. GWC's own SSR stylesheet output (css.CriticalCSS
// / css.StyleBlock) is therefore assembled as a plain string outside the
// node tree, and Document below follows the same pattern with
// tokens.WorkspaceCSS.
func Build(c contract.WorkspaceContract) ui.Node {
	return html.Div(html.Props{Class: "workspace"},
		html.A(html.Props{Class: "visually-hidden", Href: "#main-content"}, html.Text("Skip to main content")),
		header(c),
		nav(),
		html.Main(html.Props{ID: "main-content"},
			requestSection(c),
			preflightSection(c),
			simulationSection(c),
			timelineSection(c),
			actionsSection(c),
		),
		footer(c),
	)
}

// RenderToString renders the component tree (without the stylesheet) to an
// HTML string using GWC's own SSR path
// (github.com/monstercameron/GoWebComponents/v5/ui.RenderToString). This is
// what tools/uxqual/qual's fixture calls: it needs no browser and no wasm
// build, so `go test ./tools/uxqual/...` scores the real GWC output on
// every platform, including this repository's native toolchain.
func RenderToString(c contract.WorkspaceContract) (string, error) {
	return ui.RenderToString(Build(c))
}

var titleEscaper = strings.NewReplacer(`&`, "&amp;", `<`, "&lt;", `>`, "&gt;")

// Stylesheet returns the workspace's CSS as a literal string -- assembled
// the same way as GWC's own css.CriticalCSS()/StyleBlock(), string
// concatenation rather than a text ui.Node -- so selectors and attribute
// values are never HTML-entity-escaped.
func Stylesheet() string { return tokens.WorkspaceCSS() + page.LayoutCSS() }

// Document renders the full standalone HTML document (doctype, head with
// the literal stylesheet, and the component tree) for the given contract:
// the GWC-equivalent of ssr.Render, used by tools/uxqual/qual so both
// renderers are scored as complete documents.
func Document(c contract.WorkspaceContract) (string, error) {
	body, err := RenderToString(c)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString("<title>")
	b.WriteString(titleEscaper.Replace(c.Title))
	b.WriteString("</title><style>")
	b.WriteString(Stylesheet())
	b.WriteString("</style></head><body>")
	b.WriteString(body)
	b.WriteString("</body></html>")
	return b.String(), nil
}

func header(c contract.WorkspaceContract) ui.Node {
	return html.Header(html.Props{Class: "workspace-header"},
		html.H1(html.Props{}, html.Text(c.Title)),
		html.P(html.Props{}, html.Text("Worker: "+c.Request.WorkerName+" · Workspace "+c.WorkspaceID)),
	)
}

func nav() ui.Node {
	return html.Nav(html.Props{Aria: map[string]string{"label": "Workspace sections"}},
		html.Ul(html.Props{},
			html.Li(html.Props{}, html.A(html.Props{Href: "#request-section"}, html.Text("Request"))),
			html.Li(html.Props{}, html.A(html.Props{Href: "#preflight-section"}, html.Text("Preflight findings"))),
			html.Li(html.Props{}, html.A(html.Props{Href: "#simulation-section"}, html.Text("Simulation"))),
			html.Li(html.Props{}, html.A(html.Props{Href: "#timeline-section"}, html.Text("Approval timeline"))),
		),
	)
}

func requestSection(c contract.WorkspaceContract) ui.Node {
	fields := make([]ui.Node, 0, len(c.Request.Fields))
	for _, f := range c.Request.Fields {
		fields = append(fields, fieldNode(f))
	}
	return html.Section(html.Props{ID: "request-section", Aria: map[string]string{"labelledby": "request-heading"}},
		html.H2(html.Props{ID: "request-heading"}, html.Text("Requested change")),
		html.Form(html.Props{Method: "post", Action: "#"}, fields...),
	)
}

func fieldNode(f contract.RequestField) ui.Node {
	labelChildren := []ui.Node{html.Text(f.Label)}
	if f.Validation.Required {
		labelChildren = append(labelChildren, html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, html.Text(" *")))
	}
	var control ui.Node
	inputProps := html.Props{ID: f.ID, Name: f.ID, Value: f.Value}
	if f.Validation.Required {
		inputProps.Required = true
		inputProps.Aria = map[string]string{"required": "true"}
	}
	if f.Validation.Message != "" {
		if inputProps.Aria == nil {
			inputProps.Aria = make(map[string]string)
		}
		inputProps.Aria["invalid"] = "true"
		inputProps.Aria["describedby"] = f.ID + "-error"
	}
	switch f.Kind {
	case contract.FieldKindReadOnly:
		control = html.P(html.Props{ID: f.ID, Class: "field-static"}, html.Text(f.Value))
	case contract.FieldKindTextarea:
		control = html.Textarea(inputProps, html.Text(f.Value))
	case contract.FieldKindDate:
		inputProps.Type = "date"
		control = html.Input(inputProps)
	default:
		inputProps.Type = "text"
		control = html.Input(inputProps)
	}
	children := []ui.Node{
		html.Label(html.Props{For: f.ID}, labelChildren...),
		control,
	}
	if f.Validation.Message != "" {
		children = append(children, html.P(html.Props{ID: f.ID + "-error", Class: "error", Role: "alert"}, html.Text(f.Validation.Message)))
	}
	return html.Div(html.Props{Class: "field"}, children...)
}

func preflightSection(c contract.WorkspaceContract) ui.Node {
	items := make([]ui.Node, 0, len(c.Preflight))
	for _, finding := range c.Preflight {
		items = append(items, html.Li(html.Props{Class: "finding", Raw: map[string]any{"data-severity": string(finding.Severity)}},
			html.Strong(html.Props{Class: "severity-label"}, html.Text(severityLabel(finding.Severity)+": ")),
			html.Strong(html.Props{}, html.Text(finding.Label+": ")),
			html.Text(finding.Detail),
		))
	}
	return html.Section(html.Props{ID: "preflight-section", Aria: map[string]string{"labelledby": "preflight-heading"}},
		html.H2(html.Props{ID: "preflight-heading"}, html.Text("Preflight findings")),
		html.Ul(html.Props{Class: "findings", Role: "list"}, items...),
	)
}

func simulationSection(c contract.WorkspaceContract) ui.Node {
	checks := make([]ui.Node, 0, len(c.Simulation.Checks))
	for _, check := range c.Simulation.Checks {
		checks = append(checks, html.Li(html.Props{Class: "check", Raw: map[string]any{"data-severity": string(check.Status)}},
			html.Strong(html.Props{Class: "severity-label"}, html.Text(severityLabel(check.Status)+": ")),
			html.Strong(html.Props{}, html.Text(check.Label+": ")),
			html.Text(check.Detail),
		))
	}
	return html.Section(html.Props{ID: "simulation-section", Aria: map[string]string{"labelledby": "simulation-heading"}},
		html.H2(html.Props{ID: "simulation-heading"}, html.Text("Transaction simulation")),
		// Live region: assistive technology announces simulation status
		// changes without requiring focus to move.
		html.Div(html.Props{
			Class: "status-banner",
			Role:  "status",
			Aria:  map[string]string{"live": "polite"},
			Raw:   map[string]any{"data-status": string(c.Simulation.Status)},
		}, html.Strong(html.Props{Class: "status-label"}, html.Text("Status: "+simulationStatusLabel(c.Simulation.Status)+". ")), html.Text(c.Simulation.Summary)),
		html.P(html.Props{Class: "simulation-generated visually-hidden print-evidence"},
			html.Text("Generated "),
			html.Time(html.Props{Raw: map[string]any{"datetime": c.Simulation.GeneratedAt.Format("2006-01-02T15:04:05Z07:00")}}, html.Text(c.Simulation.GeneratedAt.Format("Jan 2, 2006 15:04 MST")))),
		html.Ul(html.Props{Class: "findings", Role: "list"}, checks...),
	)
}

func timelineSection(c contract.WorkspaceContract) ui.Node {
	items := make([]ui.Node, 0, len(c.Timeline))
	for _, event := range c.Timeline {
		items = append(items, html.Li(html.Props{},
			html.Time(html.Props{Raw: map[string]any{"datetime": event.At.Format("2006-01-02T15:04:05Z07:00")}},
				html.Text(event.At.Format("Jan 2, 2006 15:04 MST"))),
			html.Text(" — "),
			html.Strong(html.Props{}, html.Text(event.Actor)),
			html.Text(": "+event.Label),
		))
	}
	return html.Section(html.Props{Class: "timeline", ID: "timeline-section", Aria: map[string]string{"labelledby": "timeline-heading"}},
		html.H2(html.Props{ID: "timeline-heading"}, html.Text("Approval timeline")),
		html.Ol(html.Props{Class: "timeline"}, items...),
	)
}

func actionsSection(c contract.WorkspaceContract) ui.Node {
	forms := make([]ui.Node, 0, len(c.Actions))
	for _, a := range c.Actions {
		formChildren := []ui.Node{html.HiddenInput("transition", a.Transition)}
		if a.RequiresReason {
			formChildren = append(formChildren,
				html.Label(html.Props{Class: "visually-hidden", For: "reason-" + a.Transition}, html.Text("Reason")),
				html.Input(html.Props{Type: "text", ID: "reason-" + a.Transition, Name: "reason", Required: true, Aria: map[string]string{"required": "true"}}),
			)
		}
		formChildren = append(formChildren, html.Button(html.Props{Type: "submit", Raw: map[string]any{"data-variant": string(a.Variant)}}, html.Text(a.Label)))
		forms = append(forms, html.Form(html.Props{Method: "post", Action: "#"}, formChildren...))
	}
	return html.Section(html.Props{Class: "actions", Aria: map[string]string{"labelledby": "actions-heading"}},
		html.H2(html.Props{ID: "actions-heading", Class: "visually-hidden"}, html.Text("Available actions")),
		html.Fragment(forms...),
	)
}

func footer(c contract.WorkspaceContract) ui.Node {
	return html.Footer(html.Props{},
		html.P(html.Props{Class: "provenance visually-hidden print-evidence"},
			html.Text("Source: "), html.Strong(html.Props{}, html.Text(c.Provenance.SourceSystem)),
			html.Text(" · Capability "+c.Provenance.CapabilityID+"@"+c.Provenance.CapabilityVersion+" · As of "),
			html.Time(html.Props{Raw: map[string]any{"datetime": c.Provenance.AsOf.Format("2006-01-02T15:04:05Z07:00")}}, html.Text(c.Provenance.AsOf.Format("Jan 2, 2006 15:04 MST")))),
	)
}

func severityLabel(s contract.Severity) string {
	switch s {
	case contract.SeverityBlocking:
		return "Blocking"
	case contract.SeverityWarning:
		return "Warning"
	case contract.SeveritySuccess:
		return "Passed"
	default:
		return "Information"
	}
}

func simulationStatusLabel(s contract.SimulationStatus) string {
	switch s {
	case contract.SimulationReady:
		return "Ready"
	case contract.SimulationNeedsReview:
		return "Needs review"
	case contract.SimulationFailed:
		return "Failed"
	default:
		return "Pending"
	}
}
