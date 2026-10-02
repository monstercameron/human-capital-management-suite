//go:build js && wasm

package chatui

import "syscall/js"

// chatux026Focus puts the caret on the first element that matches selector once
// the card has been redrawn (CHATUX-026). The press that asked whether to share
// was on a button the redraw replaces with the question, and a removed element
// leaves the caret on the page body. The redraw lands on a later frame than the
// press, so a few frames are tried.
func chatux026Focus(selector string) {
	frame := js.Global().Get("requestAnimationFrame")
	document := js.Global().Get("document")
	if selector == "" || frame.Type() != js.TypeFunction || !document.Truthy() {
		return
	}
	attempts := 0
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		if target := document.Call("querySelector", selector); target.Truthy() {
			target.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			callback.Release()
			return nil
		}
		attempts++
		if attempts < 15 {
			frame.Invoke(callback)
		} else {
			callback.Release()
		}
		return nil
	})
	frame.Invoke(callback)
}
