//go:build js && wasm

package chatui

import "syscall/js"

// The Rewrite control has two layers: the menu of styles that its button opens,
// and the layer "See changes" opens from the line a rewrite leaves above the
// composer's text. Both are "writing-style" layers of the shared layer code, so
// opening one closes the other, and both carry data-chattone-for, the id of the
// composer they belong to. Everything here finds them by that id, from whichever
// part of the control it was given.

// chattoneMenuItems are the rows of the Rewrite menu that can be chosen.
const chattoneMenuItems = "[role=menuitem]:not([disabled])"

// writingStyleTarget is the composer a node of the control belongs to: from the
// toolbar, from one of the two layers, or from the line above the text.
func writingStyleTarget(node js.Value) string {
	if !node.Truthy() || node.Get("closest").Type() != js.TypeFunction {
		return ""
	}
	for _, probe := range [][2]string{{"[data-chattone-for]", "chattoneFor"}, {"[data-chattone-chip]", "chattoneChip"}, {"[data-chattone=toolbar]", "target"}} {
		if owner := node.Call("closest", probe[0]); owner.Truthy() {
			if value := owner.Get("dataset").Get(probe[1]); value.Type() == js.TypeString {
				return value.String()
			}
		}
	}
	return ""
}

func writingStyleByID(id string) js.Value {
	return js.Global().Get("document").Call("getElementById", id)
}

func writingStyleTrigger(target string) js.Value {
	return writingStyleByID(target + "-chattone-trigger")
}

func writingStyleLayers(target string) js.Value {
	return js.Global().Get("document").Call("querySelectorAll", "[data-chat-layer=writing-style][data-chattone-for='"+target+"']")
}

func hideWritingStyleLayer(layer js.Value) {
	if !layer.Truthy() {
		return
	}
	layer.Set("hidden", true)
	if layer.Call("matches", ":popover-open").Bool() {
		layer.Call("hidePopover")
	}
}

// showWritingStyleLayer opens one of the control's layers against a button.
func showWritingStyleLayer(layer, opener js.Value) {
	closeChatLayersOfKind(layer)
	layer.Set("hidden", false)
	if root := chatLayerRoot(); root.Truthy() {
		root.Set("__chatLayerOpener_writing-style", opener)
	}
	positionChatLayer(layer, opener, true)
}

// setWritingStyleExpanded keeps the buttons that open the layers honest about
// whether they are open.
func setWritingStyleExpanded(target string, menu, diff bool) {
	if trigger := writingStyleTrigger(target); trigger.Truthy() {
		trigger.Call("setAttribute", "aria-expanded", boolString(menu))
	}
	if changes := writingStyleChanges(target); changes.Truthy() {
		changes.Call("setAttribute", "aria-expanded", boolString(diff))
	}
}

// writingStyleChanges is the See changes button of the line above the text.
func writingStyleChanges(target string) js.Value {
	slot := writingStyleByID(target + "-chattone-chip")
	if !slot.Truthy() {
		return js.Undefined()
	}
	return slot.Call("querySelector", "[data-chattone-action=changes]")
}

// openWritingStyleMenu opens the menu and puts focus on its first row, or on
// its last when the person asked for it with the up arrow.
func openWritingStyleMenu(target string, last bool) {
	trigger := writingStyleByID(target + "-chattone-trigger")
	menu := writingStyleByID(target + "-chattone-menu")
	if !menu.Truthy() || !trigger.Truthy() {
		return
	}
	showWritingStyleLayer(menu, trigger)
	setWritingStyleExpanded(target, true, false)
	rows := menu.Call("querySelectorAll", chattoneMenuItems)
	if count := rows.Get("length").Int(); count > 0 {
		index := 0
		if last {
			index = count - 1
		}
		rows.Index(index).Call("focus")
	}
}

func toggleWritingStyleMenu(button js.Value) {
	target := writingStyleTarget(button)
	if target == "" {
		return
	}
	if menu := writingStyleByID(target + "-chattone-menu"); menu.Truthy() && !menu.Get("hidden").Bool() {
		closeWritingStyleLayers(button, true)
		return
	}
	openWritingStyleMenu(target, false)
}

