package chatui

import "testing"

// TestTodo_AGENTUX_050 is the emoji half of the entry: a colon in a sentence
// does not open emoji completion, Enter sends while the list has nothing
// highlighted, and the picker stays off the sidebar. The assertions are lane
// S9's, run here so the entry's own test name covers the half. The rollout
// wording half is TestTodo_AGENTUX_050 in internal/humanwork/productui.
func TestTodo_AGENTUX_050(t *testing.T) {
	t.Run("emoji completion", TestTodo_AGENTUX_050_EmojiCompletion)
	t.Run("emoji picker stays off the sidebar", TestTodo_AGENTUX_050_EmojiPickerStaysOffTheSidebar)
}
