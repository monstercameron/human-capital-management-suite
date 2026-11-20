//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

// TestIssue3885_BrowseCloseReturnsFocusToMenuTrigger: Browse channels is opened
// from a menu item that disappears with its menu, so closing the browser has to
// put focus on the menu's own trigger, not leave it on the page body.
func TestIssue3885_BrowseCloseReturnsFocusToMenuTrigger(t *testing.T) {
	global := js.Global()
	oldDoc, oldFrame := global.Get("document"), global.Get("requestAnimationFrame")
	var callbacks []js.Func
	defer func() {
		global.Set("document", oldDoc)
		global.Set("requestAnimationFrame", oldFrame)
		removeChatDialogKeyGuard()
		chatDialogTrigger = js.Undefined()
		chatDialogTriggerAction = ""
		chatDialogOpeningPending = false
		for _, callback := range callbacks {
			callback.Release()
		}
	}()
	fn := func(f func(args []js.Value) any) js.Func {
		callback := js.FuncOf(func(_ js.Value, args []js.Value) any { return f(args) })
		callbacks = append(callbacks, callback)
		return callback
	}
	body := js.ValueOf(map[string]any{"tagName": "BODY"})
	doc := js.ValueOf(map[string]any{"activeElement": body, "body": body})
	rects := fn(func([]js.Value) any { return js.ValueOf([]any{map[string]any{}}) })
	var toggle js.Value
	toggle = js.ValueOf(map[string]any{"isConnected": true, "getClientRects": rects,
		"focus": fn(func([]js.Value) any { doc.Set("activeElement", toggle); return nil })})
	// The menu item the person pressed lives in the menu's layer.
	menu := js.ValueOf(map[string]any{"className": "section-menu"})
	item := js.ValueOf(map[string]any{"isConnected": true, "getClientRects": rects,
		"closest": fn(func(args []js.Value) any {
			if args[0].String() == "[data-chat-disclosure]" {
				return menu
			}
			return js.Null()
		})})
	target := js.ValueOf(map[string]any{"closest": fn(func([]js.Value) any { return item })})
	doc.Set("addEventListener", fn(func([]js.Value) any { return nil }))
	doc.Set("removeEventListener", fn(func([]js.Value) any { return nil }))
	doc.Set("querySelector", fn(func(args []js.Value) any {
		if args[0].String() == ".section-menu [data-chat-disclosure-toggle]" {
			return toggle
		}
		return js.Null() // The dialog is closed and the menu item is gone.
	}))
	global.Set("document", doc)
	global.Set("requestAnimationFrame", fn(func(args []js.Value) any { args[0].Invoke(); return nil }))

	rememberChatDialogTriggerJS(js.ValueOf(map[string]any{"target": target}), "open-browse")
	item.Set("isConnected", false) // The menu closed when the item was pressed.
	restoreChatDialogFocus()
	if !doc.Get("activeElement").Equal(toggle) {
		t.Fatal("closing Browse channels left focus off the Channels menu trigger")
	}
}
