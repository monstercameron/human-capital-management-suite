package main

// #3768: on a phone the conversation list is a screen, so opening it is a
// browser history step and Back from a conversation lands on it before Chat is
// left. chatNavigationPhone reports the phone layout; the browser build points
// it at the viewport check.
var chatNavigationPhone = func() bool { return false }

// chatListRestore says what Back or Forward must do to the conversation list
// to match the history entry reached: set is true when the list has to change,
// and open is the state it must change to. A wider screen has no list screen,
// so its rail is never touched.
func chatListRestore(entry chatNavigationState, listOpen, phone bool) (set, open bool) {
	if !phone || entry.ShowList == listOpen {
		return false, false
	}
	return true, entry.ShowList
}
