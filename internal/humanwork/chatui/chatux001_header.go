package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-001: the conversation header gives its space to what people do many
// times a day. The name opens the details; Search, Pinned, Members and Details
// are icon buttons that say what they do and how many there are. A poll or a
// to-do list is created from the composer's add menu and found again in the
// chips under the header; the header has no button for either.

// chatux001IsChannel reports a public or private channel, the only rooms with a
// to-do list and a poll.
func chatux001IsChannel(c Conversation) bool {
	return c.Kind == PublicChannel || c.Kind == PrivateChannel
}

// chatux001Purpose is the one line under a channel's name: what the channel is
// for. The team widget's purpose is the one people edit; the conversation's own
// topic is the fallback. Empty means the type and member count are shown instead.
func chatux001Purpose(m Model, c Conversation) string {
	if c.Kind == DirectMessage {
		return ""
	}
	if c.ID == m.SelectedID && chatux001IsChannel(c) {
		if purpose := strings.TrimSpace(m.ChannelTeam.Purpose); purpose != "" {
			return purpose
		}
	}
	return strings.TrimSpace(c.Topic)
}

// chatux001PurposeLine is what the channel is for, set after the type and the
// counts by the same separator as they use, in its own span so it is the part
// the line clips when it runs out of room.
func chatux001PurposeLine(purpose string) []ui.Node {
	return []ui.Node{html.Span(html.Props{Class: "topic-purpose-part"}, html.Span(html.Props{Class: "topic-sep", Text: " · "}), html.Span(html.Props{Class: "topic-purpose", Dir: "auto", Text: purpose}))}
}

// chatux001Heading is the conversation's name. Clicking it opens the details,
// the same place the Details button opens.
func chatux001Heading(m Model, title string, enabled bool) ui.Node {
	if !enabled {
		return html.H1(html.Props{Text: title})
	}
	return html.H1(html.Props{},
		html.Button(html.Props{Class: "conversation-name-button", Type: "button", Disabled: m.Callbacks.ToggleDetails == nil, Title: m.t(KeyDetails), Data: map[string]string{"action": "header-details"}, Text: title}))
}

// chatux001PinnedCount is how many pinned messages the pinned list will show.
func chatux001PinnedCount(m Model) int { return len(m.ChannelPins) }

// chatux001MemberCount is the number the Members button states, or 0 when the
// size is not known yet.
func chatux001MemberCount(m Model, c Conversation) int {
	if c.MemberCount > 0 {
		return c.MemberCount
	}
	return len(m.Members)
}

// chatux001CountButton is an icon button that also states a number.
func chatux001CountButton(class, action, label string, disabled bool, glyph string, count int, m Model) ui.Node {
	children := []ui.Node{icon(glyph)}
	if count > 0 {
		children = append(children, html.Span(html.Props{Class: "chatux001-count", Aria: map[string]string{"hidden": "true"}, Text: m.n(count)}))
	}
	return actionButton("icon-button chatux001-action "+class, action, "", label, disabled, children...)
}

// chatux001HeaderActions returns the header's action buttons in reading order:
// Search, Pinned (only when something is pinned), Members (not in a one-to-one
// message) and Details. The to-do list and the poll have one way in, the chips
// under the header, so the header carries no button for them (CHATBUG-074).
func chatux001HeaderActions(m Model, h handlers, c Conversation, detailsBtn ui.Node) []ui.Node {
	actions := []ui.Node{}
	actions = append(actions,
		html.Button(html.Props{Class: "icon-button chatux001-search", Type: "button", Disabled: m.Callbacks.Search == nil, Data: map[string]string{"action": "chat-search-open"},
			Aria: map[string]string{"label": chatux001Text(m, "chat.ux001.search"), "keyshortcuts": chatux010KeyShortcuts(chatPlatform())}, Title: chatux010Hint(m)}, icon("search")))
	if n := chatux001PinnedCount(m); n > 0 {
		label := chatux001Textf(m, "chat.ux001.pinned_count", map[string]string{"n": m.n(n)})
		actions = append(actions, chatux001CountButton("chatux001-pinned", "header-pinned", label, m.Callbacks.ToggleDetails == nil, "pin", n, m))
	}
	// CHATMAP-005: while anyone is sharing live here, a small Map control with how many.
	if mapButton := chatmapHeaderMap(m); mapButton != nil {
		actions = append(actions, mapButton)
	}
	if c.Kind != DirectMessage {
		label := chatux001Text(m, "chat.ux001.members")
		n := chatux001MemberCount(m, c)
		if n > 0 {
			label = chatux001Textf(m, "chat.ux001.members_count", map[string]string{"n": m.n(n)})
		}
		actions = append(actions, chatux001CountButton("chatux001-members", "header-members", label, m.Callbacks.ToggleDetails == nil, "people", n, m))
	}
	return append(actions, detailsBtn)
}

// chatux001Click handles the header's own actions. "header-details" opens the
// details, "header-pinned" and "header-members" open them at the pinned list and
// the member list. It reports whether the action was one of these.
func chatux001Click(m Model, action string) bool {
	section := ""
	switch action {
	case "header-details":
	case "header-pinned":
		section = "pinned"
	case "header-members":
		section = "members"
	case "header-map":
		section = "crewmap"
	default:
		return false
	}
	cb := m.Callbacks
	if cb.ToggleDetails == nil {
		return true
	}
	// The side column shows a person or a thread over the details; opening the
	// details must close them or nothing on screen changes.
	if m.ShowThread && cb.CloseThread != nil {
		cb.CloseThread()
	}
	if m.ShowPerson && cb.ClosePerson != nil {
		cb.ClosePerson()
	}
	cb.ToggleDetails(true)
	if section != "" {
		chatux001ScrollDetails(section)
	}
	return true
}
