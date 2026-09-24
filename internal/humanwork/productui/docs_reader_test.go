package productui

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func docsReaderTestView(locale string) View {
	view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale(locale))
	view.Document = &DocumentDetail{
		Summary:    DocumentSummary{ID: "doc-42", Title: "Leave policy", VersionID: "version-7", OwnerID: "owner-1"},
		Markdown:   "# Leave policy\n\nRequests take 7 business days.\n\n## Approvals\n\n- Manager signs off.\n\n> Quoted rule.\n\n| Step | Days |\n| --- | ---: |\n| Review | 7 business days |\n\n```\ncode (1)\n```\n\n## Exceptions\n\nNone.\n",
		CanComment: true,
		Comments: []DocumentComment{
			{ID: "c-1", AuthorID: "worker-1", Author: "Riley Chen", Body: "Is this still here during budget season.", CreatedAt: "2026-09-22T10:00:00Z", VersionID: "version-7", Quote: "7 business days", Start: 20},
		},
	}
	view.AddDocumentComment = func(DocumentCommentCreateRequest, func(error)) {}
	return view
}

// M15: an open document is titled by its own h1 (the router's #page-title),
// the shell does not repeat the "Documents" head above it, and the reader,
// outline and comments are h2 sections with the text's headings beneath.
func TestDocsReader_DocumentTitleIsThePageHeading(t *testing.T) {
	doc, err := Render(docsReaderTestView("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="page-title"`, `>Leave policy</h1>`, `<h2[^>]*id="docs-reader-heading"`, `<h2[^>]*id="docs-outline-heading"`, `<h2[^>]*id="docs-comments-heading"`, `id="sec-approvals"`} {
		if !regexp.MustCompile(want).MatchString(doc) {
			t.Fatalf("document page omitted %q", want)
		}
	}
	if !regexp.MustCompile(`(?i)<h1[^>]*id="page-title"[^>]*tabindex="-1"|<h1[^>]*tabindex="-1"[^>]*id="page-title"`).MatchString(doc) {
		t.Fatal("document title is not a focusable h1#page-title")
	}
	if strings.Count(doc, `id="page-title"`) != 1 {
		t.Fatalf("page-title rendered %d times", strings.Count(doc, `id="page-title"`))
	}
	if strings.Contains(doc, `class="page-head"`) || strings.Contains(doc, "Browse documents you are authorized to read.") {
		t.Fatal("document page repeats the Documents page head")
	}
	if !regexp.MustCompile(`<h3[^>]*id="sec-approvals"`).MatchString(doc) {
		t.Fatal(`"## Approvals" is not an h3 under the h2 reader section`)
	}
	list := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	list.DocumentsReady = true
	listDoc, err := Render(list)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listDoc, `class="page-head"`) {
		t.Fatal("the library lost its page head")
	}
	if !docsOwnsPageHeading(docsReaderTestView("en-US")) || docsOwnsPageHeading(list) {
		t.Fatal("docsOwnsPageHeading does not follow the open document")
	}
}

