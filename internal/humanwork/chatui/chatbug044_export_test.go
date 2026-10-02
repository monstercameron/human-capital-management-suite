package chatui

import "testing"

// MentionMenuMarkupForTest renders the open mention menu above the main composer
// for the model, with the first option highlighted, so a test outside this
// package can render it against the product's own catalog.
func MentionMenuMarkupForTest(t *testing.T, m Model, details bool) string {
	t.Helper()
	return renderNode(t, mentionMenu(m, mentionState{Target: "chat-composer", Open: true, Active: 0, Details: details}, "chat-composer"))
}
