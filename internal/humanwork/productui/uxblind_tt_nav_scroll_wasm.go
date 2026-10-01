//go:build js && wasm

package productui

import "syscall/js"

func scrollCurrentNavigationItem(key string) {
	if key == "" {
		return
	}
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	navigation := document.Call("getElementById", "primary-nav")
	if !navigation.Truthy() {
		return
	}
	current := navigation.Call("querySelector", `[data-hcm-nav-current="true"]`)
	if current.Truthy() {
		current.Call("scrollIntoView", map[string]any{"block": "nearest", "behavior": "auto"})
	}
}
