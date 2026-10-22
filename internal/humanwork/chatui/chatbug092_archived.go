package chatui

import (
	"sort"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// CHATBUG-092: Archived is the last section of the conversation list, drawn
// inside the list's own scroll like Channels and Direct messages. It used to be
// a band fixed below the list, which a re-render could leave over a row, which
// showed through the preferences layer and whose open list was a box laid over
// the channels. A section takes its own height in the flow, so nothing can sit
// over a row and nothing sits over it.

const chatbug092ToggleArchived = "toggle-archived"

// chatbug092ArchivedRows are the archived conversations the section lists. The
// conversation that is open stays in the section it was in, so the highlighted
// row is never moved out from under the person reading it.
func chatbug092ArchivedRows(m Model) []Conversation {
	if m.ChatFeatures == nil || !m.ChatFeatures.Status {
		return nil
	}
	known := map[string]Conversation{}
	for _, c := range m.Conversations {
		known[c.ID] = c
	}
	var out []Conversation
	for id, view := range m.ChannelStatuses {
		if view.Status.Status != chatpolicy.StatusArchived || id == m.SelectedID {
			continue
		}
		c, ok := known[id]
		if !ok {
			c = Conversation{ID: id, Name: view.Status.Name, Kind: PublicChannel, Joined: true}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return displayName(m, out[i]) < displayName(m, out[j]) })
	return out
}

// chatbug092ArchivedSection is the section: a heading that folds it (closed to
// begin with) and, open, one ordinary row per archived conversation. Each row
// carries the quiet Archived tag and the row menu, whose first two items are
// Open and Restore.
func chatbug092ArchivedSection(m Model, h handlers) ui.Node {
	rows := chatbug092ArchivedRows(m)
	if len(rows) == 0 {
		return nil
	}
	open := h.local.archivedOpen
	chevron := "chevron-right"
	if open {
		chevron = "chevron-down"
	}
	title := html.Button(html.Props{Class: "section-title", Type: "button", Data: map[string]string{"action": chatbug092ToggleArchived}, Aria: map[string]string{"expanded": boolString(open)}},
		icon(chevron), html.Span(html.Props{Text: chatstateText(m, "archived")}), html.Span(html.Props{Class: "section-count", Text: m.n(len(rows))}))
	items := []ui.Node{html.WithKey(html.Div(html.Props{Class: "section-controls"}, title), "controls")}
	if open {
		for _, c := range rows {
			items = append(items, html.WithKey(railRow(m, c), "conversation:"+c.ID))
		}
	}
	return html.WithKey(html.Div(html.Props{Class: "sidebar-section chatbug092-archived", Data: map[string]string{"section-id": "archived"}}, items...), "section:archived")
}

// chatbug092HandleAction folds and unfolds the section. It reports whether it
// consumed the click.
func chatbug092HandleAction(e ui.MouseEvent, local localStore) bool {
	if action, _, _ := eventAction(e); action != chatbug092ToggleArchived {
		return false
	}
	local.update(func(u *localUI) { u.archivedOpen = !u.archivedOpen })
	return true
}

// chatbug092ArchivedMenuItems puts Open and Restore at the head of the row menu
// of an archived conversation. Restore opens the conversation's details, where
// its status is changed.
func chatbug092ArchivedMenuItems(m Model, id string, items []ui.Node) []ui.Node {
	view, ok := m.ChannelStatuses[id]
	if !ok || view.Status.Status != chatpolicy.StatusArchived {
		return items
	}
	head := []ui.Node{
		chatux040MenuItem("menu-item", "select", id, "browse", chatstateText(m, "open_channel"), m.Callbacks.SelectConversation == nil),
		chatux040MenuItem("menu-item", "rail-details", id, "refresh", chatstateText(m, "restore"), m.Callbacks.OpenConversationDetails == nil),
	}
	return append(head, items...)
}

// ChatBug092Styles quiets the section's rows: the name in the muted colour and
// the status tag without a fill, so the list reads as a place to look things up
// and not as live conversations.
const ChatBug092Styles = `.chatbug092-archived .chat-row .chat-row-name{color:var(--muted)}` +
	`.chatbug092-archived .chat-row>.chatstate-badge{background:none;border:0;padding:0;font-size:.75rem;font-weight:400}`
