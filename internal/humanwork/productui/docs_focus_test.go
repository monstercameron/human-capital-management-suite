package productui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func docsFocusRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func docsFocusView() View {
	view := NewView(PageDocs, "tenant", "hc-050-rafael-torres", "scope")
	view.ViewerSubject = "hc-050-rafael-torres"
	view.DocumentsReady = true
	view.DocumentOrigin = "https://hcm.example"
	view.DocumentLibrary = &DocumentLibrary{Folders: []DocumentFolder{{ID: "f-1", Name: "Payroll", Count: 2}}}
	view.CreateDocumentFolder = func(string, func(DocumentFolder, error)) {}
	view.RenameDocumentFolder = func(string, string, func(error)) {}
	view.DeleteDocumentFolder = func(string, func(error)) {}
	view.MoveDocuments = func([]string, string, func(error)) {}
	view.SetDocumentStarred = func(string, bool, func(error)) {}
	view.ShareDocument = func(DocumentShareRequest, func(error)) {}
	view.Documents = []DocumentSummary{
		{ID: "doc-1", Title: "Leave of absence policy", OwnerID: "hc-019-maya-chen", Status: DocumentShared},
		{ID: "doc-2", Title: "Payroll close checklist", OwnerID: "hc-050-rafael-torres", Status: DocumentPrivate, CanManageAccess: true},
	}
	return view
}

// H3: the dialogs are labelled modal surfaces a script can focus, and none
// relies on the autofocus attribute, which never fires for inserted content.
func TestDocsDialogsAreFocusableModalSurfaces(t *testing.T) {
	view := docsFocusView()
	move := docsFocusRender(t, ui.CreateElement(docsMoveDialog, docsMoveDialogProps{Locale: "en-US", Count: 1, Library: view.DocumentLibrary, MoveTo: func(string) {}, Close: func() {}, CreateFolder: view.CreateDocumentFolder}))
	share := docsFocusRender(t, ui.CreateElement(docsShareDialog, docsShareDialogProps{Locale: "en-US", DocumentID: "doc-2", Title: "Payroll close checklist", Origin: view.DocumentOrigin, Principal: view.ViewerSubject, Close: func() {}}))
	for name, markup := range map[string]string{"move": move, "share": share} {
		dialog := strings.ToLower(regexp.MustCompile(`<div[^>]*role="dialog"[^>]*>`).FindString(markup))
		if dialog == "" || !strings.Contains(dialog, `aria-modal="true"`) || !strings.Contains(dialog, `tabindex="-1"`) || !strings.Contains(dialog, `aria-labelledby=`) {
			t.Fatalf("%s dialog is not a labelled, script-focusable modal: %q", name, dialog)
		}
		if strings.Contains(strings.ToLower(markup), "autofocus") {
			t.Fatalf("%s dialog still relies on autofocus: %s", name, markup)
		}
	}
	if !strings.Contains(share, `id="docs-share-people"`) {
		t.Fatal("share dialog lost the people field its initial focus targets")
	}
}

// L3: "Not in a folder" is a place, drawn with the document glyph rather
// than the close cross, and the dialog no longer repeats the nav note.
func TestDocsMoveDialogUnfiledOption(t *testing.T) {
	view := docsFocusView()
	markup := docsFocusRender(t, ui.CreateElement(docsMoveDialog, docsMoveDialogProps{Locale: "en-US", Count: 1, Library: view.DocumentLibrary, MoveTo: func(string) {}, Close: func() {}}))
	unfiled := regexp.MustCompile(`<button[^>]*data-docs-id=""[^>]*>.*?</button>`).FindString(markup)
	if unfiled == "" || !strings.Contains(unfiled, "Not in a folder") {
		t.Fatalf("unfiled option missing: %s", markup)
	}
	if strings.Contains(unfiled, productIconPath("close")) {
		t.Fatalf("unfiled option still uses the close icon: %s", unfiled)
	}
	if !strings.Contains(unfiled, productIconPath("document")) {
		t.Fatalf("unfiled option does not use the document icon: %s", unfiled)
	}
	if strings.Contains(markup, docsText("en-US", "folders_note")) {
		t.Fatal("move dialog still repeats the folders note")
	}
}

func productIconPath(name string) string {
	for _, icon := range registeredIcons {
		if icon.Name == name {
			return icon.Path
		}
	}
	return "missing-icon:" + name
}

