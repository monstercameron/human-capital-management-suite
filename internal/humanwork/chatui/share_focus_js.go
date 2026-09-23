//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var shareTriggerID string

func rememberShareTrigger(event ui.Event) {
	target := event.JSValue().Get("target")
	if target.Truthy() {
		shareTriggerID = target.Call("closest", "[data-action='open-share']").Get("dataset").Get("id").String()
	}
}

func shareFrame(focus func() bool) {
	if !js.Global().Get("requestAnimationFrame").Truthy() {
		return
	}
	attempts := 0
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		attempts++
		if !focus() && attempts < 60 {
			js.Global().Call("requestAnimationFrame", frame)
			return nil
		}
		frame.Release()
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}

func focusShareDialog() {
	shareFrame(func() bool {
		return EnsureShareFocus()
	})
}

// EnsureShareFocus repairs focus after a deferred dialog render without
// interrupting someone already using a control inside it.
func EnsureShareFocus() bool {
	doc := js.Global().Get("document")
	dialog := doc.Call("querySelector", ".share-dialog")
	if !dialog.Truthy() {
		return false
	}
	if dialog.Call("contains", doc.Get("activeElement")).Bool() {
		return true
	}
	input := dialog.Call("querySelector", "#share-filter")
	if input.Truthy() && !input.Get("disabled").Bool() {
		input.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	} else {
		dialog.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	}
	return true
}

func restoreShareFocus() { RestoreShareFocus() }

// RestoreShareFocus returns to the original message after a successful share.
func RestoreShareFocus() {
	id := shareTriggerID
	shareFrame(func() bool {
		if js.Global().Get("document").Call("querySelector", ".share-dialog").Truthy() {
			return false
		}
		buttons := js.Global().Get("document").Call("querySelectorAll", ".message-action[data-action='menu']")
		for i := 0; i < buttons.Get("length").Int(); i++ {
			button := buttons.Index(i)
			if button.Get("dataset").Get("id").String() == id && button.Call("getClientRects").Get("length").Int() > 0 {
				button.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
				return true
			}
		}
		composer := js.Global().Get("document").Call("querySelector", "#chat-composer")
		if composer.Truthy() {
			composer.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			return true
		}
		return false
	})
}

func trapShareFocus(event ui.Event) bool {
	if event.JSValue().Get("key").String() != "Tab" {
		return false
	}
	doc := js.Global().Get("document")
	dialog := doc.Call("querySelector", ".share-dialog")
	if !dialog.Truthy() {
		return false
	}
	buttons := dialog.Call("querySelectorAll", "button:not([disabled]),input:not([disabled])")
	length := buttons.Get("length").Int()
	if length == 0 {
		dialog.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		return true
	}
	active := doc.Get("activeElement")
	first, last := buttons.Index(0), buttons.Index(length-1)
	if event.JSValue().Get("shiftKey").Bool() {
		if active.Equal(first) || !dialog.Call("contains", active).Bool() {
			last.Call("focus")
			return true
		}
	} else if active.Equal(last) || !dialog.Call("contains", active).Bool() {
		first.Call("focus")
		return true
	}
	return false
}
