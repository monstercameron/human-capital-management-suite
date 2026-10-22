package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-020: the sidebar keeps a person's place when a section is collapsed,
// marks a row that holds an unsent draft, and its row menu offers only moves
// that make sense.

// chatux020ShownCollapsed reports whether a collapsed section still shows a
// row: the conversation that is open and any with something unread, so
// collapsing a section never hides where the person is or what is waiting.
func chatux020ShownCollapsed(m Model, c Conversation) bool {
	return c.ID == m.SelectedID || c.Unread > 0 || c.Mentions > 0
}

// chatux020DraftMark is the Draft tag on a row whose conversation holds an
// unsent draft. The open conversation is not marked: its draft is in the box
// beside it. CHATUX-049: the pencil is for editing only, so the mark is text,
// with the longer sentence kept as its accessible name.
func chatux020DraftMark(m Model, c Conversation) ui.Node {
	if c.ID == m.SelectedID || !m.railDrafts[c.ID] {
		return nil
	}
	label := laneText(m, chatux020Copy, keyChatux020Draft)
	return html.Span(html.Props{Class: "chat-row-draft", Aria: map[string]string{"label": label}, Title: label}, ui.Text(laneText(m, chatux020Copy, keyChatux020DraftTag)))
}

// chatux020Drafts is the set of conversations holding text the person has not
// sent: the browser's drafts and the server-backed projection of them.
func chatux020Drafts(model Model, d *browserDrafts) map[string]bool {
	out := map[string]bool{}
	for id, body := range model.Preferences.Drafts {
		if strings.TrimSpace(body) != "" {
			out[id] = true
		}
	}
	if d != nil {
		for id, body := range d.values {
			if strings.TrimSpace(body) != "" {
				out[id] = true
			} else {
				delete(out, id)
			}
		}
	}
	if model.SelectedID != "" && strings.TrimSpace(model.Draft) != "" {
		out[model.SelectedID] = true
	}
	return out
}

// chatux020SectionFits reports whether a section may hold a conversation: the
// Channels section holds channels, Direct messages holds direct and group
// conversations, and a section the person made holds anything.
func chatux020SectionFits(section SidebarSection, c Conversation) bool {
	direct := c.Kind == DirectMessage || c.Kind == GroupChat
	switch section.ID {
	case "channels":
		return !direct
	case "direct":
		return direct
	}
	return true
}

// chatux020MoveTargets are the sections the row can be moved to: the ones it
// is not in and that fit its kind.
func chatux020MoveTargets(m Model, c Conversation) []SidebarSection {
	var out []SidebarSection
	for _, section := range chatside001Sections(m, m.Sections) {
		in := false
		for _, chat := range section.Chats {
			if chat.ID == c.ID {
				in = true
				break
			}
		}
		if !in && chatux020SectionFits(section, c) {
			out = append(out, section)
		}
	}
	return out
}

// chatux020Leaves reports whether the row menu offers Leave: channels only. A
// direct message or a group is not left; it is simply not opened.
func chatux020Leaves(c Conversation) bool {
	return c.Kind == PublicChannel || c.Kind == PrivateChannel
}

