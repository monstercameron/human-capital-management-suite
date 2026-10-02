package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func messageMenuNextIndex(key string, current, length int) int {
	if length <= 0 {
		return -1
	}
	switch key {
	case "ArrowDown":
		return (current + 1) % length
	case "ArrowUp":
		if current < 0 {
			return length - 1
		}
		return (current + length - 1) % length
	case "Home":
		return 0
	case "End":
		return length - 1
	default:
		return current
	}
}

// CHATUX-022. More actions listed eight rows and still lacked two everyday
// ones: Pin and Save were there although both are on the bar one button away,
// Reply in thread and Mark unread were not, and Message language sat between
// Edit and Pin. The menu now holds what the bar does not, in this order:
//
//	Reply in thread
//	Mark unread from here
//	Copy link
//	Copy text
//	Share to channel
//	Edit message               (the author)
//	---
//	Message language...        (the author, where translation is on)
//	---
//	Delete message             (the author; Report, and Remove for everyone
//	                            after its own divider, on someone else's)
//
// Pin and Save stay in the menu's markup after Share to channel, and the
// stylesheet shows them only where the bar does not carry them: on a phone,
// which has no bar, and in a column under 560 px, where the bar is cut down
// (CHATBUG-076). A reply in the thread pane has no Pin on its bar, so its menu
// always shows Pin.

// ChatUX022Styles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatUX022Styles = `
.message-menu :is(.menu-bar-twin,.menu-item.chatsave-action){display:none}
@media(max-width:767px),(pointer:coarse){.message-menu :is(.menu-bar-twin,.menu-item.chatsave-action){display:flex}}
@container chatmain (max-width:560px){.message-menu :is(.menu-bar-twin,.menu-item.chatsave-action){display:flex}}
.message-menu .menu-confirm-text{margin:0;padding:8px 12px 4px;max-inline-size:260px;font-size:.875rem;line-height:1.4;color:var(--hcm-color-text)}
`

const (
	keyChatux022CopyText   = "chat.ux022.copy_text"
	keyChatux022MarkUnread = "chat.ux022.mark_unread"
)

var chatux022Copy = map[string]map[string]string{
	"en-US": {keyChatux022CopyText: "Copy text", keyChatux022MarkUnread: "Mark unread from here"},
	"de-DE": {keyChatux022CopyText: "Text kopieren", keyChatux022MarkUnread: "Ab hier als ungelesen markieren"},
	"ar":    {keyChatux022CopyText: "نسخ النص", keyChatux022MarkUnread: "تحديد كغير مقروء من هنا"},
}

func chatux022Text(m Model, key string) string {
	return chatbug039Text(key, chatux022Copy[chatEmojiLocale(m.Locale)][key], chatux022Copy["en-US"][key])
}

// chatux022Item is one row of the menu: a glyph and a label, named for a
// tooltip as well because a long label can be cut in a narrow menu.
func chatux022Item(class, action, id, glyph, label string, disabled bool) ui.Node {
	return html.Button(html.Props{Class: class, Type: "button", Role: "menuitem", Disabled: disabled, Title: label,
		Data: map[string]string{"action": action, "id": id}}, icon(glyph), html.Span(html.Props{Text: label}))
}

// chatux022MenuItems is the content of a message's More actions menu.
func chatux022MenuItems(m Model, msg Message) []ui.Node {
	if chatbug081Asking(m) {
		return chatbug081Confirm(m, msg)
	}
	// The thread pane's menus are opened under "thread:<post>"; only one menu
	// is open at a time, so the open menu's name says which pane this is.
	thread := strings.HasPrefix(m.MenuID, "thread:")
	own := msg.AuthorID == m.CurrentUser
	var items []ui.Node
	if !thread && m.Callbacks.OpenThread != nil && !m.selected().Agent {
		items = append(items, chatux022Item("menu-item", "reply", msg.ID, "reply", m.t(KeyReply), false))
	}
	if !thread && m.Callbacks.MarkUnreadFrom != nil {
		items = append(items, chatux022Item("menu-item", "mark-unread", msg.ID, "eye", chatux022Text(m, keyChatux022MarkUnread), false))
	}
	copyLabel := m.t(KeyCopyLink)
	if m.PinReferenceUnavailable {
		copyLabel = m.t(KeyPinCopyGuestUnavailable)
	}
	items = append(items, chatux022Item("menu-item", "copy-link", msg.ID, "link", copyLabel, m.Callbacks.CopyLink == nil || m.PinReferenceUnavailable), copyContentsMenuItem(m, msg))
	shareDisabled := m.Callbacks.OpenShare == nil || strings.TrimSpace(msg.Body) == ""
	shareTitle := m.t(KeyShareToChannel)
	if strings.TrimSpace(msg.Body) == "" && len(msg.Attachments) > 0 {
		shareTitle = m.t(KeyShareAttachments)
	}
	shareAria := m.t(KeyShareToChannel)
	if shareTitle != m.t(KeyShareToChannel) {
		shareAria += ". " + shareTitle
	}
	items = append(items, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: shareDisabled, Title: shareTitle, Aria: map[string]string{"label": shareAria}, Data: map[string]string{"action": "open-share", "id": msg.ID}}, icon("send"), html.Span(html.Props{Text: m.t(KeyShareToChannel)})))
	pinAction, pinLabel, pinClass := "pin", m.t(KeyPin), "menu-item menu-bar-twin"
	if msg.Pinned {
		pinAction, pinLabel = "unpin", m.t(KeyUnpin)
	}
	if thread {
		pinClass = "menu-item"
	}
	items = append(items, chatux022Item(pinClass, pinAction, msg.ID, "pin", pinLabel, m.Callbacks.Pin == nil), chatsaveAction(m, msg, true))
	if listen := chatListenAction(m, msg, true); listen != nil {
		items = append(items, listen)
	}
	if own {
		// A poll or a to-do list changes with every vote and tick; its text is not
		// edited in the message box.
		if !chatcmd002IsCard(msg.Body) {
			items = append(items, chatux022Item("menu-item", "edit", msg.ID, "edit", m.t(KeyEdit), m.Callbacks.BeginEdit == nil))
		}
		if language := chatlangMenuItems(m, msg); len(language) > 0 {
			items = append(items, html.Div(html.Props{Class: "menu-separator", Role: "separator"}))
			items = append(items, language...)
		}
	}
	items = append(items, html.Div(html.Props{Class: "menu-separator", Role: "separator"}))
	// CHATBUG-030: a person's own message has exactly one destructive command,
	// Delete message. Somebody else's message offers Report, plus the
	// moderator's "Remove for everyone" (after its own separator) for a viewer
	// who holds the removal permission. Never both Delete and Remove.
	if own {
		items = append(items, chatux022Item("menu-item danger", "delete", msg.ID, "trash", m.t(KeyDelete), m.Callbacks.DeleteMessage == nil))
	} else {
		items = append(items, moderationMenuLink(m.Locale, m.SelectedID, msg.ID, "report"))
		if m.chatmod005CanRemove() {
			items = append(items, html.Div(html.Props{Class: "menu-separator", Role: "separator"}), moderationMenuLink(m.Locale, m.SelectedID, msg.ID, "remove"))
		}
	}
	return items
}

// chatux022MarkUnread asks the client to move the viewer's read position to
// just before a message of the open conversation.
func chatux022MarkUnread(m Model, id string) {
	if m.Callbacks.MarkUnreadFrom == nil {
		return
	}
	for _, msg := range m.Messages {
		if msg.ID == id {
			m.Callbacks.MarkUnreadFrom(id)
			return
		}
	}
}
