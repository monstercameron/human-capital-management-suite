//go:build !(js && wasm)

package chatui

// issue3990NoteField: the server-rendered tree has no box to read.
func issue3990NoteField() (string, bool) { return "", false }
