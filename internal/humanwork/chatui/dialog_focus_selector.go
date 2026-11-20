package chatui

import "strings"

// chatFocusDialogSelector finds the open Chat dialog whose focus the page manages:
// focus moves into it when it opens, Tab stays inside it, and focus goes back
// to the control that opened it. A dialog missing from this list opens with the
// cursor wherever it was, so typing goes nowhere.
const chatFocusDialogSelector = ".create-dialog, .browse-dialog, .add-members-dialog, .agent-profile-dialog"

// chatDialogMenuToggleSelector is the selector of the button that opens the menu
// whose element carries className (its first class names it): the place focus
// goes back to when a dialog was opened from an item that left with the menu.
func chatDialogMenuToggleSelector(className string) string {
	class := strings.Fields(className)
	if len(class) == 0 {
		return ""
	}
	return "." + class[0] + " [data-chat-disclosure-toggle]"
}
