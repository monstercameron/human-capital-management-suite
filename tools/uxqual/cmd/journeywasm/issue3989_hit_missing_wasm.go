//go:build js && wasm

package main

// chatSearchHitMissing tells the reader, in the open room's status line, that
// the search hit they followed is not on the page (deleted, no longer visible to
// them, or its page did not load) instead of leaving them on unrelated messages.
// It says nothing once they have moved to another room.
func chatSearchHitMissing(room, text string) {
	if text == "" || chatBrowser.selectedID() != room {
		return
	}
	noteChatAction(text)
}