// M9: the "⋯" menus are disclosures of plain buttons; a menu role promised
// an arrow-key model the list never had.
func TestDocsRowMenusDoNotClaimMenuRoles(t *testing.T) {
	view := docsFocusView()
	markup := docsFocusRender(t, ui.CreateElement(docsLibrary, docsLibraryProps{View: view}))
	if !strings.Contains(markup, "docs-menu-item") {
		t.Fatalf("row menu items not rendered: %s", markup)
	}
	for _, role := range []string{`role="menu"`, `role="menuitem"`} {
		if strings.Contains(markup, role) {
			t.Fatalf("docs list still declares %s", role)
		}
	}
	detail := docsFocusView()
	detail.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-2", Title: "Payroll close checklist", OwnerID: "hc-050-rafael-torres", CanManageAccess: true}, Markdown: "Body"}
	page := docsFocusRender(t, ui.CreateElement(docsDetail, docsDetailProps{View: detail}))
	if !strings.Contains(page, "docs-menu-item") || strings.Contains(page, `role="menu`) {
		t.Fatalf("document menu still declares a menu role or lost its items: %s", page)
	}
}

func TestDocsMenuArrowKeys(t *testing.T) {
	cases := []struct {
		key            string
		current, count int
		want           int
	}{
		{"ArrowDown", -1, 3, 0}, {"ArrowDown", 0, 3, 1}, {"ArrowDown", 2, 3, 0},
		{"ArrowUp", -1, 3, 2}, {"ArrowUp", 0, 3, 2}, {"ArrowUp", 2, 3, 1},
		{"Home", 2, 3, 0}, {"End", 0, 3, 2}, {"ArrowDown", 0, 0, -1},
	}
	for _, tc := range cases {
		if got := docsMenuNext(tc.key, tc.current, tc.count); got != tc.want {
			t.Fatalf("docsMenuNext(%q, %d, %d) = %d, want %d", tc.key, tc.current, tc.count, got, tc.want)
		}
	}
}

// M12: the folder forms name their focus targets by id and carry the folder
// id on Cancel, so focus can return to the folder's link; the delete
// question's confirm button is the focus target when it opens.
func TestDocsFolderFormsExposeFocusTargets(t *testing.T) {
	view := docsFocusView()
	route := docsRouteOf(view)
	nav := docsFocusRender(t, docsLibraryNav(view, route, docsNavState{}))
	if !strings.Contains(nav, `id="`+docsFolderLinkID("f-1")+`"`) {
		t.Fatalf("folder link has no id to return focus to: %s", nav)
	}
	renaming := docsFocusRender(t, docsLibraryNav(view, route, docsNavState{renaming: "f-1", renameSeed: "Payroll"}))
	if !regexp.MustCompile(`data-docs-action="folder-rename-cancel"[^>]*data-docs-id="f-1"|data-docs-id="f-1"[^>]*data-docs-action="folder-rename-cancel"`).MatchString(renaming) {
		t.Fatalf("rename Cancel does not name its folder: %s", renaming)
	}
	deleting := docsFocusRender(t, docsLibraryNav(view, route, docsNavState{deleting: "f-1"}))
	if !strings.Contains(deleting, `id="`+docsFolderDeleteConfirmID+`"`) || !strings.Contains(deleting, `role="alertdialog"`) {
		t.Fatalf("delete question has no focus target: %s", deleting)
	}
	if !regexp.MustCompile(`data-docs-action="folder-delete-cancel"[^>]*data-docs-id="f-1"|data-docs-id="f-1"[^>]*data-docs-action="folder-delete-cancel"`).MatchString(deleting) {
		t.Fatalf("delete Cancel does not name its folder: %s", deleting)
	}
	open := docsFocusRender(t, docsLibraryNav(view, route, docsNavState{folderOpen: true, renaming: "f-1"}))
	if strings.Contains(strings.ToLower(open), "autofocus") {
		t.Fatalf("folder fields still rely on autofocus: %s", open)
	}
}

// H4: the reply form mounts without autofocus, its Cancel names the thread,
// and the Reply button has an id focus returns to.
func TestDocsReplyFocusTargets(t *testing.T) {
	props := docsCommentsProps{Locale: "en-US", DocumentID: "doc-1", VersionID: "v1", CanComment: true, Add: func(DocumentCommentCreateRequest, func(error)) {}, Resolve: func(string, string, bool, func(error)) {}}
	thread := DocumentComment{ID: "c-1", Body: "Check this", CreatedAt: "2026-09-01T10:00:00Z"}
	closed := docsFocusRender(t, docsThreadCard(props, thread, nil, time.Now(), "", ui.Handler{}, ui.Handler{}, false))
	if !strings.Contains(closed, `id="`+docsReplyOpenID("c-1")+`"`) {
		t.Fatalf("Reply button has no id: %s", closed)
	}
	open := docsFocusRender(t, docsThreadCard(props, thread, nil, time.Now(), "c-1", ui.Handler{}, ui.Handler{}, false))
	if strings.Contains(strings.ToLower(open), "autofocus") || !strings.Contains(open, `id="docs-reply-c-1"`) {
		t.Fatalf("reply form relies on autofocus or lost its field: %s", open)
	}
	if !regexp.MustCompile(`data-docs-action="comment-reply-cancel"[^>]*data-docs-id="c-1"|data-docs-id="c-1"[^>]*data-docs-action="comment-reply-cancel"`).MatchString(open) {
		t.Fatalf("reply Cancel does not name its thread: %s", open)
	}
}

