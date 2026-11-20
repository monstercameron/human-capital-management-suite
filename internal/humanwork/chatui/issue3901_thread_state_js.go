//go:build js && wasm

package chatui

import "syscall/js"

func init() {
	readThreadField = func() (string, bool) {
		field := js.Global().Get("document").Call("getElementById", "thread-composer")
		if !field.Truthy() || field.Get("value").Type() != js.TypeString {
			return "", false
		}
		return field.Get("value").String(), true
	}
}
