//go:build js && wasm

package chatui

import "syscall/js"

// focusRefusedSectionField puts the caret back in the New section field once
// the refusal is drawn under it. The Create button still holds focus when the
// refusal is decided and the form is redrawn a frame or two later, so this
// keeps focusing the field until it is the active element and the message is
// in the page.
func focusRefusedSectionField() {
	frame := js.Global().Get("requestAnimationFrame")
	if frame.Type() != js.TypeFunction {
		return
	}
	attempts := 0
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		doc := js.Global().Get("document")
		input := doc.Call("getElementById", "chat-new-section")
		if input.Truthy() {
			if !doc.Get("activeElement").Equal(input) {
				input.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			}
			if doc.Get("activeElement").Equal(input) && doc.Call("getElementById", "chat-new-section-error").Truthy() {
				step.Release()
				return nil
			}
		}
		attempts++
		if attempts < 45 {
			frame.Invoke(step)
		} else {
			step.Release()
		}
		return nil
	})
	frame.Invoke(step)
}
