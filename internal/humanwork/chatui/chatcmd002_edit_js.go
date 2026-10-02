//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatcmd002FieldValues reads the fields of a card's edit form and add-option
// row from the page: each field's name (data-chatcmd002-field) and what it
// holds. The same message can be drawn twice, in the timeline and in its thread,
// and the first field of a name is the one read.
func chatcmd002FieldValues(post string) map[string]string {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil
	}
	fields := doc.Call("querySelectorAll", "[data-chatcmd002-field]")
	out := map[string]string{}
	for i := 0; i < fields.Length(); i++ {
		field := fields.Index(i)
		if field.Get("dataset").Get("chatcmd002Post").String() != post {
			continue
		}
		name := field.Get("dataset").Get("chatcmd002Field").String()
		if _, seen := out[name]; !seen {
			out[name] = field.Get("value").String()
		}
	}
	return out
}

// chatcmd002ClearField empties a field once its words have been sent.
func chatcmd002ClearField(post, name string) {
	fields := js.Global().Get("document").Call("querySelectorAll", "[data-chatcmd002-field]")
	for i := 0; i < fields.Length(); i++ {
		field := fields.Index(i)
		if field.Get("dataset").Get("chatcmd002Post").String() == post && field.Get("dataset").Get("chatcmd002Field").String() == name {
			field.Set("value", "")
		}
	}
}

// chatcmd002KeyAction is the card action a key press in one of its fields asks
// for: Enter in a field that names one (add the option, save the edit) and
// Escape in an edit form (cancel). A press that is part of composing text, or
// Enter with Shift, asks for nothing.
func chatcmd002KeyAction(e ui.KeyboardEvent) (action, post string) {
	key := e.GetKey()
	if key != "Enter" && key != "Escape" {
		return "", ""
	}
	value := e.JSValue()
	if value.Get("isComposing").Truthy() || value.Get("shiftKey").Truthy() {
		return "", ""
	}
	target := value.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return "", ""
	}
	field := target.Call("closest", "[data-chatcmd002-field]")
	if !field.Truthy() {
		return "", ""
	}
	attribute := "chatcmd002Enter"
	if key == "Escape" {
		attribute = "chatcmd002Escape"
	}
	return field.Get("dataset").Get(attribute).String(), field.Get("dataset").Get("chatcmd002Post").String()
}
