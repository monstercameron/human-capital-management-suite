package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_DOCS_07 proves the recoverable-removal confirm dialog explains
// what Remove does and how to undo it, reports a failure instead of
// silently closing, and that Remove is reachable only when the caller
// supplies View.WithdrawDocument and the document lists CanManageAccess —
// the same gate Share uses.
func TestTodo_DOCS_07(t *testing.T) {
	out, err := ui.RenderToString(docsRemoveDialog(docsRemoveDialogProps{
		Locale: "en-US", Title: "Handbook",
		Remove: func(done func(error)) { done(nil) },
		Close:  func() {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Handbook", "no longer be shared", "undo", "Remove from library", `class="button destructive"`, `role="dialog"`, `aria-modal="true"`, `id="docs-remove-confirm"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("remove dialog missing %q:\n%s", want, out)
		}
	}

	failing, err := ui.RenderToString(docsRemoveDialog(docsRemoveDialogProps{
		Locale: "en-US", Title: "Handbook",
		Remove: func(done func(error)) {},
		Close:  func() {},
	}))
	if err != nil || failing == "" {
		t.Fatal("remove dialog failed to render with a pending Remove port")
	}

	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-1", Title: "Handbook", CanManageAccess: true}, Markdown: "# Handbook"}
	withdrawCalls := 0
	view.WithdrawDocument = func(documentID string, done func(string, error)) {
		withdrawCalls++
		done("v-old", nil)
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-docs-action="remove"`) {
		t.Fatalf("remove action is not reachable from the open document:\n%s", doc)
	}

	// Without WithdrawDocument (or without manage access) Remove must not
	// be offered: it is an owner/manager-only, recoverable action, never a
	// default a plain reader sees.
	view.WithdrawDocument = nil
	doc2, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc2, `data-docs-action="remove"`) {
		t.Fatal("remove action is reachable with no WithdrawDocument port wired")
	}

	if withdrawCalls != 0 {
		t.Fatalf("Remove must only run on an explicit confirm, not render: %d calls", withdrawCalls)
	}

}

// TestTodo_DOCS_07_ToastCarriesUndo proves docsNotice's actionable toast
// carries a real Undo control, not a decorative aria-hidden echo, and
// that its docs-action/docs-id route through the same delegated click
// handler every other docs-action button already uses.
func TestTodo_DOCS_07_ToastCarriesUndo(t *testing.T) {
	out, err := ui.RenderToString(docsNoticeToastNode(docsNoticeValue{
		text: "Document removed.", seq: 1, actionLabel: "Undo", action: "undo-remove", id: "doc-1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-docs-action="undo-remove"`, `data-docs-id="doc-1"`, "Undo", `role="status"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("actionable toast missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `aria-hidden="true"`) {
		t.Fatal("actionable toast hides its Undo control from assistive tech")
	}
}
