//go:build js && wasm

package chatui

import "syscall/js"

// chatissue3877Focus moves focus to the conversation type the arrow key chose.
// focusField only restores a focus that fell to the page body, so it would
// leave focus on the card the key was pressed on; this one takes focus from
// it, once the choice has been drawn.
func chatissue3877Focus(id string) {
	doc := js.Global().Get("document")
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		step.Release()
		if el := doc.Call("getElementById", id); el.Truthy() && !doc.Get("activeElement").Equal(el) {
			el.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", step)
}
