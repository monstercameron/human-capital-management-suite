package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// ISSUE-3929: searches a person chose to keep. They sit above the automatic
// recent searches in the popover under the search box, with their own name,
// scope line, Edit and Delete, and clearing recent searches never touches them.
// Opening one fills the box and runs the ordinary search, so access is checked
// again; a saved search is words and filters, never results.

// SavedSearchItem is one kept search as the popover draws it.
type SavedSearchItem struct {
	ID    string
	Name  string
	Query string
}

const (
	issue3929NameField  = "chatsearch-saved-name"
	issue3929QueryField = "chatsearch-saved-query"
	issue3929SaveField  = "chatsearch-save-name"
	issue3929NoteID     = "chatsearch-saved-note"
	// Issue3929NoteID is the status line a finished save leaves, so the client
	// can move focus to it.
	Issue3929NoteID = issue3929NoteID
)

func issue3929Text(locale, key string) string {
	copy := map[string][3]string{
		"title":     {"Saved searches", "Gespeicherte Suchen", "عمليات البحث المحفوظة"},
		"group":     {"Saved and recent searches", "Gespeicherte und letzte Suchen", "عمليات البحث المحفوظة والأخيرة"},
		"private":   {"Only you can see these. Opening one runs a new search with the access you have now.", "Nur du siehst diese. Beim Öffnen läuft eine neue Suche mit deinem aktuellen Zugriff.", "لا يراها غيرك. عند فتح أحدها يُجرى بحث جديد بالصلاحيات المتاحة لك الآن."},
		"keeps":     {"Recent searches are remembered automatically. Clearing them keeps your saved searches.", "Letzte Suchen werden automatisch gemerkt. Beim Löschen bleiben deine gespeicherten Suchen erhalten.", "تُحفظ عمليات البحث الأخيرة تلقائياً. مسحها لا يحذف عمليات البحث المحفوظة."},
		"edit":      {"Edit", "Bearbeiten", "تعديل"},
		"delete":    {"Delete", "Löschen", "حذف"},
		"editName":  {"Edit saved search: {name}", "Gespeicherte Suche bearbeiten: {name}", "تعديل البحث المحفوظ: {name}"},
		"delName":   {"Delete saved search: {name}", "Gespeicherte Suche löschen: {name}", "حذف البحث المحفوظ: {name}"},
		"editTitle": {"Edit saved search", "Gespeicherte Suche bearbeiten", "تعديل البحث المحفوظ"},
		"name":      {"Name", "Name", "الاسم"},
		"query":     {"Words and filters", "Wörter und Filter", "الكلمات والمرشحات"},
		"update":    {"Save changes", "Änderungen speichern", "حفظ التغييرات"},
		"cancel":    {"Cancel", "Abbrechen", "إلغاء"},
		"saveLabel": {"Save this search", "Diese Suche speichern", "حفظ هذا البحث"},
		"saveHint":  {"Name it to find it again under the search box. Only you can see it.", "Gib ihr einen Namen, um sie unter dem Suchfeld wiederzufinden. Nur du siehst sie.", "امنحه اسماً لتجده مجدداً أسفل مربع البحث. لا يراه غيرك."},
		"savePlace": {"For example: Onboarding questions", "Zum Beispiel: Fragen zum Onboarding", "مثال: أسئلة التهيئة"},
		"save":      {"Save search", "Suche speichern", "حفظ البحث"},
		"saved":     {"Saved as {name}. Find it under the search box.", "Gespeichert als {name}. Du findest sie unter dem Suchfeld.", "تم الحفظ باسم {name}. ستجده أسفل مربع البحث."},
		"already":   {"Already saved as {name}.", "Bereits gespeichert als {name}.", "محفوظ مسبقاً باسم {name}."},
		"updated":   {"Saved search updated.", "Gespeicherte Suche aktualisiert.", "تم تحديث البحث المحفوظ."},
		"deleted":   {"Saved search deleted.", "Gespeicherte Suche gelöscht.", "تم حذف البحث المحفوظ."},
		"failed":    {"Could not change your saved searches. Nothing was changed. Try again.", "Deine gespeicherten Suchen konnten nicht geändert werden. Es wurde nichts geändert. Bitte erneut versuchen.", "تعذّر تغيير عمليات البحث المحفوظة. لم يتغيّر شيء. حاول مرة أخرى."},
		"nameTaken": {"You already have a saved search with that name. Choose another name.", "Du hast bereits eine gespeicherte Suche mit diesem Namen. Wähle einen anderen Namen.", "لديك بحث محفوظ بهذا الاسم. اختر اسماً آخر."},
		"limit":     {"You can keep up to 50 saved searches. Delete one to save another.", "Du kannst bis zu 50 Suchen speichern. Lösche eine, um eine weitere zu speichern.", "يمكنك حفظ ما يصل إلى 50 بحثاً. احذف أحدها لحفظ آخر."},
		"invalid":   {"Give the search a name, and check its words and filters.", "Gib der Suche einen Namen und prüfe Wörter und Filter.", "امنح البحث اسماً وراجع كلماته ومرشحاته."},
		"broken":    {"This search uses a filter that no longer works, so it will not run. Edit it to fix it.", "Diese Suche nutzt einen Filter, der nicht mehr funktioniert, und läuft deshalb nicht. Bearbeite sie, um das zu beheben.", "يستخدم هذا البحث مرشحاً لم يعد يعمل، لذا لن يُنفَّذ. عدّله لإصلاحه."},
		"noChannel": {"The channel {channel} is not in your conversations now, so this search may find nothing. Edit it to change the channel.", "Der Kanal {channel} gehört gerade nicht zu deinen Unterhaltungen, daher findet diese Suche eventuell nichts. Bearbeite sie, um den Kanal zu ändern.", "القناة {channel} ليست ضمن محادثاتك الآن، لذا قد لا يجد هذا البحث شيئاً. عدّله لتغيير القناة."},
	}[key]
	return chatbug039Text(key, issue3929Pick(copy, locale), copy[0])
}