// ToggleWritingStyleChanges opens the layer that shows the text as typed beside
// the text now in the field, against the See changes button, or closes it. It
// reports whether it is open afterwards.
func ToggleWritingStyleChanges(button js.Value) bool {
	target := writingStyleTarget(button)
	layer := writingStyleByID(target + "-chattone-options")
	if target == "" || !layer.Truthy() {
		return false
	}
	if !layer.Get("hidden").Bool() {
		closeWritingStyleLayers(button, false)
		return false
	}
	showWritingStyleLayer(layer, button)
	setWritingStyleExpanded(target, false, true)
	return true
}

// WritingStyleChangesOpen reports whether the changes layer of a composer is open.
func WritingStyleChangesOpen(target string) bool {
	layer := writingStyleByID(target + "-chattone-options")
	return layer.Truthy() && !layer.Get("hidden").Bool()
}

// ShowWritingStyleChanges opens the changes layer again after the line above
// the text was drawn anew (an edit changes what it says), against the new button.
func ShowWritingStyleChanges(target string) {
	layer, button := writingStyleByID(target+"-chattone-options"), writingStyleChanges(target)
	if !layer.Truthy() || !button.Truthy() {
		return
	}
	showWritingStyleLayer(layer, button)
	setWritingStyleExpanded(target, false, true)
}

// closeWritingStyleLayers closes both layers. Focus goes back to the button that
// opened what was open when asked: the Rewrite button, or See changes.
func closeWritingStyleLayers(node js.Value, restoreFocus bool) {
	target := writingStyleTarget(node)
	if target == "" {
		return
	}
	layers := writingStyleLayers(target)
	for i := 0; i < layers.Length(); i++ {
		hideWritingStyleLayer(layers.Index(i))
	}
	setWritingStyleExpanded(target, false, false)
	if !restoreFocus {
		return
	}
	focus := writingStyleTrigger(target)
	if node.Truthy() && node.Get("matches").Type() == js.TypeFunction && node.Call("matches", "[data-chattone-action=changes]").Bool() && node.Get("isConnected").Truthy() {
		focus = node
	}
	if focus.Truthy() && !focus.Get("disabled").Truthy() {
		focus.Call("focus")
	}
}

// WritingStyleMenuOpen reports whether the Rewrite menu of a composer is open.
func WritingStyleMenuOpen(target string) bool {
	menu := writingStyleByID(target + "-chattone-menu")
	return menu.Truthy() && !menu.Get("hidden").Bool()
}

// HandleWritingStyleMenuKey gives the control its keys. Inside the menu, Escape
// closes it and returns focus to the Rewrite button, the arrows, Home and End
// move between the rows, and Tab closes it and carries on. On the Rewrite button
// the up and down arrows open it. Escape also closes the changes layer from the
// line above the text. It reports whether the key was the control's own, so the
// caller can stop the browser acting on it.
func HandleWritingStyleMenuKey(event js.Value) bool {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return false
	}
	key := event.Get("key").String()
	if menu := target.Call("closest", "[data-chattone-menu]"); menu.Truthy() {
		switch key {
		case "Escape":
			closeWritingStyleLayers(menu, true)
			return true
		case "Tab":
			closeWritingStyleLayers(menu, false)
			return false
		case "ArrowDown", "ArrowUp", "Home", "End":
			rows := menu.Call("querySelectorAll", chattoneMenuItems)
			count := rows.Get("length").Int()
			if count == 0 {
				return true
			}
			current := -1
			active := js.Global().Get("document").Get("activeElement")
			for i := 0; i < count; i++ {
				if rows.Index(i).Equal(active) {
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
			rows.Index(next).Call("focus")
			return true
		}
		return false
	}
	if key == "Escape" {
		if owner := target.Call("closest", "[data-chattone-chip],[data-chattone-diff]"); owner.Truthy() && WritingStyleChangesOpen(writingStyleTarget(owner)) {
			closeWritingStyleLayers(owner, false)
			if changes := writingStyleChanges(writingStyleTarget(owner)); changes.Truthy() {
				changes.Call("focus")
			}
			return true
		}
		return false
	}
	trigger := target.Call("closest", "[data-chattone-action=toggle]")
	if trigger.Truthy() && !trigger.Get("disabled").Truthy() && (key == "ArrowDown" || key == "ArrowUp") {
		if id := writingStyleTarget(trigger); id != "" {
			openWritingStyleMenu(id, key == "ArrowUp")
			return true
		}
	}
	return false
}
