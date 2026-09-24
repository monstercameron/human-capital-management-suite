package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func docsReviewView() View {
	view := NewView(PageDocs, "tenant", "hc-050-rafael-torres", "scope")
	view.ViewerSubject = "hc-050-rafael-torres"
	view.DocumentsReady = true
	view.DocumentLibrary = &DocumentLibrary{Folders: []DocumentFolder{{ID: "f-1", Name: "Payroll", Count: 2}}}
	view.CreateDocumentFolder = func(string, func(DocumentFolder, error)) {}
	view.RenameDocumentFolder = func(string, string, func(error)) {}
	view.Documents = []DocumentSummary{
		{ID: "doc-1", Title: "Leave of absence policy", OwnerID: "hc-019-maya-chen", Status: DocumentShared},
		{ID: "doc-2", Title: "Payroll close checklist", OwnerID: "hc-050-rafael-torres", Status: DocumentPrivate},
	}
	return view
}

func docsRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// inputTag returns the <input> element with the given id.
func inputTag(t *testing.T, markup, id string) string {
	t.Helper()
	tag := regexp.MustCompile(`<input[^>]*id="` + id + `"[^>]*>`).FindString(markup)
	if tag == "" {
		t.Fatalf("no input #%s in %s", id, markup)
	}
	return tag
}

// C2: the folder-name boxes are uncontrolled. A rendered value would be
// written back by every render and eat keystrokes typed in between.
func TestDocsFolderNameFieldsAreUncontrolled(t *testing.T) {
	view := docsReviewView()
	route := docsRouteOf(view)
	markup := docsRender(t, docsLibraryNav(view, route, docsNavState{folderOpen: true, renaming: "f-1", renameSeed: "Payroll", renameFilled: true}))
	for _, id := range []string{docsFolderNewID, docsFolderRenameID} {
		if tag := inputTag(t, markup, id); strings.Contains(tag, "value=") {
			t.Fatalf("folder field #%s renders a value, so a late render overwrites typing: %s", id, tag)
		}
	}
	// The create button follows whether the box has text, not its text.
	if !strings.Contains(markup, `disabled type="submit">`+docsText("en-US", "folder_create")) {
		t.Fatalf("empty new-folder box left Create enabled: %s", markup)
	}
	filled := docsRender(t, docsLibraryNav(view, route, docsNavState{folderOpen: true, folderFilled: true}))
	if strings.Contains(filled, `disabled type="submit">`) || !strings.Contains(filled, `type="submit">`+docsText("en-US", "folder_create")) {
		t.Fatalf("a filled new-folder box kept Create disabled: %s", filled)
	}
	field := docsRender(t, ui.CreateElement(docsFolderNameField, docsFolderNameFieldProps{ID: docsFolderRenameID, Seed: "Payroll"}))
	if tag := inputTag(t, field, docsFolderRenameID); strings.Contains(tag, "value=") || !strings.Contains(strings.ToLower(tag), `maxlength="80"`) {
		t.Fatalf("rename field = %s, want an uncontrolled 80-character box", tag)
	}
	if strings.Contains(markup, `disabled type="submit">`+docsText("en-US", "folder_save")) {
		t.Fatalf("non-empty rename draft left Save disabled: %s", markup)
	}
	emptyRename := docsRender(t, docsLibraryNav(view, route, docsNavState{renaming: "f-1"}))
	if !strings.Contains(emptyRename, `disabled type="submit">`+docsText("en-US", "folder_save")) {
		t.Fatalf("empty rename draft left Save enabled: %s", emptyRename)
	}
}

// M14 + L10 + L12 + M1: the table counts rows across the result set, the
// owner column goes when every row is the viewer's, selects are named by
// their label alone, the folders heading sits at the list's level, and a
// search marks the table for stable row heights.
func TestDocsLibraryTableSemantics(t *testing.T) {
	view := docsReviewView()
	view.DocumentTotal = 181
	view.DocumentPage = 2
	view.DocumentPerPage = 25
	view.DocumentQuery = "policy"
	markup, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-rowcount="182"`, `aria-rowindex="1"`, `aria-rowindex="27"`, `aria-rowindex="28"`,
		`aria-labelledby="docs-sort-label"`, `aria-labelledby="docs-mode-label"`, `aria-labelledby="docs-size-label"`,
		`<h2 id="docs-folders-heading"`, `docs-table-wrap is-searching`, `title="Leave of absence policy"`,
		`181 documents match “policy”`, `class="docs-toolbar-layer"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("library markup omitted %q", want)
		}
	}
	if strings.Contains(markup, `class="docs-table docs-no-owner"`) {
		t.Fatal("a page with another owner's document hid the owner column")
	}

	mine := docsReviewView()
	mine.Documents = mine.Documents[1:]
	markup, err = Render(mine)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="docs-table docs-no-owner"`) {
		t.Fatal("a page of only the viewer's documents kept the owner column")
	}
	if strings.Contains(markup, `docs-table-wrap is-searching"`) || strings.Contains(markup, "match “") {
		t.Fatal("a list without a query was marked as searching")
	}
}

