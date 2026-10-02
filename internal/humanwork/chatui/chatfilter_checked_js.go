//go:build js && wasm

package chatui

import "syscall/js"

func filterChecked(id string) bool {
	el := js.Global().Get("document").Call("getElementById", id)
	return el.Truthy() && el.Get("checked").Bool()
}

func filterSetChecked(id string, value bool) {
	el := js.Global().Get("document").Call("getElementById", id)
	if el.Truthy() {
		el.Set("checked", value)
	}
}
