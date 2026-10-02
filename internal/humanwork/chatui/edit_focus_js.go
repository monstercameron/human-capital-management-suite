//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// lastEditingID tracks the message being edited across renders so
// syncEditFocus can tell "just cancelled/saved" (was set, now empty) apart
// from "editing something else" or "nothing ever started" (CHAT-01).
var lastEditingID string

// syncEditFocus restores focus to the message's own actions trigger when its
// edit form closes -- via Cancel, Escape, or a successful save -- instead of
// dropping focus to the document body the way a form that simply vanishes
// does. When an edit form opens it puts the caret in the box (CHATBUG-073).
func syncEditFocus(editingID string) {
	previous := lastEditingID
	lastEditingID = editingID
	if editingID != "" {
		if editingID != previous {
			focusEditBox(editingID, true)
		}
		return
	}
	if previous == "" {
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
		return
	}
	// The edit was opened from the composer with ArrowUp and the row's bar is
	// not drawn: the caret goes back to the composer it came from.
	if composer := doc.Call("getElementById", "chat-composer"); composer.Truthy() && composer.Get("focus").Type() == js.TypeFunction {
		composer.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	}
}

// editBox is the edit form's text box for a message, or a falsy value.
func editBox(id string) js.Value {
	doc := js.Global().Get("document")
	if id == "" || !doc.Truthy() || doc.Get("getElementById").Type() != js.TypeFunction {
		return js.Null()
	}
	return doc.Call("getElementById", "edit-"+id)
}

// focusEditBox puts the caret at the end of the message's text in its edit
// box and brings the box into view. The box's text is written by the field
// observer a moment after the form is drawn (fieldsync_js.go); it is written
// here first so that the caret has an end to go to. When the form is not in
// the document yet the next frame tries once more.
func focusEditBox(id string, retry bool) {
	box := editBox(id)
	if !box.Truthy() {
		if raf := js.Global().Get("requestAnimationFrame"); retry && raf.Type() == js.TypeFunction {
			var again js.Func
			again = js.FuncOf(func(js.Value, []js.Value) any {
				again.Release()
				if lastEditingID == id {
					focusEditBox(id, false)
				}
				return nil
			})
			raf.Invoke(again)
		}
		return
	}
	applyFieldValue(box)
	box.Call("focus")
	end := box.Get("value").Get("length")
	func() {
		defer func() { _ = recover() }()
		box.Call("setSelectionRange", end, end)
	}()
	syncEditBoxHeight(id)
}

// editBoxSizesItself reports whether the browser sizes a text box to its text
// (field-sizing), which the stylesheet asks for.
func editBoxSizesItself() bool {
	css := js.Global().Get("CSS")
	return css.Truthy() && css.Get("supports").Type() == js.TypeFunction && css.Call("supports", "field-sizing", "content").Truthy()
}

// syncEditBoxHeight makes the edit box as tall as its text, up to half the
// window, in a browser that does not size the box itself.
func syncEditBoxHeight(id string) {
	box := editBox(id)
	if !box.Truthy() || editBoxSizesItself() {
		return
	}
	style := box.Get("style")
	style.Set("height", "auto")
	border := box.Get("offsetHeight").Float() - box.Get("clientHeight").Float()
	height := box.Get("scrollHeight").Float() + border
	if window := js.Global().Get("innerHeight"); window.Type() == js.TypeNumber {
		height = editBoxHeight(height, window.Float())
	}
	style.Set("height", strconv.Itoa(int(height+0.5))+"px")
}

// chatbug073EditKey saves the edit when Enter is pressed in the edit box, by
// submitting the box's form so that the one save path runs. It reports whether
// it took the key.
func chatbug073EditKey(e ui.KeyboardEvent, editingID string) bool {
	if editingID == "" || !chatbug073EditSaves(e.GetKey(), shiftHeld(e), composerIsComposing(e)) {
		return false
	}
	target := e.JSValue().Get("target")
	if !target.Truthy() || target.Get("id").Type() != js.TypeString || target.Get("id").String() != "edit-"+editingID {
		return false
	}
	e.PreventDefault()
	e.StopPropagation()
	if form := target.Get("form"); form.Truthy() && form.Get("requestSubmit").Type() == js.TypeFunction {
		form.Call("requestSubmit")
	}
	return true
}
