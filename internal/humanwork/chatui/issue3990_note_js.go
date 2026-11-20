//go:build js && wasm

package chatui

import "syscall/js"

// issue3990NoteField reads the guide's private note as it stands in the box and
// reports whether the box is on the page, so a note the person cleared is kept
// cleared and a box that is not drawn yet never overwrites the saved note.
func issue3990NoteField() (string, bool) {
	el := js.Global().Get("document").Call("getElementById", issue3990NoteID)
	if !el.Truthy() {
		return "", false
	}
	v := el.Get("value")
	if v.Type() != js.TypeString {
		return "", false
	}
	return v.String(), true
}
