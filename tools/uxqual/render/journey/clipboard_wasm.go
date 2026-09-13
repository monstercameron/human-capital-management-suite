//go:build js && wasm

package journey

import "syscall/js"

// copyToClipboard writes value to the browser clipboard via the standard
// asynchronous Clipboard API. It is PROMOUX-008's copy control: the
// on-screen text is always the redacted form maskIdentifier produces, so
// this is the only place the full, unredacted identifier is used at all.
func copyToClipboard(value string) {
	navigator := js.Global().Get("navigator")
	if !navigator.Truthy() {
		return
	}
	clipboard := navigator.Get("clipboard")
	if !clipboard.Truthy() {
		return
	}
	clipboard.Call("writeText", value)
}
