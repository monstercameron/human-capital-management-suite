// HUB-032 browser evidence renderer. It renders the workspace document hub
// as a standalone HTML document so a real browser can prove that private
// drafts, explicitly shared pages, and team/channel official guidance are
// visually and textually distinguishable, at desktop and narrow widths.
// This mirrors internal/humanwork/productui's docsPage/docsDetail hub
// contract (owner, deployed version, official scope, review date, sharing
// state) without depending on the GWC runtime that package needs.
package docs

import (
	"html/template"
	"strings"
)

// HubDocument is one document card in the workspace hub list.
type HubDocument struct {
	ID, Title, Owner, Version, Scope, ReviewDue, Sharing string
	// Status is "private", "shared", "team_official", or "channel_official";
	// it drives the docs-kind-<status> class the browser spec asserts on.
	Status string
}

// HubPage is the workspace document hub contract.
type HubPage struct {
	Locale, Title string
	Documents     []HubDocument
}

var hubLabels = map[string]map[string]string{
	"en-US": {
		"heading": "Documents", "owner": "Owner", "version": "Deployed version",
		"scope": "Official scope", "review": "Review due", "sharing": "Sharing",
		"private": "Personal", "shared": "Shared", "team_official": "Team guidance", "channel_official": "Channel guidance",
	},
	"de-DE": {
		"heading": "Dokumente", "owner": "Verantwortlich", "version": "Veröffentlichte Version",
		"scope": "Offizieller Bereich", "review": "Prüfung fällig", "sharing": "Freigabe",
		"private": "Persönlich", "shared": "Geteilt", "team_official": "Teamleitfaden", "channel_official": "Kanalleitfaden",
	},
	"ar": {
		"heading": "المستندات", "owner": "المالك", "version": "النسخة المنشورة",
		"scope": "النطاق الرسمي", "review": "موعد المراجعة", "sharing": "المشاركة",
		"private": "شخصي", "shared": "مشترك", "team_official": "إرشادات الفريق", "channel_official": "إرشادات القناة",
	},
}

func hubText(locale, key string) string {
	if m, ok := hubLabels[locale]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	return hubLabels["en-US"][key]
}

var hubTemplate = template.Must(template.New("hub").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head><meta charset="utf-8"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body><div class="doc-wrap">
<h1>{{.Heading}}: {{.Title}}</h1>
<ul class="doc-hub-list">
{{range .Documents}}<li class="doc-hub-item docs-kind-{{.Status}}">
<h2>{{.Title}}</h2>
<span class="doc-hub-status-badge">{{.StatusLabel}}</span>
<dl class="doc-hub-facts">
<div><dt>{{$.OwnerLabel}}</dt><dd>{{.Owner}}</dd></div>
{{if .Version}}<div><dt>{{$.VersionLabel}}</dt><dd>{{.Version}}</dd></div>{{end}}
{{if .Scope}}<div><dt>{{$.ScopeLabel}}</dt><dd>{{.Scope}}</dd></div>{{end}}
{{if .ReviewDue}}<div><dt>{{$.ReviewLabel}}</dt><dd>{{.ReviewDue}}</dd></div>{{end}}
{{if .Sharing}}<div><dt>{{$.SharingLabel}}</dt><dd>{{.Sharing}}</dd></div>{{end}}
</dl>
</li>
{{end}}</ul>
</div></body>
</html>`))

const hubCSS = `.doc-hub-list{list-style:none;margin:0;padding:0;display:grid;gap:1rem}` +
	`.doc-hub-item{border:0.0625rem solid #d0d5dd;border-radius:0.5rem;padding:1rem}` +
	`.doc-hub-item.docs-kind-private{border-inline-start:0.25rem solid #6b7280}` +
	`.doc-hub-item.docs-kind-shared{border-inline-start:0.25rem solid #2563eb}` +
	`.doc-hub-item.docs-kind-team_official{border-inline-start:0.25rem solid #15803d}` +
	`.doc-hub-item.docs-kind-channel_official{border-inline-start:0.25rem solid #b45309}` +
	`.doc-hub-facts{display:grid;grid-template-columns:auto 1fr;gap:0.25rem 0.75rem;margin:0.5rem 0 0}` +
	`.doc-hub-facts dt{font-weight:700}.doc-hub-facts dd{margin:0}` +
	`@media (max-width:40rem){.doc-hub-facts{grid-template-columns:1fr}}` +
	`:focus-visible{outline:0.1875rem solid #1a56db;outline-offset:0.125rem}` +
	`@media (prefers-reduced-motion:reduce){*{animation:none;transition:none}}`

type hubDocumentView struct {
	HubDocument
	StatusLabel string
}

type hubView struct {
	Lang, Dir, Title, Heading                                       string
	OwnerLabel, VersionLabel, ScopeLabel, ReviewLabel, SharingLabel string
	Documents                                                       []hubDocumentView
	CSS                                                             template.CSS
}

// RenderHub renders the workspace document hub document.
func RenderHub(p HubPage) (string, error) {
	docs := make([]hubDocumentView, 0, len(p.Documents))
	for _, d := range p.Documents {
		docs = append(docs, hubDocumentView{HubDocument: d, StatusLabel: hubText(p.Locale, d.Status)})
	}
	var out strings.Builder
	err := hubTemplate.Execute(&out, hubView{
		Lang: langOf(p.Locale), Dir: dirOf(p.Locale), Title: p.Title,
		Heading: hubText(p.Locale, "heading"), OwnerLabel: hubText(p.Locale, "owner"),
		VersionLabel: hubText(p.Locale, "version"), ScopeLabel: hubText(p.Locale, "scope"),
		ReviewLabel: hubText(p.Locale, "review"), SharingLabel: hubText(p.Locale, "sharing"),
		Documents: docs, CSS: template.CSS(hubCSS),
	})
	return out.String(), err
}
