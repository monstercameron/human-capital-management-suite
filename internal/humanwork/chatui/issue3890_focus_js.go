//go:build js && wasm

package chatui

import "syscall/js"

// focusSectionNameForm moves focus into the section name field once the form is
// drawn and scrolls the sidebar so the whole form (label, field, buttons) is
// visible. The menu item that opened it still holds focus for a frame, so this
// waits for the field itself instead of only repairing a lost focus.
func focusSectionNameForm() {
	frame := js.Global().Get("requestAnimationFrame")
	if frame.Type() != js.TypeFunction {
		return
	}
	attempts := 0
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		doc := js.Global().Get("document")
		input := doc.Call("getElementById", chatside001NameField)
		if input.Truthy() {
			input.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			scrollSectionNameFormIntoView(input)
			if doc.Get("activeElement").Equal(input) {
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

// scrollSectionNameFormIntoView adjusts only the sidebar's own scroll area.
func scrollSectionNameFormIntoView(input js.Value) {
	form := input.Call("closest", "form")
	if !form.Truthy() {
		return
	}
	scroll := form.Call("closest", ".rail-scroll")
	if !scroll.Truthy() {
		return
	}
	view, box := scroll.Call("getBoundingClientRect"), form.Call("getBoundingClientRect")
	delta := issue3890ScrollDelta(view.Get("top").Float(), view.Get("bottom").Float(), box.Get("top").Float(), box.Get("bottom").Float())
	if delta != 0 {
		scroll.Set("scrollTop", scroll.Get("scrollTop").Float()+delta)
	}
}

// returnFocusToOptions puts focus on the options control (three dots) with the
// given data-id after the name form is gone. The form's removal lands on a
// later frame, so a few frames are tried; it never steals focus the person has
// already moved to another control.
func returnFocusToOptions(id string) {
	frame := js.Global().Get("requestAnimationFrame")
	if id == "" || frame.Type() != js.TypeFunction {
		return
	}
	attempts := 0
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		doc := js.Global().Get("document")
		active := doc.Get("activeElement")
		if form := doc.Call("getElementById", chatside001NameField); !form.Truthy() {
			if !active.Truthy() || active.Equal(doc.Get("body")) {
				rows := doc.Call("querySelectorAll", ".rail-row-more")
				for i := 0; i < rows.Get("length").Int(); i++ {
					if button := rows.Index(i); button.Get("dataset").Get("id").String() == id {
						button.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
						break
					}
				}
			}
			step.Release()
			return nil
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
