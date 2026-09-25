package productui

import (
	"strings"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsMarkdownBodyProps is everything the rendered text depends on. Every
// field is a string, so the props compare by value: when the document page
// re-renders for a star, a notice or a comment, the body bails out and the
// Markdown and its diagrams are neither parsed nor diffed again.
type docsMarkdownBodyProps struct {
	Locale, VersionID, Markdown string
	// ChatRefs is the reader's resolved chat references, encoded
	// (docs_chat_chips.go) so the props still compare by value.
	ChatRefs string
	// Links is the reader's authorized view of this version's doc: targets,
	// encoded (docs_markdown.go, HUB-035) so the props still compare by value.
	Links string
	// ProjectTasks carries only authorized Docs task previews; a refreshed
	// projection replaces this value so revocation removes titles immediately.
	ProjectTasks string
	// Journeys is the encoded authorized journey previews.
	Journeys string
	Navigate func(string)
	// DocumentID and Media let attachment references load their files;
	// Media is one stable pointer for the page, so it compares by value too.
	DocumentID string
	Media      *DocumentMediaPort
	// Origin is the workspace origin, so an absolute project address copied
	// from the address bar is recognised as this workspace's own.
	Origin string
}

// docsMarkdownBody is the reader's text. Its nodes are built once per
// document version and locale.
func docsMarkdownBody(props docsMarkdownBodyProps) ui.Node {
	nodes := ui.UseMemo(func() []ui.Node {
		view := View{Locale: LocaleContext{Resolved: props.Locale}, DocumentID: props.DocumentID, DocumentMedia: props.Media, DocumentOrigin: props.Origin}
		if props.ChatRefs != "" || props.Links != "" || props.ProjectTasks != "" || props.Journeys != "" {
			view.Document = &DocumentDetail{Chat: decodeDocsChatRefs(props.ChatRefs), Links: decodeDocsLinks(props.Links), ProjectTasks: decodeDocsProjectTasks(props.ProjectTasks), Journeys: decodeDocsJourneys(props.Journeys)}
		}
		view.Navigate = props.Navigate
		return docsASTMarkdownNodes(view, props.Markdown)
	}, props.Locale, props.VersionID, props.Markdown, props.ChatRefs, props.Links, props.ProjectTasks, props.Journeys, props.Navigate, props.DocumentID, props.Media, props.Origin)
	return html.Div(html.Props{ID: "docs-markdown", Class: "docs-markdown", Dir: docsContentDirection(props.Markdown), Raw: map[string]any{"tabindex": "0"}}, nodes...)
}

// docsContentDirection is the direction the document is written in, from
// its first strongly directional letter: "rtl" for Arabic or Hebrew text,
// "ltr" for other letters, "" (inherit the interface) when there are none.
// The text column, its margins and the pin gutter then follow the content;
// each block is still dir="auto" for mixed documents. It is decided here,
// not by dir="auto" on the column, because that skips every child with its
// own dir attribute and would always fall back to left-to-right.
func docsContentDirection(markdown string) string {
	for _, r := range markdown {
		switch {
		case unicode.In(r, unicode.Arabic, unicode.Hebrew, unicode.Syriac, unicode.Thaana, unicode.Nko):
			return "rtl"
		case unicode.IsLetter(r):
			return "ltr"
		}
	}
	return ""
}

// docsOwnsPageHeading reports whether the page renders its own h1: an open
// document is titled by its name, not by the shell's "Documents" head.
// docsOwnsPageHeading reports whether Docs renders its own h1#page-title
// instead of the shell's generic "Documents" head (D-7): the reader always
// did (its document title), and the library list does too now, since its
// own heading already states the current collection with its count —
// repeating that as a second, generic "Documents" head above it said
// nothing new.
func docsOwnsPageHeading(view View) bool {
	return view.Page == PageDocs
}

// docsLocaleDigits writes a number the way the locale writes its dates.
// Arabic dates come out of LocaleContext.FormatDate in Arabic-Indic digits,
// so clock times, counts and comment numbers use them too.
func docsLocaleDigits(locale, value string) string {
	if !strings.HasPrefix(locale, "ar") {
		return value
	}
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '٠' + r - '0'
		}
		return r
	}, value)
}

// docsReaderStylesheet holds the document reader's layout fixes: the page
// title as the h1, the Copy text button kept out of the text on narrow
// readers, readable comment pins placed at the end of the text column, the
// floating Comment button out of the tab order while hidden, a draft
// highlight for the passage being commented on, and authored content laid
// out in its own direction inside an RTL interface.
func docsReaderStylesheet() string {
	return `
.docs-detail-title-row h1{flex:1 1 20rem;min-width:0;margin:0;font-size:clamp(1.5rem,1.2rem + 1vw,2rem);line-height:1.2;overflow-wrap:anywhere}
.docs-detail-title-row h1:focus{outline:none}
.docs-detail-title-row h1:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-reader{display:flow-root}
.docs-copy-text{position:relative;inset:auto;float:inline-end;margin-block:0 var(--hcm-space-1);margin-inline:var(--hcm-space-2) 0}
.docs-markdown :is(p,li,h2,h3,h4,h5,h6,td,th,blockquote),.docs-comment-body,.docs-thread-quote q,.docs-compose-quote q{unicode-bidi:plaintext;text-align:start}
.docs-select-comment:not(.is-visible){visibility:hidden;transition:opacity var(--hcm-motion-fast,.14s),visibility 0s linear var(--hcm-motion-fast,.14s)}
.docs-select-comment.is-visible{visibility:visible;transition:opacity var(--hcm-motion-fast,.14s),visibility 0s}
.docs-anchor-gutter{width:1.75rem}
.docs-anchor-pin{width:1.5rem;height:1.5rem;font-size:.75rem}
.docs-anchor-pin:hover,.docs-anchor-pin.is-linked{background:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 38%,var(--surface));color:var(--ink);box-shadow:0 0 0 2px var(--hcm-color-warning,var(--warning))}
.docs-thread.is-linked .docs-thread-num{background:color-mix(in srgb,var(--hcm-color-warning,var(--warning)) 38%,var(--surface));color:var(--ink);box-shadow:0 0 0 2px var(--hcm-color-warning,var(--warning))}
.docs-markdown{padding-inline-end:2rem}
.docs-link-unavailable{color:var(--muted);text-decoration-style:dashed}
.docs-link-state{font-size:var(--hcm-font-size-small);color:var(--muted)}
#docs-markdown::highlight(docs-anchor-draft),#docs-markdown ::highlight(docs-anchor-draft){background-color:color-mix(in srgb,var(--accent) 26%,transparent)}
@media (pointer:coarse){
  .docs-anchor-pin::before{content:"";position:absolute;inset:-10px;border-radius:999px}
}
@media (prefers-reduced-motion:reduce){
  .docs-anchor-pin{transition:none}
  .docs-anchor-pin:hover,.docs-anchor-pin.is-linked{transform:none}
  .docs-select-comment,.docs-select-comment:not(.is-visible),.docs-select-comment.is-visible{transition:none}
}
`
}
