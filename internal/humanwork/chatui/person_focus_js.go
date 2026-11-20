//go:build js && wasm

package chatui

import (
	"strings"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var personPaneWasOpen bool
var personPanePersonID string
var personTrigger js.Value
var personTriggerID, personTriggerScope string
var personFocusComposer bool
var personFocusGeneration uint64
var composerFocusGeneration uint64

func rememberPersonTrigger(event ui.Event) {
	rememberPersonTriggerFrom(event.JSValue().Get("target"))
}

// rememberPersonTriggerFrom keeps the control that first opened a person. While
// a person pane is already open, a person followed from inside it (a manager, a
// teammate) is not the control to return to: Close and Back go back to where
// the person was first opened (issue 3874). A person opened from elsewhere (for
// example another author in the conversation) does become the control to
// return to.
func rememberPersonTriggerFrom(target js.Value) {
	if !target.Truthy() {
		return
	}
	if personPaneWasOpen && personTrigger.Truthy() && target.Call("closest", ".person-pane").Truthy() {
		return
	}
	personTrigger = target.Call("closest", "[data-action='open-person']")
	personTriggerID, personTriggerScope = "", ".chat-workspace"
	if !personTrigger.Truthy() {
		return
	}
	if id := personTrigger.Call("getAttribute", "data-id"); id.Truthy() {
		personTriggerID = id.String()
	}
	if details := personTrigger.Call("closest", ".chat-details"); details.Truthy() {
		personTriggerScope = ".chat-details"
	}
}

// restorePersonTriggerFocus puts focus back where the person was opened: the
// opening control when it is still on the page, else the same person's control
// in the same area (the panel is drawn again after Back), else the details
// panel's own close button.
func restorePersonTriggerFocus() {
	opts := js.ValueOf(map[string]any{"preventScroll": true})
	if personTrigger.Truthy() && personTrigger.Get("isConnected").Bool() {
		personTrigger.Call("focus", opts)
		return
	}
	doc := js.Global().Get("document")
	if personTriggerID != "" && !strings.ContainsAny(personTriggerID, "\"\\") {
		scope := personTriggerScope
		if scope == "" {
			scope = ".chat-workspace"
		}
		if el := doc.Call("querySelector", scope+" [data-action='open-person'][data-id=\""+personTriggerID+"\"]"); el.Truthy() {
			el.Call("focus", opts)
			return
		}
	}
	if el := doc.Call("querySelector", ".chat-details [data-action='close-details']"); el.Truthy() {
		el.Call("focus", opts)
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
		} else {
			restorePersonTriggerFocus()
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