func TestDocsResultsAnnouncement(t *testing.T) {
	view := docsReviewView()
	route := docsLibraryRoute{Query: "payroll"}
	if got := docsResultsAnnouncement(view, route); got != "2 documents match “payroll”" {
		t.Fatalf("announcement = %q", got)
	}
	view.Documents = view.Documents[:1]
	if got := docsResultsAnnouncement(view, route); got != "1 document matches “payroll”" {
		t.Fatalf("singular announcement = %q", got)
	}
	view.Documents = nil
	if got := docsResultsAnnouncement(view, route); got != "No documents match “payroll”" {
		t.Fatalf("empty announcement = %q", got)
	}
	view.Locale.Resolved = "de-DE"
	if got := docsResultsAnnouncement(view, route); got != "Keine Dokumente passen zu „payroll“" {
		t.Fatalf("German announcement = %q", got)
	}
	view.Refreshing, view.RefreshingRegion = true, RefreshRegionDocuments
	if got := docsResultsAnnouncement(view, route); got != "" {
		t.Fatalf("announcement while loading = %q, want silence until results settle", got)
	}
	if got := docsResultsAnnouncement(docsReviewView(), docsLibraryRoute{}); got != "" {
		t.Fatalf("announcement without a query = %q", got)
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, key := range []string{"results_n", "results_n_one", "results_none"} {
			if docsLibraryCopy[locale][key] == "" {
				t.Fatalf("%s has no %s", locale, key)
			}
		}
	}
}

// M2: the bulk bar lies over the toolbar row rather than above the table.
func TestDocsBulkBarOverlaysToolbar(t *testing.T) {
	view := docsReviewView()
	view.MoveDocuments = func([]string, string, func(error)) {}
	route := docsRouteOf(view)
	markup := docsRender(t, docsLibraryToolbar(view, route, "", ui.Handler{}, ui.Handler{}, ui.Handler{}, ui.Handler{}, ui.Handler{}, docsBulkBar(view, 1)))
	layer := strings.Index(markup, `class="docs-toolbar-layer has-bulk"`)
	toolbar := strings.Index(markup, `class="docs-toolbar"`)
	bulk := strings.Index(markup, `class="docs-bulk"`)
	if layer < 0 || toolbar < layer || bulk < toolbar {
		t.Fatalf("bulk bar is not layered over the toolbar row: %s", markup)
	}
	css := docsLibraryListStylesheet()
	for _, want := range []string{".docs-toolbar-layer>.docs-bulk{position:absolute;inset:0", ".docs-toolbar-layer.has-bulk>.docs-toolbar{visibility:hidden}", ".docs-table-wrap.is-searching .docs-row:not(.docs-row-head){min-height:", ".docs-row-head .docs-cell-title{padding-inline-start:calc(var(--hcm-space-1) + 2.15rem)}"} {
		if !strings.Contains(css, want) {
			t.Fatalf("list stylesheet omitted %q", want)
		}
	}
	if !strings.Contains(docsStylesheet(), css) {
		t.Fatal("the Docs stylesheet does not include the list stylesheet")
	}
	if strings.Contains(css, "left") || strings.Contains(css, "right") {
		t.Fatal("list stylesheet uses physical left/right")
	}
	responsive := docsLibraryStylesheet()
	for _, want := range []string{
		".docs-table.docs-no-owner .docs-cell-owner{display:none}",
		".docs-row{grid-template-columns:2.5rem minmax(0,1fr) minmax(6rem,8rem) minmax(4.5rem,6rem) 5.5rem 2.75rem}",
	} {
		if !strings.Contains(responsive, want) {
			t.Fatalf("responsive list stylesheet omitted %q", want)
		}
	}
}

// The browser-only helpers are inert outside the browser.
func TestDocsListDOMHelpersNative(t *testing.T) {
	if docsSearchFocused() {
		t.Fatal("native build reported a focused search box")
	}
	setDocsSelectAllMixed(true)
	if !docsAllOwnedByViewer(View{ViewerSubject: "a", Documents: []DocumentSummary{{OwnerID: "a"}}}) || docsAllOwnedByViewer(View{ViewerSubject: "a"}) {
		t.Fatal("docsAllOwnedByViewer misjudged ownership")
	}
}
