//go:build js && wasm

package chatui

import "syscall/js"

// chatWorkspaceMounted reports whether a chat workspace is in the document.
// The field-sync observer is installed once and watches the whole document
// for the rest of the session; every element it acts on is inside the
// workspace, so without one there is nothing for it to do.
func chatWorkspaceMounted(doc js.Value) bool {
	if !doc.Truthy() || doc.Get("querySelector").Type() != js.TypeFunction {
		return false
	}
	return doc.Call("querySelector", ".chat-workspace").Truthy()
}
