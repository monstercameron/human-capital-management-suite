package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-081, the page's part. Delete message ran at once, with no question
// and no way back, and a notice about a refused write pushed the message list
// down by its own height. Delete now asks once, inside the menu it was chosen
// from: the menu's rows are replaced by the question, Cancel and Delete message,
// and the caret starts on Cancel. The notice is drawn over the top of the list
// (ChatBug081Styles) and takes no room.

// ChatBug081Styles is joined into the workspace stylesheet through
// ChatMsgListStyles. The notice keeps its place in the tree, under the header,
// inside a slot of no height.
const ChatBug081Styles = `
.chat-workspace .chat-notice-slot{position:relative;flex:none;block-size:0;z-index:4}
.chat-workspace .chat-notice-slot>.chat-notice{position:absolute;inset-block-start:4px;inset-inline:12px;margin:0;box-shadow:var(--hcm-shadow-raised)}
`

const (
	keyChatbug081Ask    = "chat.bug081.delete_ask"
	keyChatbug081Cancel = "chat.bug081.delete_cancel"
)

var chatbug081Copy = map[string]map[string]string{
	"en-US": {keyChatbug081Ask: "Delete this message? This cannot be undone.", keyChatbug081Cancel: "Cancel"},
	"de-DE": {keyChatbug081Ask: "Diese Nachricht löschen? Das kann nicht rückgängig gemacht werden.", keyChatbug081Cancel: "Abbrechen"},
	"ar":    {keyChatbug081Ask: "هل تريد حذف هذه الرسالة؟ لا يمكن التراجع عن ذلك.", keyChatbug081Cancel: "إلغاء"},
}

func chatbug081Text(m Model, key string) string {
	return chatbug039Text(key, chatbug081Copy[chatEmojiLocale(m.Locale)][key], chatbug081Copy["en-US"][key])
}

// chatbug081NoticeSlot holds the notice bar in a slot of no height, so that a
// notice coming or going never moves the message list.
func chatbug081NoticeSlot(bar ui.Node) ui.Node {
	return html.Div(html.Props{Class: "chat-notice-slot"}, bar)
}

// chatbug081Asking reports whether the open message menu is asking whether to
// delete its message.
func chatbug081Asking(m Model) bool {
	return m.MenuID != "" && m.deleteAsk == m.MenuID
}

// chatbug081Confirm is the menu while it asks: the question, then Cancel, which
// closes the menu, then the Delete message that deletes.
func chatbug081Confirm(m Model, msg Message) []ui.Node {
	cancel := chatbug081Text(m, keyChatbug081Cancel)
	return []ui.Node{
		html.P(html.Props{ID: "chat-delete-ask", Class: "menu-confirm-text", Dir: "auto", Text: chatbug081Text(m, keyChatbug081Ask)}),
		html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Title: cancel,
			Data: map[string]string{"action": "menu", "id": m.MenuID, "delete-ask": "cancel"}, Aria: map[string]string{"describedby": "chat-delete-ask"}},
			icon("close"), html.Span(html.Props{Text: cancel})),
		html.Button(html.Props{Class: "menu-item danger", Type: "button", Role: "menuitem", Disabled: m.Callbacks.DeleteMessage == nil, Title: m.t(KeyDelete),
			Data: map[string]string{"action": "delete", "id": msg.ID, "delete-ask": "confirm"}, Aria: map[string]string{"describedby": "chat-delete-ask"}},
			icon("trash"), html.Span(html.Props{Text: m.t(KeyDelete)})),
	}
}

// chatbug081Target is the message a confirmed Delete names. The menu is the
// same in the timeline and in the thread pane, so the message may be a reply
// of the open thread, or the thread's parent when the timeline's window does
// not hold it; looking in the timeline alone made Delete on a reply do nothing.
func chatbug081Target(m Model, id string) (Message, bool) {
	for _, list := range [][]Message{m.Messages, m.ThreadMessages} {
		for _, msg := range list {
			if msg.ID == id {
				return msg, true
			}
		}
	}
	if m.ThreadParent != nil && m.ThreadParent.ID == id {
		return *m.ThreadParent, true
	}
	return Message{}, false
}

// chatbug081DeleteStep decides what a press on a control does to the question.
// A press on Delete message in a menu that is not yet asking starts the
// question and is not passed on; the press on the question's own Delete message
// is passed on and deletes; opening or closing a menu ends a question. It
// returns the menu that asks afterwards and whether the press was taken.
func chatbug081DeleteStep(action, menuID, asking string) (next string, taken bool) {
	switch action {
	case "menu":
		return "", false
	case "delete":
		if menuID != "" && asking != menuID {
			return menuID, true
		}
		return "", false
	}
	return asking, false
}

// chatbug081Settle is called while the workspace renders. A question belongs to
// the menu it was asked in: once that menu is closed, or another one is open,
// the question is over, however the menu was closed. Like forRoom it changes
// the local state before the render reads it and asks for no second render.
func chatbug081Settle(local localStore, menuID string) string {
	if local.box.deleteAsk != "" && local.box.deleteAsk != menuID {
		local.box.deleteAsk = ""
	}
	return local.box.deleteAsk
}

// chatbug081Press applies chatbug081DeleteStep to the workspace's local state
// for one press, and reports whether the press was taken.
func chatbug081Press(m Model, local localStore, action string) bool {
	asking := local.get().deleteAsk
	next, taken := chatbug081DeleteStep(action, m.MenuID, asking)
	if next != asking {
		local.update(func(u *localUI) { u.deleteAsk = next })
	}
	if taken {
		focusDeleteAsk(m.MenuID)
	}
	return taken
}
