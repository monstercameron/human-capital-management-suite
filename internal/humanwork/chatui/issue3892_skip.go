package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Issue 3892: the Skip to conversation link used to navigate to #chat-main,
// which replaced the open conversation's #channel=... fragment (and with it the
// document title). The link now carries an action; the click handler cancels the
// navigation and moves focus to the conversation instead, so the address stays
// shareable. Without script the plain fragment link still works.
const issue3892SkipAction = "skip-to-conversation"

func issue3892Skips(action string) bool { return action == issue3892SkipAction }

// issue3892Click handles the skip link's activation and reports whether it did.
func issue3892Click(e ui.Event, action string) bool {
	if !issue3892Skips(action) {
		return false
	}
	e.PreventDefault()
	issue3892FocusConversation()
	return true
}
