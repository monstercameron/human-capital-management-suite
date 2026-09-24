//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// applyComposerFormat runs one toolbar action on a composer's selection and
// reports the edit through the field's input event like typed text.
func applyComposerFormat(id, kind string) {
	doc := js.Global().Get("document")
	field := doc.Call("getElementById", id)
	if !field.Truthy() || field.Get("disabled").Truthy() || field.Get("value").Type() != js.TypeString {
		return
	}
	updated, from, to := formatSelection(field.Get("value").String(), kind, field.Get("selectionStart").Int(), field.Get("selectionEnd").Int())
	field.Set("value", updated)
	field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
	if field = doc.Call("getElementById", id); field.Truthy() {
		field.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		field.Call("setSelectionRange", from, to)
	}
}

// commandHeld reports Ctrl (or Cmd on a Mac) for the formatting shortcuts.
func commandHeld(event ui.KeyboardEvent) bool {
	e := event.JSValue()
	return e.Get("ctrlKey").Truthy() || e.Get("metaKey").Truthy()
}
