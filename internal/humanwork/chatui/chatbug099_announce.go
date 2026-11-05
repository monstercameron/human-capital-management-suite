package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-099: the keyboard path from the list to a message to its thread and
// back is announced as well as drawn. The region below names the open
// conversation, and the open thread; a screen reader announces it whenever the
// selection changes because its text changes with the model.

// chatbug099AnnounceText is what the announcer says: the open conversation's
// name, or the open thread within it. Nothing is announced without a
// selection.
func chatbug099AnnounceText(m Model) string {
	selected := m.selected()
	if selected.ID == "" {
		return ""
	}
	name := displayName(m, selected)
	if m.ShowThread && m.ThreadParentID != "" {
		return m.t(KeyThread) + ", " + name
	}
	return name
}

// chatbug099Announcer is the live region. It renders on every page so the
// announcement follows the model through every rerender.
func chatbug099Announcer(m Model) ui.Node {
	return html.P(html.Props{ID: "chat-selection-announcer", Class: "sr-only", Role: "status", Text: chatbug099AnnounceText(m)})
}
