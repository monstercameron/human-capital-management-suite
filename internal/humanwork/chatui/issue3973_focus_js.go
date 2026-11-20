//go:build js && wasm

package chatui

import "syscall/js"

func handoverFrame(focus func()) {
	raf := js.Global().Get("requestAnimationFrame")
	if raf.Type() != js.TypeFunction {
		focus()
		return
	}
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		defer frame.Release()
		focus()
		return nil
	})
	raf.Invoke(frame)
}

// focusHandoverTarget moves focus to an element of the handover panel once it
// is drawn.
func focusHandoverTarget(id string) {
	handoverFrame(func() {
		if el := js.Global().Get("document").Call("getElementById", id); el.Truthy() {
			el.Call("focus")
		}
	})
}

// focusHandoverTrigger returns focus to the header button that opened the panel.
func focusHandoverTrigger() {
	handoverFrame(func() {
		if button := js.Global().Get("document").Call("querySelector", "[data-action=handover-toggle]"); button.Truthy() {
			button.Call("focus", map[string]any{"preventScroll": true})
		}
	})
}

// handoverFocusInside says whether focus is inside the handover panel.
func handoverFocusInside() bool {
	active := js.Global().Get("document").Get("activeElement")
	return active.Truthy() && active.Get("closest").Type() == js.TypeFunction && active.Call("closest", "#chat-handover-panel").Truthy()
}

// FocusHandoverText puts the caret in the prepared brief once it is drawn.
func FocusHandoverText() { focusHandoverTarget("chat-handover-text") }
