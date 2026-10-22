package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatux042MessageBar is the hover bar of a message: the person's three
// frequently used emoji (one press each), a hairline, then the four things people
// do to a message at a glance, in this order: react (opens the picker), reply in
// thread, save, more. Everything else (Mark unread, Copy link, Copy text, Share,
// Pin, Listen, Edit, Delete) is in the More menu, and the menu repeats none of
// the actions where the bar is drawn (message_menu.go). In a narrow column the
// stylesheet drops the emoji and the hairline; the picker's first row holds them.
func chatux042MessageBar(m Model, msg Message, react chatux042React, threadAvailable, menuOpen bool) []ui.Node {
	bar := []ui.Node{}
	for _, emoji := range chatEmojiQuickReactions() {
		label := m.tf(KeyReactWith, map[string]string{"emoji": emoji})
		bar = append(bar, html.Button(html.Props{Class: "message-action quick-react", Type: "button", Disabled: m.Callbacks.ReactWith == nil,
			Data: map[string]string{"action": "react-with", "id": msg.ID, "emoji": emoji}, Aria: map[string]string{"label": label}, Title: label, Text: emoji}))
	}
	bar = append(bar, html.Span(html.Props{Class: "message-actions-divider", Aria: map[string]string{"hidden": "true"}}))
	return append(bar,
		html.Button(html.Props{Class: "message-action", Type: "button", Disabled: !react.enabled, Data: map[string]string{"action": react.action, "id": msg.ID}, Aria: map[string]string{"label": react.label, "pressed": boolString(msg.Reacted && react.action != "react-pick"), "expanded": boolString(m.PickerID == msg.ID)}, Title: react.label}, icon(react.glyph)),
		actionButton("message-action", "reply", msg.ID, m.t(KeyReply), !threadAvailable, icon("reply")),
		chatsaveAction(m, msg, false),
		html.Button(html.Props{Class: "message-action", Type: "button", Hidden: m.Callbacks.OpenMenu == nil, Disabled: m.Callbacks.OpenMenu == nil, Data: map[string]string{"action": "menu", "id": msg.ID}, Aria: map[string]string{"label": m.t(KeyMore), "haspopup": "menu", "expanded": boolString(menuOpen)}, Title: m.t(KeyMore)}, icon("more")),
	)
}

// chatux042MessageContentClass is the class of a message's content column: an
// agent's message is the standalone variant of the answer component, so its
// sources and ratings are drawn the way the card draws them.
func chatux042MessageContentClass(msg Message) string {
	if msg.PersonaActor != nil && msg.PersonaActor.valid() {
		return chatux042Class(chatux042Standalone, "message-content")
	}
	return "message-content"
}

// chatux042React is how the bar's react button is drawn: what it does, what it
// is called, its glyph, and whether it can be pressed.
type chatux042React struct {
	action, label, glyph string
	enabled              bool
}
