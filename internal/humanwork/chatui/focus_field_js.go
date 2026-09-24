//go:build js && wasm

package chatui

import "syscall/js"

// focusField returns focus to a control after a re-render. Inserting a keyed
// sibling can move the focused node, and a later render in the same burst can
// blur it again (measured: the picker's search box lost focus ~300ms after a
// pick), so for about 45 frames it restores focus whenever focus has fallen to
// the document body. It stops as soon as the person focuses anything else.
func focusField(id string) {
	doc := js.Global().Get("document")
	frames := 0
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		frames++
		el := doc.Call("getElementById", id)
		active := doc.Get("activeElement")
		lost := !active.Truthy() || active.Equal(doc.Get("body"))
		if el.Truthy() && lost {
			el.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		}
		if frames >= 45 || (!lost && !active.Equal(el)) {
			step.Release()
			return nil
		}
		js.Global().Call("requestAnimationFrame", step)
		return nil
	})
	js.Global().Call("requestAnimationFrame", step)
}