// issue3929Pick returns the entry for a locale, falling back to English.
func issue3929Pick(copy [3]string, locale string) string {
	if text := copy[chatbug039LocaleIndex(locale)]; strings.TrimSpace(text) != "" {
		return text
	}
	return copy[0]
}

func issue3929Quote(locale, text string) string {
	switch {
	case strings.HasPrefix(locale, "de"):
		return "„" + text + "“"
	case strings.HasPrefix(locale, "ar"):
		return "«" + text + "»"
	}
	return "“" + text + "”"
}

func issue3929Fill(text, name, value string) string {
	return strings.ReplaceAll(text, "{"+name+"}", value)
}

// issue3929ScopeParts says in plain words what a saved query looks for and
// where, so the scope is clear before it runs. A channel or person is named as
// the person knows it; one the page cannot place is shown as typed.
func issue3929ScopeParts(m Model, query string) ([]string, chatsearch.Parsed, error) {
	parsed, err := chatsearch.Parse(query)
	if err != nil {
		return nil, parsed, err
	}
	t := func(key string) string { return chatsearchText(m.Locale, key) }
	parts := []string{}
	if text := strings.TrimSpace(parsed.Text); text != "" {
		parts = append(parts, issue3929Quote(m.Locale, text))
	}
	for _, chip := range parsed.Chips {
		key, value, _ := strings.Cut(chip, ":")
		key = strings.ToLower(key)
		switch key {
		case "in":
			parts = append(parts, t("channel")+": "+issue3929ChannelName(m, strings.TrimPrefix(value, "#")))
		case "from":
			if value == "agent" {
				parts = append(parts, t("agent"))
			} else {
				parts = append(parts, t("person")+": "+issue3929PersonName(m, strings.TrimPrefix(value, "@")))
			}
		case "kind":
			parts = append(parts, chatsearchKind(m.Locale, chatsearch.Kind(value)))
		case "sort":
			parts = append(parts, t("order")+": "+issue3945ChipLabel(m.Locale, strings.ToLower(value)))
		case "lang", "language":
			parts = append(parts, chattoneW10Text(m.Locale, "search_written")+" "+chatlangLanguageName(m.Locale, value))
		case "has":
			parts = append(parts, t(value))
		case "is":
			if value == "thread" {
				value = "threads"
			}
			parts = append(parts, t(value))
		case "mentions":
			parts = append(parts, t("mentions"))
		default:
			parts = append(parts, t(key)+" "+value)
		}
	}
	return parts, parsed, nil
}

func issue3929ChannelName(m Model, name string) string {
	for _, c := range m.Conversations {
		if c.ID == name || strings.EqualFold(c.Name, name) || strings.EqualFold(displayName(m, c), name) {
			return displayName(m, c)
		}
	}
	return name
}

func issue3929PersonName(m Model, name string) string {
	for _, p := range mentionIndex(m) {
		if p.id == name || strings.EqualFold(p.name, name) {
			return p.name
		}
	}
	return name
}