// H6: authored content keeps its own direction inside the Arabic interface.
func TestDocsReader_AuthoredContentUsesAutoDirection(t *testing.T) {
	doc, err := Render(docsReaderTestView("ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{
		`<p[^>]*dir="auto"[^>]*>Requests take 7 business days.`,
		`<ul[^>]*dir="auto"`, `<li[^>]*dir="auto"`, `<blockquote[^>]*dir="auto"`,
		`<table[^>]*dir="auto"`, `<th[^>]*dir="auto"`, `<td[^>]*dir="auto"[^>]*>7 business days`,
		`<h3[^>]*dir="auto"`, `<pre[^>]*dir="ltr"`,
		`<p[^>]*class="docs-comment-body"[^>]*dir="auto"|<p[^>]*dir="auto"[^>]*class="docs-comment-body"`,
		`<q[^>]*dir="auto"[^>]*>7 business days`,
		`<textarea[^>]*dir="auto"`,
	} {
		if !regexp.MustCompile(pattern).MatchString(doc) {
			t.Fatalf("Arabic document page omitted %s", pattern)
		}
	}
	if !regexp.MustCompile(`<div[^>]*dir="ltr"[^>]*id="docs-markdown"|<div[^>]*id="docs-markdown"[^>]*dir="ltr"`).MatchString(doc) {
		t.Fatal("English document column does not run left to right in the Arabic interface")
	}
	for markdown, want := range map[string]string{"# Leave policy": "ltr", "## سياسة الإجازات\n\nEnglish": "rtl", "```\n1 + 2\n```\n": "", "- **עברית**": "rtl"} {
		if got := docsContentDirection(markdown); got != want {
			t.Fatalf("docsContentDirection(%q) = %q, want %q", markdown, got, want)
		}
	}
	css := docsStylesheet()
	for _, want := range []string{"unicode-bidi:plaintext", ".docs-comment-body,.docs-thread-quote q,.docs-compose-quote q"} {
		if !strings.Contains(css, want) {
			t.Fatalf("reader stylesheet omitted bidi fallback %q", want)
		}
	}
}

// L7: numbers on the document page use the digits the locale's dates use.
func TestDocsReader_ArabicDigitsMatchDates(t *testing.T) {
	if got := docsLocaleDigits("ar", "12:05 · 180"); got != "١٢:٠٥ · ١٨٠" {
		t.Fatalf("ar digits = %q", got)
	}
	if got := docsLocaleDigits("de-DE", "12:05"); got != "12:05" {
		t.Fatalf("de-DE digits changed: %q", got)
	}
	now := time.Now().UTC()
	ar := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale("ar")).Locale
	clock, err := ui.RenderToString(docsWhen(ar, now.Format(time.RFC3339), now))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`>[^<]*[0-9][^<]*</time>`).MatchString(clock) {
		t.Fatalf("Arabic clock kept Latin digits: %s", clock)
	}
	older, err := ui.RenderToString(docsWhen(ar, "2025-07-22T10:00:00Z", now))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`>[^<]*[0-9][^<]*</time>`).MatchString(older) {
		t.Fatalf("Arabic date kept Latin digits: %s", older)
	}
	if got := docsCount("ar", "comments_show_resolved", 12); strings.ContainsAny(got, "0123456789") {
		t.Fatalf("Arabic count kept Latin digits: %q", got)
	}
	doc, err := Render(docsReaderTestView("ar"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `docs-pin-c-1`) || !regexp.MustCompile(`id="docs-pin-c-1"[^>]*>١</button>`).MatchString(doc) {
		t.Fatal("Arabic comment pin is not numbered in Arabic-Indic digits")
	}
}

// H5: the rendered text depends only on comparable props, so the document
// page re-rendering for anything else bails out of re-parsing it.
func TestDocsReader_MarkdownBodyPropsCompareByValue(t *testing.T) {
	if !reflect.TypeOf(docsMarkdownBodyProps{}).Comparable() {
		t.Fatal("docsMarkdownBodyProps must be comparable for the reconciler to bail out")
	}
	for i := 0; i < reflect.TypeOf(docsMarkdownBodyProps{}).NumField(); i++ {
		// Strings compare by value; the one pointer (the page's stable media
		// port) compares by identity, which does not change between renders.
		if kind := reflect.TypeOf(docsMarkdownBodyProps{}).Field(i).Type.Kind(); kind != reflect.String && kind != reflect.Pointer {
			t.Fatalf("docsMarkdownBodyProps field %d is %s; a func, slice or map defeats the bailout", i, kind)
		}
	}
	doc, err := Render(docsReaderTestView("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(doc, `id="docs-markdown"`) != 1 {
		t.Fatal("reader text did not render exactly once")
	}
}

// H2, H8, M5, M16, L8: the reader stylesheet keeps Copy text out of the
// text, hides the idle Comment button from the tab order, sizes and rings
// the pins, and marks the draft passage.
func TestDocsReader_StylesheetFixes(t *testing.T) {
	css := docsStylesheet()
	for _, want := range []string{
		".docs-copy-text{position:relative;inset:auto;float:inline-end",
		".docs-reader{display:flow-root}",
		".docs-select-comment:not(.is-visible){visibility:hidden",
		".docs-anchor-pin{width:1.5rem;height:1.5rem",
		"box-shadow:0 0 0 2px var(--hcm-color-warning",
		"::highlight(docs-anchor-draft)",
		".docs-anchor-pin::before{content:\"\";position:absolute;inset:-10px",
		".docs-detail-title-row h1{",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("reader stylesheet omitted %q", want)
		}
	}
	reader := docsReaderStylesheet()
	for _, forbidden := range []string{"text-align:left", "text-align:right", "float:left", "float:right"} {
		if strings.Contains(reader, forbidden) {
			t.Fatalf("reader stylesheet uses physical %q", forbidden)
		}
	}
	if !strings.Contains(reader, "prefers-reduced-motion") {
		t.Fatal("reader motion ignores prefers-reduced-motion")
	}
	doc, err := Render(docsReaderTestView("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`id="docs-select-comment"[^>]*aria-keyshortcuts="Control\+Alt\+M"|aria-keyshortcuts="Control\+Alt\+M"[^>]*id="docs-select-comment"`).MatchString(doc) {
		t.Fatal("floating Comment button does not announce its keyboard shortcut")
	}
}
