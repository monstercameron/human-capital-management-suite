package productui

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// RefreshRegionDocuments scopes a warm refresh to the Docs list: the shell,
// navigation and search box stay put while the next result set loads.
const RefreshRegionDocuments = "docs-library"

// DocumentPageSize is the default number of rows on one page of the list;
// a viewer may choose 25, 50 or 100.
const DocumentPageSize = 50

// DocumentLibrary is the viewer's own organization of what they can read.
// Folders and stars never grant, widen or reveal access.
type DocumentLibrary struct {
	Folders                    []DocumentFolder
	All, Mine, Shared, Starred int
}

type DocumentFolder struct {
	ID, Name string
	Count    int
}

// DocumentAccessEntry is one row of a document's access list. Only the owner
// or a manager receives it.
type DocumentAccessEntry struct {
	SubjectID, Name, PhotoURL, Role string
	Removable                       bool
}

// NormalizeDocumentPerPage keeps a requested page size to the offered
// choices.
func NormalizeDocumentPerPage(size int) int {
	switch size {
	case 25, 50, 100:
		return size
	}
	return DocumentPageSize
}

// docsLibraryRoute is the list's address state. Every control that narrows
// the list builds its link from one of these so filters compose instead of
// resetting one another.
type docsLibraryRoute struct {
	Query, Collection, Folder, Sort, Owner, Mode string
	Page, PerPage                                int
}

func docsRouteOf(view View) docsLibraryRoute {
	collection := view.DocumentCollection
	if collection == "" {
		collection = "all"
	}
	return docsLibraryRoute{Query: view.DocumentQuery, Collection: collection, Folder: view.DocumentFolder, Sort: view.DocumentSort, Owner: view.DocumentOwner, Mode: view.DocumentSearchMode, Page: view.DocumentPage, PerPage: view.DocumentPerPage}
}

func (route docsLibraryRoute) href() string {
	values := url.Values{}
	if route.Query != "" {
		values.Set("docs_q", route.Query)
	}
	if route.Collection != "" && route.Collection != "all" {
		values.Set("collection", route.Collection)
	}
	if route.Folder != "" {
		values.Set("folder", route.Folder)
	}
	if route.Sort != "" {
		values.Set("docs_sort", route.Sort)
	}
	if route.Mode != "" && route.Mode != "smart" {
		values.Set("docs_mode", route.Mode)
	}
	if route.Owner != "" {
		values.Set("docs_owner", route.Owner)
	}
	if route.Page > 1 {
		values.Set("docs_page", strconv.Itoa(route.Page))
	}
	if route.PerPage != 0 && route.PerPage != DocumentPageSize {
		values.Set("docs_size", strconv.Itoa(route.PerPage))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/workspace/app/docs?" + encoded
	}
	return "/workspace/app/docs"
}

// view switches the left navigation: a collection or a folder, never both,
// back on the first page. Search, sort and page size carry over.
func (route docsLibraryRoute) view(collection, folder string) docsLibraryRoute {
	route.Collection, route.Folder, route.Page = collection, folder, 0
	return route
}

type docsLibraryProps struct {
	View View
}

