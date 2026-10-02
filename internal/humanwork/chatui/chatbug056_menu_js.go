//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"
)

// commandMenuShow draws the "/" list of the composer named by target in the
// page at once: names are the commands to show, in order, and active is the
// highlighted one; no names closes the list. It writes the same attributes
// composerCommandMenuView renders, so the render that follows changes nothing.
// It exists because that render can be a second away while a conversation is
// still loading, and a list that opens a second after the "/" reads as broken.
func commandMenuShow(target string, names []string, active int) {
	doc := js.Global().Get("document")
	if !doc.Truthy() || target == "" {
		return
	}
	menu := doc.Call("getElementById", target+"-commands")
	if !menu.Truthy() {
		return
	}
	menu.Call("setAttribute", "data-open", boolString(len(names) > 0)+":now")
	if len(names) > 0 {
		menu.Call("setAttribute", "role", "listbox")
	} else {
		menu.Call("removeAttribute", "role")
	}
	if active < 0 || active >= len(names) {
		active = 0
	}
	rows := menu.Call("querySelectorAll", "[data-command]")
	for i := 0; i < rows.Length(); i++ {
		row := rows.Index(i)
		order := -1
		for j, name := range names {
			if name == row.Get("dataset").Get("command").String() {
				order = j
			}
		}
		selected := order >= 0 && order == active
		row.Set("hidden", order < 0)
		row.Get("dataset").Set("order", strconv.Itoa(max(order, 0)))
		row.Get("dataset").Set("extra", strconv.Itoa(order+1))
		row.Get("classList").Call("toggle", "active", selected)
		row.Call("setAttribute", "aria-selected", boolString(selected))
		if order >= 0 {
			row.Set("id", target+"-command-"+strconv.Itoa(order+1))
		} else {
			row.Call("removeAttribute", "id")
		}
	}
}

// commandMenuSubmit sends what the composer named by target holds, the way its
// Send button does.
func commandMenuSubmit(target string) {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	field := doc.Call("getElementById", target)
	if !field.Truthy() || field.Get("closest").Type() != js.TypeFunction {
		return
	}
	form := field.Call("closest", "form")
	if !form.Truthy() {
		return
	}
	if button := form.Call("querySelector", ".send-button"); button.Truthy() && !button.Get("disabled").Truthy() {
		button.Call("click")
	}
}
