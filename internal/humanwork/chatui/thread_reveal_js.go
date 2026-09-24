//go:build js && wasm

package chatui

import "syscall/js"

// revealThreadParent keeps the message a thread was opened from on screen.
// Opening the side column narrows the timeline and re-wraps every message, so
// without this the parent's reactions and reply link slide under the composer.
func revealThreadParent(open bool, id string) {
	if !open || id == "" {
		return
	}
	var step js.Func
	frames := 0
	step = js.FuncOf(func(js.Value, []js.Value) any {
		frames++
		if frames < 2 {
			js.Global().Call("requestAnimationFrame", step)
			return nil
		}
		defer step.Release()
		el := js.Global().Get("document").Call("querySelector", ".message.thread-active")
		if el.Truthy() {
			el.Call("scrollIntoView", js.ValueOf(map[string]any{"block": "nearest"}))
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", step)
}