// issue3929ChannelMissing reports a saved channel the page cannot place among
// the person's conversations. Running it still searches only that name; it is
// never widened to all rooms, so the note says the search may find nothing.
func issue3929ChannelMissing(m Model, parsed chatsearch.Parsed) (string, bool) {
	name := strings.TrimPrefix(parsed.Filters.Conversation, "#")
	if name == "" {
		return "", false
	}
	for _, c := range m.Conversations {
		if c.ID == name || strings.EqualFold(c.Name, name) || strings.EqualFold(displayName(m, c), name) {
			return "", false
		}
	}
	return name, true
}

// issue3929Section is the saved searches inside the popover: a title, one row
// per search (open, Edit, Delete) and the standing privacy note.
func issue3929Section(m Model) []ui.Node {
	if len(m.SearchSaved) == 0 {
		return nil
	}
	t := func(key string) string { return issue3929Text(m.Locale, key) }
	nodes := []ui.Node{html.Span(html.Props{Class: "rail-search-recent-title", Text: t("title")})}
	for _, item := range m.SearchSaved {
		if m.SavedSearchEditing == item.ID {
			nodes = append(nodes, issue3929Editor(m, item))
			continue
		}
		parts, parsed, err := issue3929ScopeParts(m, item.Query)
		scope := strings.Join(parts, " · ")
		note := ""
		if err != nil {
			scope, note = item.Query, t("broken")
		} else if name, gone := issue3929ChannelMissing(m, parsed); gone {
			note = issue3929Fill(t("noChannel"), "channel", issue3929Quote(m.Locale, name))
		}
		run := html.Button(html.Props{Class: "rail-search-recent-item rail-search-saved-run", Type: "button", Dir: "auto", Title: item.Query, Disabled: err != nil,
			Data: map[string]string{"action": "search-saved", "id": item.ID}},
			icon("search"), html.Span(html.Props{Class: "rail-search-saved-text"}, html.Span(html.Props{Class: "rail-search-saved-name", Text: item.Name}), html.Span(html.Props{Class: "rail-search-saved-scope", Dir: "auto", Text: scope})))
		actions := html.Span(html.Props{Class: "rail-search-saved-actions"},
			html.Button(html.Props{Class: "rail-search-saved-act", Type: "button", Aria: map[string]string{"label": issue3929Fill(t("editName"), "name", item.Name)}, Data: map[string]string{"action": "search-saved-edit", "id": item.ID}, Disabled: m.Callbacks.SavedSearchEdit == nil, Text: t("edit")}),
			html.Button(html.Props{Class: "rail-search-saved-act", Type: "button", Aria: map[string]string{"label": issue3929Fill(t("delName"), "name", item.Name)}, Data: map[string]string{"action": "search-saved-delete", "id": item.ID}, Disabled: m.Callbacks.SavedSearchDelete == nil, Text: t("delete")}))
		row := []ui.Node{html.Div(html.Props{Class: "rail-search-saved-line"}, run, actions)}
		if note != "" {
			row = append(row, html.Span(html.Props{Class: "rail-search-recent-note rail-search-saved-warn", Text: note}))
		}
		nodes = append(nodes, html.WithKey(html.Div(html.Props{Class: "rail-search-saved-row"}, row...), "saved:"+item.ID))
	}
	nodes = append(nodes, html.Span(html.Props{Class: "rail-search-recent-note", Text: t("private")}))
	return nodes
}

// issue3929Editor is the inline Edit form of one saved search: its name and its
// words and filters.
func issue3929Editor(m Model, item SavedSearchItem) ui.Node {
	t := func(key string) string { return issue3929Text(m.Locale, key) }
	return html.WithKey(html.Div(html.Props{Class: "rail-search-saved-edit", Role: "group", Aria: map[string]string{"label": t("editTitle")}},
		html.Label(html.Props{For: issue3929NameField, Text: t("name")}),
		html.Input(html.Props{ID: issue3929NameField, Class: "chat-input", Type: "text", MaxLength: chatsearch.SavedNameMax, AutoFocus: true, AutoComplete: "off", Dir: "auto", Value: item.Name}),
		html.Label(html.Props{For: issue3929QueryField, Text: t("query")}),
		html.Input(html.Props{ID: issue3929QueryField, Class: "chat-input", Type: "text", MaxLength: chatsearch.SavedQueryMax, AutoComplete: "off", Dir: "auto", Value: item.Query}),
		html.Span(html.Props{Class: "rail-search-saved-edit-actions"},
			html.Button(html.Props{Class: "rail-search-saved-act rail-search-saved-primary", Type: "button", Data: map[string]string{"action": "search-saved-update", "id": item.ID}, Disabled: m.Callbacks.SavedSearchUpdate == nil, Text: t("update")}),
			html.Button(html.Props{Class: "rail-search-saved-act", Type: "button", Data: map[string]string{"action": "search-saved-cancel", "id": item.ID}, Text: t("cancel")}))), "saved-edit:"+item.ID)
}

