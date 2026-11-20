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
	value := field.Get("value").String()
	updated, from, to := formatSelection(value, kind, field.Get("selectionStart").Int(), field.Get("selectionEnd").Int())
	// Issue #3902: an editing command keeps the change on the browser's undo
	// stack; the value write below is the fallback and cannot be undone.
	if start, end, insert := composerMinimalEdit(value, updated); !composerEditUndoable(doc, field, start, end, insert) {
		field.Set("value", updated)
		field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
	}
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
