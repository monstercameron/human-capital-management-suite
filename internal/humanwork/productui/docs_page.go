package productui

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/docsdiagram"
)

// DocumentStatus distinguishes personal work from shared and endorsed pages.
// These labels describe the server projection; they never confer authority.
type DocumentStatus string

const (
	DocumentPrivate         DocumentStatus = "private"
	DocumentShared          DocumentStatus = "shared"
	DocumentTeamOfficial    DocumentStatus = "team_official"
	DocumentChannelOfficial DocumentStatus = "channel_official"
)

// DocumentSummary is an authorized, display-safe Knowledge projection. It
// contains no Markdown body, grant or credential. A later service adapter
// supplies only records the current principal may read.
type DocumentSummary struct {
	ID, Title, Owner, Version, Scope, ReviewDue, Sharing, UpdatedAt string
	OwnerID, VersionID, ScopeKind, ScopeID                          string
	Status                                                          DocumentStatus
	CanManageAccess, CanComment, CanEdit                            bool
	// Starred and FolderID are the viewer's own organization; ReaderCount
	// is how many people besides the owner can read it (owner view only).
	Starred     bool
	FolderID    string
	ReaderCount int
	// Snippet and Match explain a search hit: an authorized excerpt and why
	// it matched ("title", "text", "fuzzy" or "meaning").
	Snippet, Match string
}

type DocumentShareRequest struct {
	DocumentID, RecipientID string
	// Role is "viewer" or "commenter"; empty means commenter.
	Role string
}

// DocumentReviewProjection contains only actions already authorized by the
// document service for this exact immutable version. Scope and Diff are
// rendered verbatim on the review screen so a reviewer approves the exact
// audience and exact content change, never a summary of them; VersionHash
// flows into both the review and deploy action forms unchanged, so the UI
// itself cannot present one hash to the reviewer and submit another to
// deploy.
type DocumentReviewProjection struct {
	DocumentID, VersionID, VersionHash, Title           string
	Scope, Diff                                         string
	ReviewState, ReviewNote, ReviewAction, DeployAction string
	CanReview, CanDeploy                                bool
}

type DocumentSearchResult struct {
	DocumentID, VersionID, Title, Owner, Scope, Snippet, Why string
	Status                                                   DocumentStatus
}

// DocumentSearchFilters is the viewer's selected document-search filters.
// HUB-030 (typed server-side search filters) owns enforcing them; until
// that lands this UI still submits the selection as query parameters so the
// control surface exists and degrades to an unfiltered authorized result
// set rather than hiding filtering entirely.
type DocumentSearchFilters struct {
	Team, Channel, Owner, Status, DateFrom, DateTo string
}

type DocumentSearchProjection struct {
	Ready, SemanticAvailable, FallbackUsed bool
	Query, Mode                            string
	Filters                                DocumentSearchFilters
	Results                                []DocumentSearchResult
}

// DocumentDetail is an authorized read projection. Markdown remains plain
// text in the product UI; it is never treated as trusted HTML.
type DocumentDetail struct {
	Summary             DocumentSummary
	Markdown            string
	ContentHash         string
	Comments            []DocumentComment
	CanComment          bool
	CommentsUnavailable bool
	CanEdit             bool
	// Chat is the reader's view of the chat references in Markdown
	// (docs_chat_refs.go).
	Chat DocumentChatRefs
	// Links is the caller's view of every doc: target this version names,
	// one entry per distinct target in first-appearance order. A target the
	// caller cannot open carries no title (docs_markdown.go, HUB-035).
	Links []DocumentLinkTarget
}

// DocumentLinkTarget is the reader's safe view of one linked document: its
// title only when Readable, never otherwise.
type DocumentLinkTarget struct {
	DocumentID, Title string
	Readable          bool
}

// DocumentVersionProjection is one immutable version read for the compare
// flow (HUB-033). Everything but Readable is empty when the version could
// not be read.
type DocumentVersionProjection struct {
	DocumentID, VersionID, Title, Markdown string
	Readable                               bool
}

// DocumentBacklink is one inbound link to the open document from a source
// the viewer may also read (HUB-035/HUB-022).
type DocumentBacklink struct {
	SourceDocumentID, SourceVersionID, SourceTitle string
	Label, Block, State                            string
}

type DocumentComment struct {
	ID, AuthorID, Author, Body, CreatedAt, VersionID string
	// Quote, Prefix and Suffix anchor the comment to a passage of the text
	// (a text-quote selector); ParentID makes it a reply. Orphaned means the
	// quoted passage no longer appears in the version being read.
	Quote, Prefix, Suffix, ParentID string
	Resolved, Orphaned              bool
	// Start is where the quoted passage begins in the version being read,
	// or -1; threads are numbered in document order by it.
	Start int
}

type DocumentCommentCreateRequest struct {
	DocumentID, VersionID, Body     string
	Quote, Prefix, Suffix, ParentID string
}

type DocumentEditRequest struct {
	DocumentID, BaseVersionID, Title, Markdown string
}

