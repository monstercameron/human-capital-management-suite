package productui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
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
}

type DocumentShareRequest struct {
	DocumentID, RecipientID string
}

// DocumentReviewProjection contains only actions already authorized by the
// document service for this exact immutable version.
type DocumentReviewProjection struct {
	DocumentID, VersionID, VersionHash, Title           string
	ReviewState, ReviewNote, ReviewAction, DeployAction string
	CanReview, CanDeploy                                bool
}

type DocumentSearchResult struct {
	DocumentID, VersionID, Title, Owner, Scope, Snippet, Why string
	Status                                                   DocumentStatus
}

type DocumentSearchProjection struct {
	Ready, SemanticAvailable, FallbackUsed bool
	Query, Mode                            string
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
}

type DocumentComment struct {
	ID, AuthorID, Author, Body, CreatedAt, VersionID string
}

type DocumentCommentCreateRequest struct {
	DocumentID, VersionID, Body string
}

type DocumentEditRequest struct {
	DocumentID, BaseVersionID, Title, Markdown string
}

var docsCopy = map[string]map[string]string{
	"en-US": {
		"private": "My documents", "shared": "Shared with me", "team_official": "Team documents", "channel_official": "Channel documents",
		"unavailable": "Documents are not connected to this workspace yet.", "empty": "No documents are available in this view.", "owner": "Owner", "version": "Version", "deployed_version": "Deployed version", "scope": "Official scope", "review": "Review due", "sharing": "Sharing", "heading": "Documents", "lede": "Browse the documents this workspace has authorized you to read.", "search_heading": "Search documents", "search": "Search", "search_action": "Search", "search_help": "Search deployed documents by title, text, team, or channel.", "keyword": "Keyword", "semantic": "Semantic", "keyword_fallback": "Semantic search is unavailable, so keyword search is being used.", "why": "Match", "review_heading": "Review and publish", "review_state": "Review status", "create_heading": "Create a private document", "title": "Title", "markdown": "Markdown", "create_help": "Your draft starts private and becomes a new immutable version.", "create_action": "Create draft", "create_failed": "The draft could not be created. Your text is still here; try again.", "share_heading": "Share document", "share_help": "Sharing publishes the current personal version for this recipient. Later shares use the current published version.", "share_recipient": "Recipient employee ID", "share_url": "Workspace document link", "share_action": "Share document", "share_cancel": "Cancel", "share_busy": "Sharing document…", "share_failed": "The document could not be shared. The recipient was not added.", "share_success": "Document shared with this recipient.", "comments_heading": "Comments", "comments_empty": "No comments on this version yet.", "comments_unavailable": "Comments could not be loaded.", "comment_author": "Author", "comment_body": "Comment", "comment_action": "Add comment", "comment_busy": "Adding comment…", "comment_failed": "The comment could not be added. Your text is still here; try again.", "comment_help": "Comments are attached to this document version.", "edit_open": "Edit document", "edit_heading": "Edit document", "edit_help": "Saving creates a private immutable version. It stays private until an authorized share or publish action; shared readers keep seeing the published version until then.", "edit_save": "Save new version", "edit_cancel": "Cancel", "edit_busy": "Saving new version…", "edit_saved": "New private version saved. Refreshing the document; shared readers keep seeing the published version until then.", "edit_failed": "The new version could not be saved. Your edits are still here; try again.", "edit_conflict": "This document changed while you were editing. Reload it, merge your changes, and try again.",
	},
	"de-DE": {
		"private": "Meine Dokumente", "shared": "Mit mir geteilt", "team_official": "Teamdokumente", "channel_official": "Kanaldokumente",
		"unavailable": "Dokumente sind noch nicht mit diesem Arbeitsbereich verbunden.", "empty": "In dieser Ansicht sind keine Dokumente verfügbar.", "owner": "Verantwortlich", "version": "Version", "deployed_version": "Veröffentlichte Version", "scope": "Offizieller Bereich", "review": "Prüfung fällig", "sharing": "Freigabe", "heading": "Dokumente", "lede": "Durchsuchen Sie die für diesen Arbeitsbereich freigegebenen Dokumente.", "search_heading": "Dokumente durchsuchen", "search": "Suche", "search_action": "Suchen", "search_help": "Durchsuchen Sie veröffentlichte Dokumente nach Titel, Text, Team oder Kanal.", "keyword": "Schlüsselwort", "semantic": "Semantisch", "keyword_fallback": "Die semantische Suche ist nicht verfügbar; die Schlüsselwortsuche wird verwendet.", "why": "Treffer", "review_heading": "Prüfen und veröffentlichen", "review_state": "Prüfstatus", "create_heading": "Privates Dokument erstellen", "title": "Titel", "markdown": "Markdown", "create_help": "Der Entwurf bleibt privat und wird zu einer neuen unveränderlichen Version.", "create_action": "Entwurf erstellen", "create_failed": "Der Entwurf konnte nicht erstellt werden. Ihr Text bleibt erhalten; versuchen Sie es erneut.", "comments_heading": "Kommentare", "comments_empty": "Für diese Version gibt es noch keine Kommentare.", "comments_unavailable": "Kommentare konnten nicht geladen werden.", "comment_author": "Autor", "comment_body": "Kommentar", "comment_action": "Kommentar hinzufügen", "comment_busy": "Kommentar wird hinzugefügt…", "comment_failed": "Der Kommentar konnte nicht hinzugefügt werden. Ihr Text bleibt erhalten; versuchen Sie es erneut.", "comment_help": "Kommentare gehören zu dieser Dokumentversion.", "edit_open": "Dokument bearbeiten", "edit_heading": "Dokument bearbeiten", "edit_help": "Beim Speichern wird eine private, unveränderliche Version erstellt. Veröffentlichen und Teilen bleiben separate Schritte; geteilte Leser sehen bis dahin die veröffentlichte Version.", "edit_save": "Neue Version speichern", "edit_cancel": "Abbrechen", "edit_busy": "Neue Version wird gespeichert…", "edit_saved": "Neue private Version gespeichert. Das Dokument wird aktualisiert; geteilte Leser sehen bis dahin die veröffentlichte Version.", "edit_failed": "Die neue Version konnte nicht gespeichert werden. Ihre Änderungen sind noch vorhanden; versuchen Sie es erneut.", "edit_conflict": "Das Dokument wurde während der Bearbeitung geändert. Laden Sie es neu, führen Sie Ihre Änderungen zusammen und versuchen Sie es erneut.",
	},
	"ar": {
		"private": "مستنداتي", "shared": "مشتركة معي", "team_official": "مستندات الفريق", "channel_official": "مستندات القناة",
		"unavailable": "لم يتم ربط المستندات بمساحة العمل هذه بعد.", "empty": "لا توجد مستندات متاحة في هذا العرض.", "owner": "المالك", "version": "النسخة", "deployed_version": "النسخة المنشورة", "scope": "النطاق الرسمي", "review": "موعد المراجعة", "sharing": "المشاركة", "heading": "المستندات", "lede": "تصفح المستندات المصرح لك بقراءتها في مساحة العمل هذه.", "search_heading": "البحث في المستندات", "search": "بحث", "search_action": "ابحث", "search_help": "ابحث في المستندات المنشورة حسب العنوان أو النص أو الفريق أو القناة.", "keyword": "كلمة مفتاحية", "semantic": "دلالي", "keyword_fallback": "البحث الدلالي غير متاح، لذلك سيتم استخدام البحث بالكلمات المفتاحية.", "why": "سبب التطابق", "review_heading": "المراجعة والنشر", "review_state": "حالة المراجعة", "create_heading": "إنشاء مستند خاص", "title": "العنوان", "markdown": "Markdown", "create_help": "تبدأ المسودة خاصة وتصبح نسخة جديدة غير قابلة للتغيير.", "create_action": "إنشاء مسودة", "create_failed": "تعذر إنشاء المسودة. لا يزال النص محفوظًا هنا؛ حاول مرة أخرى.", "comments_heading": "التعليقات", "comments_empty": "لا توجد تعليقات على هذه النسخة بعد.", "comments_unavailable": "تعذر تحميل التعليقات.", "comment_author": "الكاتب", "comment_body": "التعليق", "comment_action": "إضافة تعليق", "comment_busy": "جارٍ إضافة التعليق…", "comment_failed": "تعذر إضافة التعليق. لا يزال النص محفوظًا هنا؛ حاول مرة أخرى.", "comment_help": "ترتبط التعليقات بنسخة المستند هذه.", "edit_open": "تحرير المستند", "edit_heading": "تحرير المستند", "edit_help": "يؤدي الحفظ إلى إنشاء نسخة خاصة غير قابلة للتغيير. يظل النشر والمشاركة خطوتين منفصلتين؛ وسيواصل القراء المشاركون رؤية النسخة المنشورة حتى ذلك الحين.", "edit_save": "حفظ نسخة جديدة", "edit_cancel": "إلغاء", "edit_busy": "جارٍ حفظ النسخة الجديدة…", "edit_saved": "تم حفظ نسخة خاصة جديدة. جارٍ تحديث المستند؛ وسيواصل القراء المشاركون رؤية النسخة المنشورة حتى ذلك الحين.", "edit_failed": "تعذر حفظ النسخة الجديدة. لا تزال تعديلاتك موجودة؛ حاول مرة أخرى.", "edit_conflict": "تم تغيير المستند أثناء تحريرك له. أعد تحميله وادمج تغييراتك ثم حاول مرة أخرى.",
	},
}

