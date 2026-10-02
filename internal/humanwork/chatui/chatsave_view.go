package chatui

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type SavedMessageRow struct {
	TenantID, ConversationID, PostID, Author, Channel, TimeLabel, Body, Note, DueLocal, DueLabel, Availability string
	Sequence                                                                                                   uint64
	Done                                                                                                       bool
	// CHATSAVE-002. What the panel needs to draw the message as the
	// conversation draws it: the author's identity for the avatar, the posted
	// time, the mentions the post carries, the attachment count and the stored
	// revision; and the item's own reminder, done time and whether the
	// conversation is a channel (written "#name").
	AuthorID      string
	SentAt, DueAt time.Time
	DoneAt        time.Time
	InChannel     bool
	Attachments   int
	Revision      uint64
	References    []ChatReference
}

type SavedMessagesView struct {
	Locale, Tab, Query, Error string
	Rows                      []SavedMessageRow
	Loading, HasMore          bool
	// ActionError is the code of a change the service refused (a save, an
	// unsave, a note), and Action says which. It is reported as that action
	// failing; the list itself loaded, so it is never reported as the list
	// failing to load.
	ActionError, Action string
	// DocPreviews and EmbedOrigin let a saved message's document reference show
	// the document's title as a link, the way the message list does.
	DocPreviews map[string]DocPreview
	EmbedOrigin string
	// CHATSAVE-002. Model is the conversation's own view model, so a saved
	// message is drawn by the same body renderer as the message it is (people,
	// agents, photos, documents); Now is the reader's clock.
	Model Model
	Now   time.Time
	// TodoCount, DoneCount and AllCount are the whole list's counts, whatever
	// the search shows.
	TodoCount, DoneCount, AllCount int
	// Expanded holds the posts whose text is shown in full. Active is the item
	// the person last used (its actions stay in view); EditingNote and
	// ReminderMenu name the item whose note is being written or whose reminder
	// menu is open; PickingDate shows the date field in that menu.
	Expanded                          map[string]bool
	Active, EditingNote, ReminderMenu string
	PickingDate                       bool
	// Undo is "done" or "remove" while the Undo line is on screen.
	Undo string
}

func chatsaveAction(m Model, msg Message, menu bool) ui.Node {
	copy := SavedMessagesCopy(m.Locale)
	class, role := "message-action chatsave-action", "button"
	if menu {
		class, role = "menu-item chatsave-action", "menuitem"
	}
	host := m.selected().HostTenantID
	if host == "" {
		host = m.CurrentTenantID
	}
	labelClass := "sr-only"
	if menu {
		labelClass = ""
	}
	return html.Button(html.Props{Class: class, Type: "button", Role: role, Title: copy.Save, Disabled: m.CurrentUser == "", Data: map[string]string{"saved-disabled": boolString(m.CurrentUser == ""), "saved-action": "save", "saved-post": msg.ID, "saved-conversation": m.SelectedID, "saved-host": host, "saved-label": copy.Save, "saved-mark": copy.Saved, "saved-remove": copy.Unsave}, Aria: map[string]string{"label": copy.Save, "pressed": "false", "keyshortcuts": "Alt+Shift+S"}},
		html.Tag("svg", html.Props{Class: "chatsave-bookmark", Raw: map[string]any{"viewBox": "0 0 24 24", "width": "20", "height": "20", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8", "aria-hidden": "true", "focusable": "false"}}, html.Tag("path", html.Props{Raw: map[string]any{"d": "M6 3h12v18l-6-4-6 4V3z"}})),
		html.Span(html.Props{Class: labelClass, Text: copy.Save, Data: map[string]string{"saved-button-label": "true"}}))
}

func chatsaveSidebar(m Model) ui.Node {
	copy := SavedMessagesCopy(m.Locale)
	return html.Div(html.Props{Class: "chatsave-sidebar", ID: "chatsave-sidebar", Data: map[string]string{"saved-sidebar": "true", "saved-locale": m.Locale}},
		html.Button(html.Props{Type: "button", Class: "chat-row chatsave-sidebar-row", Data: map[string]string{"saved-action": "toggle"}, Aria: map[string]string{"controls": "chatsave-list", "expanded": "false"}, Title: copy.Saved},
			chatsave002Icon("bookmark", false), html.Span(html.Props{Class: "chat-row-name", Text: copy.Saved}), html.Span(html.Props{Class: "chat-count chatsave-count", Hidden: m.SavedOpenCount == 0, Text: m.n(m.SavedOpenCount), Data: map[string]string{"saved-count": "true"}})))
}

func RenderSavedMessages(view SavedMessagesView) ui.Node { return chatsave002Render(view) }

func chatsavePanel(m Model) ui.Node {
	return anchoredChatLayer(html.Props{Class: "chatsave-panel", ID: "chatsave-list", Hidden: true, Role: "dialog", Data: map[string]string{"saved-mount": "true", "saved-locale": m.Locale}, Aria: map[string]string{"label": SavedMessagesCopy(m.Locale).Title}}, "saved", RenderSavedMessages(SavedMessagesView{Locale: m.Locale, Loading: true}))
}
