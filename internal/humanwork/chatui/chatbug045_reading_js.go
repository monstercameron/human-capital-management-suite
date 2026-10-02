//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// renderingEventForm is the reading settings form an event came from. A submit
// event carries it as its current target; a change event may reach the handler
// through a listener higher up, so the control that changed is asked for its form.
func renderingEventForm(event ui.Event) js.Value {
	value := event.JSValue()
	current := value.Get("currentTarget")
	if current.Truthy() && current.Get("tagName").Type() == js.TypeString && current.Get("tagName").String() == "FORM" {
		return current
	}
	if target := value.Get("target"); target.Truthy() && target.Get("closest").Type() == js.TypeFunction {
		if form := target.Call("closest", "form"); form.Truthy() {
			return form
		}
	}
	return current
}

// renderingChangeIsScope is whether the event is the "Apply to this conversation"
// box changing: that box chooses where the next change is saved and saves nothing.
func renderingChangeIsScope(event ui.Event) bool {
	value := event.JSValue()
	target := value.Get("target")
	return value.Get("type").String() == "change" && target.Truthy() && target.Get("name").String() == "conversation"
}
