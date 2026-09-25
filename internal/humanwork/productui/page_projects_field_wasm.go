//go:build js && wasm

package productui

import "syscall/js"

// projectFieldValue reads an input's live value, so text typed before the
// page hydrated still reaches the filter.
func projectFieldValue(id string) string {
	if el := js.Global().Get("document").Call("getElementById", id); el.Truthy() {
		return el.Get("value").String()
	}
	return ""
}