// docsLibrary is the Docs list: saved views and the viewer's folders on the
// left, one dense table on the right. It owns only presentation state --
// selection, open dialogs and optimistic stars -- and re-reads the server
// after every change.
func docsLibrary(props docsLibraryProps) ui.Node {
	view := props.View
	locale := view.Locale.Resolved
	route := docsRouteOf(view)
	selected := ui.UseState(map[string]bool{})
	stars := ui.UseState(map[string]bool{})
	moving := ui.UseState([]string(nil))
	sharing := ui.UseState("")
	// Folder names are typed into uncontrolled fields (docsFolderNameField):
	// the text lives in refs, so a keystroke never re-renders the list and a
	// late render can never write an older string back into the box. Only
	// whether the new-folder box is empty is state, for its submit button.
	folderDraft := ui.UseRef("")
	folderFilled := ui.UseState(false)
	folderOpen := ui.UseState(false)
	renaming := ui.UseState("")
	renameDraft := ui.UseRef("")
	renameFilled := ui.UseState(false)
	deleting := ui.UseState("")
	// removing is the row a remove confirm dialog is open for; withdrawn
	// carries the version WithdrawDocument last took live, so the toast's
	// Undo button can call RestoreDocument with it (DOCS-07).
	removing := ui.UseState("")
	withdrawnID := ui.UseState("")
	withdrawnVersion := ui.UseState("")
	notice := useDocsNotice()
	focus := useDocsFocus()
	useDocsMenuKeys()
	// The search box owns its text. Results for an earlier keystroke arrive
	// while the person is still typing, and must not overwrite what they have
	// typed since; only a navigation from elsewhere (back, a link) replaces it.
	// The text lives in a ref: a state read inside a handler can still see
	// the value from before the last keystroke's render, which sent an Enter
	// after clearing the box back to the old query.
	typed := ui.UseRef(view.DocumentQuery)
	typedTick := ui.UseState(0)
	// seen is the query the list last showed; sent holds every query this box
	// has asked for since the last outside change. Results for an earlier
	// keystroke arrive while the person is still typing; only a query this
	// box never asked for (back, forward, a link) replaces the text.
	seen := ui.UseRef(view.DocumentQuery)
	pushed := ui.UseRef(view.DocumentQuery)
	pushPending := ui.UseRef(view.DocumentQuery != "")
	sent := ui.UseRef(map[string]bool{view.DocumentQuery: true})
	if view.DocumentQuery != seen.Get() {
		seen.Set(view.DocumentQuery)
		// A query this box sent only belongs to it while the person is still
		// in the box: once they have left it, a change of address (Back,
		// Forward, the header arrows) is an outside change even when it
		// returns to a query typed earlier, such as the empty one.
		if !sent.Get()[view.DocumentQuery] || view.DocumentQuery != strings.TrimSpace(typed.Get()) && !docsSearchFocused() {
			sent.Set(map[string]bool{view.DocumentQuery: true})
			typed.Set(view.DocumentQuery)
			pushed.Set(view.DocumentQuery)
			pushPending.Set(true)
		}
	}
	remember := func(query string) {
		next := sent.Get()
		if len(next) > 64 {
			next = map[string]bool{}
		}
		next[strings.TrimSpace(query)] = true
		sent.Set(next)
	}
	// The box is never a controlled input: GWC writes a value prop on every
	// render, and a render that lands between keystrokes writes back an older
	// string and eats a character. An outside change is pushed in here, once.
	ui.UseLayoutEffect(func() func() {
		if pushPending.Get() {
			pushPending.Set(false)
			setDocsSearchValue(pushed.Get())
		}
		return nil
	})
	query := typed.Get()
	selectedCount := 0
	for _, document := range view.Documents {
		if selected.Get()[document.ID] {
			selectedCount++
		}
	}
	ui.UseLayoutEffect(func() func() {
		setDocsSelectAllMixed(selectedCount > 0 && selectedCount < len(view.Documents))
		return nil
	})

	visible := make(map[string]DocumentSummary, len(view.Documents))
	for _, document := range view.Documents {
		visible[document.ID] = document
	}
	selectedIDs := func() []string {
		ids := make([]string, 0, len(selected.Get()))
		for _, document := range view.Documents {
			if selected.Get()[document.ID] {
				ids = append(ids, document.ID)
			}
		}
		return ids
	}
	starred := func(document DocumentSummary) bool {
		if value, ok := stars.Get()[document.ID]; ok {
			return value
		}
		return document.Starred
	}
	navigate := func(href string) {
		if view.Navigate != nil {
			view.Navigate(href)
		}
	}
	folderName := func(id string) string {
		if view.DocumentLibrary != nil {
			for _, folder := range view.DocumentLibrary.Folders {
				if folder.ID == id {
					return folder.Name
				}
			}
		}
		return ""
	}

	click := ui.UseEvent(func(event ui.MouseEvent) {
		action, id, plain := docsEventAction(event)
		switch action {
		case "open":
			if plain && view.Navigate != nil {
				event.PreventDefault()
				navigate(id)
			}
		case "select":
			next := copyBoolMap(selected.Get())
			next[id] = !next[id]
			if !next[id] {
				delete(next, id)
			}
			selected.Set(next)
		case "select-all":
			next := map[string]bool{}
			if len(selectedIDs()) < len(view.Documents) {
				for _, document := range view.Documents {
					next[document.ID] = true
				}
			}
			selected.Set(next)
		case "clear-selection":
			selected.Set(map[string]bool{})
			focus(false, docsSelectAllTarget)
		case "star":
			document, ok := visible[id]
			if !ok || view.SetDocumentStarred == nil {
				return
			}
			want := !starred(document)
			next := copyBoolMap(stars.Get())
			next[id] = want
			stars.Set(next)
			view.SetDocumentStarred(id, want, func(err error) {
				if err != nil {
					reverted := copyBoolMap(stars.Get())
					delete(reverted, id)
					stars.Set(reverted)
					notice.Set(docsText(locale, "star_failed"))
				}
			})
		case "star-selected":
			if view.SetDocumentStarred == nil {
				return
			}
			next := copyBoolMap(stars.Get())
			for _, docID := range selectedIDs() {
				next[docID] = true
				view.SetDocumentStarred(docID, true, func(error) {})
			}
			stars.Set(next)
			notice.Set(docsCount(locale, "starred_n", len(selectedIDs())))
			selected.Set(map[string]bool{})
		case "move":
			moving.Set([]string{id})
		case "move-selected":
			moving.Set(selectedIDs())
		case "share":
			sharing.Set(id)
		case "remove":
			removing.Set(id)
		case "undo-remove":
			if view.RestoreDocument == nil || withdrawnID.Get() != id || withdrawnVersion.Get() == "" {
				return
			}
			version := withdrawnVersion.Get()
			withdrawnID.Set("")
			withdrawnVersion.Set("")
			view.RestoreDocument(id, version, func(err error) {
				if err != nil {
					notice.Set(docsText(locale, "restore_failed"))
					return
				}
				notice.Set(docsText(locale, "restore_done"))
			})
		case "copy-link":
			if href := docsShareableHref(view.DocumentOrigin, id); href != "" {
				copyToClipboard(href, func(err error) { notice.Set(docsCopyOutcome(locale, "link_copied", err)) })
			}
		case "owner":
			next := route
			next.Owner, next.Page = id, 0
			navigate(next.href())
		case "folder-new":
			folderDraft.Set("")
			folderFilled.Set(false)
			setDocsFieldValue(docsFolderNewID, "")
			folderOpen.Set(true)
			focus(false, "id:"+docsFolderNewID)
		case "folder-cancel":
			folderOpen.Set(false)
			focus(false, docsFolderAddTarget)
		case "folder-rename":
			renaming.Set(id)
			name := folderName(id)
			renameDraft.Set(name)
			renameFilled.Set(strings.TrimSpace(name) != "")
			focus(true, "id:"+docsFolderRenameID)
		case "folder-rename-cancel":
			renaming.Set("")
			focus(false, "id:"+docsFolderLinkID(id), docsFolderAddTarget)
		case "folder-delete":
			deleting.Set(id)
			focus(false, "id:"+docsFolderDeleteConfirmID)
		case "folder-delete-cancel":
			deleting.Set("")
			focus(false, "id:"+docsFolderLinkID(id), docsFolderAddTarget)
		case "folder-delete-confirm":
			if view.DeleteDocumentFolder == nil {
				return
			}
			name := folderName(id)
			deleting.Set("")
			focus(false, "id:"+docsFolderLinkID(id), docsFolderAddTarget)
			view.DeleteDocumentFolder(id, func(err error) {
				if err != nil {
					notice.Set(docsText(locale, "folder_delete_failed"))
					return
				}
				// The folder's link goes with it; the "+" beside the heading
				// (or the first saved view) keeps the person in the nav.
				focus(false, docsFolderAddTarget, docsNavFirstTarget)
				notice.Set(strings.ReplaceAll(docsText(locale, "folder_deleted"), "{folder}", name))
				if route.Folder == id {
					navigate(route.view("all", "").href())
				}
			})
		}
	})
	createFolder := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		name := strings.TrimSpace(folderDraft.Get())
		if name == "" || view.CreateDocumentFolder == nil {
			return
		}
		view.CreateDocumentFolder(name, func(folder DocumentFolder, err error) {
			if err != nil {
				notice.Set(docsText(locale, "folder_create_failed"))
				return
			}
			folderDraft.Set("")
			folderFilled.Set(false)
			folderOpen.Set(false)
			focus(false, docsFolderAddTarget, docsNavFirstTarget)
			notice.Set(strings.ReplaceAll(docsText(locale, "folder_created"), "{folder}", folder.Name))
		})
	})
	folderInput := ui.UseEvent(func(event ui.InputEvent) {
		value := event.GetValue()
		folderDraft.Set(value)
		// Re-render only when the box turns empty or non-empty.
		if filled := strings.TrimSpace(value) != ""; filled != folderFilled.Get() {
			folderFilled.Set(filled)
		}
	})
	renameFolder := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		id, name := renaming.Get(), strings.TrimSpace(renameDraft.Get())
		if id == "" || name == "" || view.RenameDocumentFolder == nil {
			return
		}
		view.RenameDocumentFolder(id, name, func(err error) {
			if err != nil {
				notice.Set(docsText(locale, "folder_rename_failed"))
				return
			}
			renaming.Set("")
			focus(false, "id:"+docsFolderLinkID(id), docsFolderAddTarget)
		})
	})
	renameInput := ui.UseEvent(func(event ui.InputEvent) {
		value := event.GetValue()
		renameDraft.Set(value)
		if filled := strings.TrimSpace(value) != ""; filled != renameFilled.Get() {
			renameFilled.Set(filled)
		}
	})
	searchHref := func(value string) string {
		next := route
		next.Query, next.Page = strings.TrimSpace(value), 0
		if next.Query == "" && next.Sort == "relevance" {
			next.Sort = ""
		}
		return next.href()
	}
	// A typing session is one history entry: its first settled search adds
	// an entry, later keystrokes update it, and leaving the box or pressing
	// Enter ends the session so the next search is a new step back.
	searchSession := ui.UseRef(false)
	searchInput := ui.UseEvent(func(event ui.InputEvent) {
		value := event.GetValue()
		// The box is uncontrolled, so a keystroke only needs a render when
		// it changes whether a query is typed (the rows' searching height).
		// Re-rendering the whole list on every character cost ~100 ms per
		// key (Agent P perf probe, docs-type-search).
		wasSearching := docsTypedSearching(typed.Get())
		typed.Set(value)
		if docsTypedSearching(value) != wasSearching {
			typedTick.Update(func(n int) int { return n + 1 })
		}
		if view.SearchDebounced != nil {
			remember(value)
			view.SearchDebounced(searchHref(value), !searchSession.Get())
			searchSession.Set(true)
		}
	})
	// Leaving the box ends the typing session: from then on only the query
	// it now shows counts as its own, so a later Back to an earlier query
	// (even the empty one it started from) replaces the text.
	endSession := func() {
		searchSession.Set(false)
		sent.Set(map[string]bool{strings.TrimSpace(typed.Get()): true})
	}
	searchBlur := ui.UseEvent(func(ui.FocusEvent) { endSession() })
	searchSubmit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if view.CancelSearchDebounced != nil {
			view.CancelSearchDebounced()
		}
		remember(typed.Get())
		if !searchSession.Get() {
			navigate(searchHref(typed.Get()))
		} else if view.SearchDebounced != nil {
			view.SearchDebounced(searchHref(typed.Get()), false)
			if view.CancelSearchDebounced == nil {
				navigate(searchHref(typed.Get()))
			}
		}
		endSession()
	})
	sortChange := ui.UseEvent(func(event ui.ChangeEvent) {
		next := route
		next.Sort, next.Page = event.GetValue(), 0
		navigate(next.href())
	})
	modeChange := ui.UseEvent(func(event ui.ChangeEvent) {
		next := route
		next.Mode, next.Page = event.GetValue(), 0
		navigate(next.href())
	})
	sizeChange := ui.UseEvent(func(event ui.ChangeEvent) {
		next := route
		next.PerPage, _ = strconv.Atoi(event.GetValue())
		next.Page = 0
		navigate(next.href())
	})
	// Escape backs out of the innermost thing open in the list: a folder
	// form or delete question first (focus returns to the folder or "+"),
	// then the selection. A dialog handles its own Escape.
	escape := ui.UseEvent(func(event ui.KeyboardEvent) {
		if event.GetKey() != "Escape" || docsEventInDialog(event) {
			return
		}
		switch {
		case folderOpen.Get():
			folderOpen.Set(false)
			focus(false, docsFolderAddTarget)
		case renaming.Get() != "":
			id := renaming.Get()
			renaming.Set("")
			focus(false, "id:"+docsFolderLinkID(id), docsFolderAddTarget)
		case deleting.Get() != "":
			id := deleting.Get()
			deleting.Set("")
			focus(false, "id:"+docsFolderLinkID(id), docsFolderAddTarget)
		case len(selected.Get()) > 0:
			selected.Set(map[string]bool{})
			if docsEventInside(event, ".docs-bulk") {
				focus(false, docsSelectAllTarget)
			}
		}
	})
	closeMove := func() { moving.Set(nil) }
	moveTo := func(folderID string) {
		ids := moving.Get()
		if len(ids) == 0 || view.MoveDocuments == nil {
			return
		}
		view.MoveDocuments(ids, folderID, func(err error) {
			if err != nil {
				notice.Set(docsText(locale, "move_failed"))
				return
			}
			moving.Set(nil)
			selected.Set(map[string]bool{})
			target := folderName(folderID)
			if folderID == "" {
				notice.Set(docsCount(locale, "unfiled_n", len(ids)))
				return
			}
			notice.Set(strings.ReplaceAll(docsCount(locale, "moved_n", len(ids)), "{folder}", target))
		})
	}

	// pending: the box holds a query the list has not answered yet (the
	// debounce or the request is still in flight). The list says so instead
	// of passing the previous rows off as results (D-8).
	pending := strings.TrimSpace(query) != route.Query
	main := []ui.Node{docsLibraryHeader(view, route, folderName(route.Folder), pending)}
	// The bulk bar covers the toolbar row instead of pushing the table down,
	// so the checkbox just ticked stays under the pointer.
	bulk := ui.Node(nil)
	if selectedCount > 0 {
		bulk = docsBulkBar(view, selectedCount)
	}
	main = append(main, docsLibraryToolbar(view, route, query, searchInput, searchSubmit, sortChange, modeChange, searchBlur, bulk))
	// Rows no longer reserve a result-with-snippet height while a query is
	// typed: every row then carried an empty band and the list ballooned
	// before any result existed (D-8). Results size to their own snippets.
	searching := route.Query != ""
	main = append(main, docsTable(view, route, selected.Get(), starred, sizeChange, searching, pending))

	shell := html.Div(html.Props{Class: "docs-library", OnClick: click, OnKeyDown: escape},
		docsLibraryNav(view, route, docsNavState{
			folderOpen: folderOpen.Get(), folderFilled: folderFilled.Get(), renaming: renaming.Get(), renameSeed: renameDraft.Get(), renameFilled: renameFilled.Get(), deleting: deleting.Get(),
			createFolder: createFolder, folderInput: folderInput, renameFolder: renameFolder, renameInput: renameInput,
		}),
		html.Div(html.Props{Class: "docs-main"}, main...),
		notice.Live("docs-live"),
		html.P(html.Props{Class: "sr-only docs-results-live", Raw: map[string]any{"role": "status", "aria-live": "polite", "aria-atomic": "true"}}, ui.Text(docsResultsAnnouncement(view, route))),
		notice.Toast(),
	)
	children := []ui.Node{shell}
	if ids := moving.Get(); len(ids) > 0 {
		children = append(children, ui.CreateElement(docsMoveDialog, docsMoveDialogProps{Locale: locale, Count: len(ids), Current: docsCommonFolder(visible, ids), Library: view.DocumentLibrary, MoveTo: moveTo, Close: closeMove, CreateFolder: view.CreateDocumentFolder}))
	}
	if id := sharing.Get(); id != "" {
		if document, ok := visible[id]; ok {
			children = append(children, ui.CreateElement(docsShareDialog, docsShareDialogProps{
				Locale: locale, DocumentID: document.ID, Title: document.Title, Origin: view.DocumentOrigin, People: view.People, Principal: docsViewer(view),
				Share: view.ShareDocument, ListAccess: view.ListDocumentAccess, Revoke: view.RevokeDocumentAccess, Close: func() { sharing.Set("") },
			}))
		}
	}
	if id := removing.Get(); id != "" && view.WithdrawDocument != nil {
		if document, ok := visible[id]; ok {
			children = append(children, ui.CreateElement(docsRemoveDialog, docsRemoveDialogProps{
				Locale: locale, Title: document.Title,
				Remove: func(done func(error)) {
					view.WithdrawDocument(id, func(versionID string, err error) {
						if err != nil {
							done(err)
							return
						}
						removing.Set("")
						withdrawnID.Set(id)
						withdrawnVersion.Set(versionID)
						notice.SetAction(docsText(locale, "remove_done"), docsText(locale, "undo"), "undo-remove", id)
						done(nil)
					})
				},
				Close: func() { removing.Set("") },
			}))
		}
	}
	return html.Div(html.Props{Class: "docs-hub"}, children...)
}

func copyBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}

// docsCommonFolder is the folder every selected document already sits in,
// or "" when they differ or none is filed.
func docsCommonFolder(visible map[string]DocumentSummary, ids []string) string {
	folder := ""
	for index, id := range ids {
		current := visible[id].FolderID
		if index == 0 {
			folder = current
		} else if current != folder {
			return ""
		}
	}
	return folder
}

// docsCount substitutes {n} and picks the singular key ("…_one") for one.
func docsCount(locale, key string, n int) string {
	if n == 1 {
		if one := docsText(locale, key+"_one"); one != "" {
			return strings.ReplaceAll(one, "{n}", "1")
		}
	}
	return strings.ReplaceAll(docsText(locale, key), "{n}", docsLocaleDigits(locale, strconv.Itoa(n)))
}

func docsLibraryHeader(view View, route docsLibraryRoute, folder string, pending bool) ui.Node {
	locale := view.Locale.Resolved
	title := docsText(locale, "nav_"+route.Collection)
	if route.Folder != "" && folder != "" {
		title = folder
	}
	total := view.DocumentTotal
	if total < len(view.Documents) {
		total = len(view.Documents)
	}
	// The heading already names the noun ("All documents", "Runbooks"), so
	// the count is bare: "All documents · 175", not "· 175 documents"
	// (D-11). The shorter heading also leaves the phone room to keep New
	// document on the heading's row (D-5).
	totalLabel := docsLocaleDigits(locale, strconv.Itoa(total))
	// A search result gets its own heading and count: the server's total
	// sometimes just echoes the page length (no real count behind it), so
	// that case reads as "50+ results" instead of a precise, wrong number.
	if route.Query != "" {
		title = strings.ReplaceAll(docsText(locale, "search_results_heading"), "{query}", route.Query)
		if view.DocumentTotal <= len(view.Documents) && view.DocumentNextPageToken != "" {
			totalLabel = strings.ReplaceAll(docsText(locale, "search_results_uncertain"), "{n}", docsLocaleDigits(locale, strconv.Itoa(total)))
		} else {
			totalLabel = docsCount(locale, "search_results_n", total)
		}
	}
	actions := []ui.Node{}
	if view.CreateDocument != nil {
		actions = append(actions, ui.CreateElement(docsCreateForm, docsCreateFormProps{Locale: locale, Create: view.CreateDocument}))
	}
	// The library owns the page heading now (docsOwnsPageHeading, D-7): one
	// h1#page-title stating the current collection with its count, the same
	// slot and id the shell's own head would otherwise put there, plus the
	// lede as its subtitle instead of a second copy above it.
	// Only All documents keeps a subtitle; a folder or collection name says
	// what the list is (D-15). While a query is pending the slot reports
	// the search instead.
	subtitle := ui.Node(nil)
	switch {
	case pending:
		subtitle = html.P(html.Props{Class: "subtitle docs-searching", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "searching")))
	case route.Query == "" && route.Folder == "" && (route.Collection == "" || route.Collection == "all"):
		subtitle = html.P(html.Props{Class: "subtitle"}, ui.Text(docsText(locale, "library_subtitle")))
	}
	return html.Header(html.Props{Class: "docs-main-head"},
		html.Div(html.Props{Class: "docs-main-title"},
			html.Div(html.Props{Class: "docs-main-title-block"},
				html.H1(html.Props{ID: "page-title", Raw: map[string]any{"tabindex": "-1"}}, ui.Text(title), html.Span(html.Props{Class: "docs-total", Raw: map[string]any{"role": "status"}}, ui.Text(" · "+totalLabel))),
				subtitle,
			),
		),
		html.Div(html.Props{Class: "docs-main-actions"}, actions...),
	)
}

