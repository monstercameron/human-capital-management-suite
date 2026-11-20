package chatui

import (
	"path"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3917: Shared files.
//
// Conversation details offer a Shared files entry that lists the attachments of
// the open conversation, threads included, so a person who remembers that a file
// was shared but not its name can scan for it. The list is the Chat search's File
// kind scoped to this conversation: the server answers it with the same
// membership, history, deletion and message-access rules as search, one page at
// a time. Nothing is copied: a row opens its source message, and Download uses
// the attachment path the message itself uses, which checks access again.
//
// Only attachments are listed. A document a message links to is not an uploaded
// file and stays in the message that links it.

// The states of the list, as the browser reports them.
const (
	SharedFilesLoading     = "loading"
	SharedFilesReady       = "ready"
	SharedFilesError       = "error"
	SharedFilesUnavailable = "unavailable"
)

// SharedFile is one attachment of the conversation as the list shows it.
type SharedFile struct {
	// ID is the row's identity: the message and the attachment, joined.
	ID              string
	PostID, MediaID string
	Name            string
	SenderID        string
	At              time.Time
	Sequence        uint64
	ThreadID        string
	ThreadSequence  uint64
}

// SharedFilesView is the list for one conversation. Another conversation's view
// draws nothing.
type SharedFilesView struct {
	ConversationID string
	Status         string
	Files          []SharedFile
	// More is true while older files can be read; LoadingMore while they are.
	More, LoadingMore bool
	// Name is the file-name search applied to the list.
	Name string
	// ReturnID is the row whose message was opened last, so coming back to the
	// panel lands on it.
	ReturnID string
}

var issue3917Copy = map[string][3]string{
	"title":           {"Shared files", "Geteilte Dateien", "الملفات المشتركة"},
	"show":            {"Show shared files", "Geteilte Dateien anzeigen", "عرض الملفات المشتركة"},
	"hide":            {"Hide shared files", "Geteilte Dateien ausblenden", "إخفاء الملفات المشتركة"},
	"name_label":      {"File name", "Dateiname", "اسم الملف"},
	"search":          {"Search", "Suchen", "بحث"},
	"clear":           {"Clear search", "Suche zurücksetzen", "مسح البحث"},
	"type_group":      {"File type", "Dateityp", "نوع الملف"},
	"type_all":        {"All", "Alle", "الكل"},
	"type_image":      {"Images", "Bilder", "الصور"},
	"type_document":   {"Documents", "Dokumente", "المستندات"},
	"type_media":      {"Audio and video", "Audio und Video", "الصوت والفيديو"},
	"type_other":      {"Other", "Sonstige", "أخرى"},
	"plural_image":    {"images", "Bilder", "صور"},
	"plural_document": {"documents", "Dokumente", "مستندات"},
	"plural_media":    {"audio or video files", "Audio- oder Videodateien", "ملفات صوت أو فيديو"},
	"plural_other":    {"other files", "sonstige Dateien", "ملفات أخرى"},
	"kind_image":      {"Image", "Bild", "صورة"},
	"kind_document":   {"Document", "Dokument", "مستند"},
	"kind_media":      {"Audio or video", "Audio oder Video", "صوت أو فيديو"},
	"kind_other":      {"File", "Datei", "ملف"},
	"loading":         {"Loading shared files…", "Geteilte Dateien werden geladen …", "جارٍ تحميل الملفات المشتركة…"},
	"loading_more":    {"Loading older files…", "Ältere Dateien werden geladen …", "جارٍ تحميل الملفات الأقدم…"},
	"more":            {"Show older files", "Ältere Dateien anzeigen", "عرض الملفات الأقدم"},
	"empty":           {"No files have been shared in this conversation yet.", "In dieser Unterhaltung wurden noch keine Dateien geteilt.", "لم تتم مشاركة أي ملفات في هذه المحادثة بعد."},
	"empty_name":      {"No files are named like “{query}”. Clear the search to see every file.", "Keine Datei heißt ähnlich wie „{query}“. Setzen Sie die Suche zurück, um alle Dateien zu sehen.", "لا يوجد ملف باسم يشبه “{query}”. امسح البحث لعرض كل الملفات."},
	"empty_type":      {"None of the files in this conversation are {type}.", "Keine der Dateien in dieser Unterhaltung gehört zu {type}.", "لا يوجد بين ملفات هذه المحادثة أي من {type}."},
	"empty_type_more": {"None of the files loaded so far are {type}. Show older files to keep looking.", "Keine der bisher geladenen Dateien gehört zu {type}. Zeigen Sie ältere Dateien an, um weiterzusuchen.", "لا يوجد بين الملفات المحمّلة حتى الآن أي من {type}. اعرض الملفات الأقدم لمواصلة البحث."},
	"error":           {"Shared files could not be loaded. Check your connection and try again.", "Geteilte Dateien konnten nicht geladen werden. Prüfen Sie die Verbindung und versuchen Sie es erneut.", "تعذّر تحميل الملفات المشتركة. تحقق من اتصالك وحاول مرة أخرى."},
	"unavailable":     {"Shared files are not available right now. Try again later.", "Geteilte Dateien sind derzeit nicht verfügbar. Versuchen Sie es später erneut.", "الملفات المشتركة غير متاحة حاليًا. حاول مرة أخرى لاحقًا."},
	"retry":           {"Try again", "Erneut versuchen", "أعد المحاولة"},
	"count":           {"Files shown: {count}", "Angezeigte Dateien: {count}", "الملفات المعروضة: {count}"},
	"note":            {"Only attachments on messages you can read are listed. Linked documents are not files; they stay in the message that links them.", "Aufgeführt werden nur Anhänge von Nachrichten, die Sie lesen können. Verknüpfte Dokumente sind keine Dateien; sie bleiben in der Nachricht, die sie verknüpft.", "تُعرض فقط المرفقات في الرسائل التي يمكنك قراءتها. المستندات المرتبطة ليست ملفات؛ وتبقى في الرسالة التي ترتبط بها."},
	"in_thread":       {"In a thread", "In einem Thread", "في سلسلة رد"},
	"view":            {"View message", "Nachricht anzeigen", "عرض الرسالة"},
	"view_label":      {"View the message with {name}", "Nachricht mit {name} anzeigen", "عرض الرسالة التي تحتوي على {name}"},
	"download":        {"Download", "Herunterladen", "نزّل"},
}

func issue3917Text(m Model, key string) string {
	copy := issue3917Copy[key]
	return chatbug039Text("chat.issue3917."+key, copy[chatbug039LocaleIndex(m.Locale)], copy[0])
}

// issue3917Kind groups a file by its name: image, document, media (audio or
// video) or other.
func issue3917Kind(name string) string {
	switch strings.ToLower(strings.TrimPrefix(path.Ext(strings.TrimSpace(name)), ".")) {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "svg", "heic", "avif", "tif", "tiff":
		return "image"
	case "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "odt", "ods", "odp", "txt", "md", "csv", "rtf":
		return "document"
	case "mp3", "wav", "m4a", "ogg", "oga", "flac", "aac", "opus", "mp4", "mov", "webm", "mkv", "avi", "m4v":
		return "media"
	}
	return "other"
}

// issue3917KindLabel is the short type shown on a row: the extension in capitals
// when the name has one, else the group's word.
func issue3917KindLabel(m Model, name string) string {
	if ext := strings.ToUpper(strings.TrimPrefix(path.Ext(strings.TrimSpace(name)), ".")); ext != "" && len(ext) <= 5 {
		return ext
	}
	return issue3917Text(m, "kind_"+issue3917Kind(name))
}

var issue3917Types = []string{"all", "image", "document", "media", "other"}

// issue3917Visible is the files the chosen type keeps, in order.
func issue3917Visible(files []SharedFile, kind string) []SharedFile {
	if kind == "" || kind == "all" {
		return files
	}
	kept := make([]SharedFile, 0, len(files))
	for _, file := range files {
		if issue3917Kind(file.Name) == kind {
			kept = append(kept, file)
		}
	}
	return kept
}

func issue3917Sender(m Model, file SharedFile) string {
	if name := chatKnownName(m, file.SenderID); name != "" {
		if file.SenderID == m.CurrentUser {
			return m.t(KeyYou)
		}
		return name
	}
	return m.t(KeyTodoMemberFallback)
}

func issue3917Row(m Model, file SharedFile) ui.Node {
	meta := []string{issue3917KindLabel(m, file.Name), issue3917Sender(m, file)}
	if !file.At.IsZero() {
		meta = append(meta, chatux019PinTime(m, file.At))
	}
	if file.ThreadID != "" {
		meta = append(meta, issue3917Text(m, "in_thread"))
	}
	view := issue3917Text(m, "view")
	label := chatcmd002Fill(issue3917Text(m, "view_label"), "name", file.Name)
	download := Chatattach001Text(m.Locale, "download")
	return html.Li(html.Props{Class: "shared-file-row", Data: map[string]string{"shared-file-id": file.ID}},
		html.Div(html.Props{Class: "shared-file-text"},
			html.Strong(html.Props{Class: "shared-file-name", Dir: "auto", Text: file.Name}),
			html.Span(html.Props{Class: "shared-file-meta", Dir: "auto", Text: strings.Join(meta, " · ")})),
		html.Div(html.Props{Class: "shared-file-actions"},
			html.Button(html.Props{Class: "button secondary small shared-file-open", Type: "button", Disabled: m.Callbacks.OpenSharedFileSource == nil, Title: label,
				Data: map[string]string{"action": "shared-file-open", "id": file.ID}, Aria: map[string]string{"label": label}, Text: view}),
			html.Button(html.Props{Class: "button secondary small shared-file-download", Type: "button", Disabled: m.Callbacks.DownloadAttachment == nil || file.PostID == "" || file.MediaID == "",
				Data: map[string]string{"action": "download-attachment", "id": file.PostID, "extra": file.MediaID}, Aria: map[string]string{"label": download + ": " + file.Name}, Title: download + ": " + file.Name, Text: download})))
}

// issue3917Section is the Shared files entry of Conversation details. It is nil
// when the client cannot list files. Collapsed it shows nothing of the files,
// not even a count; the list is read when the entry is first opened.
func issue3917Section(m Model, h handlers) ui.Node {
	if m.Callbacks.LoadSharedFiles == nil || m.SelectedID == "" {
		return nil
	}
	view := m.SharedFiles
	if view.ConversationID != m.SelectedID {
		view = SharedFilesView{}
	}
	open := h.local.sharedFilesOpen
	toggleLabel := issue3917Text(m, "show")
	if open {
		toggleLabel = issue3917Text(m, "hide")
	}
	children := []ui.Node{html.Div(html.Props{Class: "details-section-head"},
		html.H3(html.Props{Text: issue3917Text(m, "title")}),
		html.Div(html.Props{Class: "details-section-actions"},
			html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "shared-files-toggle"},
				Aria: map[string]string{"expanded": boolString(open), "controls": "chat-details-files-body"}, Text: toggleLabel})))}
	if open {
		children = append(children, issue3917Body(m, h, view))
	}
	return html.Section(html.Props{ID: "chat-details-files", Class: "details-section details-shared-files", Dir: direction(m.Locale), Aria: map[string]string{"label": issue3917Text(m, "title")}}, children...)
}

