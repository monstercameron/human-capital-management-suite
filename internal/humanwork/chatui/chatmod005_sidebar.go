package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ModerationPageHref is the page the Moderation entry opens; the browser client
// fetches it and shows it over the chat, so there is no route of its own.
const ModerationPageHref = "/api/chat/moderation/page"

// chatmod005Sidebar is the Moderation entry of the Chat sidebar. A moderator
// sees the number of open items they can open; anybody who was sent a notice
// (a removed message, the outcome of a report) sees the number of recent
// notices. Everyone else sees nothing: the entry never promises a page that is
// empty for them.
func chatmod005Sidebar(m Model) ui.Node {
	if !m.Moderation.Visible() {
		return ui.Text("")
	}
	count := m.Moderation.Attention()
	title := chatremoveText(m.Locale, "moderation")
	if !m.Moderation.Moderator {
		title = chatremoveText(m.Locale, "notices_title")
	}
	label := title
	if count > 0 {
		label += ", " + ModerationCountLabel(m.Locale, count, m.Moderation.Moderator)
	}
	return html.Div(html.Props{Class: "chatmod005-sidebar", ID: "chatmod005-sidebar"},
		html.Button(html.Props{Type: "button", Class: "chat-row chatsave-sidebar-row chatmod005-row", Title: label, Aria: map[string]string{"label": label}, Data: map[string]string{"chatremove-open": ModerationPageHref + "?locale=" + m.Locale}},
			icon("warning"),
			html.Span(html.Props{Class: "chat-row-name", Text: title}),
			html.Span(html.Props{Class: "chat-count chatmod005-count", Hidden: count == 0, Text: m.n(count), Data: map[string]string{"moderation-count": "true"}})))
}

// ModerationCountLabel is the spoken form of the number on the entry.
func ModerationCountLabel(locale string, count int, moderator bool) string {
	key := "sidebar_notices"
	if moderator {
		key = "sidebar_open"
	}
	return strings.ReplaceAll(chatremoveText(locale, key), "{n}", chatCount(locale, count))
}
