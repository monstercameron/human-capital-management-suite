//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// issue3929ActiveID is the id of the element holding focus, so Enter and
// Escape inside the saved-search editor can be told from other fields.
func issue3929ActiveID() string {
	active := js.Global().Get("document").Get("activeElement")
	if !active.Truthy() {
		return ""
	}
	return active.Get("id").String()
}

// issue3929Composing reports an input method still composing text, whose Enter
// confirms the composition rather than the form.
func issue3929Composing(e ui.KeyboardEvent) bool { return e.JSValue().Get("isComposing").Truthy() }
