// HUB-034 browser/security evidence renderer. It renders the reviewer and
// publisher deployment-controls screen as a standalone HTML document: the
// exact diff, content hash, and deploy scope for one candidate version, plus
// review/deploy actions that only appear under current authority. Both the
// review and deploy forms for one item always carry the identical version
// id and hash from the same projection, so the UI itself cannot present a
// reviewer with one hash while submitting a different one to deploy.
package docs

import (
	"html/template"
	"strings"
)

// ReviewItem is one candidate version awaiting review or deployment.
type ReviewItem struct {
	DocumentID, VersionID, VersionHash, Title string
	// Scope is the exact audience/placement this deploy would take effect
	// for (e.g. "People Ops (team)"); Diff is the exact content change,
	// rendered verbatim and never summarized or truncated.
	Scope, Diff                             string
	ReviewState, ReviewAction, DeployAction string
	// CanReview/CanDeploy are the server's current-authority decision for
	// this exact viewer and this exact version; the renderer trusts nothing
	// else to decide whether an action form appears.
	CanReview, CanDeploy bool
}

// ReviewPage is the reviewer/publisher deployment-controls contract.
type ReviewPage struct {
	Locale, Title string
	Reviews       []ReviewItem
}

var reviewLabels = map[string]map[string]string{
	"en-US": {
		"heading": "Review and publish", "hash": "Exact content hash", "scope": "Deploy scope",
		"diff": "Exact changes", "state": "Review status", "review": "Review", "deploy": "Deploy",
		"unauthorized": "You do not currently have authority to review or deploy this version.",
	},
	"de-DE": {
		"heading": "Prüfen und veröffentlichen", "hash": "Exakter Inhaltshash", "scope": "Bereitstellungsbereich",
		"diff": "Exakte Änderungen", "state": "Prüfstatus", "review": "Prüfen", "deploy": "Veröffentlichen",
		"unauthorized": "Sie sind derzeit nicht berechtigt, diese Version zu prüfen oder zu veröffentlichen.",
	},
	"ar": {
		"heading": "المراجعة والنشر", "hash": "تجزئة المحتوى الدقيقة", "scope": "نطاق النشر",
		"diff": "التغييرات الدقيقة", "state": "حالة المراجعة", "review": "مراجعة", "deploy": "نشر",
		"unauthorized": "ليست لديك حاليًا صلاحية مراجعة هذه النسخة أو نشرها.",
	},
}

func reviewText(locale, key string) string {
	if m, ok := reviewLabels[locale]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	return reviewLabels["en-US"][key]
}

const reviewCSS = `.doc-review-list{list-style:none;margin:0;padding:0;display:grid;gap:1rem}` +
	`.doc-review-item{border:0.0625rem solid #d0d5dd;border-radius:0.5rem;padding:1rem}` +
	`.doc-review-facts{display:grid;grid-template-columns:auto 1fr;gap:0.25rem 0.75rem;margin:0.5rem 0}` +
	`.doc-review-facts dt{font-weight:700}.doc-review-facts dd{margin:0;overflow-wrap:anywhere}` +
	`.doc-review-diff{white-space:pre-wrap;background:#f2f4f7;border-radius:0.25rem;padding:0.75rem;overflow:auto}` +
	`.doc-review-actions{display:flex;gap:0.5rem;flex-wrap:wrap}` +
	`.doc-review-unauthorized{color:#7a1f1f}` +
	`@media (max-width:40rem){.doc-review-facts{grid-template-columns:1fr}.doc-review-actions{flex-direction:column;align-items:stretch}}` +
	`:focus-visible{outline:0.1875rem solid #1a56db;outline-offset:0.125rem}` +
	`@media (prefers-reduced-motion:reduce){*{animation:none;transition:none}}`

var reviewTemplate = template.Must(template.New("review").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head><meta charset="utf-8"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body><div class="doc-wrap">
<h1>{{.Heading}}</h1>
<ul class="doc-review-list">
{{range .Reviews}}<li class="doc-review-item" data-document-id="{{.DocumentID}}" data-version-id="{{.VersionID}}">
<h2>{{.Title}}</h2>
<dl class="doc-review-facts">
<div><dt>{{$.StateLabel}}</dt><dd>{{.ReviewState}}</dd></div>
<div><dt>{{$.HashLabel}}</dt><dd class="doc-review-hash">{{.VersionHash}}</dd></div>
<div><dt>{{$.ScopeLabel}}</dt><dd>{{.Scope}}</dd></div>
</dl>
{{if .Diff}}<h3>{{$.DiffLabel}}</h3><pre class="doc-review-diff">{{.Diff}}</pre>{{end}}
{{if or .CanReview .CanDeploy}}<div class="doc-review-actions">
{{if .CanReview}}<form method="post" action="{{.ReviewAction}}" class="doc-action-form doc-review-form">
<input type="hidden" name="document_id" value="{{.DocumentID}}">
<input type="hidden" name="version_id" value="{{.VersionID}}">
<input type="hidden" name="version_hash" value="{{.VersionHash}}">
<button type="submit">{{$.ReviewLabel}}</button>
</form>{{end}}
{{if .CanDeploy}}<form method="post" action="{{.DeployAction}}" class="doc-action-form doc-deploy-form">
<input type="hidden" name="document_id" value="{{.DocumentID}}">
<input type="hidden" name="version_id" value="{{.VersionID}}">
<input type="hidden" name="version_hash" value="{{.VersionHash}}">
<button type="submit">{{$.DeployLabel}}</button>
</form>{{end}}
</div>{{else}}<p class="doc-review-unauthorized" role="status">{{$.UnauthorizedLabel}}</p>{{end}}
</li>
{{end}}</ul>
</div></body>
</html>`))

type reviewView struct {
	Lang, Dir, Title, Heading                    string
	StateLabel, HashLabel, ScopeLabel, DiffLabel string
	ReviewLabel, DeployLabel, UnauthorizedLabel  string
	Reviews                                      []ReviewItem
	CSS                                          template.CSS
}

// RenderReview renders the reviewer/publisher deployment-controls document.
func RenderReview(p ReviewPage) (string, error) {
	var out strings.Builder
	err := reviewTemplate.Execute(&out, reviewView{
		Lang: langOf(p.Locale), Dir: dirOf(p.Locale), Title: p.Title,
		Heading: reviewText(p.Locale, "heading"), StateLabel: reviewText(p.Locale, "state"),
		HashLabel: reviewText(p.Locale, "hash"), ScopeLabel: reviewText(p.Locale, "scope"),
		DiffLabel: reviewText(p.Locale, "diff"), ReviewLabel: reviewText(p.Locale, "review"),
		DeployLabel: reviewText(p.Locale, "deploy"), UnauthorizedLabel: reviewText(p.Locale, "unauthorized"),
		Reviews: p.Reviews, CSS: template.CSS(reviewCSS),
	})
	return out.String(), err
}