// issue3929StatusNote is the line that tells how the last change to the saved
// searches went. Failures are announced assertively; successes politely.
func issue3929StatusNote(m Model, id string) ui.Node {
	key := m.SavedSearchStatus
	if key == "" {
		return nil
	}
	text := issue3929Fill(issue3929Text(m.Locale, key), "name", issue3929Quote(m.Locale, m.SavedSearchName))
	if strings.TrimSpace(text) == "" {
		return nil
	}
	role, class := "status", "rail-search-recent-note"
	switch key {
	case "failed", "nameTaken", "limit", "invalid":
		role, class = "alert", class+" rail-search-recent-error"
	}
	return html.Span(html.Props{ID: id, Class: class, Role: role, TabIndex: -1, Text: text})
}

// issue3929Keeps is the sentence beside Clear that says it leaves the saved
// searches alone; it is drawn only when there are saved searches.
func issue3929Keeps(m Model) ui.Node {
	if len(m.SearchSaved) == 0 {
		return nil
	}
	return html.Span(html.Props{Class: "rail-search-recent-note", Text: issue3929Text(m.Locale, "keeps")})
}

// issue3929SaveControl is the form on the results page that keeps the search
// on screen under a name. Where the search is already kept it says so instead.
func issue3929SaveControl(m Model) ui.Node {
	view := m.ChatSearch
	if view == nil || view.Loading || strings.TrimSpace(view.Query) == "" || chatsearchInputError(view.Error) {
		return nil
	}
	if _, err := chatsearch.Parse(view.Query); err != nil {
		return nil
	}
	t := func(key string) string { return issue3929Text(m.Locale, key) }
	query := strings.TrimSpace(view.Query)
	for _, item := range m.SearchSaved {
		if item.Query == query {
			note := issue3929Fill(t("already"), "name", issue3929Quote(m.Locale, item.Name))
			if m.SavedSearchStatus == "saved" && m.SavedSearchName == item.Name {
				note = issue3929Fill(t("saved"), "name", issue3929Quote(m.Locale, item.Name))
			}
			return html.P(html.Props{ID: issue3929NoteID, Class: "chatsearch-save-note", TabIndex: -1, Role: "status", Dir: "auto", Text: note})
		}
	}
	disabled := m.Callbacks.SavedSearchSave == nil
	children := []ui.Node{
		html.Label(html.Props{For: issue3929SaveField, Class: "chatsearch-save-label", Text: t("saveLabel")}),
		html.Span(html.Props{ID: "chatsearch-save-hint", Class: "chatsearch-save-hint", Text: t("saveHint")}),
		html.Span(html.Props{Class: "chatsearch-save-row"},
			html.Input(html.Props{ID: issue3929SaveField, Name: "name", Type: "text", MaxLength: chatsearch.SavedNameMax, AutoComplete: "off", Dir: "auto", Placeholder: t("savePlace"), Disabled: disabled, Aria: map[string]string{"describedby": "chatsearch-save-hint"}}),
			html.Button(html.Props{Type: "submit", Class: "chatsearch-primary", Disabled: disabled, Text: t("save")})),
	}
	if note := issue3929StatusNote(m, issue3929NoteID); note != nil {
		children = append(children, note)
	}
	return html.Form(html.Props{Class: "chatsearch-save", Data: map[string]string{"chatsearch-form": "save"}}, children...)
}

