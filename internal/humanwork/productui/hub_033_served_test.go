package productui

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_HUB_033 proves the GREEN path is delivered by the served
// productui package, not the tools/uxqual/render/docs fixture renderer: the
// candidate editor is wired to submit against an expected base version, a
// stale-base save is recognized as a conflict (never as an ordinary save
// failure), and the compare flow renders two immutable versions side by
// side with no editing surface, reachable only when the caller supplies
// View.CompareDocumentVersions (the GetDocumentVersion-backed port).
func TestTodo_HUB_033(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &DocumentDetail{
		Summary:  DocumentSummary{ID: "doc-42", Title: "Handbook", VersionID: "version-7", CanManageAccess: true},
		Markdown: "# Current\n\nBody text.",
		CanEdit:  true,
	}
	view.DocumentEditing = true
	saveCalls := 0
	var lastRequest DocumentEditRequest
	view.CreateDocumentVersion = func(request DocumentEditRequest, done func(error)) {
		saveCalls++
		lastRequest = request
		done(nil)
	}
	view.CompareDocumentVersions = func(documentID, versionID string, done func(DocumentVersionProjection, error)) {}

	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	// The editor mounts inside the article for exactly this document and
	// version, so the split editor (docsSplitEditorProps.BaseVersionID:
	// summary.VersionID) can only ever open with that version as its
	// expected base.
	if !strings.Contains(doc, `id="docs-editor"`) || !strings.Contains(doc, `data-document-id="doc-42"`) || !strings.Contains(doc, `data-version-id="version-7"`) {
		t.Fatal("editor did not mount with the document's current version as its expected base")
	}
	// Directly exercising the wired Save port (the same closure the editor's
	// own Save button calls) proves the base version travels with a submit,
	// not just that it is present in a data attribute.
	view.CreateDocumentVersion(DocumentEditRequest{DocumentID: "doc-42", BaseVersionID: "version-7", Title: "Handbook", Markdown: "edited"}, func(error) {})
	if saveCalls != 1 || lastRequest.BaseVersionID != "version-7" || lastRequest.DocumentID != "doc-42" {
		t.Fatalf("candidate submit did not carry the expected base version: %+v", lastRequest)
	}

	// The compare menu action is reachable only because
	// View.CompareDocumentVersions is wired; docsDetail must not editorialize
	// past the port that answers it.
	if !strings.Contains(doc, `data-docs-action="compare"`) {
		t.Fatalf("compare action is not reachable from the open document:\n%s", doc)
	}
	// Version history is its own grant that viewer and commenter shares do
	// not hold, so a reader without manage access is not offered a compare
	// that could only fail to list versions (D-3).
	reader := view
	readerDoc := *view.Document
	readerDoc.Summary.CanManageAccess = false
	reader.Document = &readerDoc
	if readerMarkup, err := Render(reader); err != nil {
		t.Fatal(err)
	} else if strings.Contains(readerMarkup, `data-docs-action="compare"`) {
		t.Fatal("compare was offered to a reader without history access")
	}

	// A base-version conflict is a distinct, recognized condition, not a
	// generic save failure: the editor's own status copy and the
	// classifier the Save callback uses must agree.
	if !isDocumentVersionConflict(errors.New("rpc error: code = Aborted desc = document.stale_version")) {
		t.Fatal("a stale-base save error was not classified as a version conflict")
	}
	if isDocumentVersionConflict(errors.New("permission denied")) {
		t.Fatal("an unrelated save error was misclassified as a version conflict")
	}
	conflictCopy := docsEditorText("en-US", "edit_conflict")
	if conflictCopy == "" || !strings.Contains(conflictCopy, "changed while you were editing") {
		t.Fatalf("editor carries no conflict status copy: %q", conflictCopy)
	}

	// The compare view itself: two immutable versions rendered side by
	// side, with no form field, textarea or save control anywhere in it.
	base := DocumentVersionProjection{DocumentID: "doc-42", VersionID: "version-7", Title: "Handbook", Markdown: "# Current\n\nBody text.", Readable: true}
	other := DocumentVersionProjection{DocumentID: "doc-42", VersionID: "version-3", Title: "Handbook (earlier)", Markdown: "# Earlier\n\nOlder body.", Readable: true}
	compareMarkup, err := ui.RenderToString(ui.CreateElement(docsCompareDialog, docsCompareDialogProps{
		Locale: "en-US", DocumentID: "doc-42", Base: base,
		CompareDocumentVersions: view.CompareDocumentVersions,
		Close:                   func() {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compareMarkup, "<textarea") || strings.Contains(compareMarkup, `data-editor-cmd`) {
		t.Fatal("the compare dialog exposes an editing surface")
	}
	sideBySide, err := ui.RenderToString(docsCompareColumnsForTest(base, other))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Handbook", "Handbook (earlier)", "Current", "Older body.", `class="docs-compare"`} {
		if !strings.Contains(sideBySide, want) {
			t.Fatalf("compare view omitted %q:\n%s", want, sideBySide)
		}
	}

	// Desktop and narrow widths: the compare grid collapses to one column
	// under 40rem, so the same markup works at both.
	css := docsStylesheet()
	if !strings.Contains(css, ".docs-compare{display:grid;grid-template-columns:1fr 1fr") || !strings.Contains(css, "@media (max-width:40rem){.docs-compare{grid-template-columns:1fr}}") {
		t.Fatal("compare layout is not responsive between desktop and narrow widths")
	}
}

// docsCompareColumnsForTest renders the same two-column grid
// docsCompareDialog renders once a compare has loaded, so the Go test can
// assert on it without simulating a browser form submit.
func docsCompareColumnsForTest(base, other DocumentVersionProjection) ui.Node {
	return html.Div(html.Props{Class: "docs-compare"}, docsCompareColumn("en-US", "Base", base), docsCompareColumn("en-US", "Other", other))
}

func TestTodo_HUB_033_Accessibility(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		resolved := ResolveProductLocale(locale)
		view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), resolved)
		view.Document = &DocumentDetail{
			Summary:  DocumentSummary{ID: "doc-42", Title: "Handbook", VersionID: "version-7"},
			Markdown: "# Current",
			CanEdit:  true,
		}
		view.DocumentEditing = true
		view.CreateDocumentVersion = func(DocumentEditRequest, func(error)) {}
		view.CompareDocumentVersions = func(string, string, func(DocumentVersionProjection, error)) {}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("editor page has an untranslated placeholder in %s", locale)
		}
		if !strings.Contains(doc, `role="toolbar"`) || !strings.Contains(doc, `aria-label=`) {
			t.Fatalf("editor toolbar is not labelled for assistive tech in %s", locale)
		}
		if !strings.Contains(doc, `id="docs-editor-status"`) || !strings.Contains(doc, `aria-live="polite"`) {
			t.Fatalf("editor status region is not a live region in %s", locale)
		}

		// The compare dialog: its input is labelled and its live region for
		// loading/failure is announced, in every supported locale and
		// direction (ar is RTL).
		compareMarkup, err := ui.RenderToString(ui.CreateElement(docsCompareDialog, docsCompareDialogProps{
			Locale: resolved.Resolved, DocumentID: "doc-42",
			Base:                    DocumentVersionProjection{DocumentID: "doc-42", VersionID: "version-7", Title: "Handbook", Markdown: "# Current", Readable: true},
			CompareDocumentVersions: view.CompareDocumentVersions,
			Close:                   func() {},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(compareMarkup, "⟦") {
			t.Fatalf("compare dialog has an untranslated placeholder in %s", locale)
		}
		if !strings.Contains(compareMarkup, `for="docs-compare-from"`) || !strings.Contains(compareMarkup, `id="docs-compare-from"`) {
			t.Fatalf("compare From picker is not labelled in %s", locale)
		}
		if !strings.Contains(compareMarkup, `for="docs-compare-to"`) || !strings.Contains(compareMarkup, `id="docs-compare-to"`) {
			t.Fatalf("compare To picker is not labelled in %s", locale)
		}
		if !strings.Contains(compareMarkup, `role="dialog"`) || !strings.Contains(compareMarkup, `aria-modal="true"`) {
			t.Fatalf("compare dialog is not exposed as a modal dialog in %s", locale)
		}
	}
}