var docsWorkspaceCopy = map[string]map[string]string{
	"en-US": {"create_open": "New document", "create_cancel": "Cancel", "create_busy": "Creating document…", "create_help": "Only you can read this draft until you share or publish it.", "updated": "Updated", "status_private": "Personal", "status_shared": "Shared", "status_team_official": "Team guidance", "status_channel_official": "Channel guidance", "back": "All documents", "markdown_content": "Document content", "browse_all": "All accessible", "browse_private": "Mine", "browse_shared": "Shared with me", "browse_search": "Search documents", "browse_submit": "Search", "browse_clear": "Clear search", "browse_next": "Next page", "browse_count": "on this page", "browse_no_match": "No documents match this search.", "browse_no_shared": "No documents have been shared with you.", "browse_no_private": "You have not created a document yet."},
	"de-DE": {"create_open": "Neues Dokument", "create_cancel": "Abbrechen", "create_busy": "Dokument wird erstellt…", "create_help": "Nur Sie können diesen Entwurf lesen, bis Sie ihn freigeben oder veröffentlichen.", "updated": "Aktualisiert", "status_private": "Persönlich", "status_shared": "Geteilt", "status_team_official": "Teamleitfaden", "status_channel_official": "Kanalleitfaden", "back": "Alle Dokumente", "markdown_content": "Dokumentinhalt", "browse_all": "Alle verfügbaren", "browse_private": "Meine", "browse_shared": "Mit mir geteilt", "browse_search": "Dokumente durchsuchen", "browse_submit": "Suchen", "browse_clear": "Suche löschen", "browse_next": "Nächste Seite", "browse_count": "auf dieser Seite", "browse_no_match": "Keine Dokumente entsprechen dieser Suche.", "browse_no_shared": "Es wurden keine Dokumente mit Ihnen geteilt.", "browse_no_private": "Sie haben noch kein Dokument erstellt."},
	"ar":    {"create_open": "مستند جديد", "create_cancel": "إلغاء", "create_busy": "جارٍ إنشاء المستند…", "create_help": "يمكنك وحدك قراءة هذه المسودة حتى تشاركها أو تنشرها.", "updated": "تم التحديث", "status_private": "شخصي", "status_shared": "مشترك", "status_team_official": "إرشادات الفريق", "status_channel_official": "إرشادات القناة", "back": "كل المستندات", "markdown_content": "محتوى المستند", "browse_all": "كل المتاح", "browse_private": "مستنداتي", "browse_shared": "مشترك معي", "browse_search": "البحث في المستندات", "browse_submit": "بحث", "browse_clear": "مسح البحث", "browse_next": "الصفحة التالية", "browse_count": "في هذه الصفحة", "browse_no_match": "لا توجد مستندات تطابق هذا البحث.", "browse_no_shared": "لم تتم مشاركة أي مستندات معك.", "browse_no_private": "لم تنشئ مستندًا بعد."},
}

