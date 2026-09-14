//go:build js && wasm

package productui

import "syscall/js"

// copyToClipboard writes value to the browser clipboard via the standard
// asynchronous Clipboard API. It never blocks the render and it never
// echoes value anywhere else: the only other place this identifier appears
// is the redacted, masked text TechnicalDetails already drew on screen.
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
