//go:build js && wasm

package chatui

import "syscall/js"

func jsStringValue(v js.Value) string {
	if v.Type() == js.TypeString {
		return v.String()
	}
	return ""
}

// bindMessageMenuSurfaceGuard closes an open message menu when another surface
// opens (CHATBUG-030). A layer (the Saved list, a tray, the search panel) opens
// by showing a popover, which announces itself with a toggle event; the Saved
// list's own click handler also stops the click before the workspace's
// outside-click rule sees it, so a click that reaches the window and is neither
// inside the menu nor on a delegated control closes the menu as well.
func bindMessageMenuSurfaceGuard(closeMenu func()) func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() || closeMenu == nil {
		return nil
	}
	window := js.Global()
	toggle := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		if jsStringValue(event.Get("newState")) != "open" {
			return nil
		}
		target := event.Get("target")
		if !target.Truthy() || target.Get("dataset").Type() != js.TypeObject {
			return nil
		}
		if messageMenuClosesFor(jsStringValue(target.Get("dataset").Get("chatLayer"))) {
			closeMenu()
		}
		return nil
	})
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return nil
		}
		if target.Call("closest", ".message-menu,[data-action],[data-chat-layer]").Truthy() {
			return nil
		}
		if target.Call("closest", "[data-saved-action],[data-saved-sidebar]").Truthy() {
			closeMenu()
		}
		return nil
	})
	doc.Call("addEventListener", "toggle", toggle, true)
	window.Call("addEventListener", "click", click, true)
	return func() {
		doc.Call("removeEventListener", "toggle", toggle, true)
		window.Call("removeEventListener", "click", click, true)
		toggle.Release()
		click.Release()
	}
}

// chatRowElement finds the timeline row or thread reply with the message id.
func chatRowElement(id string) js.Value {
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("querySelectorAll").Type() != js.TypeFunction {
		return js.Null()
	}
	rows := doc.Call("querySelectorAll", ".message[data-message-id],.thread-message[data-message-id]")
	for i := 0; i < rows.Get("length").Int(); i++ {
		row := rows.Index(i)
		if jsStringValue(row.Get("dataset").Get("messageId")) == id {
			return row
		}
	}
	return js.Null()
}

// reconcileChatRowActions drops the remembered pointer or focus row when the
// browser no longer agrees with it: the element was replaced or removed (a
// removed element never reports its pointer or focus leaving), the pointer is
// elsewhere, or focus is. Without it a hover bar outlives its row.
func reconcileChatRowActions(local localStore) {
	state := local.get()
	if state.pointerRow == "" && state.focusRow == "" {
		return
	}
	doc := js.Global().Get("document")
	clearPointer, clearFocus := false, false
	if state.pointerRow != "" {
		row := chatRowElement(state.pointerRow)
		clearPointer = chatRowActionsStale(state.pointerRow, row.Truthy(), row.Truthy() && row.Call("matches", ":hover").Bool())
	}
	if state.focusRow != "" {
		row := chatRowElement(state.focusRow)
		held := false
		if row.Truthy() {
			if active := doc.Get("activeElement"); active.Truthy() {
				held = row.Call("contains", active).Bool()
			}
		}
		clearFocus = chatRowActionsStale(state.focusRow, row.Truthy(), held)
	}
	if clearPointer || clearFocus {
		local.update(func(u *localUI) {
			if clearPointer {
				u.pointerRow = ""
			}
			if clearFocus {
				u.focusRow = ""
			}
		})
	}
}

// bindChatRowActionGuard reconciles the active-row state when the pointer
// leaves the page, the window loses focus, or a press lands outside every row.
func bindChatRowActionGuard(local localStore) func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil
	}
	window := js.Global()
	leave := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Get("relatedTarget").Truthy() {
			return nil
		}
		reconcileChatRowActions(local)
		return nil
	})
	away := js.FuncOf(func(js.Value, []js.Value) any { reconcileChatRowActions(local); return nil })
	press := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if target.Truthy() && target.Get("closest").Type() == js.TypeFunction && target.Call("closest", ".message[data-message-id],.thread-message[data-message-id]").Truthy() {
			return nil
		}
		reconcileChatRowActions(local)
		return nil
	})
	doc.Call("addEventListener", "mouseout", leave)
	window.Call("addEventListener", "blur", away)
	doc.Call("addEventListener", "pointerdown", press, true)
	return func() {
		doc.Call("removeEventListener", "mouseout", leave)
		window.Call("removeEventListener", "blur", away)
		doc.Call("removeEventListener", "pointerdown", press, true)
		leave.Release()
		away.Release()
		press.Release()
	}
}
