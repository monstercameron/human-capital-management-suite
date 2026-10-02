//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// viewportAtLeast reports whether the window is at least px wide. A window
// that cannot say counts as wide.
func viewportAtLeast(px int) bool {
	match := js.Global().Get("matchMedia")
	if match.Type() != js.TypeFunction {
		return true
	}
	return js.Global().Call("matchMedia", "(min-width:"+itoa(px)+"px)").Get("matches").Bool()
}

// openComposerAddMenu returns the open Add menu, or an undefined value.
func openComposerAddMenu() js.Value {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return js.Undefined()
	}
	return doc.Call("querySelector", ".chat-composer .composer-add-menu:not([hidden])")
}

// closeComposerAddMenu closes the Add menu through the shared disclosure code,
// returning focus to the + button when asked.
func closeComposerAddMenu(restoreFocus bool) {
	if menu := openComposerAddMenu(); menu.Truthy() {
		closeChatDisclosureLayer(menu, restoreFocus)
	}
}

// inComposerAdd reports whether a node sits inside the Add control.
func inComposerAdd(node js.Value) bool {
	return node.Truthy() && node.Get("closest").Type() == js.TypeFunction && node.Call("closest", ".composer-add").Truthy()
}

// dismissComposerAddMenu closes the Add menu for a click that landed outside
// the Add control.
func dismissComposerAddMenu(e ui.Event) {
	menu := openComposerAddMenu()
	if !menu.Truthy() || inComposerAdd(e.JSValue().Get("target")) {
		return
	}
	closeChatDisclosureLayer(menu, false)
}

// composerAddMenuKey gives the Add menu its keys while focus is inside it:
// Escape closes it and returns focus to the + button, the arrows and Home and
// End move between its items, and Tab closes it and carries on to the next
// control. It reports whether the key was the menu's own.
func composerAddMenuKey(e ui.KeyboardEvent) bool {
	menu := openComposerAddMenu()
	if !menu.Truthy() || !inComposerAdd(e.JSValue().Get("target")) {
		return false
	}
	switch key := e.GetKey(); key {
	case "Escape":
		closeChatDisclosureLayer(menu, true)
		return true
	case "Tab":
		closeChatDisclosureLayer(menu, false)
		return false
	case "ArrowDown", "ArrowUp", "Home", "End":
		items := menu.Call("querySelectorAll", "[role=menuitem]:not([disabled])")
		count := items.Get("length").Int()
		if count == 0 {
			return true
		}
		current := -1
		active := js.Global().Get("document").Get("activeElement")
		for i := 0; i < count; i++ {
			if items.Index(i).Equal(active) {
				current = i
			}
		}
		next := 0
		switch key {
		case "ArrowDown":
			next = (current + 1) % count
		case "ArrowUp":
			next = (current - 1 + count) % count
			if current < 0 {
				next = count - 1
			}
		case "End":
			next = count - 1
		}
		items.Index(next).Call("focus")
		return true
	}
	return false
}

// clickComposerControl presses one of the composer's own controls, the way the
// person would. The Add menu's Location and Voice message items press the
// location and voice controls this way, so each opens exactly what it always
// has.
func clickComposerControl(selector string) {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	if control := doc.Call("querySelector", ".chat-composer "+selector); control.Truthy() && !control.Get("disabled").Truthy() {
		control.Call("click")
	}
}

// insertComposerMention puts an @ at the composer's caret and reports it
// through the field's input event, so the mention list opens as it does for a
// typed @. The caret is set before the event fires: the list reads it there.
func insertComposerMention(id string) {
	doc := js.Global().Get("document")
	field := doc.Call("getElementById", id)
	if !field.Truthy() || field.Get("disabled").Truthy() || field.Get("value").Type() != js.TypeString {
		return
	}
	start, end := field.Get("selectionStart"), field.Get("selectionEnd")
	value := field.Get("value").String()
	from, to := len(value), len(value)
	if start.Type() == js.TypeNumber && end.Type() == js.TypeNumber {
		from, to = start.Int(), end.Int()
	}
	updated, caret := composerMentionInsert(value, from, to)
	field.Set("value", updated)
	field.Call("setSelectionRange", caret, caret)
	field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
	if field = doc.Call("getElementById", id); field.Truthy() {
		field.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		field.Call("setSelectionRange", caret, caret)
	}
}

// openComposerLocation presses the location control, the way /location does.
func openComposerLocation() { clickComposerControl("[data-chatmap-action=toggle]") }

// rememberComposerLayerOpener makes the + button the anchor of the poll or
// to-do card an Add menu item opens: the menu item that was pressed is gone as
// soon as the menu closes, and the card is placed against, and returns focus to,
// the button that is still on screen.
func rememberComposerLayerOpener(kind string) {
	root := chatLayerRoot()
	doc := js.Global().Get("document")
	if !root.Truthy() || !doc.Truthy() {
		return
	}
	trigger := doc.Call("querySelector", ".chat-composer .composer-add-trigger")
	if !trigger.Truthy() {
		return
	}
	rect := trigger.Call("getBoundingClientRect")
	root.Set("__chatLayerOpener", trigger)
	root.Set("__chatLayerOpener_"+kind, trigger)
	root.Set("__chatLayerRect_"+kind, []any{rect.Get("left").Float(), rect.Get("top").Float(), rect.Get("right").Float(), rect.Get("bottom").Float()})
}