func issue3917Body(m Model, h handlers, view SharedFilesView) ui.Node {
	kind := h.local.sharedFilesType
	if kind == "" {
		kind = "all"
	}
	nameValue := view.Name
	search := html.Div(html.Props{Class: "shared-files-search"},
		html.Label(html.Props{For: "chat-shared-files-name", Text: issue3917Text(m, "name_label")}),
		html.Div(html.Props{Class: "shared-files-search-row"},
			html.Input(html.Props{ID: "chat-shared-files-name", Class: "chat-input", Type: "search", Name: "name", MaxLength: 120, AutoComplete: "off", Data: map[string]string{"chat-value": nameValue}}),
			html.Button(html.Props{Class: "button small", Type: "button", Disabled: view.Status == SharedFilesLoading, Data: map[string]string{"action": "shared-files-search"}, Text: issue3917Text(m, "search")})))
	if nameValue != "" {
		search = html.Div(html.Props{Class: "shared-files-search"}, search,
			html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "shared-files-clear"}, Text: issue3917Text(m, "clear")}))
	}
	chips := make([]ui.Node, 0, len(issue3917Types))
	for _, t := range issue3917Types {
		chips = append(chips, html.Button(html.Props{Class: "shared-files-chip", Type: "button",
			Data: map[string]string{"action": "shared-files-type", "id": t}, Aria: map[string]string{"pressed": boolString(t == kind)}, Text: issue3917Text(m, "type_"+t)}))
	}
	nodes := []ui.Node{search, html.Div(html.Props{Class: "shared-files-types", Role: "group", Aria: map[string]string{"label": issue3917Text(m, "type_group")}}, chips...)}
	status, statusRole, list := "", "status", ui.Node(nil)
	loadedAny := len(view.Files) > 0
	switch {
	case view.Status == SharedFilesError || view.Status == SharedFilesUnavailable:
		status, statusRole = issue3917Text(m, view.Status), "alert"
	case view.Status == "" || (view.Status == SharedFilesLoading && !loadedAny):
		status = issue3917Text(m, "loading")
	default:
		shown := issue3917Visible(view.Files, kind)
		switch {
		case len(shown) > 0:
			rows := make([]ui.Node, 0, len(shown))
			for _, file := range shown {
				rows = append(rows, issue3917Row(m, file))
			}
			list = html.Ul(html.Props{Class: "shared-files-list", Aria: map[string]string{"label": issue3917Text(m, "title")}}, rows...)
			status = chatcmd002Fill(issue3917Text(m, "count"), "count", m.n(len(shown)))
		case loadedAny:
			status = chatcmd002Fill(issue3917Text(m, "empty_type"), "type", issue3917Text(m, "plural_"+kind))
			if view.More {
				status = chatcmd002Fill(issue3917Text(m, "empty_type_more"), "type", issue3917Text(m, "plural_"+kind))
			}
		case view.Name != "":
			status = chatcmd002Fill(issue3917Text(m, "empty_name"), "query", view.Name)
		default:
			status = issue3917Text(m, "empty")
		}
	}
	nodes = append(nodes, html.P(html.Props{ID: "chat-shared-files-status", Class: "shared-files-status", Role: statusRole, Aria: map[string]string{"live": "polite"}, Text: status}))
	if view.Status == SharedFilesError || view.Status == SharedFilesUnavailable {
		nodes = append(nodes, html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "shared-files-retry"}, Text: issue3917Text(m, "retry")}))
	}
	if list != nil {
		nodes = append(nodes, list)
	}
	if view.More && view.Status != SharedFilesError && view.Status != SharedFilesUnavailable {
		more := issue3917Text(m, "more")
		if view.LoadingMore {
			more = issue3917Text(m, "loading_more")
		}
		nodes = append(nodes, html.Button(html.Props{Class: "button secondary small shared-files-more", Type: "button", Disabled: view.LoadingMore || m.Callbacks.MoreSharedFiles == nil,
			Data: map[string]string{"action": "shared-files-more"}, Text: more}))
	}
	nodes = append(nodes, html.P(html.Props{Class: "field-hint shared-files-note", Text: issue3917Text(m, "note")}))
	return html.Div(html.Props{ID: "chat-details-files-body", Class: "shared-files-body"}, nodes...)
}

