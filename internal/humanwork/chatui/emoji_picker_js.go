//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func emojiPickerNode(id string) js.Value {
	return js.Global().Get("document").Call("getElementById", id+"-emoji-picker")
}

func setEmojiPickerOpen(picker js.Value, open bool) {
	if !picker.Truthy() {
		return
	}
	trigger := js.Global().Get("document").Call("querySelector", "[data-action=emoji-toggle][data-id='"+picker.Get("id").String()+"']")
	// Picker IDs end in -emoji-picker while targets are textarea IDs.
	if !trigger.Truthy() {
		pickerID := picker.Get("id").String()
		targetID := pickerID[:len(pickerID)-len("-emoji-picker")]
		trigger = js.Global().Get("document").Call("querySelector", "[data-action=emoji-toggle][data-id='"+targetID+"']")
	}
	if open {
		picker.Call("removeAttribute", "hidden")
		if trigger.Truthy() {
			trigger.Call("setAttribute", "aria-expanded", "true")
		}
		return
	}
	picker.Call("setAttribute", "hidden", "")
	if trigger.Truthy() {
		trigger.Call("setAttribute", "aria-expanded", "false")
	}
}

func toggleEmojiPicker(targetID string) {
	picker := emojiPickerNode(targetID)
	if !picker.Truthy() {
		return
	}
	wasOpen := !picker.Call("hasAttribute", "hidden").Bool()
	closeEmojiPickers(false)
	if wasOpen {
		trigger := emojiPickerTrigger(targetID)
		if trigger.Truthy() {
			trigger.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		}
		return
	}
	setEmojiPickerOpen(picker, true)
	buttons := picker.Call("querySelectorAll", "button")
	if buttons.Get("length").Int() > 0 {
		buttons.Index(0).Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	}
}

func emojiPickerTrigger(targetID string) js.Value {
	return js.Global().Get("document").Call("querySelector", "[data-action=emoji-toggle][data-id='"+targetID+"']")
}

func closeEmojiPickers(restoreFocus bool) {
	doc := js.Global().Get("document")
	pickers := doc.Call("querySelectorAll", ".emoji-picker:not([hidden])")
	for i := 0; i < pickers.Get("length").Int(); i++ {
		picker := pickers.Index(i)
		id := picker.Get("id").String()
		targetID := id[:len(id)-len("-emoji-picker")]
		setEmojiPickerOpen(picker, false)
		if restoreFocus {
			if trigger := emojiPickerTrigger(targetID); trigger.Truthy() {
				trigger.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			}
		}
	}
}

func insertComposerEmoji(targetID, emoji string) {
	doc := js.Global().Get("document")
	field := doc.Call("getElementById", targetID)
	if !field.Truthy() || field.Get("disabled").Truthy() || emoji == "" {
		return
	}
	value := field.Get("value").String()
	updated, caret := insertEmojiAtUTF16(value, emoji, field.Get("selectionStart").Int(), field.Get("selectionEnd").Int())
	field.Set("value", updated)
	// This marks the field as user-typed and follows the same draft callback as
	// keyboard input, so a render cannot roll back a picker insertion.
	event := js.Global().Get("Event").New("input", map[string]any{"bubbles": true})
	field.Call("dispatchEvent", event)
	field = doc.Call("getElementById", targetID)
	if !field.Truthy() {
		return
	}
	field.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	field.Call("setSelectionRange", caret, caret)
	closeEmojiPickers(false)
}

func handleEmojiPickerKey(event ui.KeyboardEvent) bool {
	eventValue := event.JSValue()
	target := eventValue.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return false
	}
	picker := target.Call("closest", ".emoji-picker")
	if !picker.Truthy() {
		return false
	}
	key := event.GetKey()
	if key == "Escape" {
		id := picker.Get("id").String()
		targetID := id[:len(id)-len("-emoji-picker")]
		setEmojiPickerOpen(picker, false)
		if trigger := emojiPickerTrigger(targetID); trigger.Truthy() {
			trigger.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		}
		return true
	}
	indexDelta := 0
	switch key {
	case "ArrowRight":
		indexDelta = 1
	case "ArrowLeft":
		indexDelta = -1
	case "ArrowDown":
		indexDelta = 4
	case "ArrowUp":
		indexDelta = -4
	default:
		return false
	}
	buttons := picker.Call("querySelectorAll", "button")
	length := buttons.Get("length").Int()
	if length == 0 {
		return false
	}
	active := target.Call("closest", "button")
	index := 0
	for i := 0; i < length; i++ {
		if buttons.Index(i).Equal(active) {
			index = i
			break
		}
	}
	if key == "ArrowRight" || key == "ArrowLeft" {
		if picker.Call("closest", "[dir=rtl]").Truthy() {
			indexDelta = -indexDelta
		}
	}
	next := (index + indexDelta) % length
	if next < 0 {
		next += length
	}
	buttons.Index(next).Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	return true
}
