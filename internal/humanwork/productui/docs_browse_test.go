package productui

import (
	"strings"
	"testing"
)

func TestDocumentBrowseSearchAndCursorControls(t *testing.T) {
	view := NewView(PageDocs, "tenant", "actor", "scope")
	view.DocumentsReady = true
	view.DocumentQuery = "needle"
	view.DocumentCollection = "shared"
	view.DocumentNextPageToken = "next-cursor"
	view.Documents = []DocumentSummary{{ID: "doc-1", Title: "Needle policy", Status: DocumentShared}}
	markup, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="docs_q"`, `value="needle"`, `name="collection"`, `value="shared"`, `aria-current="page"`, `cursor=next-cursor`, `1 on this page`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("browse UI omitted %q", want)
		}
	}
}
