//go:build js && wasm

package chatui

import "syscall/js"

// issue3892FocusConversation puts focus on the message list without touching
// the address. The list is not a tab stop, so it gets a script-only one first.
func issue3892FocusConversation() {
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	target := document.Call("getElementById", "chat-main")
	if !target.Truthy() {
		return
	}
	if target.Call("getAttribute", "tabindex").IsNull() {
		target.Call("setAttribute", "tabindex", "-1")
	}
	target.Call("focus", js.ValueOf(map[string]any{"preventScroll": false}))
}