func docsLibraryToolbar(view View, route docsLibraryRoute, typed string, input, submit, sortChange, modeChange, blur ui.Handler, bulk ui.Node) ui.Node {
	locale := view.Locale.Resolved
	search := []ui.Node{
		html.Label(html.Props{For: "docs-browse-query", Class: "sr-only"}, ui.Text(docsText(locale, "browse_search"))),
		productIcon("search", "docs-find-icon"),
		html.Input(html.Props{ID: "docs-browse-query", Name: "docs_q", Type: "search", Placeholder: docsText(locale, "search_placeholder"), MaxLength: 200, AutoComplete: "off", OnInput: input, OnBlur: blur,
			Aria: map[string]string{"controls": "docs-results"}}),
	}
	for key, value := range map[string]string{"collection": route.Collection, "folder": route.Folder, "docs_sort": route.Sort, "docs_owner": route.Owner, "docs_mode": route.Mode} {
		if value != "" && value != "all" {
			search = append(search, html.Input(html.Props{Type: "hidden", Name: key, Value: value}))
		}
	}
	chips := []ui.Node{}
	if route.Owner != "" {
		clear := route
		clear.Owner, clear.Page = "", 0
		chips = append(chips, docsFilterChip(docsText(locale, "owner"), docsOwnerName(view, route.Owner), clear.href(), docsText(locale, "clear_owner")))
	}
	sortValue := route.Sort
	if sortValue == "" {
		sortValue = "updated"
		if route.Query != "" {
			sortValue = "relevance"
		}
	}
	sortOptions := []ui.Node{}
	for _, option := range []string{"relevance", "updated", "updated_asc", "title", "title_desc", "owner", "owner_desc"} {
		if option == "relevance" && route.Query == "" {
			continue
		}
		sortOptions = append(sortOptions, html.Option(html.Props{Value: option, Selected: sortValue == option}, ui.Text(docsText(locale, "sort_"+option))))
	}
	// Each select is named by its visible label alone (aria-labelledby);
	// a wrapping label would also read out every option's text.
	sortSelect := html.Label(html.Props{Class: "docs-sort"},
		html.Span(html.Props{ID: "docs-sort-label", Class: "docs-sort-label"}, ui.Text(docsText(locale, "sort_label"))),
		// NAV-01: Value binds the live select element to route.Sort (the URL),
		// not only the initial <option selected> markup. Without it, a
		// popstate that changes docs_sort re-renders the option list but the
		// DOM control a reader already interacted with keeps showing its old
		// choice until they touch it again.
		html.Select(html.Props{ID: "docs-sort", Name: "docs_sort", Value: sortValue, OnChange: sortChange, Aria: map[string]string{"labelledby": "docs-sort-label"}}, sortOptions...),
	)
	mode := route.Mode
	if mode == "" {
		mode = "smart"
	}
	modeOptions := []ui.Node{}
	for _, option := range []string{"smart", "contains", "fuzzy", "meaning"} {
		label := docsText(locale, "mode_"+option)
		if option == "meaning" && !view.DocumentSemanticAvailable {
			label = docsText(locale, "mode_meaning_off")
		}
		modeOptions = append(modeOptions, html.Option(html.Props{Value: option, Selected: mode == option, Disabled: option == "meaning" && !view.DocumentSemanticAvailable && mode != "meaning"}, ui.Text(label)))
	}
	modeSelect := html.Label(html.Props{Class: "docs-sort docs-mode"},
		html.Span(html.Props{ID: "docs-mode-label", Class: "docs-sort-label"}, ui.Text(docsText(locale, "mode_label"))),
		html.Select(html.Props{ID: "docs-mode", Name: "docs_mode", Value: mode, OnChange: modeChange, Aria: map[string]string{"labelledby": "docs-mode-label"}, Raw: map[string]any{"title": docsText(locale, "mode_help_"+mode)}}, modeOptions...),
	)
	row := html.Div(html.Props{Class: "docs-toolbar"},
		html.Form(html.Props{Action: "/workspace/app/docs", Method: "get", Class: "docs-find", Raw: map[string]any{"role": "search"}, OnSubmit: submit}, search...),
		modeSelect,
		sortSelect,
	)
	// The bulk bar lies over the toolbar row (same box, whatever it wraps
	// to); the row stays mounted underneath so the search box keeps its text.
	layerClass := "docs-toolbar-layer"
	if bulk != nil {
		layerClass += " has-bulk"
	}
	row = html.Div(html.Props{Class: layerClass}, row, bulk)
	if route.Query != "" && (mode == "meaning" || mode == "smart") && view.DocumentSemanticAvailable && view.DocumentSemanticPending > 0 {
		chips = append(chips, html.P(html.Props{Class: "docs-notice docs-mode-notice", Role: "status"}, ui.Text(docsCount(locale, "indexing_n", view.DocumentSemanticPending))))
	}
	if route.Query != "" && mode == "meaning" && view.DocumentSearchModeUsed != "" && view.DocumentSearchModeUsed != "meaning" {
		chips = append(chips, html.P(html.Props{Class: "docs-notice docs-mode-notice", Role: "status"}, ui.Text(docsText(locale, "keyword_fallback"))))
	}
	// The structure never changes with the chips: a notice appearing while
	// someone types must not move, and so recreate, the search box.
	return html.Div(html.Props{Class: "docs-toolbar-stack"}, row, html.Div(html.Props{Class: "docs-chips", Hidden: len(chips) == 0}, chips...))
}

func docsFilterChip(label, value, clearHref, clearLabel string) ui.Node {
	return html.Span(html.Props{Class: "docs-chip"},
		html.Span(html.Props{Class: "docs-chip-label"}, ui.Text(label)),
		html.Span(html.Props{Class: "docs-chip-value"}, ui.Text(value)),
		html.A(html.Props{Class: "docs-chip-clear", Href: clearHref, Aria: map[string]string{"label": clearLabel}, Data: map[string]string{"docs-action": "open", "docs-id": clearHref}}, productIcon("close", "docs-chip-icon")),
	)
}

func docsBulkBar(view View, count int) ui.Node {
	locale := view.Locale.Resolved
	actions := []ui.Node{html.Span(html.Props{Class: "docs-bulk-count"}, ui.Text(docsCount(locale, "selected_n", count)))}
	if view.MoveDocuments != nil {
		actions = append(actions, html.Button(html.Props{Class: "button secondary compact", Type: "button", Data: map[string]string{"docs-action": "move-selected"}}, productIcon("folder", "docs-button-icon"), ui.Text(docsText(locale, "move_action"))))
	}
	if view.SetDocumentStarred != nil {
		actions = append(actions, html.Button(html.Props{Class: "button secondary compact", Type: "button", Data: map[string]string{"docs-action": "star-selected"}}, productIcon("favorite", "docs-button-icon"), ui.Text(docsText(locale, "star_action"))))
	}
	actions = append(actions, html.Button(html.Props{Class: "docs-bulk-clear", Type: "button", Data: map[string]string{"docs-action": "clear-selection"}}, ui.Text(docsText(locale, "clear_selection"))))
	return html.Div(html.Props{Class: "docs-bulk", Raw: map[string]any{"role": "toolbar", "aria-label": docsText(locale, "bulk_label")}}, actions...)
}

func docsTable(view View, route docsLibraryRoute, selected map[string]bool, starred func(DocumentSummary) bool, sizeChange ui.Handler, searching, pending bool) ui.Node {
	locale := view.Locale.Resolved
	if len(view.Documents) == 0 {
		return docsEmpty(view, route)
	}
	now := time.Now()
	all := len(selected) > 0 && len(selected) >= len(view.Documents)
	head := html.Div(html.Props{Class: "docs-row docs-row-head", Role: "row", Aria: map[string]string{"rowindex": "1"}},
		html.Span(html.Props{Class: "docs-cell docs-cell-select", Role: "columnheader"},
			html.Label(html.Props{Class: "docs-select-hit"}, html.Input(html.Props{Type: "checkbox", Checked: all, Aria: map[string]string{"label": docsText(locale, "select_all")}, Data: map[string]string{"docs-action": "select-all"}}))),
		docsSortHeader(view, route, "docs-cell-title", "title", "title", "title_desc"),
		docsSortHeader(view, route, "docs-cell-owner", "owner", "owner", "owner_desc"),
		html.Span(html.Props{Class: "docs-cell docs-cell-access", Role: "columnheader"}, ui.Text(docsText(locale, "access"))),
		docsSortHeader(view, route, "docs-cell-updated", "updated", "updated", "updated_asc"),
		html.Span(html.Props{Class: "docs-cell docs-cell-actions", Role: "columnheader"}, html.Span(html.Props{Class: "sr-only"}, ui.Text(docsText(locale, "actions")))),
	)
	rows := make([]ui.Node, 0, len(view.Documents)+1)
	rows = append(rows, head)
	// Row positions count across the whole result set, not this page, so
	// assistive technology reports "row 27 of 181" on page two.
	rowIndex := (max(route.Page, 1)-1)*NormalizeDocumentPerPage(route.PerPage) + 1
	for _, document := range view.Documents {
		if strings.TrimSpace(document.Title) == "" {
			continue
		}
		rowIndex++
		rows = append(rows, html.WithKey(docsTableRow(view, route, document, selected[document.ID], starred(document), now, rowIndex), "doc:"+document.ID))
	}
	tableClass := "docs-table"
	if docsAllOwnedByViewer(view) {
		// Every row says "You": the owner column carries nothing, and its
		// width goes back to the titles.
		tableClass += " docs-no-owner"
	}
	total := max(view.DocumentTotal, len(view.Documents))
	children := []ui.Node{html.Div(html.Props{Class: tableClass, Role: "table", Aria: map[string]string{"labelledby": "page-title", "rowcount": strconv.Itoa(total + 1)}}, rows...)}
	children = append(children, docsPager(view, route, sizeChange))
	props := html.Props{ID: "docs-results", Class: "docs-table-wrap"}
	if searching {
		props.Class += " is-searching"
	}
	if view.Refreshing && view.RefreshingRegion == RefreshRegionDocuments {
		props.Class += " is-refreshing"
		props.Aria = map[string]string{"busy": "true"}
	}
	if pending {
		props.Class += " is-pending"
		props.Aria = map[string]string{"busy": "true"}
	}
	return html.Div(props, children...)
}

