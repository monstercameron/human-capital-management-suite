package chatpilot

import (
	"bytes"
	"html/template"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/tokens"
)

// RenderDashboardDocument renders the pilot SLO dashboard a design partner
// and the accountable owner view during a served chat pilot (see CHAT-053's
// GREEN: "Named design partner uses served chat with SLO dashboards,
// incident playbook, rollback and observed adoption criteria"). It presents
// the same verified metric-vs-budget facts Review checks, plus the gate's
// current fail-closed decision, as a standalone accessible document. It
// reuses internal/experience/tokens.WorkspaceCSS so this surface's contrast
// evidence matches the rest of the platform's accessibility evidence, the
// same way internal/governance/privacy.RenderNoticeDocument does for its
// PRIV-002 Browser evidence.
//
// This is the fixture/demo renderer TestTodo_CHAT_053_Browser scores,
// standing in for whatever hosting page a served pilot cell renders the
// dashboard into; it makes no claim about being wired into a specific HTTP
// route.
func RenderDashboardDocument(decision Decision, tenantID string, metrics []Metric) (string, error) {
	view := dashboardView{
		Title:    "Chat pilot SLO dashboard",
		TenantID: tenantID,
		Ready:    decision.Ready,
		Reasons:  decision.Reasons,
		Metrics:  make([]metricRow, 0, len(metrics)),
		CSS:      template.CSS(tokens.WorkspaceCSS()),
	}
	for _, m := range metrics {
		view.Metrics = append(view.Metrics, metricRow{
			Name:        m.Name,
			Observed:    m.Observed,
			Limit:       m.Limit,
			Unit:        m.Unit,
			Samples:     m.Samples,
			WithinLimit: m.Samples > 0 && m.Observed <= m.Limit,
		})
	}
	var buf bytes.Buffer
	if err := dashboardTemplate.Execute(&buf, view); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type dashboardView struct {
	Title    string
	TenantID string
	Ready    bool
	Reasons  []string
	Metrics  []metricRow
	CSS      template.CSS
}

type metricRow struct {
	Name        string
	Observed    float64
	Limit       float64
	Unit        string
	Samples     uint64
	WithinLimit bool
}

var dashboardTemplate = template.Must(template.New("chat-pilot-dashboard").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>{{.CSS}}</style>
</head>
<body>
<header class="workspace-header">
<h1>{{.Title}}</h1>
<p>Pilot tenant: <span id="pilot-tenant">{{.TenantID}}</span></p>
<p><a id="dashboard-refresh" href="#metrics-heading">View current SLO metrics</a></p>
</header>
<main id="main-content">
<section aria-labelledby="status-heading">
<h2 id="status-heading">Pilot readiness</h2>
<p role="status" aria-live="polite" id="pilot-status">{{if .Ready}}Ready: every signed evidence kind, adoption threshold and SLO metric verified.{{else}}Not ready: {{len .Reasons}} unresolved reason(s).{{end}}</p>
{{if not .Ready}}
<ul id="pilot-reasons">
{{range .Reasons}}<li>{{.}}</li>
{{end}}
</ul>
{{end}}
</section>
<section aria-labelledby="metrics-heading">
<h2 id="metrics-heading">SLO metrics</h2>
<table>
<caption>Observed chat and workflow metrics against their signed budgets</caption>
<thead>
<tr><th scope="col">Metric</th><th scope="col">Observed</th><th scope="col">Limit</th><th scope="col">Unit</th><th scope="col">Samples</th><th scope="col">Status</th></tr>
</thead>
<tbody>
{{range .Metrics}}<tr><th scope="row">{{.Name}}</th><td>{{.Observed}}</td><td>{{.Limit}}</td><td>{{.Unit}}</td><td>{{.Samples}}</td><td>{{if .WithinLimit}}Within budget{{else}}Exceeds budget{{end}}</td></tr>
{{end}}
</tbody>
</table>
</section>
</main>
</body>
</html>
`))
