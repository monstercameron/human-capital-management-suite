//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatcmd003RadioTarget reads the preview radio a key press landed on: its
// setting, its value and the values of the group in the order they are drawn.
func chatcmd003RadioTarget(e ui.KeyboardEvent) (group, current string, options []string, ok bool) {
	target := e.JSValue().Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return "", "", nil, false
	}
	radio := target.Call("closest", `[role="radio"][data-action="chatcmd003-set"]`)
	if !radio.Truthy() {
		return "", "", nil, false
	}
	box := radio.Call("closest", `[role="radiogroup"]`)
	if !box.Truthy() {
		return "", "", nil, false
	}
	list := box.Call("querySelectorAll", `[role="radio"][data-action="chatcmd003-set"]`)
	for i := 0; i < list.Length(); i++ {
		options = append(options, list.Index(i).Get("dataset").Get("extra").String())
	}
	return radio.Get("dataset").Get("id").String(), radio.Get("dataset").Get("extra").String(), options, true
}

// chatcmd003RadioFocus moves focus to the option the arrow key chose. The group
// is drawn again with the new choice first, so the focus waits for that frame.
func chatcmd003RadioFocus(id string) {
	doc := js.Global().Get("document")
	frames := 0
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		frames++
		if el := doc.Call("getElementById", id); el.Truthy() && (frames >= 2 || !doc.Get("activeElement").Equal(el)) {
			if !doc.Get("activeElement").Equal(el) {
				el.Call("focus")
			}
			if frames >= 3 {
				step.Release()
				return nil
			}
		}
		if frames >= 12 {
			step.Release()
			return nil
		}
		js.Global().Call("requestAnimationFrame", step)
		return nil
	})
	js.Global().Call("requestAnimationFrame", step)
}