// issue3917Click handles the entry's buttons. It reports whether the press was
// one of them.
func issue3917Click(e ui.MouseEvent, m Model, local localStore) bool {
	action, id, _ := eventAction(e)
	switch action {
	case "shared-files-toggle":
		open := !local.get().sharedFilesOpen
		local.update(func(u *localUI) { u.sharedFilesOpen = open })
		// Opening the entry reads the list again, so a file whose message was
		// removed since the last look is not shown from memory.
		name := ""
		if m.SharedFiles.ConversationID == m.SelectedID {
			name = m.SharedFiles.Name
		}
		if open && m.Callbacks.LoadSharedFiles != nil {
			m.Callbacks.LoadSharedFiles(name)
		}
	case "shared-files-search":
		if m.Callbacks.LoadSharedFiles != nil {
			m.Callbacks.LoadSharedFiles(strings.TrimSpace(domValue("chat-shared-files-name")))
		}
	case "shared-files-clear":
		setDOMValue("chat-shared-files-name", "")
		if m.Callbacks.LoadSharedFiles != nil {
			m.Callbacks.LoadSharedFiles("")
		}
	case "shared-files-retry":
		if m.Callbacks.LoadSharedFiles != nil {
			m.Callbacks.LoadSharedFiles(m.SharedFiles.Name)
		}
	case "shared-files-type":
		for _, t := range issue3917Types {
			if t == id {
				local.update(func(u *localUI) { u.sharedFilesType = id })
			}
		}
	case "shared-files-more":
		if m.Callbacks.MoreSharedFiles != nil && !m.SharedFiles.LoadingMore {
			m.Callbacks.MoreSharedFiles()
		}
	case "shared-file-open":
		if m.Callbacks.OpenSharedFileSource != nil && id != "" {
			m.Callbacks.OpenSharedFileSource(id)
		}
	default:
		return false
	}
	return true
}

