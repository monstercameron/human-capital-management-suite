//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var personPaneWasOpen bool
var personPanePersonID string
var personTrigger js.Value
var personFocusComposer bool
var personFocusGeneration uint64
var composerFocusGeneration uint64

func rememberPersonTrigger(event ui.Event) {
	target := event.JSValue().Get("target")
	if target.Truthy() {
		personTrigger = target.Call("closest", "[data-action='open-person']")
	}
}

func syncPersonFocus(open bool) {
	syncPersonFocusFor(open, "")
}

func syncPersonFocusFor(open bool, personID string) {
	if js.Global().Get("requestAnimationFrame").Type() != js.TypeFunction || !js.Global().Get("document").Truthy() {
		return
	}
	if open == personPaneWasOpen && (!open || personID == personPanePersonID) {
		return
	}
	personPaneWasOpen = open
	personPanePersonID = ""
	if open {
		personPanePersonID = personID
	}
	personFocusGeneration++
	generation := personFocusGeneration
	attempts := 0
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		if generation != personFocusGeneration {
			frame.Release()
			return nil
		}
		if open {
			heading := js.Global().Get("document").Call("querySelector", ".chat-workspace .person-pane-heading")
			if heading.Truthy() && heading.Get("isConnected").Bool() {
				heading.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
				frame.Release()
				return nil
			}
			attempts++
			if attempts < 60 {
				js.Global().Call("requestAnimationFrame", frame)
				return nil
			}
		} else if personFocusComposer {
			personFocusComposer = false
		} else if personTrigger.Truthy() && personTrigger.Get("isConnected").Bool() {
			personTrigger.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		}
		frame.Release()
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}

// FocusComposer moves focus into the selected direct message after the person
// action finishes. The frame runs after the room and profile pane reconcile.
func FocusComposer() {
	if js.Global().Get("requestAnimationFrame").Type() != js.TypeFunction || !js.Global().Get("document").Truthy() {
		return
	}
	personFocusComposer = true
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		composer := js.Global().Get("document").Call("querySelector", ".chat-workspace #chat-composer")
		if composer.Truthy() && composer.Get("isConnected").Bool() && !composer.Get("disabled").Bool() {
			composer.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		}
		frame.Release()
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}

// FocusComposerFor waits for an asynchronously opened room to own the
// composer. A later DM request supersedes the pending focus attempt.
func FocusComposerFor(roomID string) {
	if roomID == "" || js.Global().Get("requestAnimationFrame").Type() != js.TypeFunction || !js.Global().Get("document").Truthy() {
		return
	}
	composerFocusGeneration++
	generation := composerFocusGeneration
	personFocusComposer = true
	attempts := 0
	seenRoom := false
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		if generation != composerFocusGeneration {
			frame.Release()
			return nil
		}
		root := js.Global().Get("document").Call("querySelector", ".chat-workspace")
		if root.Truthy() && root.Get("isConnected").Bool() {
			selected := root.Get("dataset").Get("selectedId").String()
			if selected == roomID {
				seenRoom = true
				composer := root.Call("querySelector", "#chat-composer")
				personPane := root.Call("querySelector", ".person-pane")
				if composer.Truthy() && !composer.Get("disabled").Bool() && !personPane.Truthy() {
					composer.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
					// A close-focus frame may already be queued after this one.
					// Keep suppression through that frame, then release it.
					var settle js.Func
					settle = js.FuncOf(func(js.Value, []js.Value) any {
						if generation == composerFocusGeneration {
							personFocusComposer = false
						}
						settle.Release()
						return nil
					})
					js.Global().Call("requestAnimationFrame", settle)
					frame.Release()
					return nil
				}
			} else if seenRoom {
				personFocusComposer = false
				frame.Release()
				return nil
			}
		}
		attempts++
		if attempts < 600 {
			js.Global().Call("requestAnimationFrame", frame)
		} else {
			personFocusComposer = false
			frame.Release()
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}
