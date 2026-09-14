// Package ssr is the Go server-rendered HTML fallback renderer for the
// Promotion workspace contract (tools/uxqual/contract). It uses only
// html/template and tools/uxqual/tokens.
//
// The page is fully functional with no JavaScript at all: forms submit with
// a normal full-page POST, and every semantic action is a native <button>
// inside a <form>. That is the "progressive enhancement" baseline the
// go-only technology constitution calls for -- the enhancement is that a
// later, GENERATED (never handwritten) script could layer on top without
// changing what the page means; this renderer does not ship one, so there
// is nothing on the request path but server-rendered HTML and CSS.
package ssr

import (
	"bytes"
	"html/template"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/contract"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/page"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
)

// Render produces the full HTML document for the given workspace contract.
func Render(c contract.WorkspaceContract) (string, error) {
	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, viewModel(c)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type fieldView struct {
	ID       string
	Label    string
	InputTag template.HTML
	Required bool
	Message  string
}

type actionView struct {
	Label          string
	Variant        string
	Transition     string
	RequiresReason bool
}

type view struct {
	Title       string
	WorkspaceID string
	WorkerName  string
	CSS         template.CSS
	Fields      []fieldView
	Preflight   []contract.PreflightFinding
	Simulation  contract.SimulationResult
	Timeline    []contract.TimelineEvent
	Actions     []actionView
	Provenance  contract.Provenance
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

func viewModel(c contract.WorkspaceContract) view {
	fields := make([]fieldView, 0, len(c.Request.Fields))
	for _, f := range c.Request.Fields {
		fields = append(fields, fieldView{
			ID:       f.ID,
			Label:    f.Label,
			InputTag: inputTag(f),
			Required: f.Validation.Required,
			Message:  f.Validation.Message,
		})
	}
	actions := make([]actionView, 0, len(c.Actions))
	for _, a := range c.Actions {
		actions = append(actions, actionView{
			Label:          a.Label,
			Variant:        string(a.Variant),
			Transition:     a.Transition,
			RequiresReason: a.RequiresReason,
		})
	}
	return view{
		Title:       c.Title,
		WorkspaceID: c.WorkspaceID,
		WorkerName:  c.Request.WorkerName,
		CSS:         template.CSS(pageCSS()),
		Fields:      fields,
		Preflight:   c.Preflight,
		Simulation:  c.Simulation,
		Timeline:    c.Timeline,
		Actions:     actions,
		Provenance:  c.Provenance,
	}
}

// inputTag renders the one native control for a RequestField. Read-only
// fields render as text, never as a disabled/unfocusable control standing
// in for real content (a disabled control is unreachable by keyboard and
// invisible to some screen-reader navigation modes).
func inputTag(f contract.RequestField) template.HTML {
	req := ""
	if f.Validation.Required {
		req = ` required aria-required="true"`
	}
	var descr string
	if f.Validation.Message != "" {
		descr = ` aria-describedby="` + f.ID + `-error" aria-invalid="true"`
	}
	switch f.Kind {
	case contract.FieldKindReadOnly:
		return template.HTML(`<p id="` + f.ID + `" class="field-static">` + template.HTMLEscapeString(f.Value) + `</p>`)
	case contract.FieldKindTextarea:
		return template.HTML(`<textarea id="` + f.ID + `" name="` + f.ID + `"` + req + descr + `>` + template.HTMLEscapeString(f.Value) + `</textarea>`)
	case contract.FieldKindDate:
		return template.HTML(`<input type="date" id="` + f.ID + `" name="` + f.ID + `" value="` + template.HTMLEscapeString(f.Value) + `"` + req + descr + `>`)
	case contract.FieldKindMoney, contract.FieldKindLookup, contract.FieldKindText:
		fallthrough
	default:
		return template.HTML(`<input type="text" id="` + f.ID + `" name="` + f.ID + `" value="` + template.HTMLEscapeString(f.Value) + `"` + req + descr + `>`)
	}
}

// pageCSS returns the workspace stylesheet plus the shared page primitive
// rules. Both renderers call the same composition so their documents remain
// byte-identical where the stylesheet is concerned.
func pageCSS() string { return tokens.WorkspaceCSS() + page.LayoutCSS() }

var pageTemplate = template.Must(template.New("workspace").Funcs(template.FuncMap{
	"severity": severityLabel, "simulationStatus": simulationStatusLabel,
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>{{.CSS}}</style>
</head>
<body>
<div class="workspace">
<a class="visually-hidden" href="#main-content">Skip to main content</a>
<header class="workspace-header">
<h1>{{.Title}}</h1>
<p>Worker: {{.WorkerName}} &middot; Workspace {{.WorkspaceID}}</p>
</header>
<nav aria-label="Workspace sections">
<ul>
<li><a href="#request-section">Request</a></li>
<li><a href="#preflight-section">Preflight findings</a></li>
<li><a href="#simulation-section">Simulation</a></li>
<li><a href="#timeline-section">Approval timeline</a></li>
</ul>
</nav>
<main id="main-content">
<section id="request-section" aria-labelledby="request-heading">
<h2 id="request-heading">Requested change</h2>
<form method="post" action="#">
{{range .Fields}}
<div class="field">
<label for="{{.ID}}">{{.Label}}{{if .Required}} <span aria-hidden="true">*</span>{{end}}</label>
{{.InputTag}}
{{if .Message}}<p id="{{.ID}}-error" class="error" role="alert">{{.Message}}</p>{{end}}
</div>
{{end}}
</form>
</section>

<section id="preflight-section" aria-labelledby="preflight-heading">
<h2 id="preflight-heading">Preflight findings</h2>
<ul class="findings" role="list">
{{range .Preflight}}
<li class="finding" data-severity="{{.Severity}}">
<strong class="severity-label">{{severity .Severity}}:</strong> <strong>{{.Label}}:</strong> {{.Detail}}
</li>
{{end}}
</ul>
</section>

<section id="simulation-section" aria-labelledby="simulation-heading">
<h2 id="simulation-heading">Transaction simulation</h2>
<div class="status-banner" data-status="{{.Simulation.Status}}" role="status" aria-live="polite">
<strong class="status-label">Status: {{simulationStatus .Simulation.Status}}.</strong> {{.Simulation.Summary}}
</div>
<p class="simulation-generated visually-hidden print-evidence">Generated <time datetime="{{.Simulation.GeneratedAt.Format "2006-01-02T15:04:05Z07:00"}}">{{.Simulation.GeneratedAt.Format "Jan 2, 2006 15:04 MST"}}</time></p>
<ul class="findings" role="list">
{{range .Simulation.Checks}}
<li class="check" data-severity="{{.Status}}">
<strong class="severity-label">{{severity .Status}}:</strong> <strong>{{.Label}}:</strong> {{.Detail}}
</li>
{{end}}
</ul>
</section>

<section class="timeline" id="timeline-section" aria-labelledby="timeline-heading">
<h2 id="timeline-heading">Approval timeline</h2>
<ol class="timeline">
{{range .Timeline}}
<li><time datetime="{{.At.Format "2006-01-02T15:04:05Z07:00"}}">{{.At.Format "Jan 2, 2006 15:04 MST"}}</time>
&mdash; <strong>{{.Actor}}</strong>: {{.Label}}</li>
{{end}}
</ol>
</section>

<section class="actions" aria-labelledby="actions-heading">
<h2 id="actions-heading" class="visually-hidden">Available actions</h2>
{{range .Actions}}
<form method="post" action="#">
<input type="hidden" name="transition" value="{{.Transition}}">
{{if .RequiresReason}}
<label class="visually-hidden" for="reason-{{.Transition}}">Reason</label>
<input type="text" id="reason-{{.Transition}}" name="reason" required aria-required="true">
{{end}}
<button type="submit" data-variant="{{.Variant}}">{{.Label}}</button>
</form>
{{end}}
</section>
</main>
<footer>
<p class="provenance visually-hidden print-evidence">Source: <strong>{{.Provenance.SourceSystem}}</strong> &middot; Capability {{.Provenance.CapabilityID}}@{{.Provenance.CapabilityVersion}} &middot; As of <time datetime="{{.Provenance.AsOf.Format "2006-01-02T15:04:05Z07:00"}}">{{.Provenance.AsOf.Format "Jan 2, 2006 15:04 MST"}}</time></p>
</footer>
</div>
</body>
</html>
`))
