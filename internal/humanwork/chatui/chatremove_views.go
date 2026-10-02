package chatui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func ModerationCountText(locale string, count int) string {
	return strings.ReplaceAll(chatremoveText(locale, "count"), "{n}", strconv.Itoa(count))
}

// ModerationMenuEntries takes current server-projected permission, not a role
// name. The links open the shared removal/report form in the moderation page.
func ModerationMenuEntries(locale, cid, id string, canRemove bool) []ui.Node {
	items := []ui.Node{moderationMenuLink(locale, cid, id, "report")}
	if canRemove {
		items = append(items, moderationMenuLink(locale, cid, id, "remove"))
	}
	return items
}

// moderationMenuLink is one entry of the message menu that opens the shared
// report or removal form; action is "report" or "remove".
func moderationMenuLink(locale, cid, id, action string) ui.Node {
	q := url.Values{"conversation": {cid}, "post": {id}, "action": {action}, "locale": {locale}}
	glyph, label := "warning", chatremoveText(locale, action)
	if action == "remove" {
		// CHATBUG-030: named for what it does, so it is never mistaken for the
		// author's own Delete message.
		glyph, label = "trash", chatremoveText(locale, "remove_everyone")
	}
	return html.Button(html.Props{Class: "menu-item danger", Type: "button", Role: "menuitem", Title: label, Data: map[string]string{"chatremove-open": "/api/chat/moderation/page?" + q.Encode()}}, icon(glyph), html.Span(html.Props{Text: label}))
}

func ModerationRestoredNotice(locale string) ui.Node {
	return html.P(html.Props{Class: "chatremove-restored", Role: "status", Text: chatremoveText(locale, "restored")})
}

func chatremoveIsTombstone(msg Message) bool { return msg.Body == chat.RemovedByAdministrator }

// ProjectModerationMessage clears media and reaction projections before a
// thread, saved item, notification or sidebar preview renders the tombstone.
func ProjectModerationMessage(locale string, msg Message) Message {
	if !chatremoveIsTombstone(msg) {
		return msg
	}
	msg.Body = chatremoveText(locale, "removed")
	msg.Attachments = nil
	msg.Chips = nil
	msg.Reactions = 0
	msg.Reacted = false
	msg.Pinned = false
	msg.PersonaReferences = nil
	return msg
}

func ProjectModerationSavedMessage(locale string, msg Message) Message {
	if !chatremoveIsTombstone(msg) {
		return msg
	}
	msg = ProjectModerationMessage(locale, msg)
	msg.Body = chatremoveText(locale, "saved_removed")
	return msg
}

func chatremoveThreadModel(m Model) Model {
	m.Messages = append([]Message(nil), m.Messages...)
	for i := range m.Messages {
		m.Messages[i] = ProjectModerationMessage(m.Locale, m.Messages[i])
	}
	m.ThreadMessages = append([]Message(nil), m.ThreadMessages...)
	for i := range m.ThreadMessages {
		m.ThreadMessages[i] = ProjectModerationMessage(m.Locale, m.ThreadMessages[i])
	}
	if m.ThreadParent != nil {
		parent := ProjectModerationMessage(m.Locale, *m.ThreadParent)
		m.ThreadParent = &parent
	}
	return m
}
