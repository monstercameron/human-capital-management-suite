//go:build js && wasm

package chatui

import "syscall/js"

// issue3963Field reads a box of the contact finder as it stands and reports
// whether it is on the page, so text the person cleared stays cleared and a box
// that is not drawn yet never overwrites what was kept.
func issue3963Field(id string) (string, bool) {
	el := js.Global().Get("document").Call("getElementById", id)
	if !el.Truthy() {
		return "", false
	}
	v := el.Get("value")
	if v.Type() != js.TypeString {
		return "", false
	}
	return v.String(), true
}
