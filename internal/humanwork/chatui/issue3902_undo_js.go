//go:build js && wasm

package chatui

import "syscall/js"

// composerEditUndoable replaces the UTF-16 range [start,end) of a composer
// field with text through the browser's own editing command, so Ctrl+Z reverses
// it the way it reverses typing (Issue #3902). The browser fires the field's
// input event itself. It reports false, leaving the field untouched, where the
// command is not available or refuses, and the caller then writes the value and
// reports the input as before.
func composerEditUndoable(doc, field js.Value, start, end int, text string) bool {
	if !doc.Truthy() || doc.Get("execCommand").Type() != js.TypeFunction {
		return false
	}
	field.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	field.Call("setSelectionRange", start, end)
	if text == "" {
		if start == end {
			return true // nothing to remove; "delete" on a caret would remove a character
		}
		return doc.Call("execCommand", "delete", false).Truthy()
	}
	return doc.Call("execCommand", "insertText", false, text).Truthy()
}