func docsTableRow(view View, route docsLibraryRoute, document DocumentSummary, selected, starred bool, now time.Time, rowIndex int) ui.Node {
	locale := view.Locale.Resolved
	href := docsDocumentHref(document.ID)
	kind := docsDisplayStatus(document)
	owner := docsOwnerName(view, document.OwnerID)
	isMine := document.OwnerID != "" && document.OwnerID == docsViewer(view)
	if isMine {
		owner = docsText(locale, "you")
	}
	starLabel := docsText(locale, "star")
	if starred {
		starLabel = docsText(locale, "unstar")
	}
	rowClass := "docs-row docs-kind-" + kind
	if selected {
		rowClass += " is-selected"
	}
	titleCell := []ui.Node{
		// title: a long title is cut with an ellipsis; the tooltip keeps it.
		html.A(html.Props{Class: "docs-title-link", Href: href, Raw: map[string]any{"title": document.Title}, Data: map[string]string{"docs-action": "open", "docs-id": href}}, docsTitleNodes(document.Title, route.Query)...),
	}
	if folder := docsFolderLabel(view, document.FolderID); folder != "" && view.DocumentFolder == "" {
		titleCell = append(titleCell, html.Span(html.Props{Class: "docs-folder-tag"}, productIcon("folder", "docs-tag-icon"), ui.Text(folder)))
	}
	// Official guidance also says which scope endorses it, which version is
	// deployed and when it is due for review; a personal draft has none.
	sub := []ui.Node{}
	if scope := strings.TrimSpace(document.Scope); scope != "" {
		sub = append(sub, html.Span(html.Props{Class: "docs-sub-scope"}, ui.Text(scope)))
	}
	if version := docsDisplayVersion(document); version != "" {
		sub = append(sub, html.Span(html.Props{}, ui.Text(docsText(locale, "version")+" "+version)))
	}
	if due := strings.TrimSpace(document.ReviewDue); due != "" {
		sub = append(sub, html.Span(html.Props{}, ui.Text(docsText(locale, "review")+" "+due)))
	}
	if len(sub) > 0 {
		titleCell = []ui.Node{html.Span(html.Props{Class: "docs-title-line"}, titleCell...), html.Span(html.Props{Class: "docs-sub"}, sub...)}
	}
	// While searching, a hit says why it matched and shows the passage.
	if route.Query != "" && (document.Snippet != "" || document.Match != "") {
		hit := []ui.Node{}
		if document.Match != "" {
			hit = append(hit, html.Span(html.Props{Class: "docs-match docs-match-" + document.Match}, ui.Text(docsText(locale, "match_"+document.Match))))
		}
		if excerpt := docsSearchExcerpt(document.Snippet, route.Query, document.Match); excerpt != "" {
			hit = append(hit, html.Span(html.Props{Class: "docs-snippet-text"}, docsHighlight(excerpt, route.Query)...))
		}
		if len(sub) == 0 {
			titleCell = []ui.Node{html.Span(html.Props{Class: "docs-title-line"}, titleCell...)}
		}
		titleCell = append(titleCell, html.Span(html.Props{Class: "docs-hit"}, hit...))
		sub = append(sub, nil)
	}
	starProps := html.Props{Class: "docs-star", Type: "button", Aria: map[string]string{"label": starLabel + ": " + document.Title, "pressed": strconv.FormatBool(starred)}, Data: map[string]string{"docs-action": "star", "docs-id": document.ID}, Disabled: view.SetDocumentStarred == nil}
	// Below the container width where the Access column is hidden, this
	// icon carries the same information beside the title instead (D-5); the
	// column's own text stays the accessible copy at every wider width.
	accessIcon, accessIconLabel := docsAccessIconText(locale, document, isMine, true)
	return html.Div(html.Props{Class: rowClass, Role: "row", Aria: map[string]string{"rowindex": strconv.Itoa(rowIndex)}, Data: map[string]string{"document-id": document.ID}},
		html.Span(html.Props{Class: "docs-cell docs-cell-select", Role: "cell"},
			html.Label(html.Props{Class: "docs-select-hit"}, html.Input(html.Props{Type: "checkbox", Checked: selected, Aria: map[string]string{"label": docsText(locale, "select") + ": " + document.Title}, Data: map[string]string{"docs-action": "select", "docs-id": document.ID}}))),
		html.Span(html.Props{Class: "docs-cell docs-cell-title", Role: "cell"},
			html.Button(starProps, productIcon("favorite", "docs-star-icon")),
			html.Span(html.Props{Class: "docs-title-access-icon", Aria: map[string]string{"label": accessIconLabel}}, productIcon(accessIcon, "docs-access-icon")),
			html.Span(html.Props{Class: "docs-title-stack", Data: map[string]string{"lines": strconv.Itoa(min(len(sub), 1) + 1)}}, titleCell...),
			// Phone rows hide the owner and updated columns for space; this
			// line stands in for both, shown only under the phone container
			// query in docsLibraryStylesheet.
			html.Span(html.Props{Class: "docs-row-phone-meta"}, ui.Text(owner+" · "), docsWhen(view.Locale, document.UpdatedAt, now)),
		),
		html.Span(html.Props{Class: "docs-cell docs-cell-owner", Role: "cell"},
			personAvatar(docsOwnerName(view, document.OwnerID), "", docsOwnerPhoto(view, document.OwnerID), "tiny"),
			docsOwnerControl(view, document, owner, isMine),
		),
		html.Span(html.Props{Class: "docs-cell docs-cell-access", Role: "cell"}, docsAccessLabelCompact(locale, document, isMine)),
		html.Span(html.Props{Class: "docs-cell docs-cell-updated", Role: "cell"}, docsWhen(view.Locale, document.UpdatedAt, now)),
		html.Span(html.Props{Class: "docs-cell docs-cell-actions", Role: "cell"}, docsRowMenu(view, document, starred, isMine)),
	)
}

func docsOwnerControl(view View, document DocumentSummary, owner string, isMine bool) ui.Node {
	if isMine || document.OwnerID == "" || view.DocumentOwner == document.OwnerID {
		return html.Span(html.Props{Class: "docs-owner-name"}, ui.Text(owner))
	}
	label := strings.ReplaceAll(docsText(view.Locale.Resolved, "owner_filter"), "{name}", owner)
	return html.Button(html.Props{Class: "docs-owner-name docs-owner-filter", Type: "button", Raw: map[string]any{"title": label}, Aria: map[string]string{"label": label}, Data: map[string]string{"docs-action": "owner", "docs-id": document.OwnerID}}, ui.Text(owner))
}

// docsIfClass returns class when cond holds, otherwise "" — a small helper
// for a node whose class name depends on view state (e.g. edit mode).
func docsIfClass(cond bool, class string) string {
	if cond {
		return class
	}
	return ""
}

// docsAccessLabel says who can read a document in words, the column the
// list is organized around: a private draft, something the viewer shared,
// something shared with the viewer, or official guidance.
func docsAccessLabel(locale string, document DocumentSummary, isMine bool) ui.Node {
	icon, text := docsAccessIconText(locale, document, isMine, false)
	return html.Span(html.Props{Class: "docs-access docs-access-" + docsDisplayStatus(document)}, productIcon(icon, "docs-access-icon"), html.Span(html.Props{}, ui.Text(text)))
}

// docsAccessLabelCompact is docsAccessLabel's short form for the library
// table column ("Shared", "Private", "You +4"), where "Shared with you" and
// similar longer phrases starve the title next to it (D-5).
func docsAccessLabelCompact(locale string, document DocumentSummary, isMine bool) ui.Node {
	icon, text := docsAccessIconText(locale, document, isMine, true)
	kind := docsDisplayStatus(document)
	// Round-4 D-1/D-14: a document someone else owns is by definition
	// shared with the viewer, so "Shared" on most rows said nothing and
	// cost the title column its width. Those rows (and official guidance,
	// whose scope already shows under the title) carry only the icon, with
	// the words kept as the tooltip and the accessible name; text stays for
	// the viewer's own documents ("Private", "You +4"), where it informs.
	if !isMine || kind == string(DocumentTeamOfficial) || kind == string(DocumentChannelOfficial) {
		_, long := docsAccessIconText(locale, document, isMine, false)
		return html.Span(html.Props{Class: "docs-access docs-access-iconic docs-access-" + kind, Raw: map[string]any{"title": long}}, productIcon(icon, "docs-access-icon"), html.Span(html.Props{Class: "sr-only"}, ui.Text(long)))
	}
	return html.Span(html.Props{Class: "docs-access docs-access-" + kind}, productIcon(icon, "docs-access-icon"), html.Span(html.Props{}, ui.Text(text)))
}