var docsCopy = map[string]map[string]string{
	"en-US": {
		"private": "My documents", "shared": "Shared with me", "team_official": "Team documents", "channel_official": "Channel documents",
		"unavailable": "Documents are not connected to this workspace yet.", "empty": "No documents are available in this view.", "owner": "Owner", "version": "Version", "deployed_version": "Deployed version", "scope": "Official scope", "review": "Review due", "sharing": "Sharing", "heading": "Documents", "lede": "Browse the documents this workspace has authorized you to read.", "search_heading": "Search documents", "search": "Search", "search_action": "Search", "search_help": "Search deployed documents by title, text, team, or channel.", "keyword": "Keyword", "semantic": "Semantic", "keyword_fallback": "Semantic search is unavailable, so keyword search is being used.", "why": "Match", "provenance_keyword": "Keyword match", "provenance_semantic": "Semantic match", "provenance_title": "Title match", "provenance_fuzzy": "Fuzzy match", "search_filters": "Filters", "filter_team": "Team", "filter_channel": "Channel", "filter_owner": "Owner", "filter_status": "Status", "filter_date_from": "From", "filter_date_to": "To", "search_no_results": "No documents match this search.", "review_hash": "Exact content hash", "review_scope": "Deploy scope", "review_diff": "Exact changes", "review_unauthorized": "You do not currently have authority to review or deploy this version.", "review_heading": "Review and publish", "review_state": "Review status", "create_heading": "Create a private document", "title": "Title", "markdown": "Markdown", "create_help": "Your draft starts private and becomes a new immutable version.", "create_action": "Create draft", "create_failed": "The draft could not be created. Your text is still here; try again.", "share_heading": "Share document", "share_help": "Sharing publishes the current personal version for this recipient. Later shares use the current published version.", "share_recipient": "Recipient employee ID", "share_url": "Workspace document link", "share_action": "Share document", "share_cancel": "Cancel", "share_busy": "Sharing document…", "share_failed": "The document could not be shared. The recipient was not added.", "share_success": "Document shared with this recipient.", "comments_heading": "Comments", "comments_empty": "No comments on this version yet.", "comments_unavailable": "Comments could not be loaded.", "comment_author": "Author", "comment_body": "Comment", "comment_action": "Add comment", "comment_busy": "Adding comment…", "comment_failed": "The comment could not be added. Your text is still here; try again.", "comment_help": "Comments are attached to this document version.", "edit_open": "Edit document", "edit_heading": "Edit document", "edit_help": "Saving creates a private immutable version. It stays private until an authorized share or publish action; shared readers keep seeing the published version until then.", "edit_save": "Save new version", "edit_cancel": "Cancel", "edit_busy": "Saving new version…", "edit_saved": "New private version saved. Refreshing the document; shared readers keep seeing the published version until then.", "edit_failed": "The new version could not be saved. Your edits are still here; try again.", "edit_conflict": "This document changed while you were editing. Reload it, merge your changes, and try again.", "link_unavailable": "Not available to you", "compare_action": "Compare versions", "compare_heading": "Compare versions", "compare_close": "Close comparison", "compare_base": "Your base version", "compare_other": "Current version", "compare_loading": "Loading versions…", "compare_failed": "One of these versions could not be loaded.", "backlinks_heading": "Linked from", "backlinks_empty": "No other document you can read links here yet.", "backlinks_state_stale": "may be out of date", "backlinks_state_broken": "link needs review", "backlinks_loading": "Loading linked documents…",
	},
	"de-DE": {
		"private": "Meine Dokumente", "shared": "Mit mir geteilt", "team_official": "Teamdokumente", "channel_official": "Kanaldokumente",
		"unavailable": "Dokumente sind noch nicht mit diesem Arbeitsbereich verbunden.", "empty": "In dieser Ansicht sind keine Dokumente verfügbar.", "owner": "Verantwortlich", "version": "Version", "deployed_version": "Veröffentlichte Version", "scope": "Offizieller Bereich", "review": "Prüfung fällig", "sharing": "Freigabe", "heading": "Dokumente", "lede": "Durchsuchen Sie die für diesen Arbeitsbereich freigegebenen Dokumente.", "search_heading": "Dokumente durchsuchen", "search": "Suche", "search_action": "Suchen", "search_help": "Durchsuchen Sie veröffentlichte Dokumente nach Titel, Text, Team oder Kanal.", "keyword": "Schlüsselwort", "semantic": "Semantisch", "keyword_fallback": "Die semantische Suche ist nicht verfügbar; die Schlüsselwortsuche wird verwendet.", "why": "Treffer", "provenance_keyword": "Schlüsselworttreffer", "provenance_semantic": "Semantischer Treffer", "provenance_title": "Titeltreffer", "provenance_fuzzy": "Unscharfer Treffer", "search_filters": "Filter", "filter_team": "Team", "filter_channel": "Kanal", "filter_owner": "Verantwortlich", "filter_status": "Status", "filter_date_from": "Von", "filter_date_to": "Bis", "search_no_results": "Keine Dokumente entsprechen dieser Suche.", "review_hash": "Exakter Inhaltshash", "review_scope": "Bereitstellungsbereich", "review_diff": "Exakte Änderungen", "review_unauthorized": "Sie sind derzeit nicht berechtigt, diese Version zu prüfen oder zu veröffentlichen.", "review_heading": "Prüfen und veröffentlichen", "review_state": "Prüfstatus", "create_heading": "Privates Dokument erstellen", "title": "Titel", "markdown": "Markdown", "create_help": "Der Entwurf bleibt privat und wird zu einer neuen unveränderlichen Version.", "create_action": "Entwurf erstellen", "create_failed": "Der Entwurf konnte nicht erstellt werden. Ihr Text bleibt erhalten; versuchen Sie es erneut.", "comments_heading": "Kommentare", "comments_empty": "Für diese Version gibt es noch keine Kommentare.", "comments_unavailable": "Kommentare konnten nicht geladen werden.", "comment_author": "Autor", "comment_body": "Kommentar", "comment_action": "Kommentar hinzufügen", "comment_busy": "Kommentar wird hinzugefügt…", "comment_failed": "Der Kommentar konnte nicht hinzugefügt werden. Ihr Text bleibt erhalten; versuchen Sie es erneut.", "comment_help": "Kommentare gehören zu dieser Dokumentversion.", "edit_open": "Dokument bearbeiten", "edit_heading": "Dokument bearbeiten", "edit_help": "Beim Speichern wird eine private, unveränderliche Version erstellt. Veröffentlichen und Teilen bleiben separate Schritte; geteilte Leser sehen bis dahin die veröffentlichte Version.", "edit_save": "Neue Version speichern", "edit_cancel": "Abbrechen", "edit_busy": "Neue Version wird gespeichert…", "edit_saved": "Neue private Version gespeichert. Das Dokument wird aktualisiert; geteilte Leser sehen bis dahin die veröffentlichte Version.", "edit_failed": "Die neue Version konnte nicht gespeichert werden. Ihre Änderungen sind noch vorhanden; versuchen Sie es erneut.", "edit_conflict": "Das Dokument wurde während der Bearbeitung geändert. Laden Sie es neu, führen Sie Ihre Änderungen zusammen und versuchen Sie es erneut.", "link_unavailable": "Für Sie nicht verfügbar", "compare_action": "Versionen vergleichen", "compare_heading": "Versionen vergleichen", "compare_close": "Vergleich schließen", "compare_base": "Ihre Ausgangsversion", "compare_other": "Aktuelle Version", "compare_loading": "Versionen werden geladen…", "compare_failed": "Eine dieser Versionen konnte nicht geladen werden.", "backlinks_heading": "Verlinkt von", "backlinks_empty": "Noch kein für Sie lesbares Dokument verlinkt hierher.", "backlinks_state_stale": "möglicherweise veraltet", "backlinks_state_broken": "Link muss geprüft werden", "backlinks_loading": "Verlinkte Dokumente werden geladen…",
	},
	"ar": {
		"private": "مستنداتي", "shared": "مشتركة معي", "team_official": "مستندات الفريق", "channel_official": "مستندات القناة",
		"unavailable": "لم يتم ربط المستندات بمساحة العمل هذه بعد.", "empty": "لا توجد مستندات متاحة في هذا العرض.", "owner": "المالك", "version": "النسخة", "deployed_version": "النسخة المنشورة", "scope": "النطاق الرسمي", "review": "موعد المراجعة", "sharing": "المشاركة", "heading": "المستندات", "lede": "تصفح المستندات المصرح لك بقراءتها في مساحة العمل هذه.", "search_heading": "البحث في المستندات", "search": "بحث", "search_action": "ابحث", "search_help": "ابحث في المستندات المنشورة حسب العنوان أو النص أو الفريق أو القناة.", "keyword": "كلمة مفتاحية", "semantic": "دلالي", "keyword_fallback": "البحث الدلالي غير متاح، لذلك سيتم استخدام البحث بالكلمات المفتاحية.", "why": "سبب التطابق", "provenance_keyword": "تطابق كلمة مفتاحية", "provenance_semantic": "تطابق دلالي", "provenance_title": "تطابق العنوان", "provenance_fuzzy": "تطابق تقريبي", "search_filters": "عوامل التصفية", "filter_team": "الفريق", "filter_channel": "القناة", "filter_owner": "المالك", "filter_status": "الحالة", "filter_date_from": "من", "filter_date_to": "إلى", "search_no_results": "لا توجد مستندات تطابق هذا البحث.", "review_hash": "تجزئة المحتوى الدقيقة", "review_scope": "نطاق النشر", "review_diff": "التغييرات الدقيقة", "review_unauthorized": "ليست لديك حاليًا صلاحية مراجعة هذه النسخة أو نشرها.", "review_heading": "المراجعة والنشر", "review_state": "حالة المراجعة", "create_heading": "إنشاء مستند خاص", "title": "العنوان", "markdown": "Markdown", "create_help": "تبدأ المسودة خاصة وتصبح نسخة جديدة غير قابلة للتغيير.", "create_action": "إنشاء مسودة", "create_failed": "تعذر إنشاء المسودة. لا يزال النص محفوظًا هنا؛ حاول مرة أخرى.", "comments_heading": "التعليقات", "comments_empty": "لا توجد تعليقات على هذه النسخة بعد.", "comments_unavailable": "تعذر تحميل التعليقات.", "comment_author": "الكاتب", "comment_body": "التعليق", "comment_action": "إضافة تعليق", "comment_busy": "جارٍ إضافة التعليق…", "comment_failed": "تعذر إضافة التعليق. لا يزال النص محفوظًا هنا؛ حاول مرة أخرى.", "comment_help": "ترتبط التعليقات بنسخة المستند هذه.", "edit_open": "تحرير المستند", "edit_heading": "تحرير المستند", "edit_help": "يؤدي الحفظ إلى إنشاء نسخة خاصة غير قابلة للتغيير. يظل النشر والمشاركة خطوتين منفصلتين؛ وسيواصل القراء المشاركون رؤية النسخة المنشورة حتى ذلك الحين.", "edit_save": "حفظ نسخة جديدة", "edit_cancel": "إلغاء", "edit_busy": "جارٍ حفظ النسخة الجديدة…", "edit_saved": "تم حفظ نسخة خاصة جديدة. جارٍ تحديث المستند؛ وسيواصل القراء المشاركون رؤية النسخة المنشورة حتى ذلك الحين.", "edit_failed": "تعذر حفظ النسخة الجديدة. لا تزال تعديلاتك موجودة؛ حاول مرة أخرى.", "edit_conflict": "تم تغيير المستند أثناء تحريرك له. أعد تحميله وادمج تغييراتك ثم حاول مرة أخرى.", "link_unavailable": "غير متاح لك", "compare_action": "مقارنة النسخ", "compare_heading": "مقارنة النسخ", "compare_close": "إغلاق المقارنة", "compare_base": "نسختك الأساسية", "compare_other": "النسخة الحالية", "compare_loading": "جارٍ تحميل النسخ…", "compare_failed": "تعذر تحميل إحدى هاتين النسختين.", "backlinks_heading": "روابط من", "backlinks_empty": "لا يوجد بعد أي مستند يمكنك قراءته يرتبط بهذا المستند.", "backlinks_state_stale": "قد يكون قديمًا", "backlinks_state_broken": "الرابط يحتاج إلى مراجعة", "backlinks_loading": "جارٍ تحميل المستندات المرتبطة…",
	},
}

