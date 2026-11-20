package chatui

import "strings"

// #3866: leaving the share dialog without sharing returns focus to the message
// it was opened from. The More actions button is the first choice; it sits in a
// bar that is hidden until the message is hovered or holds focus, so it often
// cannot take focus. The message itself (a focusable article) is the stable
// stand-in, and focusing it also brings the bar back. The composer is only the
// last resort for a message that is no longer on the page.

// shareReturnSelectors lists, in order, what takes focus when the dialog for
// message id closes.
func shareReturnSelectors(id string) []string {
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(id)
	return []string{
		`.message-action[data-action="menu"][data-id="` + quoted + `"]`,
		`[data-message-id="` + quoted + `"]`,
	}
}
