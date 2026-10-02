//go:build js && wasm

package main

import "syscall/js"

// CHATBUG-078. The Saved panel, the Moderation page and the search results each
// take the main area of Chat, and none of them knew about the others, so
// Moderation was drawn under an open Saved panel. Whichever opens announces
// itself on the document; the others that are open give way without taking
// focus from the one that was just opened.
const chatPageOpenedEvent = "chat-page-opened"

// announceChatPage says which of "saved", "moderation" or "search" has opened.
func announceChatPage(kind string) {
	document := js.Global().Get("document")
	event := js.Global().Get("CustomEvent").New(chatPageOpenedEvent, map[string]any{"detail": kind})
	document.Call("dispatchEvent", event)
}

// chatPageOpenedKind is the page an event from announceChatPage names.
func chatPageOpenedKind(event js.Value) string {
	if event.Truthy() {
		if detail := event.Get("detail"); detail.Type() == js.TypeString {
			return detail.String()
		}
	}
	return ""
}