// docsAccessIconText resolves the icon and text both access labels share.
func docsAccessIconText(locale string, document DocumentSummary, isMine, compact bool) (icon, text string) {
	kind := docsDisplayStatus(document)
	suffix := ""
	if compact {
		suffix = "_compact"
	}
	icon, text = "privacy", docsText(locale, "access_private"+suffix)
	switch {
	case kind == string(DocumentTeamOfficial) || kind == string(DocumentChannelOfficial):
		icon, text = "check", docsText(locale, "status_"+kind)
	case isMine && document.ReaderCount > 0:
		if compact {
			icon, text = "people", strings.ReplaceAll(docsText(locale, "access_readers_compact"), "{n}", docsLocaleDigits(locale, strconv.Itoa(document.ReaderCount)))
		} else {
			icon, text = "people", docsCount(locale, "access_readers", document.ReaderCount)
		}
	case isMine && kind == string(DocumentShared):
		icon, text = "people", docsText(locale, "access_shared"+suffix)
	case !isMine:
		icon, text = "people", docsText(locale, "access_with_you"+suffix)
	}
	return icon, text
}

func docsRowMenu(view View, document DocumentSummary, starred, isMine bool) ui.Node {
	locale := view.Locale.Resolved
	item := func(action, icon, label string) ui.Node {
		return html.Button(html.Props{Class: "docs-menu-item", Type: "button", Data: map[string]string{"docs-action": action, "docs-id": document.ID}}, productIcon(icon, "docs-menu-icon"), html.Span(html.Props{}, ui.Text(label)))
	}
	items := []ui.Node{}
	if document.CanManageAccess && view.ShareDocument != nil {
		items = append(items, item("share", "share", docsText(locale, "share_open")))
	}
	if view.DocumentOrigin != "" {
		items = append(items, item("copy-link", "link", docsText(locale, "copy_link")))
	}
	if view.MoveDocuments != nil {
		items = append(items, item("move", "folder", docsText(locale, "move_to_folder")))
	}
	if view.SetDocumentStarred != nil {
		label := docsText(locale, "star")
		if starred {
			label = docsText(locale, "unstar")
		}
		items = append(items, item("star", "favorite", label))
	}
	if !isMine && document.OwnerID != "" && view.DocumentOwner != document.OwnerID {
		items = append(items, item("owner", "people", strings.ReplaceAll(docsText(locale, "owner_filter"), "{name}", docsOwnerName(view, document.OwnerID))))
	}
	// Remove is recoverable and reaches only owners/managers, the same
	// gate Share uses (DOCS-07).
	if document.CanManageAccess && view.WithdrawDocument != nil {
		items = append(items, html.Button(html.Props{Class: "docs-menu-item docs-menu-danger", Type: "button", Data: map[string]string{"docs-action": "remove", "docs-id": document.ID}}, productIcon("trash", "docs-menu-icon"), html.Span(html.Props{}, ui.Text(docsText(locale, "remove_action")))))
	}
	if len(items) == 0 {
		return nil
	}
	return ui.CreateElement(TransientPopover, TransientPopoverProps{
		Kind: "docs-row", Class: "docs-row-menu", TriggerClass: "docs-row-menu-trigger", PanelClass: "docs-row-menu-panel", Group: "docs-row-menu",
		Label: docsText(locale, "actions") + ": " + document.Title, Trigger: []ui.Node{productIcon("more", "docs-more-icon")},
		Children: []ui.Node{html.Div(html.Props{Class: "docs-menu"}, items...)},
	})
}

func docsEmpty(view View, route docsLibraryRoute) ui.Node {
	locale := view.Locale.Resolved
	key := "empty"
	switch {
	case route.Query != "" || route.Owner != "":
		key = "browse_no_match"
	case route.Folder != "":
		key = "empty_folder"
	case route.Collection == "starred":
		key = "empty_starred"
	case route.Collection == "shared":
		key = "browse_no_shared"
	case route.Collection == "private":
		key = "browse_no_private"
	}
	return html.Div(html.Props{Class: "docs-empty", Raw: map[string]any{"role": "status"}},
		html.P(html.Props{Class: "docs-empty-title"}, ui.Text(docsText(locale, key))),
		html.P(html.Props{Class: "docs-empty-hint"}, ui.Text(docsText(locale, key+"_hint"))),
	)
}

type docsNavState struct {
	folderOpen, folderFilled                             bool
	renaming, renameSeed, deleting                       string
	renameFilled                                         bool
	createFolder, folderInput, renameFolder, renameInput ui.Handler
}

func docsLibraryNav(view View, route docsLibraryRoute, state docsNavState) ui.Node {
	locale := view.Locale.Resolved
	library := view.DocumentLibrary
	if library == nil {
		library = &DocumentLibrary{}
	}
	link := func(collection, icon string, count int) ui.Node {
		target := route.view(collection, "")
		props := html.Props{Class: "docs-nav-link", Href: target.href(), Data: map[string]string{"docs-action": "open", "docs-id": target.href()}}
		if route.Folder == "" && route.Collection == collection {
			props.Aria = map[string]string{"current": "page"}
		}
		children := []ui.Node{productIcon(icon, "docs-nav-icon"), html.Span(html.Props{Class: "docs-nav-label"}, ui.Text(docsText(locale, "nav_"+collection)))}
		if count > 0 {
			// Same digits as the header count (D-17): Arabic-Indic in ar.
			children = append(children, html.Span(html.Props{Class: "docs-nav-count"}, ui.Text(docsLocaleDigits(locale, strconv.Itoa(count)))))
		}
		return html.Li(html.Props{}, html.A(props, children...))
	}
	views := html.Ul(html.Props{Class: "docs-nav-list"},
		link("all", "journeys", library.All),
		link("starred", "favorite", library.Starred),
		link("private", "privacy", library.Mine),
		link("shared", "people", library.Shared),
	)
	folders := make([]DocumentFolder, len(library.Folders))
	copy(folders, library.Folders)
	sort.SliceStable(folders, func(i, j int) bool { return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name) })
	items := make([]ui.Node, 0, len(folders)+1)
	for _, folder := range folders {
		items = append(items, html.WithKey(docsFolderItem(view, route, folder, state), "folder:"+folder.ID))
	}
	if state.folderOpen {
		items = append(items, html.WithKey(html.Li(html.Props{Class: "docs-folder-form-row"},
			html.Form(html.Props{Class: "docs-folder-form", OnSubmit: state.createFolder},
				html.Label(html.Props{For: "docs-folder-new", Class: "sr-only"}, ui.Text(docsText(locale, "folder_name"))),
				ui.CreateElement(docsFolderNameField, docsFolderNameFieldProps{ID: docsFolderNewID, Placeholder: docsText(locale, "folder_name"), OnInput: state.folderInput}),
				html.Div(html.Props{Class: "docs-folder-form-actions"},
					html.Button(html.Props{Class: "button primary compact", Type: "submit", Disabled: !state.folderFilled}, ui.Text(docsText(locale, "folder_create"))),
					html.Button(html.Props{Class: "button secondary compact", Type: "button", Data: map[string]string{"docs-action": "folder-cancel"}}, ui.Text(docsText(locale, "create_cancel"))),
				),
			)), "folder-new"))
	}
	if len(items) == 0 {
		items = append(items, html.WithKey(html.Li(html.Props{Class: "docs-folder-empty"}, ui.Text(docsText(locale, "folders_empty"))), "folder-empty"))
	}
	addFolder := ui.Node(nil)
	if view.CreateDocumentFolder != nil {
		addFolder = html.Button(html.Props{Class: "docs-nav-add", Type: "button", Aria: map[string]string{"label": docsText(locale, "folder_new")}, Raw: map[string]any{"title": docsText(locale, "folder_new")}, Data: map[string]string{"docs-action": "folder-new"}}, productIcon("plus", "docs-nav-icon"))
	}
	// The folders-are-private note used to sit under the list permanently
	// (D-29); it now lives on an info control beside the heading, reachable
	// by hover or keyboard, instead of taking space on every visit.
	folderNote := html.Button(html.Props{Class: "docs-nav-info", Type: "button", Aria: map[string]string{"label": docsText(locale, "folders_note")}, Raw: map[string]any{"title": docsText(locale, "folders_note")}}, productIcon("help", "docs-nav-icon"))
	return html.Nav(html.Props{Class: "docs-nav", Aria: map[string]string{"label": docsText(locale, "heading")}},
		views,
		html.Div(html.Props{Class: "docs-nav-section"},
			html.H2(html.Props{ID: "docs-folders-heading"}, ui.Text(docsText(locale, "folders"))),
			folderNote,
			addFolder,
		),
		html.Ul(html.Props{Class: "docs-nav-list docs-folder-list", Aria: map[string]string{"labelledby": "docs-folders-heading"}}, items...),
	)
}

