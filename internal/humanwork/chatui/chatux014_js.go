//go:build js && wasm

package chatui

import "syscall/js"

// domChecked reads a checkbox by id.
func domChecked(id string) bool {
	el := js.Global().Get("document").Call("getElementById", id)
	return el.Truthy() && el.Get("checked").Truthy()
}

// setDOMChecked sets a checkbox by id.
func setDOMChecked(id string, checked bool) {
	if el := js.Global().Get("document").Call("getElementById", id); el.Truthy() {
		el.Set("checked", checked)
	}
}
