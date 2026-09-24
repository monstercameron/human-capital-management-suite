// HUB-036 browser/accessibility evidence renderer. It renders the keyword
// and semantic document search screen as a standalone HTML document: each
// result card labels why it matched (provenance), filters are offered,
// snippets are rendered as safe escaped text (html/template autoescapes),
// and a vector-backend outage still shows lexical results with a visible
// fallback notice instead of blanking the page.
package docs

import (
	"html/template"
	"strings"
)

// SearchFilters is the viewer's selected search filters.
type SearchFilters struct {
	Team, Channel, Owner, Status, DateFrom, DateTo string
}

// SearchResult is one authorized, display-safe search hit.
type SearchResult struct {
	DocumentID, VersionID, Title, Owner, Scope, Snippet string
	// Why is the raw match reason ("keyword", "semantic", "title", "fuzzy");
	// the renderer turns it into a locale-appropriate provenance label.
	Why string
}

// SearchPage is the document search contract.
type SearchPage struct {
	Locale, Title, Query, Mode      string
	SemanticAvailable, FallbackUsed bool
	Filters                         SearchFilters
	Results                         []SearchResult
}

var searchLabels = map[string]map[string]string{
	"en-US": {
		"heading": "Search documents", "query": "Search", "keyword": "Keyword", "semantic": "Semantic",
		"fallback": "Semantic search is unavailable, so keyword search is being used.",
		"filters":  "Filters", "team": "Team", "channel": "Channel", "owner": "Owner", "status": "Status",
		"date_from": "From", "date_to": "To", "submit": "Search",
		"provenance_keyword": "Keyword match", "provenance_semantic": "Semantic match",
		"provenance_title": "Title match", "provenance_fuzzy": "Fuzzy match",
		"no_results": "No documents match this search.",
	},
	"de-DE": {
		"heading": "Dokumente durchsuchen", "query": "Suche", "keyword": "Schlüsselwort", "semantic": "Semantisch",
		"fallback": "Die semantische Suche ist nicht verfügbar; die Schlüsselwortsuche wird verwendet.",
		"filters":  "Filter", "team": "Team", "channel": "Kanal", "owner": "Verantwortlich", "status": "Status",
		"date_from": "Von", "date_to": "Bis", "submit": "Suchen",
		"provenance_keyword": "Schlüsselworttreffer", "provenance_semantic": "Semantischer Treffer",
		"provenance_title": "Titeltreffer", "provenance_fuzzy": "Unscharfer Treffer",
		"no_results": "Keine Dokumente entsprechen dieser Suche.",
	},
	"ar": {
		"heading": "البحث في المستندات", "query": "بحث", "keyword": "كلمة مفتاحية", "semantic": "دلالي",
		"fallback": "البحث الدلالي غير متاح، لذلك سيتم استخدام البحث بالكلمات المفتاحية.",
		"filters":  "عوامل التصفية", "team": "الفريق", "channel": "القناة", "owner": "المالك", "status": "الحالة",
		"date_from": "من", "date_to": "إلى", "submit": "ابحث",
		"provenance_keyword": "تطابق كلمة مفتاحية", "provenance_semantic": "تطابق دلالي",
		"provenance_title": "تطابق العنوان", "provenance_fuzzy": "تطابق تقريبي",
		"no_results": "لا توجد مستندات تطابق هذا البحث.",
	},
}

func searchText(locale, key string) string {
	if m, ok := searchLabels[locale]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	return searchLabels["en-US"][key]
}

// searchProvenanceLabel turns a raw match reason into a locale label. An
// unrecognized reason is never suppressed -- it is shown verbatim so a
// human can still see why a result appeared instead of losing that signal.
func searchProvenanceLabel(locale, why string) string {
	switch strings.ToLower(strings.TrimSpace(why)) {
	case "semantic", "meaning":
		return searchText(locale, "provenance_semantic")
	case "keyword", "text":
		return searchText(locale, "provenance_keyword")
	case "title":
		return searchText(locale, "provenance_title")
	case "fuzzy":
		return searchText(locale, "provenance_fuzzy")
	default:
		return why
	}
}

const searchCSS = `.doc-search-filters{display:flex;flex-wrap:wrap;gap:0.75rem;margin:0.75rem 0}` +
	`.doc-search-filters label{display:block;font-weight:700}` +
	`.doc-search-fallback{border-inline-start:0.25rem solid #b45309;padding-inline-start:0.75rem;margin:0.75rem 0}` +
	`.doc-search-result{border-block-start:0.0625rem solid #d0d5dd;padding-block:0.75rem}` +
	`.doc-search-provenance{display:inline-block;font-weight:700;padding:0.125rem 0.5rem;border-radius:0.25rem;background:#eef2ff}` +
	`.doc-search-snippet{margin:0.5rem 0}` +
	`@media (max-width:40rem){.doc-search-filters{flex-direction:column}}` +
	`:focus-visible{outline:0.1875rem solid #1a56db;outline-offset:0.125rem}` +
	`@media (prefers-reduced-motion:reduce){*{animation:none;transition:none}}`

