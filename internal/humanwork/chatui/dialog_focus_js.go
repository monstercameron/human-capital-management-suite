//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var chatDialogTrigger js.Value
var chatDialogTriggerAction string
var chatDialogOpeningPending bool
var chatDialogKeyGuard js.Func
var chatDialogKeyGuardDocument js.Value

const chatDialogFocusSelector = "button:not([disabled]),a[href],input:not([disabled]):not([type='hidden']),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex='-1'])"

func rememberChatDialogTrigger(event ui.Event, action string) {
	rememberChatDialogTriggerJS(event.JSValue(), action)
}

func rememberChatDialogTriggerJS(event js.Value, action string) {
	if action != "open-create" && action != "open-browse" {
		return
	}
	target := event.Get("target")
	if !target.Truthy() {
		return
	}
	trigger := target.Call("closest", "button[data-action='"+action+"']")
	if trigger.Truthy() {
		chatDialogTrigger = trigger
		chatDialogTriggerAction = action
		chatDialogOpeningPending = true
		installChatDialogKeyGuard()
	}
}

func installChatDialogKeyGuard() {
	doc := js.Global().Get("document")
	if !doc.Truthy() || chatDialogKeyGuardDocument.Equal(doc) {
		return
	}
	removeChatDialogKeyGuard()
	chatDialogKeyGuard = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			guardChatDialogTab(args[0])
		}
		return nil
	})
	chatDialogKeyGuardDocument = doc
	doc.Call("addEventListener", "keydown", chatDialogKeyGuard, true)
}

func removeChatDialogKeyGuard() {
	if chatDialogKeyGuardDocument.Truthy() && chatDialogKeyGuard.Value.Truthy() {
		chatDialogKeyGuardDocument.Call("removeEventListener", "keydown", chatDialogKeyGuard, true)
		chatDialogKeyGuard.Release()
	}
	chatDialogKeyGuard = js.Func{}
	chatDialogKeyGuardDocument = js.Undefined()
}

func guardChatDialogTab(event js.Value) {
	if event.Get("key").String() != "Tab" {
		return
	}
	doc := js.Global().Get("document")
	dialog := doc.Call("querySelector", ".create-dialog, .browse-dialog")
	if dialog.Truthy() && dialog.Call("getClientRects").Get("length").Int() > 0 {
		if trapChatDialogFocusJS(event) {
			event.Call("preventDefault")
		}
		return
	}
	if chatDialogOpeningPending {
		event.Call("preventDefault")
	}
}

func focusChatDialog() {
	doc := js.Global().Get("document")
	attempts := 0
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		attempts++
		dialog := doc.Call("querySelector", ".create-dialog, .browse-dialog")
		if dialog.Truthy() && dialog.Call("getClientRects").Get("length").Int() > 0 {
			if !dialog.Call("contains", doc.Get("activeElement")).Bool() {
				target := dialog.Call("querySelector", "[autofocus]")
				if !target.Truthy() || target.Get("disabled").Truthy() {
					target = firstChatDialogFocusable(dialog)
				}
				if target.Truthy() {
					target.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
				}
			}
			chatDialogOpeningPending = false
			removeChatDialogKeyGuard()
			frame.Release()
			return nil
		}
		if attempts >= 60 || !js.Global().Get("requestAnimationFrame").Truthy() {
			frame.Release()
			return nil
		}
		js.Global().Call("requestAnimationFrame", frame)
		return nil
	})
	if js.Global().Get("requestAnimationFrame").Truthy() {
		js.Global().Call("requestAnimationFrame", frame)
		return
	}
	frame.Invoke()
}

func trapChatDialogFocus(event ui.Event) bool {
	return trapChatDialogFocusJS(event.JSValue())
}

func trapChatDialogFocusJS(event js.Value) bool {
	if event.Get("key").String() != "Tab" {
		return false
	}
	doc := js.Global().Get("document")
	dialog := doc.Call("querySelector", ".create-dialog, .browse-dialog")
	if !dialog.Truthy() || dialog.Call("getClientRects").Get("length").Int() == 0 {
		return false
	}
	focusable := chatDialogFocusables(dialog)
	if len(focusable) == 0 {
		return false
	}
	active := doc.Get("activeElement")
	first, last := focusable[0], focusable[len(focusable)-1]
	if event.Get("shiftKey").Bool() {
		if active.Equal(first) || !dialog.Call("contains", active).Bool() {
			last.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			return true
		}
		return false
	}
	if active.Equal(last) || !dialog.Call("contains", active).Bool() {
		first.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		return true
	}
	return false
}

func restoreChatDialogFocus() {
	doc := js.Global().Get("document")
	attempts := 0
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		attempts++
		dialog := doc.Call("querySelector", ".create-dialog, .browse-dialog")
		if dialog.Truthy() && dialog.Call("getClientRects").Get("length").Int() > 0 {
			if attempts < 60 && js.Global().Get("requestAnimationFrame").Truthy() {
				js.Global().Call("requestAnimationFrame", frame)
				return nil
			}
			frame.Release()
			return nil
		}
		trigger := chatDialogTrigger
		if !trigger.Truthy() || !trigger.Get("isConnected").Bool() || trigger.Call("getClientRects").Get("length").Int() == 0 {
			if chatDialogTriggerAction != "" {
				trigger = doc.Call("querySelector", "button[data-action='"+chatDialogTriggerAction+"']")
			}
		}
		if trigger.Truthy() && trigger.Get("isConnected").Bool() && trigger.Call("getClientRects").Get("length").Int() > 0 {
			trigger.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			chatDialogTrigger = js.Undefined()
			chatDialogTriggerAction = ""
			frame.Release()
			return nil
		}
		if attempts < 60 && js.Global().Get("requestAnimationFrame").Truthy() {
			js.Global().Call("requestAnimationFrame", frame)
			return nil
		}
		chatDialogTrigger = js.Undefined()
		chatDialogTriggerAction = ""
		frame.Release()
		return nil
	})
	if js.Global().Get("requestAnimationFrame").Truthy() {
		js.Global().Call("requestAnimationFrame", frame)
		return
	}
	frame.Invoke()
}

func firstChatDialogFocusable(dialog js.Value) js.Value {
	focusable := chatDialogFocusables(dialog)
	if len(focusable) == 0 {
		return js.Undefined()
	}
	return focusable[0]
}

func chatDialogFocusables(dialog js.Value) []js.Value {
	items := dialog.Call("querySelectorAll", chatDialogFocusSelector)
	out := make([]js.Value, 0, items.Get("length").Int())
	for i := 0; i < items.Get("length").Int(); i++ {
		item := items.Index(i)
		if item.Get("hidden").Truthy() || item.Get("ariaHidden").String() == "true" || item.Get("disabled").Truthy() {
			continue
		}
		if item.Call("getClientRects").Get("length").Int() == 0 {
			continue
		}
		closedDetails := item.Call("closest", "details:not([open])")
		if closedDetails.Truthy() && item.Get("tagName").String() != "SUMMARY" {
			continue
		}
		out = append(out, item)
	}
	return out
}
