//go:build js && wasm

package chatui

import "syscall/js"

// RestoreFieldValue puts text back in an emptied composer after a send that
// failed. setDOMValue marks the box "cleared", and the field sync then refuses
// any text the application writes back, so the restore writes the property
// itself and ends that state. It writes only into an empty box: text the author
// has typed since is never replaced.
func RestoreFieldValue(id, value string) {
	if value == "" {
		return
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("getElementById").Type() != js.TypeFunction {
		return
	}
	el := doc.Call("getElementById", id)
	if !el.Truthy() || el.Get("value").String() != "" {
		return
	}
	el.Set("value", value)
	el.Set("__chatTyped", false)
	el.Set("__chatCleared", false)
	setComposerSendReady(id, value)
	// The refused text is what the author edits next: the caret is in the box,
	// after the text, whatever the refusal did to focus (CHATUX-018).
	if !doc.Get("activeElement").Equal(el) {
		el.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	}
	if end := el.Get("value").Get("length"); end.Type() == js.TypeNumber && el.Get("setSelectionRange").Type() == js.TypeFunction {
		el.Call("setSelectionRange", end, end)
	}
}
