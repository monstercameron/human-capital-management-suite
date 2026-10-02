package chatui

// CHATBUG-030: presentation state that the browser owns (which row is under the
// pointer or holds focus, which layer is open) is mirrored in the chat client
// from events. An event is not delivered when its element is removed, so the
// mirror needs a rule for when it is stale. The rules are here, apart from the
// DOM code that asks them.

// messageMenuClosesFor reports whether opening a layer of the given kind
// (the value of its data-chat-layer attribute) must close an open message menu.
// Only the menu's own kind keeps it open; an unmarked element is not a layer.
func messageMenuClosesFor(kind string) bool { return kind != "" && kind != "menu" }

// chatRowActionsStale reports whether the action bar remembered for a row must
// be dropped: the bar belongs to the row under the pointer or holding focus, so
// a row that is gone, or is neither, no longer owns one.
func chatRowActionsStale(row string, exists, held bool) bool {
	return row != "" && (!exists || !held)
}
