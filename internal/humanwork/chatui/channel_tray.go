package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// channelTray shows a channel's to-do list and poll inside the chat, under
// the header: a slim bar of summary chips that is always visible once either
// exists, and the opened widget as a card above the timeline. Nothing here
// needs the details pane.
func channelTray(m Model, h handlers, open string) ui.Node {
	c := m.selected()
	if m.SelectedID == "" || (c.Kind != PublicChannel && c.Kind != PrivateChannel) {
		return html.Div(html.Props{Class: "channel-tray-slot"})
	}
	// CHATCMD-002: the bar names the open polls and lists posted as messages
	// here, each a way to its message, and the channel's own list and poll,
	// on one line with the rest behind "+N more".
	entries := chatcmd002TrayEntries(m)
	chips := chatcmd002TrayChips(m, open, entries)
	more := chatcmd002TrayMoreBody(m, entries)
	if open == chatcmd002TrayMore && more == nil {
		open = ""
	}
	if len(chips) == 0 && open == "" {
		return html.Div(html.Props{Class: "channel-tray-slot"})
	}
	children := []ui.Node{}
	if len(chips) > 0 {
		children = append(children, html.Div(html.Props{Class: "channel-tray-bar", Role: "toolbar", Aria: map[string]string{"label": m.t(KeyChannelTools)}}, chips...))
	}
	if open != "" {
		var body ui.Node
		title := m.t(KeyTodoTitle)
		switch open {
		case "poll":
			title = m.t(KeyPollTitle)
			body = channelPollSection(m, h)
		case chatcmd002TrayMore:
			title = chatcmd002TrayText(m, "more-title")
			body = more
		default:
			body = channelTodoSection(m, h)
		}
		children = append(children, anchoredChatLayer(html.Props{Class: "channel-tray-card", Role: "dialog", Data: map[string]string{"tray": open}, Aria: map[string]string{"label": title}}, open,
			html.Div(html.Props{Class: "channel-tray-head"}, html.H2(html.Props{Text: title}), actionButton("icon-button", "tray-close", "", m.t(KeyClose), false, icon("close"))),
			body))
	}
	return html.Div(html.Props{Class: "channel-tray", Data: map[string]string{"open": open}}, children...)
}

// nz formats a count that may legitimately be zero. itoa, and the client's
// Number callback built on it, render zero as "", which left "of 1 done".
func (m Model) nz(v int) string {
	if s := m.n(v); s != "" {
		return s
	}
	return chatNumeral(m.Locale, "0")
}

// todoRuleDisclosure keeps a task to one line (CHATBUG-074). Who may complete
// it is said only when it is not everyone, as a short chip, and the person who
// may change that rule gets it behind the row's menu button instead of a
// labelled control under every task.
func todoRuleDisclosure(m Model, h handlers, item ChannelTodoItem) ui.Node {
	mode := item.CompletionMode
	if mode == "" {
		mode = "EVERYONE"
	}
	var chip ui.Node
	if mode != "EVERYONE" {
		key := "rule-creator"
		if mode == "ME_AND_SELECTED" {
			key = "rule-selected"
		}
		chip = html.Span(html.Props{Class: "channel-todo-rule-chip", Title: m.t(KeyTodoModeLabel) + ": " + m.t(todoModeKey(mode)), Text: chatcmd003Text(m, key)})
	}
	if !item.CanManageCompletionPolicy {
		if chip == nil {
			return html.Span(html.Props{Class: "channel-todo-rule-none"})
		}
		return chip
	}
	label := m.t(KeyTodoModeLabel) + ": " + m.t(todoModeKey(mode))
	toggle := html.Button(html.Props{Class: "icon-button channel-todo-rule-toggle", Type: "button", Title: label,
		Data: map[string]string{"chat-disclosure-toggle": "true"}, Aria: map[string]string{"label": label, "expanded": "false"}}, icon("more"))
	return html.Div(html.Props{Class: "channel-todo-rule", Data: map[string]string{"chat-disclosure": "true"}},
		chip, toggle,
		html.Div(html.Props{Class: "chat-disclosure-body channel-todo-rule-body", Hidden: true, Data: map[string]string{"chat-disclosure-body": "true"}}, todoPolicyControls(m, h, item)))
}