var docsWorkspaceCopy = map[string]map[string]string{
	"en-US": {"create_open": "New document", "create_cancel": "Cancel", "create_busy": "Creating document…", "create_help": "Only you can read this draft until you share or publish it.", "updated": "Updated", "status_private": "Personal", "status_shared": "Shared", "status_team_official": "Team guidance", "status_channel_official": "Channel guidance", "back": "All documents", "markdown_content": "Document content", "browse_all": "All accessible", "browse_private": "Mine", "browse_shared": "Shared with me", "browse_search": "Search documents", "browse_submit": "Search", "browse_clear": "Clear search", "browse_next": "Next page", "browse_count": "on this page", "browse_no_match": "No documents match this search.", "browse_no_shared": "No documents have been shared with you.", "browse_no_private": "You have not created a document yet."},
	"de-DE": {"create_open": "Neues Dokument", "create_cancel": "Abbrechen", "create_busy": "Dokument wird erstellt…", "create_help": "Nur Sie können diesen Entwurf lesen, bis Sie ihn freigeben oder veröffentlichen.", "updated": "Aktualisiert", "status_private": "Persönlich", "status_shared": "Geteilt", "status_team_official": "Teamleitfaden", "status_channel_official": "Kanalleitfaden", "back": "Alle Dokumente", "markdown_content": "Dokumentinhalt", "browse_all": "Alle verfügbaren", "browse_private": "Meine", "browse_shared": "Mit mir geteilt", "browse_search": "Dokumente durchsuchen", "browse_submit": "Suchen", "browse_clear": "Suche löschen", "browse_next": "Nächste Seite", "browse_count": "auf dieser Seite", "browse_no_match": "Keine Dokumente entsprechen dieser Suche.", "browse_no_shared": "Es wurden keine Dokumente mit Ihnen geteilt.", "browse_no_private": "Sie haben noch kein Dokument erstellt."},
	"ar":    {"create_open": "مستند جديد", "create_cancel": "إلغاء", "create_busy": "جارٍ إنشاء المستند…", "create_help": "يمكنك وحدك قراءة هذه المسودة حتى تشاركها أو تنشرها.", "updated": "تم التحديث", "status_private": "شخصي", "status_shared": "مشترك", "status_team_official": "إرشادات الفريق", "status_channel_official": "إرشادات القناة", "back": "كل المستندات", "markdown_content": "محتوى المستند", "browse_all": "كل المتاح", "browse_private": "مستنداتي", "browse_shared": "مشترك معي", "browse_search": "البحث في المستندات", "browse_submit": "بحث", "browse_clear": "مسح البحث", "browse_next": "الصفحة التالية", "browse_count": "في هذه الصفحة", "browse_no_match": "لا توجد مستندات تطابق هذا البحث.", "browse_no_shared": "لم تتم مشاركة أي مستندات معك.", "browse_no_private": "لم تنشئ مستندًا بعد."},
}

func docsText(locale, key string) string {
	if copy, ok := docsLibraryCopy[locale]; ok && copy[key] != "" {
		return copy[key]
	}
	if copy, ok := docsWorkspaceCopy[locale]; ok && copy[key] != "" {
		return copy[key]
	}
	if copy, ok := docsCopy[locale]; ok && copy[key] != "" {
		return copy[key]
	}
	if fallback := map[string]string{"open": "Open"}[key]; fallback != "" {
		return fallback
	}
	if copy := docsLibraryCopy["en-US"]; copy[key] != "" {
		return copy[key]
	}
	if copy := docsWorkspaceCopy["en-US"]; copy[key] != "" {
		return copy[key]
	}
	return docsCopy["en-US"][key]
}