func docsText(locale, key string) string {
	if copy, ok := docsWorkspaceCopy[locale]; ok && copy[key] != "" {
		return copy[key]
	}
	if copy, ok := docsCopy[locale]; ok && copy[key] != "" {
		return copy[key]
	}
	if fallback := map[string]string{"open": "Open"}[key]; fallback != "" {
		return fallback
	}
	if copy := docsWorkspaceCopy["en-US"]; copy[key] != "" {
		return copy[key]
	}
	return docsCopy["en-US"][key]
}

func docsPage(view View) ui.Node {
	locale := view.Locale.Resolved
	if view.Document != nil {
		return docsDocumentDetail(view, *view.Document)
	}
	if !view.DocumentsReady {
		return html.Div(html.Props{Class: "docs-hub"},
			html.P(html.Props{Class: "docs-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "unavailable"))),
		)
	}
	children := make([]ui.Node, 0, 6)
	if view.CreateDocument != nil {
		children = append(children, ui.CreateElement(docsCreateForm, docsCreateFormProps{Locale: locale, Create: view.CreateDocument}))
	}
	children = append(children, docsBrowseControls(view))
	if view.DocumentSearch.Ready {
		children = append(children, docsSearch(view))
	}
	groups := []struct {
		status DocumentStatus
		key    string
	}{
		{DocumentPrivate, "private"}, {DocumentShared, "shared"},
		{DocumentTeamOfficial, "team_official"}, {DocumentChannelOfficial, "channel_official"},
	}
	sections := make([]ui.Node, 0, len(groups))
	for _, group := range groups {
		rows := make([]ui.Node, 0)
		for _, document := range view.Documents {
			if document.Status != group.status || strings.TrimSpace(document.Title) == "" {
				continue
			}
			rows = append(rows, docsListRow(view, document))
		}
		if len(rows) == 0 {
			continue
		}
		sections = append(sections, html.Tag("section", html.Props{Class: "docs-section"},
			html.Div(html.Props{Class: "docs-section-heading"},
				html.H2(html.Props{}, ui.Text(docsText(locale, group.key))),
				html.Span(html.Props{Class: "docs-count"}, ui.Text(strconv.Itoa(len(rows))+" "+docsText(locale, "browse_count"))),
			),
			html.Ul(html.Props{Class: "docs-list"}, rows...),
		))
	}
	if len(sections) == 0 {
		message := "empty"
		if view.DocumentQuery != "" {
			message = "browse_no_match"
		} else if view.DocumentCollection == "shared" {
			message = "browse_no_shared"
		} else if view.DocumentCollection == "private" {
			message = "browse_no_private"
		}
		children = append(children, html.P(html.Props{Class: "docs-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, message))))
	} else {
		children = append(children, sections...)
	}
	if view.DocumentNextPageToken != "" {
		children = append(children, html.Nav(html.Props{Class: "docs-pagination", Raw: map[string]any{"aria-label": docsText(locale, "browse_next")}},
			appLink(view, html.Props{Class: "button secondary"}, docsBrowseHref(view.DocumentQuery, view.DocumentCollection, view.DocumentNextPageToken), ui.Text(docsText(locale, "browse_next"))),
		))
	}
	if reviews := docsReviewControls(locale, view.DocumentReviews); reviews != nil {
		children = append(children, reviews)
	}
	return html.Div(html.Props{Class: "docs-hub"}, children...)
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

func docsBrowseControls(view View) ui.Node {
	locale := view.Locale.Resolved
	collection := view.DocumentCollection
	if collection == "" {
		collection = "all"
	}
	tabs := make([]ui.Node, 0, 3)
	for _, option := range []struct{ value, label string }{{"all", "browse_all"}, {"private", "browse_private"}, {"shared", "browse_shared"}} {
		props := html.Props{Href: docsBrowseHref(view.DocumentQuery, option.value, ""), Class: "docs-collection-link"}
		if collection == option.value {
			props.Raw = map[string]any{"aria-current": "page"}
		}
		tabs = append(tabs, appLink(view, props, props.Href, ui.Text(docsText(locale, option.label))))
	}
	form := []ui.Node{
		html.Label(html.Props{For: "docs-browse-query", Class: "sr-only"}, ui.Text(docsText(locale, "browse_search"))),
		html.Input(html.Props{ID: "docs-browse-query", Name: "docs_q", Type: "search", Value: view.DocumentQuery, Placeholder: docsText(locale, "browse_search"), MaxLength: 200}),
		html.Button(html.Props{Type: "submit", Class: "button secondary"}, ui.Text(docsText(locale, "browse_submit"))),
	}
	if collection != "all" {
		form = append(form, html.Input(html.Props{Type: "hidden", Name: "collection", Value: collection}))
	}
	if view.DocumentQuery != "" {
		form = append(form, appLink(view, html.Props{Class: "docs-clear"}, docsBrowseHref("", collection, ""), ui.Text(docsText(locale, "browse_clear"))))
	}
	query := view.DocumentQuery
	input := html.Props{ID: "docs-browse-query", Name: "docs_q", Type: "search", Value: view.DocumentQuery, Placeholder: docsText(locale, "browse_search"), MaxLength: 200}
	input.OnInput = ui.UseEvent(func(event ui.InputEvent) { query = event.GetValue() })
	form[1] = html.Input(input)
	formProps := html.Props{Action: "/workspace/app/docs", Method: "get", Class: "docs-browse-form", Raw: map[string]any{"role": "search"}}
	if view.Navigate != nil {
		formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			view.Navigate(docsBrowseHref(strings.TrimSpace(query), collection, ""))
		})
	}
	return html.Section(html.Props{Class: "docs-browse", Raw: map[string]any{"aria-label": docsText(locale, "browse_search")}},
		html.Form(formProps, form...),
		html.Nav(html.Props{Class: "docs-collections", Raw: map[string]any{"aria-label": docsText(locale, "browse_all")}}, tabs...),
	)
}

func docsListRow(view View, document DocumentSummary) ui.Node {
	locale := view.Locale.Resolved
	meta := make([]ui.Node, 0, 4)
	if owner := docsDisplayOwner(document); owner != "" {
		meta = append(meta, html.Span(html.Props{}, ui.Text(owner)))
	}
	if scope := strings.TrimSpace(document.Scope); scope != "" {
		meta = append(meta, html.Span(html.Props{}, ui.Text(scope)))
	}
	if version := docsDisplayVersion(document); version != "" {
		meta = append(meta, html.Span(html.Props{}, ui.Text(docsText(locale, "version")+" "+version)))
	}
	if sharing := strings.TrimSpace(document.Sharing); sharing != "" && sharing != string(document.Status) {
		meta = append(meta, html.Span(html.Props{}, ui.Text(sharing)))
	}
	if updated := strings.TrimSpace(document.UpdatedAt); updated != "" {
		meta = append(meta, html.Span(html.Props{}, ui.Text(docsText(locale, "updated")+" "+updated)))
	}
	if due := strings.TrimSpace(document.ReviewDue); due != "" {
		meta = append(meta, html.Span(html.Props{}, ui.Text(docsText(locale, "review")+" "+due)))
	}
	status := docsText(locale, "status_"+docsDisplayStatus(document))
	return html.Li(html.Props{Class: "docs-row", Data: map[string]string{"document-id": document.ID, "owner-id": document.OwnerID, "version-id": document.VersionID}},
		appLink(view, html.Props{Class: "docs-row-link", Raw: map[string]any{"aria-label": docsText(locale, "open") + ": " + document.Title}}, docsDocumentHref(document.ID),
			html.Span(html.Props{Class: "docs-row-main"},
				html.Span(html.Props{Class: "docs-row-title"}, ui.Text(document.Title)),
				html.Span(html.Props{Class: "docs-row-meta"}, meta...),
			),
			html.Span(html.Props{Class: "docs-status docs-status-" + string(document.Status)}, ui.Text(status)),
		),
	)
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

func docsDocumentDetail(view View, document DocumentDetail) ui.Node {
	locale := view.Locale.Resolved
	editOpen := ui.UseState(false)
	fields := make([]ui.Node, 0, 5)
	sharing := strings.TrimSpace(document.Summary.Sharing)
	if sharing == "" {
		sharing = string(document.Summary.Status)
	}
	if sharing != "" {
		label := docsText(locale, "status_"+sharing)
		if label == "" {
			label = sharing
		}
		fields = append(fields, docsMetaField(locale, "sharing", label))
	}
	if owner := docsDisplayOwner(document.Summary); owner != "" {
		fields = append(fields, docsMetaField(locale, "owner", owner))
	}
	if version := docsDisplayVersion(document.Summary); version != "" {
		fields = append(fields, docsMetaField(locale, "version", version))
	}
	if document.Summary.Scope != "" {
		fields = append(fields, docsMetaField(locale, "scope", document.Summary.Scope))
	}
	if document.Summary.UpdatedAt != "" {
		fields = append(fields, docsMetaField(locale, "updated", document.Summary.UpdatedAt))
	}
	reader := html.Section(html.Props{Class: "docs-reader", Raw: map[string]any{"aria-labelledby": "docs-reader-heading"}},
		html.H3(html.Props{ID: "docs-reader-heading", Class: "sr-only"}, ui.Text(docsText(locale, "markdown_content"))),
		html.Div(html.Props{Class: "docs-markdown", Raw: map[string]any{"tabindex": "0"}}, docsASTMarkdownNodes(view, document.Markdown)...),
	)
	rail := []ui.Node{docsCommentThread(locale, document.Comments, document.CommentsUnavailable)}
	if document.Summary.CanManageAccess && view.ShareDocument != nil {
		rail = append(rail, ui.CreateElement(docsShareForm, docsShareFormProps{Locale: locale, DocumentID: document.Summary.ID, Origin: view.DocumentOrigin, People: view.People, Share: view.ShareDocument}))
	}
	if document.CanComment && view.AddDocumentComment != nil {
		rail = append(rail, ui.CreateElement(docsCommentForm, docsCommentFormProps{Locale: locale, DocumentID: document.Summary.ID, VersionID: document.Summary.VersionID, Add: view.AddDocumentComment}))
	}
	var primary ui.Node = reader
	if document.CanEdit && view.CreateDocumentVersion != nil {
		primary = ui.CreateElement(docsEditForm, docsEditFormProps{Locale: locale, DocumentID: document.Summary.ID, BaseVersionID: document.Summary.VersionID, Title: document.Summary.Title, Markdown: document.Markdown, Open: editOpen, Save: view.CreateDocumentVersion})
		if !editOpen.Get() {
			primary = reader
		}
	}
	headerTitle := []ui.Node{html.H2(html.Props{}, ui.Text(document.Summary.Title))}
	if document.CanEdit && view.CreateDocumentVersion != nil && !editOpen.Get() {
		headerTitle = append(headerTitle, html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) { editOpen.Set(true) })}, ui.Text(docsText(locale, "edit_open"))))
	}
	children := []ui.Node{
		appLink(view, html.Props{Class: "button secondary docs-back"}, docsBrowseHref(view.DocumentQuery, view.DocumentCollection, view.DocumentPageToken), ui.Text(docsText(locale, "back"))),
		html.Header(html.Props{Class: "docs-detail-header"},
			html.Div(html.Props{Class: "docs-detail-title-row"}, headerTitle...),
			html.Tag("dl", html.Props{Class: "docs-detail-meta"}, fields...),
		),
		html.Div(html.Props{Class: "docs-detail-layout"},
			html.Div(html.Props{Class: "docs-detail-content"}, primary),
			html.Aside(html.Props{Class: "docs-detail-rail"}, rail...),
		),
	}
	return html.Article(html.Props{Class: "docs-detail", Data: map[string]string{"document-id": document.Summary.ID, "version-id": document.Summary.VersionID}}, children...)
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
.docs-hub{width:100%;display:grid;gap:calc(var(--hcm-space-3)*var(--hcm-density));padding-block:var(--hcm-space-1) var(--hcm-space-4)}
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
.docs-create-actions{display:flex;justify-content:flex-start;margin-block-start:var(--hcm-space-1)}
.docs-browse{display:grid;gap:var(--hcm-space-2);min-width:0}
.docs-browse-form{display:flex;align-items:center;gap:var(--hcm-space-1);max-width:48rem}
.docs-browse-form input[type=search]{flex:1;min-width:0;min-height:var(--hcm-control-height);padding:var(--hcm-space-1) var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-clear{color:var(--accent);font-size:var(--hcm-font-size-small)}
.docs-collections{display:flex;flex-wrap:nowrap;gap:var(--hcm-space-1);overflow-x:auto;border-block-end:1px solid var(--line)}
.docs-collection-link{display:inline-flex;align-items:center;flex:none;white-space:nowrap;min-height:var(--hcm-control-height);padding-inline:var(--hcm-space-1);border-block-end:2px solid transparent;color:var(--muted);text-decoration:none;font-weight:600}
.docs-collection-link:hover{color:var(--ink);background:var(--surface-subtle,var(--soft))}
.docs-collection-link[aria-current=page]{color:var(--ink);border-block-end-color:var(--accent)}
.docs-collection-link:focus-visible,.docs-clear:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.docs-pagination{display:flex;justify-content:flex-end}
.docs-section{min-width:0}
.docs-section-heading{display:flex;align-items:baseline;gap:var(--hcm-space-1);margin-block-end:var(--hcm-space-1)}
.docs-section-heading h2{margin:0;font-size:var(--hcm-font-size-body);font-weight:700}
.docs-count{color:var(--muted);font-size:var(--hcm-font-size-small);font-variant-numeric:tabular-nums}
.docs-list{list-style:none;margin:0;padding:0;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-resting);overflow:hidden}
.docs-list>.docs-row{display:block;width:100%;max-width:none;min-width:0}
.docs-row+.docs-row{border-block-start:1px solid var(--line)}
.docs-row-link{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-2);min-height:var(--hcm-control-height);padding:calc(var(--hcm-space-2)*var(--hcm-density));color:var(--ink);text-decoration:none;transition:background var(--hcm-motion-fast) var(--hcm-motion-easing)}
.docs-row-link:hover{background:var(--surface-subtle,var(--soft))}
.docs-row-link:focus-visible{position:relative;outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:calc(-1*var(--hcm-focus-ring-width))}
.docs-row-main{display:grid;gap:var(--hcm-space-1);min-width:0}
.docs-row-title{font-weight:650;overflow-wrap:anywhere}
.docs-row-meta{display:flex;flex-wrap:wrap;gap:var(--hcm-space-1);color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-row-meta:empty{display:none}
.docs-row-meta>span+span::before{content:"·";margin-inline-end:var(--hcm-space-1)}
.docs-status{flex:none;padding:calc(var(--hcm-space-1)/2) var(--hcm-space-1);border-radius:var(--hcm-radius-control);font-size:var(--hcm-font-size-small);font-weight:600;line-height:var(--hcm-line-height)}
.docs-status-private{background:var(--surface-subtle,var(--soft));color:var(--muted)}
.docs-status-shared{background:var(--hcm-color-brand-soft);color:var(--accent)}
.docs-status-team_official,.docs-status-channel_official{background:var(--hcm-color-info-surface);color:var(--hcm-color-info)}
.docs-empty{margin:0;padding:var(--hcm-space-3);background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);color:var(--muted)}
.docs-detail{width:100%;max-width:120rem;margin-inline:auto;display:grid;gap:var(--hcm-space-2);padding-block:var(--hcm-space-1) var(--hcm-space-3)}
.docs-share{display:grid;gap:var(--hcm-space-1);max-width:48rem;padding:calc(var(--hcm-space-3)*var(--hcm-density));background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-resting)}
.docs-share h3{margin:0}.docs-share-url,.docs-share-form input{width:100%;box-sizing:border-box;min-height:var(--hcm-control-height);padding:var(--hcm-space-1) var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-share-form{display:grid;gap:var(--hcm-space-1)}
.docs-edit{display:grid;gap:var(--hcm-space-1);padding:var(--hcm-space-2);background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface)}.docs-edit h3{margin:0}.docs-edit-form{display:grid;gap:var(--hcm-space-1)}.docs-edit-form input,.docs-edit-form textarea{width:100%;box-sizing:border-box;min-height:var(--hcm-control-height);padding:var(--hcm-space-1) var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}.docs-edit-form textarea{min-height:14lh;resize:vertical;font-family:var(--hcm-font-mono)}.docs-edit-actions{display:flex;gap:var(--hcm-space-1);flex-wrap:wrap}
.docs-back{justify-self:start}
.docs-detail-header{display:grid;gap:var(--hcm-space-2)}
.docs-detail-title-row{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-2);flex-wrap:wrap}.docs-detail-title-row h2{min-width:0;flex:1}
.docs-detail-header h2{margin:0;overflow-wrap:anywhere}
.docs-detail-layout{display:grid;grid-template-columns:minmax(0,1fr) minmax(18rem,22rem);align-items:start;gap:var(--hcm-space-3)}
.docs-detail-content{min-width:0;display:grid;gap:var(--hcm-space-3)}
.docs-detail-rail{min-width:0;display:grid;gap:var(--hcm-space-2);position:sticky;top:var(--hcm-space-2)}
.docs-detail-meta{display:flex;flex-wrap:wrap;gap:var(--hcm-space-2) var(--hcm-space-3);margin:0;padding:0}
.docs-detail-meta>div{display:grid;gap:calc(var(--hcm-space-1)/2)}
.docs-detail-meta dt{color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-detail-meta dd{margin:0;font-weight:600;overflow-wrap:anywhere}
.docs-reader{min-width:0;padding:calc(var(--hcm-space-3)*var(--hcm-density));background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-resting)}
.docs-comments,.docs-comment-compose{display:grid;gap:var(--hcm-space-2);padding:calc(var(--hcm-space-3)*var(--hcm-density));background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-resting)}
.docs-comments h3,.docs-comment-compose h3{margin:0}.docs-comment-list{display:grid;gap:var(--hcm-space-2);list-style:none;margin:0;padding:0}.docs-comment{padding:var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-control)}.docs-comment-meta{margin:0 0 var(--hcm-space-1);color:var(--muted);font-size:var(--hcm-font-size-small)}.docs-comment-body{margin:0;white-space:pre-wrap;overflow-wrap:anywhere}.docs-comment-empty{color:var(--muted)}.docs-comment-form{display:grid;gap:var(--hcm-space-1)}.docs-comment-form textarea{width:100%;box-sizing:border-box;min-height:7lh;padding:var(--hcm-space-1) var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);resize:vertical}
.docs-reader>h3{margin:0 0 var(--hcm-space-2);font-size:var(--hcm-font-size-small);color:var(--muted);font-weight:600}
.docs-markdown{min-width:0;max-width:85ch;overflow-wrap:anywhere;font:inherit;line-height:var(--hcm-line-height)}
.docs-markdown>:first-child{margin-block-start:0}.docs-markdown>:last-child{margin-block-end:0}
.docs-markdown h3,.docs-markdown h4,.docs-markdown h5,.docs-markdown h6{margin:var(--hcm-space-3) 0 var(--hcm-space-1);color:var(--ink);font-weight:700;line-height:1.3}
.docs-markdown h3{font-size:var(--hcm-font-size-heading)}.docs-markdown h4{font-size:var(--hcm-font-size-body)}
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
@media(max-width:900px){.docs-detail-layout{grid-template-columns:minmax(0,1fr)}.docs-detail-rail{position:static}.docs-detail-rail>*{max-width:none}}
@media(max-width:700px){.docs-row-link{align-items:flex-start;flex-direction:column}.docs-create-header{align-items:flex-start;flex-direction:column}.docs-create-header .button{order:-1;align-self:flex-end}.docs-detail-meta{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:390px){.docs-detail-meta{grid-template-columns:minmax(0,1fr)}.docs-reader,.docs-create,.docs-search,.docs-review-item{padding:var(--hcm-space-2)}.docs-browse-form{flex-wrap:wrap}.docs-browse-form input[type=search]{flex-basis:100%}}
@media(prefers-reduced-motion:reduce){.docs-row-link{transition:none}}
`
}
