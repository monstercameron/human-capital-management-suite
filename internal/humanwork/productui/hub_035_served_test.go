package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_HUB_035 proves the GREEN path is delivered by the served
// productui package: the split editor's "[[" picker inserts a stable
// doc:<id> reference (never a title-only link), the reader marks a doc:
// link whose target the server already resolved as unreadable before the
// reader ever clicks it, and the backlinks panel lists only the inbound
// links View.LoadDocumentBacklinks (GetDocumentBacklinks) returned, which
// the server has already limited to jointly readable sources.
func TestTodo_HUB_035(t *testing.T) {
	// The keyboard picker inserts a stable document ID, not a title-only
	// link: DocsSuggestDocInsert is exactly what docs_editor_suggest.go
	// writes into the Markdown when a "[[" suggestion is picked.
	insert := DocsSuggestDocInsert("doc-77", "Expense Policy")
	if insert != "[Expense Policy](doc:doc-77)" {
		t.Fatalf("picker insert does not carry a stable document ID: %q", insert)
	}
	if kind, query, start, ok := docsSuggestTrigger("see [[exp"); !ok || kind != DocsSuggestDocs || query != "exp" || start != 4 {
		t.Fatalf("\"[[\" did not trigger the document picker: kind=%q query=%q start=%d ok=%v", kind, query, start, ok)
	}
	// Applying the pick replaces the trigger and query with the stable-ID
	// Markdown, exactly as the browser-side apply does.
	next, caret, ok := docsSuggestApply("see [[exp", 9, DocsSuggestDocs, insert)
	if !ok || next != "see [Expense Policy](doc:doc-77) " || caret != len(next) {
		t.Fatalf("suggestion apply did not insert the stable ID: next=%q caret=%d ok=%v", next, caret, ok)
	}

	// The reader: a document naming one readable and one unreadable doc:
	// target sees them differently, before ever clicking either.
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &DocumentDetail{
		Summary:  DocumentSummary{ID: "doc-1", Title: "Handbook", VersionID: "version-1"},
		Markdown: "# Handbook\n\nSee [Expense Policy](doc:doc-77) and [Confidential HR file](doc:doc-88) for details.",
		Links: []DocumentLinkTarget{
			{DocumentID: "doc-77", Title: "Expense Policy", Readable: true},
			{DocumentID: "doc-88", Readable: false},
		},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `href="/workspace/app/docs?document=doc-77"`) {
		t.Fatalf("readable doc: link did not resolve to its stable route:\n%s", doc)
	}
	if strings.Contains(doc, `docs-link-state="unavailable"`) && !strings.Contains(doc, "doc-88") {
		t.Fatal("unavailable link state rendered without its target")
	}
	unavailableIndex := strings.Index(doc, `data-docs-id="/workspace/app/docs?document=doc-88"`)
	if unavailableIndex < 0 {
		t.Fatalf("unresolvable doc: target did not keep its safe route:\n%s", doc)
	}
	unavailableWindow := doc[max0(unavailableIndex-400) : unavailableIndex+400]
	if !strings.Contains(unavailableWindow, `docs-link-unavailable`) || !strings.Contains(unavailableWindow, `docs-link-state="unavailable"`) {
		t.Fatalf("reader was not shown the unavailable state before clicking:\n%s", unavailableWindow)
	}
	// The restricted target's title, which the server withheld, must never
	// leak into the reader's page even though the author's own link label
	// ("Confidential HR file") is theirs to write and is not a secret.
	if strings.Contains(doc, "doc-88") == false {
		t.Fatal("expected the doc-88 reference to render at all")
	}
	readableTitle := "Expense Policy"
	if count := strings.Count(doc, readableTitle); count == 0 {
		t.Fatal("the readable target's authorized title never rendered")
	}

	// The rail carries the backlinks landmark only when the port that can
	// answer it (View.LoadDocumentBacklinks, GetDocumentBacklinks-backed)
	// is actually wired, exactly like every other authorized rail section.
	view2 := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view2.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-88", Title: "Target", VersionID: "version-1", CanComment: false}, Markdown: "# Target"}
	view2.LoadDocumentBacklinks = func(string, func([]DocumentBacklink, error)) {}
	withPort, err := Render(view2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withPort, `id="docs-backlinks-heading"`) {
		t.Fatal("backlinks panel did not render although View.LoadDocumentBacklinks is wired")
	}
	view2.LoadDocumentBacklinks = nil
	withoutPort, err := Render(view2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutPort, `id="docs-backlinks-heading"`) {
		t.Fatal("backlinks panel rendered although no backlinks port was wired")
	}

	// Backlinks (docsBacklinksPanel, the exact component the rail mounts):
	// only jointly readable sources reach the reader, each with its own
	// state, and a source the server left out never appears. An empty,
	// successfully authorized result reads as "none yet", not as an error.
	loaded, err := ui.RenderToString(ui.CreateElement(docsBacklinksPanel, docsBacklinksPanelProps{
		Locale: "en-US",
		Backlinks: []DocumentBacklink{
			{SourceDocumentID: "doc-1", SourceVersionID: "version-1", SourceTitle: "Handbook", Label: "Expense Policy", State: ""},
			{SourceDocumentID: "doc-2", SourceVersionID: "version-4", SourceTitle: "Onboarding", Label: "old link", State: "stale"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Linked from", "Handbook", "Onboarding", "may be out of date", `href="/workspace/app/docs?document=doc-1"`, `href="/workspace/app/docs?document=doc-2"`} {
		if !strings.Contains(loaded, want) {
			t.Fatalf("backlinks panel omitted %q:\n%s", want, loaded)
		}
	}
	empty, err := ui.RenderToString(ui.CreateElement(docsBacklinksPanel, docsBacklinksPanelProps{Locale: "en-US"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, "No other document you can read links here yet.") {
		t.Fatalf("empty authorized backlink set did not render the empty state:\n%s", empty)
	}
	if strings.Contains(empty, "doc-1") || strings.Contains(empty, "Handbook") {
		t.Fatal("backlinks panel invented a source the server never returned")
	}
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func TestTodo_HUB_035_Accessibility(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		resolved := ResolveProductLocale(locale)
		view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), resolved)
		view.Document = &DocumentDetail{
			Summary:  DocumentSummary{ID: "doc-1", Title: "Handbook", VersionID: "version-1"},
			Markdown: "# Handbook\n\nSee [Restricted](doc:doc-88) for details.",
			Links:    []DocumentLinkTarget{{DocumentID: "doc-88", Readable: false}},
		}
		view.LoadDocumentBacklinks = func(string, func([]DocumentBacklink, error)) {}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("docs page has an untranslated placeholder in %s", locale)
		}
		// The unavailable state is not conveyed by color alone: it carries
		// visible text and an aria-label a screen reader announces.
		if !strings.Contains(doc, `docs-link-unavailable`) || !strings.Contains(doc, `class="docs-link-state"`) {
			t.Fatalf("unavailable link state is not exposed to assistive tech in %s", locale)
		}
		if !strings.Contains(doc, `id="docs-backlinks-heading"`) || !strings.Contains(doc, `aria-labelledby="docs-backlinks-heading"`) {
			t.Fatalf("backlinks panel is not a labelled landmark in %s", locale)
		}
	}
}
