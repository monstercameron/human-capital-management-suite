//go:build js && wasm

package chatui

import "syscall/js"

// chatShellPopoverOpen reports whether a workspace-header menu (Account,
// Language, Notifications) is open; Escape then belongs to the shell.
func chatShellPopoverOpen() bool {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return false
	}
	return doc.Call("querySelector", "details[data-hcm-transient-popover][open]").Truthy()
}
