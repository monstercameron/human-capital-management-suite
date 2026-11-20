//go:build js && wasm

package main

// chatsearchOrderFocusPending is set when the Newest first / Oldest first
// switch was activated: the answer redraws the switch, and the focus goes back
// to the option now in use, not to the summary (ISSUE-3945).
var chatsearchOrderFocusPending bool

// chatsearchRepairOrderFocus returns focus to the chosen order option after the
// redraw, and reports whether it took the job from the generic repair.
func chatsearchRepairOrderFocus() bool {
	if !chatsearchOrderFocusPending {
		return false
	}
	chatsearchOrderFocusPending = false
	chatsearchFocusSelector(".chatsearch-order-option.is-selected", 0, true)
	return true
}