func docsFolderItem(view View, route docsLibraryRoute, folder DocumentFolder, state docsNavState) ui.Node {
	locale := view.Locale.Resolved
	if state.renaming == folder.ID {
		return html.Li(html.Props{Class: "docs-folder-form-row"},
			html.Form(html.Props{Class: "docs-folder-form", OnSubmit: state.renameFolder},
				html.Label(html.Props{For: "docs-folder-rename", Class: "sr-only"}, ui.Text(docsText(locale, "folder_name"))),
				ui.CreateElement(docsFolderNameField, docsFolderNameFieldProps{ID: docsFolderRenameID, Seed: state.renameSeed, OnInput: state.renameInput}),
				html.Div(html.Props{Class: "docs-folder-form-actions"},
					html.Button(html.Props{Class: "button primary compact", Type: "submit", Disabled: !state.renameFilled}, ui.Text(docsText(locale, "folder_save"))),
					html.Button(html.Props{Class: "button secondary compact", Type: "button", Data: map[string]string{"docs-action": "folder-rename-cancel", "docs-id": folder.ID}}, ui.Text(docsText(locale, "create_cancel"))),
				),
			))
	}
	if state.deleting == folder.ID {
		return html.Li(html.Props{Class: "docs-folder-confirm", Raw: map[string]any{"role": "alertdialog", "aria-labelledby": "docs-folder-confirm-" + folder.ID}},
			html.P(html.Props{ID: "docs-folder-confirm-" + folder.ID}, ui.Text(strings.ReplaceAll(docsText(locale, "folder_delete_confirm"), "{folder}", folder.Name))),
			html.Div(html.Props{Class: "docs-folder-form-actions"},
				html.Button(html.Props{ID: docsFolderDeleteConfirmID, Class: "button secondary compact docs-danger", Type: "button", Data: map[string]string{"docs-action": "folder-delete-confirm", "docs-id": folder.ID}}, ui.Text(docsText(locale, "folder_delete"))),
				html.Button(html.Props{Class: "button secondary compact", Type: "button", Data: map[string]string{"docs-action": "folder-delete-cancel", "docs-id": folder.ID}}, ui.Text(docsText(locale, "create_cancel"))),
			))
	}
	target := route.view("all", folder.ID)
	props := html.Props{ID: docsFolderLinkID(folder.ID), Class: "docs-nav-link", Href: target.href(), Data: map[string]string{"docs-action": "open", "docs-id": target.href()}}
	if route.Folder == folder.ID {
		props.Aria = map[string]string{"current": "page"}
	}
	menu := []ui.Node{}
	if view.RenameDocumentFolder != nil {
		menu = append(menu, html.Button(html.Props{Class: "docs-menu-item", Type: "button", Data: map[string]string{"docs-action": "folder-rename", "docs-id": folder.ID}}, productIcon("edit", "docs-menu-icon"), html.Span(html.Props{}, ui.Text(docsText(locale, "folder_rename")))))
	}
	if view.DeleteDocumentFolder != nil {
		menu = append(menu, html.Button(html.Props{Class: "docs-menu-item docs-menu-danger", Type: "button", Data: map[string]string{"docs-action": "folder-delete", "docs-id": folder.ID}}, productIcon("trash", "docs-menu-icon"), html.Span(html.Props{}, ui.Text(docsText(locale, "folder_delete")))))
	}
	children := []ui.Node{html.A(props, productIcon("folder", "docs-nav-icon"), html.Span(html.Props{Class: "docs-nav-label"}, ui.Text(folder.Name)), html.Span(html.Props{Class: "docs-nav-count"}, ui.Text(docsLocaleDigits(view.Locale.Resolved, strconv.Itoa(folder.Count)))))}
	if len(menu) > 0 {
		children = append(children, ui.CreateElement(TransientPopover, TransientPopoverProps{
			Kind: "docs-folder", Class: "docs-folder-menu", TriggerClass: "docs-row-menu-trigger", PanelClass: "docs-row-menu-panel", Group: "docs-row-menu",
			Label: docsText(locale, "actions") + ": " + folder.Name, Trigger: []ui.Node{productIcon("more", "docs-more-icon")},
			Children: []ui.Node{html.Div(html.Props{Class: "docs-menu"}, menu...)},
		}))
	}
	return html.Li(html.Props{Class: "docs-folder-item"}, children...)
}

func docsFolderLabel(view View, id string) string {
	if id == "" || view.DocumentLibrary == nil {
		return ""
	}
	for _, folder := range view.DocumentLibrary.Folders {
		if folder.ID == id {
			return folder.Name
		}
	}
	return ""
}

// docsOwnerName resolves an owner subject through the authorized people
// directory, falling back to the subject rendered as a name.
func docsOwnerName(view View, ownerID string) string {
	for _, person := range view.People {
		if docsPersonIs(person, ownerID) {
			if name := docsPersonName(person); name != "" {
				return name
			}
		}
	}
	return humanizeSubject(ownerID)
}

func docsOwnerPhoto(view View, ownerID string) string {
	for _, person := range view.People {
		if docsPersonIs(person, ownerID) {
			return person.PhotoURL
		}
	}
	return ""
}

// humanizeSubject turns a persona subject such as "hc-050-rafael-torres"
// into "Rafael Torres"; an opaque id is returned unchanged.
func humanizeSubject(subject string) string {
	parts := strings.Split(strings.TrimSpace(subject), "-")
	words := make([]string, 0, len(parts))
	numeric := func(part string) bool {
		return strings.IndexFunc(part, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
	}
	for index, part := range parts {
		// "hc-050-…": a short tenant prefix ahead of the persona number is
		// not part of the name.
		if part == "" || numeric(part) || index+1 < len(parts) && numeric(parts[index+1]) {
			continue
		}
		words = append(words, strings.ToUpper(part[:1])+part[1:])
	}
	if len(words) == 0 {
		return subject
	}
	return strings.Join(words, " ")
}

// docsWhen renders an update time the way people scan a list: the clock for
// today, the date otherwise. The full instant stays in the datetime
// attribute and the tooltip.
func docsWhen(locale LocaleContext, value string, now time.Time) ui.Node {
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return html.Span(html.Props{}, ui.Text(value))
	}
	zone, zoneErr := time.LoadLocation(locale.normalized().TimeZone)
	if zoneErr != nil {
		zone = time.UTC
	}
	local, today := at.In(zone), now.In(zone)
	// An earlier year reads the same way as the current-year dates plus the
	// year ("Dec 14, 2025", "14. Dez. 2025"), not the civil day-first form,
	// which had shown "14 Dec 2025" in an en-US list of "Sep 22"s (D-13).
	label := docsCompareDateLabel(locale, at, false)
	switch {
	case local.Year() == today.Year() && local.YearDay() == today.YearDay():
		// Same digits as the locale's dates (L7): Arabic-Indic in ar.
		label = docsLocaleDigits(locale.Resolved, local.Format("15:04"))
		if locale.Resolved == "" || locale.Resolved == DefaultProductLocale {
			label = local.Format("3:04 PM")
		}
	case local.Year() == today.Year():
		// A date in the current year never needs to repeat it; every locale
		// gets its own month-day order and digits, not just en-US.
		label = docsShortDateLabel(locale.Resolved, local)
	}
	return html.Tag("time", html.Props{Raw: map[string]any{"datetime": at.UTC().Format(time.RFC3339), "title": formatInstantLabel(locale, at)}}, ui.Text(label))
}

// docsShortDateLabel renders a same-year date without its year, in the
// locale's own month-day order: "Sep 22" (en-US), "22. Sep." (de-DE), and
// Arabic-Indic digits with the Arabic month name (ar). Unlike
// formatCivilDateLabel/LocaleContext.FormatDate, it never prints the year,
// so it stays usable everywhere docsWhen already omits the current year.
func docsShortDateLabel(locale string, at time.Time) string {
	switch locale {
	case "de-DE":
		return strconv.Itoa(at.Day()) + ". " + docsShortMonthsDE[at.Month()-1] + "."
	case "ar":
		return docsLocaleDigits(locale, strconv.Itoa(at.Day())) + " " + docsMonthsAR[at.Month()-1]
	default:
		return at.Format("Jan 2")
	}
}

var docsShortMonthsDE = [12]string{"Jan", "Feb", "März", "Apr", "Mai", "Juni", "Juli", "Aug", "Sep", "Okt", "Nov", "Dez"}
var docsMonthsAR = [12]string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"}