// issue3929Act handles the delegated controls of the saved searches. Opening
// one is the ordinary search; every change goes through the client, which
// reports back through Model.SavedSearchStatus.
func issue3929Act(m Model, action, id string) {
	cb := m.Callbacks
	switch action {
	case "search-saved":
		for _, item := range m.SearchSaved {
			if item.ID == id && cb.Search != nil {
				setDOMValue("chat-search", item.Query)
				cb.Search(item.Query)
				return
			}
		}
	case "search-saved-edit":
		if cb.SavedSearchEdit != nil {
			cb.SavedSearchEdit(id)
		}
	case "search-saved-cancel":
		if cb.SavedSearchEdit != nil {
			cb.SavedSearchEdit("")
		}
	case "search-saved-update":
		if cb.SavedSearchUpdate != nil {
			cb.SavedSearchUpdate(id, domValue(issue3929NameField), domValue(issue3929QueryField))
		}
	case "search-saved-delete":
		if cb.SavedSearchDelete != nil {
			cb.SavedSearchDelete(id)
		}
	}
}

const issue3929Styles = `.chat-workspace .rail-search-saved-line{display:flex;align-items:stretch;gap:2px}` +
	`.chat-workspace .rail-search-saved-run{flex:1 1 auto;min-inline-size:0;align-items:flex-start}` +
	`.chat-workspace .rail-search-saved-text{display:flex;flex-direction:column;min-inline-size:0;gap:1px}` +
	`.chat-workspace .rail-search-saved-name{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.chat-workspace .rail-search-saved-scope{color:var(--muted);font-size:.75rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.chat-workspace .rail-search-saved-run:disabled{opacity:.6;cursor:not-allowed}` +
	`.chat-workspace .rail-search-saved-actions{display:flex;flex:none;gap:2px;align-items:center}` +
	`.chat-workspace .rail-search-saved-act{min-block-size:32px;margin:0;padding:4px 8px;border:0;border-radius:var(--hcm-radius-xs);background:transparent;color:var(--ink);font:inherit;font-size:.8125rem;cursor:pointer;text-decoration:underline}` +
	`.chat-workspace .rail-search-saved-act:hover,.chat-workspace .rail-search-saved-act:focus-visible{background:var(--soft)}` +
	`.chat-workspace .rail-search-saved-act:disabled{opacity:.5;cursor:not-allowed}` +
	`.chat-workspace .rail-search-saved-warn{color:var(--danger,var(--ink))}` +
	`.chat-workspace .rail-search-saved-edit{display:flex;flex-direction:column;gap:4px;padding:4px 8px}` +
	`.chat-workspace .rail-search-saved-edit label{font-size:.75rem;color:var(--muted)}` +
	`.chat-workspace .rail-search-saved-edit-actions{display:flex;flex-wrap:wrap;gap:4px}` +
	`.chat-workspace .rail-search-saved-primary{font-weight:600}` +
	`@media (pointer:coarse){.chat-workspace .rail-search-saved-act{min-block-size:44px;min-inline-size:44px}.chat-workspace .rail-search-saved-run{min-block-size:44px}}` +
	`@media (max-width:560px){.chat-workspace .rail-search-saved-line{flex-wrap:wrap}.chat-workspace .rail-search-saved-actions{inline-size:100%;justify-content:flex-end}}`

const issue3929ResultStyles = `.chatsearch-save{display:flex;flex-direction:column;gap:.375rem;margin-block:.5rem}` +
	`.chatsearch-save-label{font-weight:600}` +
	`.chatsearch-save-hint{font-size:.8125rem;color:var(--hcm-color-text-muted,var(--hcm-color-text))}` +
	`.chatsearch-save-row{display:flex;flex-wrap:wrap;gap:.5rem;align-items:stretch}` +
	`.chatsearch-save-row input{flex:1 1 12rem;min-inline-size:0;min-block-size:44px}` +
	`.chatsearch-save-row button{min-block-size:44px}` +
	`.chatsearch-save-note{margin-block:.5rem}`

// issue3929KeyAction says what a key does inside the saved-search editor:
// Enter in its name or words field saves the change, Escape closes it. Any
// other key, or a key outside the editor, does nothing here.
func issue3929KeyAction(m Model, pressed, activeID string, composing bool) string {
	if m.SavedSearchEditing == "" || composing {
		return ""
	}
	if activeID != issue3929NameField && activeID != issue3929QueryField {
		return ""
	}
	switch pressed {
	case "Enter":
		return "search-saved-update"
	case "Escape":
		return "search-saved-cancel"
	}
	return ""
}

// issue3929Key lets the keyboard finish or leave the inline editor.
func issue3929Key(e ui.KeyboardEvent, m Model) bool {
	action := issue3929KeyAction(m, e.GetKey(), issue3929ActiveID(), issue3929Composing(e))
	if action == "" {
		return false
	}
	issue3929Act(m, action, m.SavedSearchEditing)
	return true
}
