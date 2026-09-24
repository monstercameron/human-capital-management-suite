// Package productui: static fixture entry points for
// tools/uxqual/cmd/genfixtures. The G-L reachability audit reopened
// HUB-033/HUB-035 because their evidence came from tools/uxqual/render/docs,
// a fixture renderer outside the shipped binary; these functions render the
// exact served components instead (docs_editor.go, docs_editor_suggest.go,
// docs_compare.go, docs_backlinks.go), so the Playwright fixtures genfixtures
// writes are the real productui output, byte for byte, not a hand-authored
// approximation of it. Every fixture here uses no-op or Readable-only ports:
// no fixture ever performs a network call, and none is reachable from the
// live app.
package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsFixtureDocument wraps one rendered component in a standalone HTML
// document carrying the product's own stylesheet, so a Playwright pass can
// assert on real layout (responsive breakpoints, focus order) without a
// live server.
func docsFixtureDocument(locale, title string, node ui.Node) (string, error) {
	body, err := ui.RenderToString(node)
	if err != nil {
		return "", err
	}
	dir := "ltr"
	if strings.HasPrefix(locale, "ar") {
		dir = "rtl"
	}
	var out strings.Builder
	out.WriteString("<!DOCTYPE html><html lang=\"")
	out.WriteString(locale)
	out.WriteString("\" dir=\"")
	out.WriteString(dir)
	out.WriteString("\"><head><meta charset=\"utf-8\"><title>")
	out.WriteString(title)
	out.WriteString("</title><style>")
	out.WriteString(docsStylesheet())
	out.WriteString("</style></head><body>")
	out.WriteString(body)
	out.WriteString("</body></html>")
	return out.String(), nil
}

// DocsEditorFixture renders the served split editor (docs_editor.go) at
// rest, for HUB-033's Browser evidence: the exact chrome, toolbar and base
// version wiring the live app mounts, with inert Save/Cancel ports so the
// fixture never calls out.
func DocsEditorFixture(locale, documentID, baseVersionID, title, markdown string) (string, error) {
	node := ui.CreateElement(docsSplitEditor, docsSplitEditorProps{
		Locale: locale, DocumentID: documentID, BaseVersionID: baseVersionID, Title: title, Markdown: markdown,
		Save: func(DocumentEditRequest, func(error)) {}, Cancel: func() {},
	})
	return docsFixtureDocument(locale, "Editor fixture", node)
}

// DocsEditorConflictFixture renders the same conflict status line the live
// editor shows once its Save port reports a stale base (the isDocumentVersionConflict
// path in docs_interactions.go), through the shared docsEditorStatusNode
// docsSplitEditor itself renders from.
func DocsEditorConflictFixture(locale string) (string, error) {
	return docsFixtureDocument(locale, "Editor conflict fixture", docsEditorStatusNode(locale, "edit_conflict", "alert"))
}

// DocsCompareFixture renders the served two-version compare grid
// (docs_compare.go's docsCompareColumn) exactly as docsCompareDialog
// renders it once a compare has loaded, for HUB-033's side-by-side and
// narrow-width Browser evidence.
func DocsCompareFixture(locale string, base, other DocumentVersionProjection) (string, error) {
	node := html.Div(html.Props{Class: "docs-compare"}, docsCompareColumn(locale, docsText(locale, "compare_base"), base), docsCompareColumn(locale, docsText(locale, "compare_other"), other))
	return docsFixtureDocument(locale, "Compare fixture", node)
}

// DocsSuggestFixture renders the served "[[" / "@" / "#" reference picker
// listbox (docs_editor_suggest.go's docsSuggestList) already open with the
// given rows, for HUB-035's Browser evidence that the keyboard picker's
// options carry the same stable IDs DocsSuggestDocInsert writes.
func DocsSuggestFixture(locale, kind string, items []DocsReferenceSuggestion) (string, error) {
	node := ui.CreateElement(docsSuggestList, docsSuggestListProps{
		Locale: locale, Initial: docsSuggestState{Open: true, Kind: kind, Items: items},
	})
	return docsFixtureDocument(locale, "Suggest fixture", node)
}

// DocsBacklinksFixture renders the served backlinks panel
// (docs_backlinks.go) with the given already-authorized rows, for HUB-035's
// Browser evidence that link states read as text at desktop and narrow
// widths.
func DocsBacklinksFixture(locale string, backlinks []DocumentBacklink) (string, error) {
	node := ui.CreateElement(docsBacklinksPanel, docsBacklinksPanelProps{Locale: locale, Backlinks: backlinks})
	return docsFixtureDocument(locale, "Backlinks fixture", node)
}

// docsFixtureView builds the same NewView/ApplyLocale projection the
// PRIMARY, Security and Accessibility tests for HUB-032/034/036 already use
// (docs_page_test.go), so these fixtures and those Go tests exercise one
// identical presentation model.
func docsFixtureView(locale string) View {
	return ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale(locale))
}

// DocsHubFixture renders the served workspace document hub through the
// exact same path the live app and TestTodo_HUB_032 use: productui.Render
// over docsPage/docsLibrary (docs_page.go, docs_library.go), not a
// hand-authored approximation of it. This is HUB-032's Browser evidence for
// private-vs-team-official cards (owner, deployed version, official scope,
// review date, sharing state), reopened because its prior evidence came
// from tools/uxqual/render/docs, outside the shipped binary.
func DocsHubFixture(locale string, documents []DocumentSummary) (string, error) {
	view := docsFixtureView(locale)
	view.DocumentsReady = true
	view.Documents = documents
	return Render(view)
}

// DocsReviewFixture renders the served reviewer/publisher deployment
// controls (docsReviewControls in docs_interactions.go) through
// productui.Render, the same path TestTodo_HUB_034 and TestTodo_HUB_034_Security
// use: for an authorized reviewer/publisher, review and deploy forms for one
// item always carry the identical version hash from the same
// DocumentReviewProjection, so the UI itself cannot present one hash to the
// reviewer and submit another to deploy. This is HUB-034's Browser evidence,
// reopened because its prior evidence came from tools/uxqual/render/docs,
// outside the shipped binary.
func DocsReviewFixture(locale string, reviews []DocumentReviewProjection) (string, error) {
	view := docsFixtureView(locale)
	view.DocumentsReady = true
	view.DocumentReviews = reviews
	return Render(view)
}

// DocsSearchFixture renders the served keyword/semantic document search
// screen (docsSearch in docs_interactions.go) through productui.Render, the
// same path TestTodo_HUB_036 and TestTodo_HUB_036_Accessibility use: result
// provenance labels, filters, html/template-safe escaped snippets, and a
// visible fallback notice when semantic search is unavailable. This is
// HUB-036's Browser evidence, reopened because its prior evidence came from
// tools/uxqual/render/docs, outside the shipped binary.
func DocsSearchFixture(locale string, search DocumentSearchProjection) (string, error) {
	view := docsFixtureView(locale)
	view.DocumentsReady = true
	view.DocumentSearch = search
	return Render(view)
}
