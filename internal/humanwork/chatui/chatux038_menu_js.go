//go:build js && wasm

package chatui

import "syscall/js"

// openWorkspaceMenu presses the shell's own phone menu button. The workspace
// bar that carries it is hidden while Chat is open, and a click on a hidden
// button still reaches its handler.
func openWorkspaceMenu() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	if button := doc.Call("getElementById", "nav-drawer-trigger"); button.Truthy() {
		button.Call("click")
	}
}
