package docs

import (
	"html/template"
	"strings"
)

// PickerDoc is one linkable document: its stable id and human title.
type PickerDoc struct {
	ID, Title string
}

// PickerPage is the link picker contract. Only document ids are
// submitted, never titles, so inserted links stay stable across renames.
type PickerPage struct {
	Locale, Title string
	Docs          []PickerDoc
}

// BacklinkEntry is one rendered backlink. Restricted entries carry no
// source title: the reader learns a link exists, never from whom.
type BacklinkEntry struct {
	SourceDocID, SourceTitle, Label, State string
	Restricted                             bool
}

// BacklinksPage is the backlinks view contract.
type BacklinksPage struct {
	Locale, Title, TargetDocID string
	Links                      []BacklinkEntry
}

var pickerLabels = map[string]map[string]string{
	"en-US": {
		"heading":  "Insert a document link",
		"target":   "Target document",
		"help":     "Links use stable document ids shaped doc:id, with an optional #anchor. Titles are never stored in links.",
		"block":    "Anchor (optional)",
		"submit":   "Insert link",
		"backhead": "Links to this document",
	},
	"de-DE": {
		"heading":  "Dokumentlink einfügen",
		"target":   "Zieldokument",
		"help":     "Links verwenden stabile Dokument-IDs der Form doc:id mit optionalem #anchor. Titel werden nie in Links gespeichert.",
		"block":    "Anker (optional)",
		"submit":   "Link einfügen",
		"backhead": "Links auf dieses Dokument",
	},
}

func pickerText(locale, key string) string {
	if m, ok := pickerLabels[locale]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	return pickerLabels["en-US"][key]
}

var pickerTemplate = template.Must(template.New("picker").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head><meta charset="utf-8"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body><div class="doc-wrap">
<h1>{{.Heading}}: {{.Title}}</h1>
<form method="post" action="{{.Action}}">
<label class="doc-field" for="target-doc">{{.TargetLabel}}</label>
<p id="picker-help">{{.Help}}</p>
<select id="target-doc" name="target_doc" aria-describedby="picker-help">
{{range .Docs}}<option value="{{.ID}}">{{.Title}}</option>
{{end}}</select>
<label class="doc-field" for="target-block">{{.BlockLabel}}</label>
<input id="target-block" name="target_block" type="text" autocomplete="off">
<p><button type="submit">{{.Submit}}</button></p>
</form>
</div></body>
</html>`))

var backlinksTemplate = template.Must(template.New("backlinks").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head><meta charset="utf-8"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body><div class="doc-wrap">
<h1>{{.Heading}}: {{.Title}}</h1>
<ul class="doc-backlinks">
{{range .Links}}<li>{{if .Restricted}}<span class="doc-restricted">Restricted</span> {{else}}<span>{{.SourceDocID}}</span> <span>{{.SourceTitle}}</span> {{end}}<span>&ldquo;{{.Label}}&rdquo;</span> <span class="doc-state">State: {{.State}}</span></li>
{{end}}</ul>
</div></body>
</html>`))

type pickerView struct {
	Lang, Dir, Title, Heading, TargetLabel, Help, BlockLabel, Submit, Action string
	Docs                                                                     []PickerDoc
	CSS                                                                      template.CSS
}

// RenderPicker renders the link picker document.
func RenderPicker(p PickerPage) (string, error) {
	var out strings.Builder
	err := pickerTemplate.Execute(&out, pickerView{
		Lang: langOf(p.Locale), Dir: dirOf(p.Locale), Title: p.Title,
		Heading: pickerText(p.Locale, "heading"), TargetLabel: pickerText(p.Locale, "target"),
		Help: pickerText(p.Locale, "help"), BlockLabel: pickerText(p.Locale, "block"),
		Submit: pickerText(p.Locale, "submit"), Action: "/docs/links",
		Docs: p.Docs, CSS: template.CSS(pageCSS),
	})
	return out.String(), err
}

type backlinksView struct {
	Lang, Dir, Title, Heading string
	Links                     []BacklinkEntry
	CSS                       template.CSS
}

// RenderBacklinks renders the backlinks view document.
func RenderBacklinks(p BacklinksPage) (string, error) {
	var out strings.Builder
	err := backlinksTemplate.Execute(&out, backlinksView{
		Lang: langOf(p.Locale), Dir: dirOf(p.Locale), Title: p.Title,
		Heading: pickerText(p.Locale, "backhead"), Links: p.Links, CSS: template.CSS(pageCSS),
	})
	return out.String(), err
}
