//go:build js && wasm

package chatui

import "syscall/js"

// focusDialogTarget moves focus to the first element the selector names, once
// it is in the page, whatever holds focus now. It waits up to 45 frames for a
// control the next render brings in, and gives up quietly if it never comes.
func focusDialogTarget(selector string) {
	doc := js.Global().Get("document")
	frames := 0
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		frames++
		if el := doc.Call("querySelector", selector); el.Truthy() {
			if !el.Equal(doc.Get("activeElement")) {
				el.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			}
			step.Release()
			return nil
		}
		if frames >= 45 {
			step.Release()
			return nil
		}
		js.Global().Call("requestAnimationFrame", step)
		return nil
	})
	js.Global().Call("requestAnimationFrame", step)
}