func docsPage(view View) ui.Node {
	locale := view.Locale.Resolved
	if view.Document != nil {
		return ui.CreateElement(docsDetail, docsDetailProps{View: view})
	}
	if view.DocumentUnavailable {
		back := strings.TrimSpace(view.DocumentReturnHref)
		if !strings.HasPrefix(back, "/workspace/app/docs") {
			back = "/workspace/app/docs"
		}
		return html.Div(html.Props{Class: "docs-hub docs-unavailable"},
			html.Div(html.Props{Class: "docs-empty", Role: "status"},
				html.P(html.Props{Class: "docs-empty-title"}, ui.Text(docsText(locale, "document_unavailable"))),
				html.P(html.Props{Class: "docs-empty-hint"}, ui.Text(docsText(locale, "document_unavailable_hint"))),
				appLink(view, html.Props{Class: "docs-back"}, back, productIcon("history-back", "docs-back-icon"), html.Span(html.Props{}, ui.Text(docsText(locale, "back_to_documents")))),
			))
	}
	if !view.DocumentsReady {
		return html.Div(html.Props{Class: "docs-hub"},
			html.P(html.Props{Class: "docs-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "unavailable"))),
		)
	}
	children := []ui.Node{ui.CreateElement(docsLibrary, docsLibraryProps{View: view})}
	if view.DocumentSearch.Ready {
		children = append(children, docsSearch(view))
	}
	if reviews := docsReviewControls(locale, view.DocumentReviews); reviews != nil {
		children = append(children, reviews)
	}
	if len(children) == 1 {
		return children[0]
	}
	return html.Div(html.Props{Class: "docs-hub-stack"}, children...)
}

func docsBrowseHref(query, collection, cursor string) string {
	values := url.Values{}
	if query != "" {
		values.Set("docs_q", query)
	}
	if collection != "" && collection != "all" {
		values.Set("collection", collection)
	}
	if cursor != "" {
		values.Set("cursor", cursor)
	}
	if encoded := values.Encode(); encoded != "" {
		return "/workspace/app/docs?" + encoded
	}
	return "/workspace/app/docs"
}

func docsDocumentHref(documentID string) string {
	// A document link is a stable, shareable address. Browse filters are
	// intentionally left behind when opening a document; they are not part of
	// the document identity and are not admitted by the document route profile.
	return "/workspace/app/docs?document=" + url.QueryEscape(documentID)
}

func docsShareableHref(origin, documentID string) string {
	route := docsDocumentHref(documentID)
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + route
}

func docsDisplayStatus(document DocumentSummary) string {
	sharing := strings.ToLower(strings.TrimSpace(document.Sharing))
	switch DocumentStatus(sharing) {
	case DocumentPrivate, DocumentShared, DocumentTeamOfficial, DocumentChannelOfficial:
		return sharing
	default:
		return string(document.Status)
	}
}

func docsDisplayOwner(document DocumentSummary) string {
	if owner := strings.TrimSpace(document.Owner); owner != "" && owner != document.OwnerID {
		return owner
	}
	return strings.TrimSpace(document.OwnerID)
}

func docsDisplayVersion(document DocumentSummary) string {
	if strings.TrimSpace(document.Version) == "" || document.Version == document.VersionID {
		return ""
	}
	return document.Version
}

type docsDetailProps struct {
	View View
}

// docsDetail is one document: where it lives, who owns it and who can read
// it, then the text with its outline and comments beside it.
func docsDetail(props docsDetailProps) ui.Node {
	view := props.View
	document := *view.Document
	locale := view.Locale.Resolved
	shareOpen := ui.UseState(false)
	moveOpen := ui.UseState(false)
	compareOpen := ui.UseState(false)
	backlinks := ui.UseState([]DocumentBacklink{})
	backlinksLoading := ui.UseState(false)
	backlinksUnavailable := ui.UseState(false)
	starOverride := ui.UseState(0)
	notice := useDocsNotice()
	useDocsMenuKeys()
	anchor := ui.UseState(docsAnchorDraft{})
	// The floating button must not take the selection away on mousedown,
	// or there is nothing left to read by the time the click lands.
	keepSelection := ui.UseEvent(func(event ui.MouseEvent) { event.PreventDefault() })
	activeComment := ui.UseState("")
	// Hovering a pin, passage or thread links them through classes and the
	// highlight registry (docsSetLinked); it never re-renders the article.
	hoveredComment := ui.UseRef("")
	activeRef := ui.UseRef("")
	activeRef.Set(activeComment.Get())
	composerFocus := ui.UseState(0)
	// Threads about a passage are numbered in the order their passages
	// appear; the same number marks the passage's pin in the margin.
	pinned := docsNumberedThreads(document.Comments)
	numbers := make(map[string]int, len(pinned))
	pinIDs := make([]string, 0, len(pinned))
	for index, comment := range pinned {
		numbers[comment.ID] = index + 1
		pinIDs = append(pinIDs, comment.ID)
	}
	// What the render marks as linked is the clicked thread; hover adds to
	// that in the DOM only.
	linked := activeComment.Get()
	currentLink := func() string {
		if id := hoveredComment.Get(); id != "" {
			return id
		}
		return activeRef.Get()
	}
	// Selecting text in the reader offers a Comment button beside it; the
	// passages comments point at are highlighted without touching the DOM.
	// Both window listeners live exactly as long as this page.
	ui.UseEffectOf(func() func() { return docsWatchSelection() }, "docs-selection")
	ui.UseEffectOf(func() func() { return docsWatchLayout() }, "docs-layout")
	draft := anchor.Get()
	ui.UseLayoutEffect(func() func() {
		current := currentLink()
		docsPaintAnchors(document.Comments, current)
		docsLayoutAnchors(pinIDs, current)
		docsSetLinked(current)
		docsPaintDraft(draft.Quote, draft.Prefix, draft.Suffix)
		return nil
	})
	ui.UseEffectOf(func() func() {
		if view.LoadDocumentBacklinks == nil {
			return nil
		}
		backlinksLoading.Set(true)
		backlinksUnavailable.Set(false)
		documentID := document.Summary.ID
		view.LoadDocumentBacklinks(documentID, func(rows []DocumentBacklink, err error) {
			backlinksLoading.Set(false)
			if err != nil {
				backlinksUnavailable.Set(true)
				return
			}
			backlinks.Set(rows)
		})
		return nil
	}, document.Summary.ID)
	ui.UseEffectOf(func() func() {
		if composerFocus.Get() > 0 {
			docsRevealComposer()
		}
		return nil
	}, composerFocus.Get())
	hover := ui.UseEvent(func(event ui.MouseEvent) {
		if id := docsLinkedAt(event); id != hoveredComment.Get() {
			hoveredComment.Set(id)
			docsSetLinked(currentLink())
		}
	})
	leave := ui.UseEvent(func(ui.MouseEvent) {
		if hoveredComment.Get() != "" {
			hoveredComment.Set("")
			docsSetLinked(currentLink())
		}
	})
	summary := document.Summary
	starred := summary.Starred
	if starOverride.Get() != 0 {
		starred = starOverride.Get() > 0
	}
	isMine := summary.OwnerID != "" && summary.OwnerID == docsViewer(view)
	click := ui.UseEvent(func(event ui.MouseEvent) {
		action, id, plain := docsEventAction(event)
		switch action {
		case "open":
			if plain && view.Navigate != nil {
				event.PreventDefault()
				view.Navigate(id)
			}
		case "star":
			if view.SetDocumentStarred == nil {
				return
			}
			want := !starred
			if want {
				starOverride.Set(1)
			} else {
				starOverride.Set(-1)
			}
			view.SetDocumentStarred(summary.ID, want, func(err error) {
				if err != nil {
					starOverride.Set(0)
					notice.Set(docsText(locale, "star_failed"))
				}
			})
		case "share":
			shareOpen.Set(true)
		case "move":
			moveOpen.Set(true)
		case "compare":
			compareOpen.Set(true)
		case "edit":
			if view.Navigate != nil {
				view.Navigate(docsEditHref(summary.ID, true))
			}
		case "copy-link":
			if href := docsShareableHref(view.DocumentOrigin, summary.ID); href != "" {
				copyToClipboard(href, func(err error) { notice.Set(docsCopyOutcome(locale, "link_copied", err)) })
			}
		case "jump":
			// In-page anchors scroll the reader; they never touch the route.
			event.PreventDefault()
			docsScrollIntoView(id)
		case "copy-text":
			copyToClipboard(document.Markdown, func(err error) { notice.Set(docsCopyOutcome(locale, "text_copied", err)) })
		case "comment-selection":
			if quote, prefix, suffix, ok := docsCurrentSelection(); ok {
				anchor.Set(docsAnchorDraft{Quote: quote, Prefix: prefix, Suffix: suffix})
				docsClearSelection()
				composerFocus.Set(composerFocus.Get() + 1)
			}
		case "pin":
			activeComment.Set(id)
			docsScrollIntoView("docs-thread-" + id)
		default:
			if id := docsAnchorAt(event); id != "" {
				activeComment.Set(id)
				docsScrollIntoView("docs-thread-" + id)
			}
		}
	})
	moveTo := func(folderID string) {
		if view.MoveDocuments == nil {
			return
		}
		view.MoveDocuments([]string{summary.ID}, folderID, func(err error) {
			if err != nil {
				notice.Set(docsText(locale, "move_failed"))
				return
			}
			moveOpen.Set(false)
			if folderID == "" {
				notice.Set(docsCount(locale, "unfiled_n", 1))
				return
			}
			notice.Set(strings.ReplaceAll(docsCount(locale, "moved_n", 1), "{folder}", docsFolderLabel(view, folderID)))
		})
	}

	back := strings.TrimSpace(view.DocumentReturnHref)
	if !strings.HasPrefix(back, "/workspace/app/docs") {
		back = "/workspace/app/docs"
	}
	crumbs := []ui.Node{html.A(html.Props{Class: "docs-back", Href: back, Data: map[string]string{"docs-action": "open", "docs-id": back}}, productIcon("history-back", "docs-back-icon"), html.Span(html.Props{}, ui.Text(docsText(locale, "back_to_documents"))))}
	if folder := docsFolderLabel(view, summary.FolderID); folder != "" {
		href := docsLibraryRoute{Collection: "all", Folder: summary.FolderID}.href()
		crumbs = append(crumbs,
			html.A(html.Props{Class: "docs-crumb-folder", Href: href, Data: map[string]string{"docs-action": "open", "docs-id": href}}, productIcon("folder", "docs-tag-icon"), html.Span(html.Props{}, ui.Text(folder))))
	}

	// The editor is part of the address (docs_edit=1), so Back leaves it.
	editing := view.DocumentEditing && document.CanEdit && view.CreateDocumentVersion != nil
	actions := []ui.Node{}
	if view.SetDocumentStarred != nil {
		label := docsText(locale, "star")
		if starred {
			label = docsText(locale, "unstar")
		}
		actions = append(actions, html.Button(html.Props{Class: "docs-star docs-star-lg", Type: "button", Aria: map[string]string{"label": label, "pressed": strconv.FormatBool(starred)}, Raw: map[string]any{"title": label}, Data: map[string]string{"docs-action": "star"}}, productIcon("favorite", "docs-star-icon")))
	}
	if document.CanEdit && view.CreateDocumentVersion != nil && !editing {
		actions = append(actions, html.Button(html.Props{Class: "button secondary", Type: "button", Data: map[string]string{"docs-action": "edit"}}, productIcon("edit", "docs-button-icon"), ui.Text(docsText(locale, "edit_open"))))
	}
	if summary.CanManageAccess && view.ShareDocument != nil {
		actions = append(actions, html.Button(html.Props{Class: "button primary", Type: "button", Data: map[string]string{"docs-action": "share"}}, productIcon("share", "docs-button-icon"), ui.Text(docsText(locale, "share_open"))))
	} else if view.DocumentOrigin != "" {
		actions = append(actions, html.Button(html.Props{Class: "button secondary", Type: "button", Data: map[string]string{"docs-action": "copy-link"}}, productIcon("link", "docs-button-icon"), ui.Text(docsText(locale, "copy_link"))))
	}
	menu := []ui.Node{}
	if summary.CanManageAccess && view.ShareDocument != nil && view.DocumentOrigin != "" {
		menu = append(menu, html.Button(html.Props{Class: "docs-menu-item", Type: "button", Data: map[string]string{"docs-action": "copy-link"}}, productIcon("link", "docs-menu-icon"), html.Span(html.Props{}, ui.Text(docsText(locale, "copy_link")))))
	}
	if view.MoveDocuments != nil {
		menu = append(menu, html.Button(html.Props{Class: "docs-menu-item", Type: "button", Data: map[string]string{"docs-action": "move"}}, productIcon("folder", "docs-menu-icon"), html.Span(html.Props{}, ui.Text(docsText(locale, "move_to_folder")))))
	}
	if view.DocumentMedia != nil {
		menu = append(menu, ui.CreateElement(docsExportMenu, docsExportMenuProps{Locale: locale, DocumentID: summary.ID, Media: view.DocumentMedia, Notify: notice.Set}))
	}
	if view.CompareDocumentVersions != nil {
		menu = append(menu, html.Button(html.Props{Class: "docs-menu-item", Type: "button", Data: map[string]string{"docs-action": "compare"}}, productIcon("history", "docs-menu-icon"), html.Span(html.Props{}, ui.Text(docsText(locale, "compare_action")))))
	}
	if len(menu) > 0 {
		actions = append(actions, ui.CreateElement(TransientPopover, TransientPopoverProps{
			Kind: "docs-detail", Class: "docs-row-menu", TriggerClass: "docs-row-menu-trigger docs-detail-more", PanelClass: "docs-row-menu-panel", Label: docsText(locale, "actions"),
			Trigger: []ui.Node{productIcon("more", "docs-more-icon")}, Children: []ui.Node{html.Div(html.Props{Class: "docs-menu"}, menu...)},
		}))
	}

	owner := docsOwnerName(view, summary.OwnerID)
	if strings.HasPrefix(owner, "hc-") || owner == summary.OwnerID {
		if name := strings.TrimSpace(summary.Owner); name != "" && name != summary.OwnerID {
			owner = name
		}
	}
	if isMine {
		owner = docsText(locale, "you")
	}
	fact := func(key string, value ...ui.Node) ui.Node {
		return html.Div(html.Props{Class: "docs-fact"}, html.Tag("dt", html.Props{}, ui.Text(docsText(locale, key))), html.Tag("dd", html.Props{}, value...))
	}
	facts := []ui.Node{
		fact("owner", personAvatar(docsOwnerName(view, summary.OwnerID), "", docsOwnerPhoto(view, summary.OwnerID), "tiny"), html.Span(html.Props{}, ui.Text(owner))),
		fact("access", docsAccessLabel(locale, summary, isMine)),
	}
	if when := docsWhen(view.Locale, summary.UpdatedAt, time.Now()); when != nil {
		facts = append(facts, fact("updated", when))
	}
	if version := docsDisplayVersion(summary); version != "" {
		facts = append(facts, fact("version", ui.Text(version)))
	}
	if summary.Scope != "" {
		facts = append(facts, fact("scope", ui.Text(summary.Scope)))
	}

	pins := make([]ui.Node, 0, len(pinned))
	for _, comment := range pinned {
		class := "docs-anchor-pin is-unplaced"
		if comment.ID == linked {
			class += " is-linked"
		}
		number := docsLocaleDigits(locale, strconv.Itoa(numbers[comment.ID]))
		label := strings.NewReplacer("{n}", number, "{quote}", docsClip(comment.Quote, 60)).Replace(docsText(locale, "comment_pin"))
		pins = append(pins, html.Button(html.Props{Key: "pin:" + comment.ID, ID: "docs-pin-" + comment.ID, Class: class, Type: "button", Aria: map[string]string{"label": label}, Raw: map[string]any{"title": docsClip(comment.Quote, 80)}, Data: map[string]string{"docs-action": "pin", "docs-id": comment.ID, "pin-id": comment.ID}}, ui.Text(number)))
	}
	markdown := docsWithoutLeadingTitle(document.Markdown, summary.Title)
	// The document title is the page's h1; the text, outline and comments
	// are its h2 sections, and the text's own headings sit beneath them.
	// The reader runs in the document's own direction, so Copy text floats
	// to the end of its lines rather than into their start; the button keeps
	// the interface's direction for its own label.
	reader := html.Section(html.Props{ID: "docs-reader-box", Class: "docs-reader", Dir: docsContentDirection(markdown), Raw: map[string]any{"aria-labelledby": "docs-reader-heading"}},
		html.H2(html.Props{ID: "docs-reader-heading", Class: "sr-only"}, ui.Text(docsText(locale, "markdown_content"))),
		html.Button(html.Props{Class: "docs-copy-text", Type: "button", Dir: string(view.Locale.normalized().Direction), Raw: map[string]any{"title": docsText(locale, "copy_text_help")}, Data: map[string]string{"docs-action": "copy-text"}}, productIcon("copy", "docs-button-icon"), html.Span(html.Props{}, ui.Text(docsText(locale, "copy_text")))),
		html.Div(html.Props{Class: "docs-anchor-gutter", Aria: map[string]string{"label": docsText(locale, "comment_pins")}, Role: "group", Hidden: len(pins) == 0}, pins...),
		ui.CreateElement(docsMarkdownBody, docsMarkdownBodyProps{Locale: locale, VersionID: summary.VersionID, Markdown: markdown, ChatRefs: encodeDocsChatRefs(document.Chat), Links: encodeDocsLinks(document.Links), DocumentID: summary.ID, Media: view.DocumentMedia}),
		ui.CreateElement(docsAttachmentsSection, docsAttachmentsSectionProps{Locale: locale, DocumentID: summary.ID, VersionID: summary.VersionID, Media: view.DocumentMedia}),
	)
	var primary ui.Node = reader
	if editing {
		primary = ui.CreateElement(docsSplitEditor, docsSplitEditorProps{
			Locale: locale, DocumentID: summary.ID, BaseVersionID: summary.VersionID, Title: summary.Title, Markdown: document.Markdown,
			Save: view.CreateDocumentVersion, Media: view.DocumentMedia, Suggest: view.SuggestDocsReferences,
			Cancel: func() {
				if view.Navigate != nil {
					view.Navigate(docsEditHref(summary.ID, false))
				}
			},
		})
	}
	rail := []ui.Node{}
	outline := ui.UseMemo(func() []docsHeading { return docsOutline(markdown) }, summary.VersionID, markdown)
	if len(outline) > 1 && !editing {
		items := make([]ui.Node, 0, len(outline))
		top := outline[0].Level
		for _, heading := range outline {
			top = min(top, heading.Level)
		}
		for _, heading := range outline {
			items = append(items, html.Li(html.Props{Class: "docs-outline-d" + strconv.Itoa(heading.Level-top)}, html.A(html.Props{Href: "#" + heading.ID, Data: map[string]string{"docs-action": "jump", "docs-id": heading.ID}}, ui.Text(heading.Text))))
		}
		rail = append(rail, html.Nav(html.Props{Class: "docs-outline", Aria: map[string]string{"labelledby": "docs-outline-heading"}},
			html.H2(html.Props{ID: "docs-outline-heading"}, ui.Text(docsText(locale, "outline"))),
			html.Ol(html.Props{}, items...)))
	}
	if !editing {
		rail = append(rail, ui.CreateElement(docsComments, docsCommentsProps{
			Locale: locale, LocaleContext: view.Locale, DocumentID: summary.ID, VersionID: summary.VersionID, Principal: docsViewer(view),
			Comments: document.Comments, People: view.People, CanComment: document.CanComment, Unavailable: document.CommentsUnavailable,
			Add: view.AddDocumentComment, Resolve: view.ResolveDocumentComment,
			Anchor: anchor.Get(), ClearAnchor: func() { anchor.Set(docsAnchorDraft{}) },
			Active: activeComment.Get(), Linked: linked, Numbers: numbers, Activate: func(id string) { activeComment.Set(id) }, ComposerFocusSignal: composerFocus.Get(),
		}))
		if view.LoadDocumentBacklinks != nil {
			rail = append(rail, docsBacklinksPanel(docsBacklinksPanelProps{
				Locale: locale, Backlinks: backlinks.Get(), Loading: backlinksLoading.Get(), Unavailable: backlinksUnavailable.Get(),
			}))
		}
	}
	layoutClass := "docs-detail-layout"
	if editing {
		layoutClass += " is-editing"
	}
	children := []ui.Node{
		html.Nav(html.Props{Class: "docs-crumbs", Aria: map[string]string{"label": docsText(locale, "breadcrumb")}}, crumbs...),
		html.Header(html.Props{Class: "docs-detail-header docs-kind-" + docsDisplayStatus(summary)},
			html.Div(html.Props{Class: "docs-detail-title-row"},
				// The shell leaves out its "Documents" head here
				// (docsOwnsPageHeading), so this is the page title the
				// router focuses and the main region is named by.
				html.H1(html.Props{ID: "page-title", TabIndex: -1}, ui.Text(summary.Title)),
				html.Div(html.Props{Class: "docs-detail-actions"}, actions...),
			),
			html.Tag("dl", html.Props{Class: "docs-facts"}, facts...),
		),
		html.Div(html.Props{Class: layoutClass},
			html.Div(html.Props{Class: "docs-detail-content"}, primary),
			html.Aside(html.Props{Class: "docs-detail-rail", Hidden: editing}, rail...),
		),
		notice.Live(""),
		notice.Toast(),
		html.Tag("svg", html.Props{Class: "docs-connector", Raw: map[string]any{"aria-hidden": "true", "focusable": "false"}}, html.Tag("path", html.Props{ID: "docs-connector-path"})),
	}
	if document.CanComment && view.AddDocumentComment != nil && !editing {
		// Out of the tab order while hidden (visibility), and reachable
		// from the keyboard through Ctrl+Alt+M (docsWatchSelection).
		children = append(children, html.Button(html.Props{ID: "docs-select-comment", Class: "docs-select-comment", Type: "button", Raw: map[string]any{"aria-keyshortcuts": "Control+Alt+M"}, Data: map[string]string{"docs-action": "comment-selection"}, OnMouseDown: keepSelection},
			productIcon("chat", "docs-button-icon"), ui.Text(docsText(locale, "comment_on_selection"))))
	}
	if shareOpen.Get() {
		children = append(children, ui.CreateElement(docsShareDialog, docsShareDialogProps{
			Locale: locale, DocumentID: summary.ID, Title: summary.Title, Origin: view.DocumentOrigin, People: view.People, Principal: docsViewer(view),
			Share: view.ShareDocument, ListAccess: view.ListDocumentAccess, Revoke: view.RevokeDocumentAccess, Close: func() { shareOpen.Set(false) },
		}))
	}
	if moveOpen.Get() {
		children = append(children, ui.CreateElement(docsMoveDialog, docsMoveDialogProps{Locale: locale, Count: 1, Current: summary.FolderID, Library: view.DocumentLibrary, MoveTo: moveTo, Close: func() { moveOpen.Set(false) }, CreateFolder: view.CreateDocumentFolder}))
	}
	if compareOpen.Get() && view.CompareDocumentVersions != nil {
		children = append(children, ui.CreateElement(docsCompareDialog, docsCompareDialogProps{
			Locale: locale, DocumentID: summary.ID,
			Base:                    DocumentVersionProjection{DocumentID: summary.ID, VersionID: summary.VersionID, Title: summary.Title, Markdown: document.Markdown, Readable: true},
			CompareDocumentVersions: view.CompareDocumentVersions,
			Close:                   func() { compareOpen.Set(false) },
		}))
	}
	return html.Article(html.Props{Class: "docs-detail", OnClick: click, OnPointerMove: hover, OnMouseLeave: leave, Data: map[string]string{"document-id": summary.ID, "version-id": summary.VersionID}}, children...)
}

// docsMarkdownNodes keeps document content as text and upgrades only plain
// stable-id doc: references to application links. Pinned versions and anchors
// stay text until the read contract can honor them without changing meaning.
func docsMarkdownNodes(view View, markdown string) []ui.Node {
	nodes := make([]ui.Node, 0, 4)
	for len(markdown) > 0 {
		linkIndex, linkEnd, linkLabel, linkTarget := findDocMarkdownLink(markdown)
		docIndex := strings.Index(markdown, "doc:")
		if linkIndex < 0 || (docIndex >= 0 && docIndex < linkIndex) {
			linkIndex, linkEnd, linkLabel, linkTarget = -1, -1, "", ""
		}
		index := linkIndex
		if index < 0 {
			index = docIndex
		}
		if index < 0 {
			nodes = append(nodes, ui.Text(markdown))
			break
		}
		if index > 0 {
			nodes = append(nodes, ui.Text(markdown[:index]))
		}
		if linkIndex == index {
			href, _ := docsInternalHref(linkTarget)
			nodes = append(nodes, appLink(view, html.Props{Raw: map[string]any{"aria-label": docsText(view.Locale.Resolved, "open") + ": " + linkLabel}}, href, ui.Text(linkLabel)))
			markdown = markdown[linkEnd:]
			continue
		}
		markdown = markdown[index:]
		end := 4
		for end < len(markdown) && isDocTargetChar(markdown[end]) {
			end++
		}
		rawTarget := markdown[:end]
		if href, ok := docsInternalHref(rawTarget); ok {
			nodes = append(nodes, appLink(view, html.Props{Raw: map[string]any{"aria-label": docsText(view.Locale.Resolved, "open") + ": " + rawTarget}}, href, ui.Text(rawTarget)))
			markdown = markdown[end:]
			continue
		}
		nodes = append(nodes, ui.Text(markdown[:4]))
		markdown = markdown[4:]
	}
	return nodes
}

func findDocMarkdownLink(markdown string) (index, end int, label, target string) {
	for offset := 0; offset < len(markdown); {
		open := strings.IndexByte(markdown[offset:], '[')
		if open < 0 {
			return -1, -1, "", ""
		}
		open += offset
		closeLabel := strings.Index(markdown[open+1:], "](")
		if closeLabel < 0 {
			return -1, -1, "", ""
		}
		closeLabel += open + 1
		closeTarget := strings.IndexByte(markdown[closeLabel+2:], ')')
		if closeTarget < 0 {
			return -1, -1, "", ""
		}
		closeTarget += closeLabel + 2
		candidate := markdown[closeLabel+2 : closeTarget]
		if _, ok := docsInternalHref(candidate); ok {
			return open, closeTarget + 1, markdown[open+1 : closeLabel], candidate
		}
		offset = closeTarget + 1
	}
	return -1, -1, "", ""
}

func isDocTargetChar(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("-_.~@#", rune(value))
}

func docsInternalHref(target string) (string, bool) {
	if !strings.HasPrefix(target, "doc:") {
		return "", false
	}
	value := strings.TrimPrefix(target, "doc:")
	if value == "" {
		return "", false
	}
	if !validDocPart(value) {
		return "", false
	}
	return "/workspace/app/docs?document=" + url.QueryEscape(value), true
}

func validDocPart(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if !isDocTargetChar(value[index]) || value[index] == '@' || value[index] == '#' {
			return false
		}
	}
	return true
}

func docsMetaField(locale, key, value string) ui.Node {
	return html.Div(html.Props{},
		html.Tag("dt", html.Props{}, ui.Text(docsText(locale, key))),
		html.Tag("dd", html.Props{}, ui.Text(value)),
	)
}

func docsField(locale, key, value string) ui.Node {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return html.P(html.Props{Class: "docs-field"},
		html.Strong(html.Props{}, ui.Text(docsText(locale, key)+": ")),
		ui.Text(value),
	)
}

func docsStylesheet() string {
	return `
.docs-create-trigger{display:flex;justify-content:flex-end}
.docs-create-stack{display:grid;gap:var(--hcm-space-2)}
.docs-create{display:grid;gap:calc(var(--hcm-space-3)*var(--hcm-density));padding:calc(var(--hcm-space-3)*var(--hcm-density));background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-resting)}
.docs-create-header{display:flex;justify-content:space-between;align-items:start;gap:var(--hcm-space-2)}
.docs-create-header h2{margin:0;font-size:var(--hcm-font-size-body)}
.docs-create-header p{margin:var(--hcm-space-1) 0 0;max-width:65ch}
.docs-create-form{display:grid;grid-template-columns:minmax(0,1fr);gap:var(--hcm-space-1);max-width:65ch}
.docs-create-form label{font-weight:600;color:var(--ink)}
.docs-create-form input,.docs-create-form textarea,.docs-search input,.docs-search select{width:100%;min-height:var(--hcm-control-height);padding:var(--hcm-space-1) var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-create-form textarea{min-height:12lh;resize:vertical;font-family:var(--hcm-font-mono)}
.docs-create-actions{margin-block-start:var(--hcm-space-1)}
.docs-edit{display:grid;gap:var(--hcm-space-1);padding:var(--hcm-space-2);background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface)}.docs-edit h3{margin:0}.docs-edit-form{display:grid;gap:var(--hcm-space-1)}.docs-edit-form input,.docs-edit-form textarea{width:100%;box-sizing:border-box;min-height:var(--hcm-control-height);padding:var(--hcm-space-1) var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}.docs-edit-form textarea{min-height:14lh;resize:vertical;font-family:var(--hcm-font-mono)}.docs-edit-actions{display:flex;gap:var(--hcm-space-1);flex-wrap:wrap}
.docs-comment-compose{display:grid;gap:var(--hcm-space-2);padding:calc(var(--hcm-space-3)*var(--hcm-density));background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-resting)}
.docs-comment-compose h3{margin:0}.docs-comment-list{display:grid;gap:var(--hcm-space-2);list-style:none;margin:0;padding:0}.docs-comment{padding:var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-control)}.docs-comment-empty{color:var(--muted)}.docs-comment-form{display:grid;gap:var(--hcm-space-1)}.docs-comment-form textarea{width:100%;box-sizing:border-box;min-height:7lh;padding:var(--hcm-space-1) var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);resize:vertical}
.docs-reader>h2{margin:0 0 var(--hcm-space-2);font-size:var(--hcm-font-size-small);color:var(--muted);font-weight:600}
.docs-markdown{min-width:0;overflow-wrap:anywhere;font:inherit}
.docs-markdown>:first-child{margin-block-start:0}.docs-markdown>:last-child{margin-block-end:0}
.docs-markdown h2,.docs-markdown h3,.docs-markdown h4,.docs-markdown h5,.docs-markdown h6{margin:var(--hcm-space-3) 0 var(--hcm-space-1);color:var(--ink);font-weight:700;line-height:1.3}
.docs-markdown h2{font-size:var(--hcm-font-size-heading)}.docs-markdown h3,.docs-markdown h4{font-size:var(--hcm-font-size-body)}
.docs-markdown p,.docs-markdown ul,.docs-markdown ol,.docs-markdown blockquote{margin:0 0 var(--hcm-space-2)}
.docs-markdown ul,.docs-markdown ol{padding-inline-start:var(--hcm-space-3)}
.docs-markdown li+li{margin-block-start:var(--hcm-space-1)}
.docs-markdown a{color:var(--accent);text-decoration:underline;text-underline-offset:.15em}
.docs-markdown code,.docs-markdown pre{font-family:var(--hcm-font-mono)}
.docs-markdown code{padding:.1em .3em;background:var(--surface-subtle,var(--soft));border-radius:var(--hcm-radius-xs)}
.docs-markdown pre{overflow:auto;padding:var(--hcm-space-2);background:var(--surface-subtle,var(--soft));border-radius:var(--hcm-radius-control)}
.docs-markdown pre code{padding:0;background:transparent}
.docs-markdown blockquote{padding-inline-start:var(--hcm-space-2);border-inline-start:2px solid var(--line);color:var(--muted)}
.docs-search,.docs-review-item{padding:var(--hcm-space-3);background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface)}
.docs-search h2,.docs-review h2{margin:0 0 var(--hcm-space-2);font-size:var(--hcm-font-size-body)}
.docs-search-row,.docs-actions{display:flex;flex-wrap:wrap;gap:var(--hcm-space-1);align-items:end}
.docs-search-row input{flex:1 1 20ch}.docs-search-row select{width:auto}
.docs-search label{display:block;font-weight:600}
.docs-search-result{border-block-start:1px solid var(--line);padding-block:var(--hcm-space-2)}
.docs-search-result h3{margin:0 0 var(--hcm-space-1)}
.docs-snippet{margin:var(--hcm-space-1) 0}
.docs-notice{padding-inline-start:var(--hcm-space-2);border-inline-start:var(--hcm-radius-xs) solid var(--hcm-color-warning);color:var(--hcm-color-warning)}
.docs-review>ul{list-style:none;display:grid;gap:var(--hcm-space-2);margin:0;padding:0}
.docs-action-form{display:inline-flex}
.docs-field{margin:var(--hcm-space-1) 0;overflow-wrap:anywhere}
.docs-search-filters{display:flex;flex-wrap:wrap;gap:var(--hcm-space-2);margin:var(--hcm-space-2) 0;border:0;padding:0}
.docs-search-filters label{font-weight:600}
.docs-review-diff-wrap{margin:var(--hcm-space-2) 0}
.docs-review-diff{white-space:pre-wrap;overflow:auto;padding:var(--hcm-space-2);background:var(--surface-subtle,var(--soft));border-radius:var(--hcm-radius-control)}
.docs-search-provenance{display:inline-block;font-weight:600;padding:0 var(--hcm-space-1);border-radius:var(--hcm-radius-xs);background:var(--surface-subtle,var(--soft))}
@media (max-width:40rem){.docs-search-filters{flex-direction:column}.docs-actions{flex-direction:column;align-items:stretch}}
` + docsLibraryStylesheet() + docsLibraryListStylesheet() + docsEditorStylesheet() + docsdiagram.Stylesheet() + docsVisualStylesheet() + docsReaderStylesheet() + docsDialogStylesheet() + docsChatRefsStylesheet() + docsMediaStylesheet() + docsCompareStylesheet() + docsBacklinksStylesheet()
}

// docsWithoutLeadingTitle drops a first heading that only repeats the
// document's title, which the page header already shows.
func docsWithoutLeadingTitle(markdown, title string) string {
	trimmed := strings.TrimLeft(markdown, " \t\r\n")
	line, rest, _ := strings.Cut(trimmed, "\n")
	heading := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
	if strings.HasPrefix(strings.TrimSpace(line), "#") && strings.EqualFold(heading, strings.TrimSpace(title)) {
		return rest
	}
	return markdown
}

// docsEditHref is the open document's address with the editor on or off;
// the library it was reached from stays in the history behind it.
func docsEditHref(documentID string, editing bool) string {
	values := url.Values{}
	values.Set("document", documentID)
	if editing {
		values.Set("docs_edit", "1")
	}
	return "/workspace/app/docs?" + values.Encode()
}
