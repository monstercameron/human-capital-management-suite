//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The builder redraws the chat page after each press and then moves focus; both
// exist only in the browser build.
func init() {
	analysisActive.refresh = refreshChatRoute
	analysisActive.focus = func(selector string) {
		ui.PostAsync(func() {
			if el := js.Global().Get("document").Call("querySelector", selector); el.Truthy() {
				el.Call("focus")
			}
		})
	}
}