var searchTemplate = template.Must(template.New("search").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head><meta charset="utf-8"><title>{{.Title}}</title><style>{{.CSS}}</style></head>
<body><div class="doc-wrap">
<h1>{{.Heading}}</h1>
<form method="get" action="/docs/search" role="search" aria-labelledby="doc-search-heading">
<h2 id="doc-search-heading" class="sr-only">{{.Heading}}</h2>
<label for="doc-search-query">{{.QueryLabel}}</label>
<input id="doc-search-query" name="q" type="search" value="{{.Query}}">
<select id="doc-search-mode" name="mode" aria-label="{{.QueryLabel}}">
<option value="keyword"{{if ne .Mode "semantic"}} selected{{end}}>{{.KeywordLabel}}</option>
<option value="semantic"{{if eq .Mode "semantic"}} selected{{end}}{{if not .SemanticAvailable}} disabled{{end}}>{{.SemanticLabel}}</option>
</select>
<fieldset class="doc-search-filters"><legend>{{.FiltersLabel}}</legend>
<div><label for="doc-search-team">{{.TeamLabel}}</label><input id="doc-search-team" name="team" value="{{.Filters.Team}}"></div>
<div><label for="doc-search-channel">{{.ChannelLabel}}</label><input id="doc-search-channel" name="channel" value="{{.Filters.Channel}}"></div>
<div><label for="doc-search-owner">{{.OwnerLabel}}</label><input id="doc-search-owner" name="owner" value="{{.Filters.Owner}}"></div>
<div><label for="doc-search-status">{{.StatusLabel}}</label><input id="doc-search-status" name="status" value="{{.Filters.Status}}"></div>
<div><label for="doc-search-date-from">{{.DateFromLabel}}</label><input id="doc-search-date-from" name="date_from" type="date" value="{{.Filters.DateFrom}}"></div>
<div><label for="doc-search-date-to">{{.DateToLabel}}</label><input id="doc-search-date-to" name="date_to" type="date" value="{{.Filters.DateTo}}"></div>
</fieldset>
<button type="submit">{{.SubmitLabel}}</button>
</form>
{{if .FallbackUsed}}<p class="doc-search-fallback" role="status">{{.FallbackLabel}}</p>{{end}}
{{if .Results}}<ul class="doc-search-results">
{{range .Results}}<li class="doc-search-result" data-document-id="{{.DocumentID}}">
<h3>{{.Title}}</h3>
<span class="doc-search-provenance">{{.ProvenanceLabel}}</span>
<p>{{.Owner}} &middot; {{.Scope}}</p>
<p class="doc-search-snippet">{{.Snippet}}</p>
</li>
{{end}}</ul>{{else}}<p role="status">{{.NoResultsLabel}}</p>{{end}}
</div></body>
</html>`))

type searchResultView struct {
	SearchResult
	ProvenanceLabel string
}

type searchView struct {
	Lang, Dir, Title, Heading, Query, Mode                          string
	QueryLabel, KeywordLabel, SemanticLabel, FiltersLabel           string
	TeamLabel, ChannelLabel, OwnerLabel, StatusLabel, DateFromLabel string
	DateToLabel, SubmitLabel, FallbackLabel, NoResultsLabel         string
	SemanticAvailable, FallbackUsed                                 bool
	Filters                                                         SearchFilters
	Results                                                         []searchResultView
	CSS                                                             template.CSS
}

// RenderSearch renders the document search document.
func RenderSearch(p SearchPage) (string, error) {
	results := make([]searchResultView, 0, len(p.Results))
	for _, r := range p.Results {
		results = append(results, searchResultView{SearchResult: r, ProvenanceLabel: searchProvenanceLabel(p.Locale, r.Why)})
	}
	var out strings.Builder
	err := searchTemplate.Execute(&out, searchView{
		Lang: langOf(p.Locale), Dir: dirOf(p.Locale), Title: p.Title, Query: p.Query, Mode: p.Mode,
		Heading: searchText(p.Locale, "heading"), QueryLabel: searchText(p.Locale, "query"),
		KeywordLabel: searchText(p.Locale, "keyword"), SemanticLabel: searchText(p.Locale, "semantic"),
		FiltersLabel: searchText(p.Locale, "filters"), TeamLabel: searchText(p.Locale, "team"),
		ChannelLabel: searchText(p.Locale, "channel"), OwnerLabel: searchText(p.Locale, "owner"),
		StatusLabel: searchText(p.Locale, "status"), DateFromLabel: searchText(p.Locale, "date_from"),
		DateToLabel: searchText(p.Locale, "date_to"), SubmitLabel: searchText(p.Locale, "submit"),
		FallbackLabel: searchText(p.Locale, "fallback"), NoResultsLabel: searchText(p.Locale, "no_results"),
		SemanticAvailable: p.SemanticAvailable, FallbackUsed: p.FallbackUsed,
		Filters: p.Filters, Results: results, CSS: template.CSS(searchCSS),
	})
	return out.String(), err
}