// issue3917Key gives the name field Enter (search). It reports whether the key
// was used.
func issue3917Key(e ui.KeyboardEvent, m Model) bool {
	if e.GetKey() != "Enter" || !sharedFilesNameFocused() || m.Callbacks.LoadSharedFiles == nil {
		return false
	}
	m.Callbacks.LoadSharedFiles(strings.TrimSpace(domValue("chat-shared-files-name")))
	return true
}

const issue3917Styles = `.chat-workspace .shared-files-body{display:flex;flex-direction:column;gap:8px;min-width:0}` +
	`.chat-workspace .shared-files-search{display:flex;flex-direction:column;gap:4px;min-width:0}` +
	`.chat-workspace .shared-files-search-row{display:flex;gap:6px;min-width:0}.chat-workspace .shared-files-search-row .chat-input{flex:1 1 auto;min-width:0;box-sizing:border-box}` +
	`.chat-workspace .shared-files-types{display:flex;flex-wrap:wrap;gap:6px}` +
	`.chat-workspace .shared-files-chip{min-height:32px;padding:0 12px;border:1px solid var(--line);border-radius:999px;background:var(--panel);color:inherit;font:inherit;font-size:.8125rem;cursor:pointer}` +
	`.chat-workspace .shared-files-chip[aria-pressed=true]{background:var(--accent-soft,var(--soft));border-color:var(--accent,currentColor);font-weight:600}` +
	`.chat-workspace .shared-files-chip:focus-visible,.chat-workspace .shared-files-body .button:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chat-workspace .shared-files-status{margin:0;color:var(--muted);font-size:.8125rem;overflow-wrap:anywhere}` +
	`.chat-workspace .shared-files-list{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:6px}` +
	`.chat-workspace .shared-file-row{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:6px 8px;padding:8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);min-width:0}` +
	`.chat-workspace .shared-file-text{display:flex;flex-direction:column;gap:2px;min-width:0;flex:1 1 10rem}` +
	`.chat-workspace .shared-file-name{overflow-wrap:anywhere}.chat-workspace .shared-file-meta{color:var(--muted);font-size:.8125rem;overflow-wrap:anywhere}` +
	`.chat-workspace .shared-file-actions{display:flex;flex-wrap:wrap;gap:6px}` +
	`.chat-workspace .shared-files-note{margin:0}` +
	`@media(max-width:560px){.chat-workspace .shared-files-body .button,.chat-workspace .shared-files-chip,.chat-workspace .shared-files-search-row .chat-input{min-height:44px}.chat-workspace .shared-file-actions{flex:1 1 100%}.chat-workspace .shared-file-actions .button{flex:1 1 auto}}`

// SharedFilesText is a word of the Shared files list in the given locale, for
// the notices the browser raises outside the list itself.
func SharedFilesText(locale, key string) string {
	return issue3917Text(Model{Locale: locale}, key)
}
