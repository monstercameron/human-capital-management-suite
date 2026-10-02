package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-064. A phone has no hover bar, so a message could not be reacted to
// or replied to in a thread: its More menu held neither. Pressing a message
// there (a long press, or its more button) opens a sheet at the foot of the
// screen. Its first row is the quick reactions and Add reaction, then Reply in
// thread and Save, then Copy link and the rest of the menu. The row is in the
// menu's markup always and shown by the styles below 768 px, where the bar is
// not drawn, the way the bar's twins Pin and Save are (message_menu.go).

const keyChatbug064Reactions = "chat.bug064.reactions"

var chatbug064Copy = map[string]map[string]string{
	"en-US": {keyChatbug064Reactions: "Reactions"},
	"de-DE": {keyChatbug064Reactions: "Reaktionen"},
	"ar":    {keyChatbug064Reactions: "التفاعلات"},
}

func chatbug064Text(m Model, key string) string {
	return chatbug039Text(key, chatbug064Copy[chatEmojiLocale(m.Locale)][key], chatbug064Copy["en-US"][key])
}

// chatbug064MenuItems is the message menu with the phone's reaction row ahead
// of the rows message_menu.go lists. A menu that is asking whether to delete
// is only that question.
func chatbug064MenuItems(m Model, msg Message) []ui.Node {
	items := chatux022MenuItems(m, msg)
	if chatbug081Asking(m) {
		return items
	}
	row := chatbug064ReactionRow(m, msg)
	if row == nil {
		return items
	}
	return append([]ui.Node{row}, items...)
}

// chatbug064ReactionRow is the quick reactions and Add reaction, one press
// each. It is absent where the conversation cannot be reacted in.
func chatbug064ReactionRow(m Model, msg Message) ui.Node {
	if m.Callbacks.ReactWith == nil && m.Callbacks.OpenPicker == nil {
		return nil
	}
	buttons := []ui.Node{}
	for _, emoji := range chatEmojiQuickReactions() {
		label := m.tf(KeyReactWith, map[string]string{"emoji": emoji})
		buttons = append(buttons, html.Button(html.Props{Class: "menu-reaction", Type: "button", Role: "menuitem", Disabled: m.Callbacks.ReactWith == nil, Title: label,
			Data: map[string]string{"action": "react-with", "id": msg.ID, "emoji": emoji}, Aria: map[string]string{"label": label}, Text: emoji}))
	}
	if m.Callbacks.OpenPicker != nil {
		buttons = append(buttons, html.Button(html.Props{Class: "menu-reaction menu-reaction-add", Type: "button", Role: "menuitem", Title: m.t(KeyReact),
			Data: map[string]string{"action": "react-pick", "id": msg.ID}, Aria: map[string]string{"label": m.t(KeyReact), "haspopup": "dialog"}}, icon("smile")))
	}
	return html.Div(html.Props{Class: "menu-reaction-row", Role: "group", Aria: map[string]string{"label": chatbug064Text(m, keyChatbug064Reactions)}}, buttons...)
}
