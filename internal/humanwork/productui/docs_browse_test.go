package productui

import (
	"strings"
	"testing"
)

func TestDocumentBrowseSearchAndPages(t *testing.T) {
	view := NewView(PageDocs, "tenant", "actor", "scope")
	view.DocumentsReady = true
	view.DocumentQuery = "needle"
	view.DocumentCollection = "shared"
	view.DocumentNextPageToken = "next-cursor"
	view.DocumentTotal = 120
	view.Documents = []DocumentSummary{{ID: "doc-1", Title: "Needle policy", Status: DocumentShared}}
	markup, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	// Search keeps the view it was typed in; pages are addressed by number so
	// a reload or a shared link lands on the same page; the active view and
	// page are marked; while searching, Best match is offered.
	for _, want := range []string{`name="docs_q"`, `value="needle"`, `name="collection"`, `value="shared"`, `aria-current="page"`, `docs_page=2`, `docs_page=3`, `1–1 of 120`, `120 documents`, `value="relevance"`, `name="docs_mode"`, `Rows per page`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("browse UI omitted %q", want)
		}
	}
	if strings.Contains(markup, "next-cursor") {
		t.Fatal("an expiring cursor leaked into a link")
	}
}

func TestDocumentLibraryRouteComposesFilters(t *testing.T) {
	route := docsLibraryRoute{Query: "leave", Collection: "all", Folder: "f-1", Sort: "title", Owner: "hc-019", Mode: "fuzzy", Page: 3, PerPage: 25}
	if got := route.href(); got != "/workspace/app/docs?docs_mode=fuzzy&docs_owner=hc-019&docs_page=3&docs_q=leave&docs_size=25&docs_sort=title&folder=f-1" {
		t.Fatalf("route = %q", got)
	}
	if got := route.view("starred", "").href(); got != "/workspace/app/docs?collection=starred&docs_mode=fuzzy&docs_owner=hc-019&docs_q=leave&docs_size=25&docs_sort=title" {
		t.Fatalf("switching view = %q, want search, mode, sort and size kept, folder and page reset", got)
	}
	for in, want := range map[int]int{0: 50, 25: 25, 100: 100, 70: 50} {
		if got := NormalizeDocumentPerPage(in); got != want {
			t.Fatalf("NormalizeDocumentPerPage(%d) = %d, want %d", in, got, want)
		}
	}
}
