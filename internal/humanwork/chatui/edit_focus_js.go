//go:build js && wasm

package chatui

import "syscall/js"

// lastEditingID tracks the message being edited across renders so
// syncEditFocus can tell "just cancelled/saved" (was set, now empty) apart
// from "editing something else" or "nothing ever started" (CHAT-01).
var lastEditingID string

// syncEditFocus restores focus to the message's own actions trigger when its
// edit form closes -- via Cancel, Escape, or a successful save -- instead of
// dropping focus to the document body the way a form that simply vanishes
// does.
func syncEditFocus(editingID string) {
	previous := lastEditingID
	lastEditingID = editingID
	if editingID != "" || previous == "" {
		return
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("querySelector").Type() != js.TypeFunction {
		return
	}
	// Edit is a menu-item that only exists in the DOM while that message's
	// "More actions" menu is open; the menu's own trigger button is always
	// present, so that is what gets focus back.
	target := doc.Call("querySelector", `[data-action="menu"][data-id="`+previous+`"]`)
	if target.Truthy() && target.Get("focus").Type() == js.TypeFunction {
		target.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	}
}
