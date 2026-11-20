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
// is drawn again with the new choice first, and that can land several frames
// after the key, so the focus is put back each frame the page has lost it or
// still holds another option of the group, until it has stayed on the chosen
// option for a few frames. Focus the person moved elsewhere is left alone.
func chatcmd003RadioFocus(id string) {
	doc := js.Global().Get("document")
	frames, steady := 0, 0
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		frames++
		el := doc.Call("getElementById", id)
		active := doc.Get("activeElement")
		switch {
		case el.Truthy() && active.Equal(el):
			steady++
		case el.Truthy() && (!active.Truthy() || active.Equal(doc.Get("body")) || chatcmd003InRadioGroup(active)):
			steady = 0
			el.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		case el.Truthy():
			// Focus went somewhere else on purpose.
			step.Release()
			return nil
		}
		if steady >= 4 || frames >= 60 {
			step.Release()
			return nil
		}
		js.Global().Call("requestAnimationFrame", step)
		return nil
	})
	js.Global().Call("requestAnimationFrame", step)
}

// chatcmd003InRadioGroup reports whether el sits inside one of the preview
// radio groups.
func chatcmd003InRadioGroup(el js.Value) bool {
	if el.Get("closest").Type() != js.TypeFunction {
		return false
	}
	return el.Call("closest", `.chatcmd003-preview [role="radiogroup"]`).Truthy()
}
