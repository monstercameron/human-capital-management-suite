//go:build js && wasm

package chatui

import "syscall/js"

// sendLaterFocusInside says whether focus is inside the Send later panel.
func sendLaterFocusInside() bool {
	active := js.Global().Get("document").Get("activeElement")
	return active.Truthy() && active.Get("closest").Type() == js.TypeFunction && active.Call("closest", "#"+issue3788PanelID).Truthy()
}
