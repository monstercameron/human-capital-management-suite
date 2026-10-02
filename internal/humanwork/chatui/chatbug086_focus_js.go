//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatbug086Press focuses the composer's text area when the press was inside a
// composer and on none of its controls. The caret stays where it was.
func chatbug086Press(e ui.Event) {
	target := e.JSValue().Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	form := target.Call("closest", chatbug086Composers)
	if !form.Truthy() {
		return
	}
	doc := js.Global().Get("document")
	field := form.Call("querySelector", "textarea.composer-input")
	selecting := false
	if selection := js.Global().Call("getSelection"); selection.Truthy() && !selection.Get("isCollapsed").Bool() {
		// Only a selection made inside this composer is one the press could end.
		anchor := selection.Get("anchorNode")
		selecting = anchor.Truthy() && form.Call("contains", anchor).Bool()
	}
	control := target.Call("closest", chatbug086Controls)
	onControl := chatbug086OnControl(control.Truthy(), control.Truthy() && !control.Equal(form) && form.Call("contains", control).Bool())
	if !chatbug086FocusesField(true, onControl, selecting,
		field.Truthy() && !field.Get("disabled").Truthy(), field.Truthy() && doc.Get("activeElement").Equal(field)) {
		return
	}
	field.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
}

// chatbug086Typed reports whether an input event in a composer is the person
// typing there.
func chatbug086Typed(e ui.Event, target string) bool {
	doc := js.Global().Get("document")
	field := doc.Call("getElementById", target)
	return chatbug086OpensList(e.JSValue().Get("isTrusted").Truthy(), field.Truthy() && doc.Get("activeElement").Equal(field))
}

// chatbug086HoldCaret keeps the caret at caret in a composer whose text was
// just replaced. A render in the same burst can blur the box (see focusField);
// for about 45 frames focus that has fallen to the page goes back to the box,
// with the caret where the replacement left it. It stops as soon as the person
// focuses anything else or the text changes under it.
func chatbug086HoldCaret(id string, caret int) {
	doc := js.Global().Get("document")
	field := doc.Call("getElementById", id)
	if !field.Truthy() {
		return
	}
	text := field.Get("value").String()
	frames := 0
	var step js.Func
	step = js.FuncOf(func(js.Value, []js.Value) any {
		frames++
		el := doc.Call("getElementById", id)
		active := doc.Get("activeElement")
		lost := !active.Truthy() || active.Equal(doc.Get("body"))
		same := el.Truthy() && el.Get("value").String() == text
		if lost && same {
			el.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			el.Call("setSelectionRange", caret, caret)
		}
		if frames >= 45 || !same || (!lost && !active.Equal(el)) {
			step.Release()
			return nil
		}
		js.Global().Call("requestAnimationFrame", step)
		return nil
	})
	js.Global().Call("requestAnimationFrame", step)
}
