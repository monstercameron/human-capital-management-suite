package privacy

import (
	"bytes"
	"html/template"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/tokens"
)

// RenderNoticeDocument renders a minimal, standalone HTML document
// presenting notice under a human-readable purpose label. It reuses
// internal/experience/tokens.WorkspaceCSS -- the same shared design tokens
// tools/uxqual/qual's WCAG 2.2 AA contrast check scores -- so this
// presentation channel's evidence is provably consistent with the rest of
// the platform's accessibility evidence rather than a bespoke one-off
// screen, and so tools/uxqual/qual's structural checks (landmarks, label
// association, live region, reflow) can score it directly without this
// package reimplementing them.
//
// It is the fixture/demo renderer TestTodo_PRIV_002_Browser scores, standing
// in for whatever real notice-presentation surface a caller builds; this
// package does not claim to be that surface.
func RenderNoticeDocument(n Notice) (string, error) {
	view := noticeView{
		Title:        "Privacy notice",
		Purpose:      n.Purpose,
		Jurisdiction: n.Jurisdiction,
		Version:      n.Version,
		DataClasses:  n.DataClasses,
		CSS:          template.CSS(tokens.WorkspaceCSS()),
	}
	var buf bytes.Buffer
	if err := noticeTemplate.Execute(&buf, view); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type noticeView struct {
	Title        string
	Purpose      string
	Jurisdiction string
	Version      string
	DataClasses  []string
	CSS          template.CSS
}

var noticeTemplate = template.Must(template.New("notice").Parse(`<!doctype html>
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
</header>
<main id="main-content">
<section aria-labelledby="notice-heading">
<h2 id="notice-heading">{{.Purpose}}</h2>
<p>Jurisdiction: {{.Jurisdiction}} &middot; Version {{.Version}}</p>
<ul class="findings" role="list">
{{range .DataClasses}}<li>{{.}}</li>
{{end}}
</ul>
<div class="status-banner" role="status" aria-live="polite">Notice ready for review.</div>
<form method="post" action="#">
<div class="field">
<label for="ack">I have read and understood this notice</label>
<input type="checkbox" id="ack" name="ack">
</div>
<button type="submit">Acknowledge</button>
</form>
</section>
</main>
<footer>
<p class="visually-hidden">Notice {{.Version}}</p>
</footer>
</body>
</html>
`))
