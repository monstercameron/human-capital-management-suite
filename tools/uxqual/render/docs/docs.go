// Package docs renders the documentation-hub candidate editor and version
// compare flow as standalone HTML documents. The editor submits new
// candidates with their expected base and announces base conflicts; the
// compare view renders two immutable versions with no editing surface, so
// UI edits can never touch deployed bytes. Compare bodies arrive as
// pre-rendered safe HTML from the server-side Markdown renderer.
package docs

import (
	"html/template"
	"strings"
)

// EditorPage is the candidate editor contract.
type EditorPage struct {
	Locale, Title, DocumentID             string
	BaseVersionID, BaseHash, Draft, Error string
	Action                                string
	Conflict                              *BaseConflict
}

// BaseConflict names the base the author started from and the version live now.
type BaseConflict struct {
	ExpectedVersionID, LiveVersionID string
}

// CompareVersion is one immutable side of a comparison.
type CompareVersion struct {
	VersionID, ShortHash, Title string
	BodyHTML                    template.HTML
}

// ComparePage is the version compare contract.
type ComparePage struct {
	Locale, Title string
	Base, Other   CompareVersion
}

var labels = map[string]map[string]string{
	"en-US": {
		"heading":  "Propose a candidate",
		"body":     "Markdown body",
		"submit":   "Submit candidate",
		"conflict": "Base moved while you edited.",
	},
	"de-DE": {
		"heading":  "Kandidaten vorschlagen",
		"body":     "Markdown-Text",
		"submit":   "Kandidaten einreichen",
		"conflict": "Die Basis hat sich während der Bearbeitung geändert.",
	},
}

func text(locale, key string) string {
	if m, ok := labels[locale]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	return labels["en-US"][key]
}

func langOf(locale string) string {
	if locale == "" {
		return "en-US"
	}
	return locale
}

func dirOf(locale string) string {
	if strings.HasPrefix(locale, "ar") {
		return "rtl"
	}
	return "ltr"
}

const pageCSS = `.doc-wrap{max-width:56rem;margin:0 auto;padding:1rem}` +
	`.doc-field{display:block;margin:1rem 0}` +
	`.doc-field textarea{width:100%;min-height:16rem}` +
	`.doc-error{color:#7a1f1f}` +
	`.doc-conflict{border:0.125rem solid #7a1f1f;padding:1rem;margin:1rem 0}` +
	`.doc-compare{display:grid;grid-template-columns:1fr 1fr;gap:1rem}` +
	`@media (max-width:40rem){.doc-compare{grid-template-columns:1fr}}` +
	`label{font-weight:700}` +
	`:focus-visible{outline:0.1875rem solid #1a56db;outline-offset:0.125rem}` +
	`@media (prefers-reduced-motion:reduce){*{animation:none;transition:none}}`

var editorTemplate = template.Must(template.New("editor").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head><meta charset="utf-8"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body><div class="doc-wrap">
<h1>{{.Heading}}: {{.Title}}</h1>
{{if .Conflict}}<div class="doc-conflict" role="alert"><strong>{{.ConflictText}}</strong> <span>Expected {{.Conflict.ExpectedVersionID}}, live now {{.Conflict.LiveVersionID}}.</span></div>{{end}}
<form method="post" action="{{.Action}}">
<input type="hidden" name="base_version" value="{{.BaseVersionID}}">
<input type="hidden" name="base_hash" value="{{.BaseHash}}">
<label class="doc-field" for="markdown">{{.BodyLabel}}</label>
<textarea id="markdown" name="markdown"{{if .Error}} aria-describedby="markdown-error"{{end}}>{{.Draft}}</textarea>
{{if .Error}}<p class="doc-error" id="markdown-error">{{.Error}}</p>{{end}}
<p><button type="submit">{{.Submit}}</button></p>
</form>
</div></body>
</html>`))

var compareTemplate = template.Must(template.New("compare").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head><meta charset="utf-8"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body><div class="doc-wrap">
<h1>{{.Title}}</h1>
<div class="doc-compare">
<section aria-label="{{.Base.VersionID}}"><h2>{{.Base.VersionID}} <code>{{.Base.ShortHash}}</code></h2>{{.Base.BodyHTML}}</section>
<section aria-label="{{.Other.VersionID}}"><h2>{{.Other.VersionID}} <code>{{.Other.ShortHash}}</code></h2>{{.Other.BodyHTML}}</section>
</div>
</div></body>
</html>`))

type editorView struct {
	Lang, Dir, Title, Heading, BodyLabel, Submit, ConflictText, Action string
	DocumentID, BaseVersionID, BaseHash, Draft, Error                  string
	Conflict                                                           *BaseConflict
	CSS                                                                template.CSS
}

// RenderEditor renders the candidate editor document.
func RenderEditor(p EditorPage) (string, error) {
	var out strings.Builder
	err := editorTemplate.Execute(&out, editorView{
		Lang: langOf(p.Locale), Dir: dirOf(p.Locale), Title: p.Title,
		Heading: text(p.Locale, "heading"), BodyLabel: text(p.Locale, "body"),
		Submit: text(p.Locale, "submit"), ConflictText: text(p.Locale, "conflict"),
		Action: p.Action, DocumentID: p.DocumentID, BaseVersionID: p.BaseVersionID,
		BaseHash: p.BaseHash, Draft: p.Draft, Error: p.Error, Conflict: p.Conflict,
		CSS: template.CSS(pageCSS),
	})
	return out.String(), err
}

type compareView struct {
	Lang, Dir, Title string
	Base, Other      CompareVersion
	CSS              template.CSS
}

// RenderCompare renders the two-version comparison document.
func RenderCompare(p ComparePage) (string, error) {
	var out strings.Builder
	err := compareTemplate.Execute(&out, compareView{
		Lang: langOf(p.Locale), Dir: dirOf(p.Locale), Title: p.Title,
		Base: p.Base, Other: p.Other, CSS: template.CSS(pageCSS),
	})
	return out.String(), err
}