func TestDocsThreadNeighbourAfterResolve(t *testing.T) {
	threads := []DocumentComment{{ID: "a"}, {ID: "b", Resolved: true}, {ID: "c"}, {ID: "d"}}
	for _, tc := range []struct {
		id       string
		resolved bool
		want     string
	}{{"a", false, "c"}, {"c", false, "d"}, {"d", false, "c"}, {"b", true, ""}} {
		if got := docsThreadNeighbour(threads, tc.resolved, tc.id); got != tc.want {
			t.Fatalf("neighbour of %s = %q, want %q", tc.id, got, tc.want)
		}
	}
	if got := docsThreadFocusTargets(""); len(got) == 0 || strings.HasPrefix(got[0], "id:docs-thread-") {
		t.Fatalf("no-neighbour targets should fall back to the composer: %v", got)
	}
	if got := docsThreadFocusTargets("c"); got[0] != "id:docs-thread-c" {
		t.Fatalf("neighbour target = %v", got)
	}
}

// H4: the route announcer names the open document, not "Documents".
func TestDocsRouteAnnouncerNamesDocument(t *testing.T) {
	view := docsFocusView()
	view.Title = "Documents"
	if got := routeAnnouncementTitle(view); got != "Documents" {
		t.Fatalf("list announcement = %q", got)
	}
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-1", Title: "Leave of absence policy"}}
	if got := routeAnnouncementTitle(view); got != "Leave of absence policy" {
		t.Fatalf("document announcement = %q", got)
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	announcer := regexp.MustCompile(`<div[^>]*route-announcer[^>]*>([^<]*)</div>`).FindStringSubmatch(doc)
	if announcer == nil || !strings.Contains(announcer[1], "Leave of absence policy") {
		t.Fatalf("route announcer does not name the document: %v", announcer)
	}
}

// M4: a copy that did not happen is reported, never "Link copied".
func TestDocsCopyReportsFailure(t *testing.T) {
	var got error
	called := false
	copyToClipboard("https://hcm.example/x", func(err error) { called, got = true, err })
	if !called || got == nil {
		t.Fatalf("copy without a clipboard reported success: called=%v err=%v", called, got)
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		if docsCopyOutcome(locale, "link_copied", got) == docsText(locale, "link_copied") {
			t.Fatalf("%s: failed copy shows the success message", locale)
		}
		if docsCopyOutcome(locale, "link_copied", nil) != docsText(locale, "link_copied") {
			t.Fatalf("%s: successful copy lost its message", locale)
		}
	}
}

// Every string this work added exists in all three locales.
func TestDocsFocusCopyIsLocalized(t *testing.T) {
	for _, key := range []string{"copy_failed", "copy_failed_link", "share_link_label", "create_discard_confirm", "create_discard", "create_keep"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if docsLibraryCopy[locale][key] == "" {
				t.Fatalf("%s missing %q", locale, key)
			}
		}
		if docsLibraryCopy["de-DE"][key] == docsLibraryCopy["en-US"][key] || docsLibraryCopy["ar"][key] == docsLibraryCopy["en-US"][key] {
			t.Fatalf("%q is not translated", key)
		}
	}
}

// M3: the notice's live region is present and empty at rest, so the first
// and every repeated message is a change screen readers announce.
func TestDocsNoticeLiveRegionAtRest(t *testing.T) {
	markup := docsFocusRender(t, ui.CreateElement(docsLibrary, docsLibraryProps{View: docsFocusView()}))
	live := regexp.MustCompile(`<p[^>]*class="sr-only docs-live"[^>]*>(.*?)</p>`).FindStringSubmatch(markup)
	if live == nil || !strings.Contains(live[0], `aria-live="polite"`) || live[1] != "" {
		t.Fatalf("notice live region missing or not empty at rest: %v", live)
	}
	if strings.Contains(markup, "docs-toast") {
		t.Fatal("a toast rendered with no notice")
	}
}

func TestDocsDialogStylesheetUsesFocusTokens(t *testing.T) {
	css := docsDialogStylesheet()
	if !strings.Contains(docsStylesheet(), css) {
		t.Fatal("dialog stylesheet is not part of the Docs stylesheet")
	}
	for _, want := range []string{".docs-menu-item:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus)", ".docs-pick-chip .docs-pick-remove{width:1.5rem;height:1.5rem}", ".docs-move-list{background:", ".docs-create-discard"} {
		if !strings.Contains(css, want) {
			t.Fatalf("dialog stylesheet omitted %q", want)
		}
	}
	for _, forbidden := range []string{"text-align:left", "text-align:right", "margin-left", "margin-right"} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("dialog stylesheet uses physical %q", forbidden)
		}
	}
}
