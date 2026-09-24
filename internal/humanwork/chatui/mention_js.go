//go:build js && wasm

package chatui

import "syscall/js"

// composerSelection reads a composer's text and caret. The caret is the
// selection end in UTF-16 units; a range selection reports no caret so typing
// over a selection never opens the suggestion list.
func composerSelection(id string) (string, int, bool) {
	field := js.Global().Get("document").Call("getElementById", id)
	if !field.Truthy() {
		return "", 0, false
	}
	value := field.Get("value")
	start, end := field.Get("selectionStart"), field.Get("selectionEnd")
	if value.Type() != js.TypeString || start.Type() != js.TypeNumber || end.Type() != js.TypeNumber || start.Int() != end.Int() {
		return "", 0, false
	}
	return value.String(), end.Int(), true
}

// replaceComposerText writes a composer's new text, keeps the caret where the
// caller put it and reports the change through the field's own input event, so
// the draft callback and a later render agree with what the field shows.
func replaceComposerText(id, value string, caret int) {
	doc := js.Global().Get("document")
	field := doc.Call("getElementById", id)
	if !field.Truthy() || field.Get("disabled").Truthy() {
		return
	}
	field.Set("value", value)
	field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
	if field = doc.Call("getElementById", id); !field.Truthy() {
		return
	}
	field.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	field.Call("setSelectionRange", caret, caret)
}