// chatux020RailMenu is the menu behind a sidebar row's three dots: what the
// conversation is (Details, Copy link), whether it counts as read, the
// notification choice under its own heading, where it sits, and Leave.
func chatux020RailMenu(m Model, h handlers) ui.Node {
	if m.RailMenuID == "" {
		return nil
	}
	if menu, ok := chatside001Menu(m); ok {
		return menu
	}
	id := m.RailMenuID
	var c Conversation
	found := false
	for _, conversation := range m.Conversations {
		if conversation.ID == id {
			c, found = conversation, true
			break
		}
	}
	name := m.t(KeyConversation)
	if found {
		name = displayName(m, c)
	}
	// CHATUX-040: every item leads with a glyph, so the labels share one edge.
	item := func(action, glyph, label string, disabled bool) ui.Node {
		return chatux040MenuItem("menu-item", action, id, glyph, label, disabled)
	}
	items := []ui.Node{
		item("rail-details", "info", m.t(KeyDetails), m.Callbacks.OpenConversationDetails == nil),
		item("rail-copy-reference", "link", m.t(KeyCopyConversationReference), m.Callbacks.CopyConversationReference == nil || !found),
	}
	if found {
		if c.Unread > 0 || c.Mentions > 0 {
			items = append(items, item("rail-mark-read", "check", laneText(m, chatux020Copy, keyChatux020MarkRead), m.Callbacks.MarkConversationRead == nil))
		} else {
			items = append(items, item("rail-mark-unread", "unread", laneText(m, chatux020Copy, keyChatux020MarkUnread), m.Callbacks.MarkConversationUnread == nil))
		}
		items = append(items, chatside001FavoriteItem(m, c))
	}
	// An archived conversation's menu leads with Open and Restore (CHATBUG-092).
	items = chatbug092ArchivedMenuItems(m, id, items)
	// The notification choice is its own group with a heading, so the three
	// radios read as one setting and not as three more actions.
	mode := m.Preferences.Notifications[id]
	if mode == "" {
		mode = NotifyAll
	}
	headingID := "rail-menu-notify-heading"
	radios := []ui.Node{html.Div(html.Props{ID: headingID, Class: "menu-heading", Text: laneText(m, chatux020Copy, keyChatux020Notifications)})}
	for _, choice := range []struct {
		action string
		mode   NotificationMode
		key    string
	}{{"rail-notify-all", NotifyAll, KeyNotifyAll}, {"rail-notify-mentions", NotifyMention, KeyNotifyMentions}, {"rail-notify-mute", NotifyMute, KeyNotifyMute}} {
		check := ""
		if mode == choice.mode {
			check = "✓"
		}
		radios = append(radios, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitemradio", Disabled: m.Callbacks.SetConversationNotification == nil, Data: map[string]string{"action": choice.action, "id": id},
			Aria: map[string]string{"checked": boolString(mode == choice.mode)}},
			html.Span(html.Props{Class: "rail-menu-check", Aria: map[string]string{"hidden": "true"}, Text: check}), html.Span(html.Props{Text: m.t(choice.key)})))
	}
	items = append(items, html.Div(html.Props{Class: "menu-group", Role: "group", Aria: map[string]string{"labelledby": headingID}}, radios...))
	// Moves: only to a section the row is not in and that fits its kind.
	if found {
		for _, section := range chatux020MoveTargets(m, c) {
			sectionName := section.Name
			switch section.ID {
			case "channels":
				sectionName = m.t(KeySectionChannels)
			case "direct":
				sectionName = m.t(KeySectionDirect)
			case chatside001Favorites:
				sectionName = laneText(m, chatside001Copy, keyChatside001Favorites)
			}
			items = append(items, chatux040MenuItem("menu-item", "rail-move-section", id, "section-move", m.tf(KeyMoveToSection, map[string]string{"name": sectionName}), m.Callbacks.MoveConversationSection == nil, section.ID))
		}
		items = append(items, chatside001NewSectionItem(m, c))
	}
	if found && chatux020Leaves(c) {
		label, action, class := laneText(m, chatux020Copy, keyChatux020Leave), "rail-leave", "menu-item"
		if h.local.railLeave == id {
			label, action, class = laneTextf(m, chatux020Copy, keyChatux020LeaveConfirm, map[string]string{"name": name}), "rail-leave-confirm", "menu-item danger"
		}
		items = append(items, chatux040MenuItem(class, action, id, "logout", label, m.Callbacks.LeaveConversation == nil))
	}
	// Reorder is the last thing in the menu, after a divider.
	if found {
		items = append(items, chatux020Reorder(m, id)...)
	}
	return anchoredChatLayer(html.Props{Class: "rail-row-menu", Role: "menu", Aria: map[string]string{"label": m.tf(KeyConversationMore, map[string]string{"name": name})}}, "rail-menu", items...)
}

// chatux020Click handles the row menu's own steps. Leave takes two presses: the
// first turns the item into a confirmation and keeps the menu open, the second
// leaves. Opening a menu forgets a half-confirmed leave. It reports whether it
// consumed the click.
func chatux020Click(e ui.MouseEvent, m Model, local localStore) bool {
	action, id, _ := eventAction(e)
	closeMenu := func() {
		if m.Callbacks.OpenRailMenu != nil {
			m.Callbacks.OpenRailMenu("")
		}
	}
	switch action {
	case "rail-menu", "close-browse", "browse-open", "browse-to-create":
		// A half-confirmed leave is forgotten when the menu or the Browse
		// dialog it was in closes.
		if local.get().railLeave != "" {
			local.update(func(u *localUI) { u.railLeave = "" })
		}
	case "rail-leave":
		local.update(func(u *localUI) { u.railLeave = id })
		return true
	case "rail-leave-confirm":
		local.update(func(u *localUI) { u.railLeave = "" })
		if m.Callbacks.LeaveConversation != nil {
			m.Callbacks.LeaveConversation(id)
		}
		closeMenu()
		return true
	case "rail-mark-read":
		if m.Callbacks.MarkConversationRead != nil {
			m.Callbacks.MarkConversationRead(id)
		}
		closeMenu()
		return true
	case "rail-mark-unread":
		if m.Callbacks.MarkConversationUnread != nil {
			m.Callbacks.MarkConversationUnread(id)
		}
		closeMenu()
		return true
	}
	return false
}