// docsPager places the list: which rows are showing out of how many, page
// links with the current page marked, and the page size.
func docsPager(view View, route docsLibraryRoute, sizeChange ui.Handler) ui.Node {
	locale := view.Locale.Resolved
	perPage := NormalizeDocumentPerPage(route.PerPage)
	total := max(view.DocumentTotal, len(view.Documents))
	page := max(route.Page, 1)
	pages := max((total+perPage-1)/perPage, 1)
	// Everything fits on one page at the default size: the header already
	// states the count, and a page-size control has nothing to page (D-14).
	// A non-default size keeps the control so it can be set back.
	if pages == 1 && page == 1 && perPage == DocumentPageSize && view.DocumentNextPageToken == "" {
		return nil
	}
	first := min((page-1)*perPage+1, total)
	last := max(min(first+len(view.Documents)-1, total), first)
	summary := strings.NewReplacer("{first}", strconv.Itoa(first), "{last}", strconv.Itoa(last), "{total}", strconv.Itoa(total)).Replace(docsText(locale, "range_of"))
	link := func(target int, label string, class string, aria string) ui.Node {
		next := route
		next.Page = target
		props := html.Props{Class: "docs-page-link " + class, Href: next.href(), Data: map[string]string{"docs-action": "open", "docs-id": next.href()}}
		if aria != "" {
			props.Aria = map[string]string{"label": aria}
		}
		if target == page {
			props.Aria = map[string]string{"current": "page", "label": strings.ReplaceAll(docsText(locale, "page_n"), "{n}", strconv.Itoa(target))}
		}
		return html.A(props, ui.Text(label))
	}
	items := []ui.Node{}
	if page > 1 {
		items = append(items, link(page-1, "‹", "docs-page-step", docsText(locale, "page_previous")))
	}
	shown := map[int]bool{1: true, pages: true}
	for n := page - 1; n <= page+1; n++ {
		if n >= 1 && n <= pages {
			shown[n] = true
		}
	}
	// A single hidden page between two shown ones saves nothing by becoming
	// an ellipsis; only a gap of two or more collapses (D-11).
	for n := 2; n < pages; n++ {
		if !shown[n] && shown[n-1] && shown[n+1] {
			shown[n] = true
		}
	}
	gap := false
	for n := 1; n <= pages; n++ {
		if !shown[n] {
			if !gap {
				items = append(items, html.Span(html.Props{Class: "docs-page-gap", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text("…")))
				gap = true
			}
			continue
		}
		gap = false
		items = append(items, link(n, strconv.Itoa(n), "", strings.ReplaceAll(docsText(locale, "page_n"), "{n}", strconv.Itoa(n))))
	}
	if page < pages {
		items = append(items, link(page+1, "›", "docs-page-step", docsText(locale, "page_next")))
	}
	sizes := []ui.Node{}
	for _, size := range []int{25, 50, 100} {
		sizes = append(sizes, html.Option(html.Props{Value: strconv.Itoa(size), Selected: size == perPage}, ui.Text(strconv.Itoa(size))))
	}
	children := []ui.Node{html.Span(html.Props{Class: "docs-more-count"}, ui.Text(summary))}
	if pages > 1 {
		children = append(children, html.Nav(html.Props{Class: "docs-pages", Aria: map[string]string{"label": docsText(locale, "pages_label")}}, items...))
	}
	children = append(children, html.Label(html.Props{Class: "docs-sort docs-page-size"},
		html.Span(html.Props{ID: "docs-size-label", Class: "docs-sort-label"}, ui.Text(docsText(locale, "rows_per_page"))),
		html.Select(html.Props{Name: "docs_size", Value: strconv.Itoa(perPage), OnChange: sizeChange, Aria: map[string]string{"labelledby": "docs-size-label"}}, sizes...)))
	return html.Div(html.Props{Class: "docs-more"}, children...)
}

// docsTitleNodes is a row title, with the query marked while searching: a
// title hit otherwise showed its reason only as a "Title" chip beside an
// unrelated passage, and the matched word was highlighted nowhere (D-2).
func docsTitleNodes(title, query string) []ui.Node {
	if strings.TrimSpace(query) == "" {
		return []ui.Node{ui.Text(title)}
	}
	return docsHighlight(title, query)
}

// docsExcerptLead is how many characters of context an excerpt keeps
// before the first matched word, so the word lands on the first of the
// snippet's two clamped lines instead of past the clamp.
const docsExcerptLead = 48

// docsSearchExcerpt turns the server's ~200-character snippet into the
// passage a two-line row can actually show. The server centres the snippet
// on the first body occurrence, so the matched word sat about 100
// characters in and was cut off by the two-line clamp, leaving the row
// showing the document's flattened lead ("…September 19, 2026 Purpose
// Supports…") with no highlight at all (D-2). The excerpt starts a short
// lead before the first matched word, at a word boundary. A title hit
// whose snippet never mentions the query has no body passage worth
// showing (the title is highlighted instead), so it gets none. Trailing
// punctuation before a closing ellipsis is dropped ("work.…").
func docsSearchExcerpt(snippet, query, match string) string {
	snippet = strings.TrimSpace(snippet)
	if snippet == "" {
		return ""
	}
	lower := strings.ToLower(snippet)
	at := -1
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if len(word) < 2 {
			continue
		}
		if i := strings.Index(lower, word); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	if at < 0 && match == "title" {
		return ""
	}
	// ToLower can change byte lengths outside ASCII; only trust the offset
	// when the two strings still line up.
	if at > 0 && len(lower) == len(snippet) {
		head := []rune(snippet[:at])
		if len(head) > docsExcerptLead {
			cut := len(head) - docsExcerptLead
			for cut < len(head) && head[cut-1] != ' ' {
				cut++
			}
			rest := strings.TrimLeft(string(head[cut:]), " .,;:·—-")
			snippet = "…" + rest + snippet[at:]
		}
	}
	if trimmed, ok := strings.CutSuffix(snippet, "…"); ok {
		snippet = strings.TrimRight(trimmed, " .,;:·—-") + "…"
	}
	return snippet
}

// docsHighlight marks each query word inside a snippet, case-insensitively.
func docsHighlight(text, query string) []ui.Node {
	words := strings.Fields(strings.ToLower(query))
	lower := strings.ToLower(text)
	var out []ui.Node
	for len(text) > 0 {
		at, size := -1, 0
		for _, word := range words {
			if len(word) < 2 {
				continue
			}
			if i := strings.Index(lower, word); i >= 0 && (at < 0 || i < at) {
				at, size = i, len(word)
			}
		}
		if at < 0 {
			out = append(out, ui.Text(text))
			break
		}
		if at > 0 {
			out = append(out, ui.Text(text[:at]))
		}
		out = append(out, html.Tag("mark", html.Props{}, ui.Text(text[at:at+size])))
		text, lower = text[at+size:], lower[at+size:]
	}
	return out
}

// docsSortHeader is a column heading that sorts the list by that column. The
// first click uses the column's natural order (A to Z, newest first); a
// second click reverses it. aria-sort tells assistive technology the state.
func docsSortHeader(view View, route docsLibraryRoute, class, key, natural, reversed string) ui.Node {
	locale := view.Locale.Resolved
	current := route.Sort
	if current == "" {
		current = "updated"
		if route.Query != "" {
			current = "relevance"
		}
	}
	next := route
	next.Page = 0
	state, arrow := "none", ""
	switch current {
	case natural:
		next.Sort = reversed
		state, arrow = map[bool]string{true: "descending", false: "ascending"}[natural == "updated"], "↑"
		if natural == "updated" {
			arrow = "↓"
		}
	case reversed:
		next.Sort = natural
		state, arrow = map[bool]string{true: "ascending", false: "descending"}[natural == "updated"], "↓"
		if natural == "updated" {
			arrow = "↑"
		}
	default:
		next.Sort = natural
	}
	href := next.href()
	label := strings.ReplaceAll(docsText(locale, "sort_by"), "{column}", docsText(locale, key))
	children := []ui.Node{ui.Text(docsText(locale, key))}
	if arrow != "" {
		children = append(children, html.Span(html.Props{Class: "docs-sort-arrow", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text(arrow)))
	}
	return html.Span(html.Props{Class: "docs-cell " + class, Role: "columnheader", Aria: map[string]string{"sort": state}},
		html.A(html.Props{Class: "docs-sort-link", Href: href, Raw: map[string]any{"title": label}, Data: map[string]string{"docs-action": "open", "docs-id": href}}, children...))
}

// docsViewer is the signed-in subject documents are owned and shared by.
func docsViewer(view View) string {
	if subject := strings.TrimSpace(view.ViewerSubject); subject != "" {
		return subject
	}
	return strings.TrimSpace(view.Principal)
}
