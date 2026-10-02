//go:build js && wasm

package chatui

import "syscall/js"

var chatPlatformOnce struct {
	done  bool
	value string
}

// chatPlatform is the platform string, read once: navigator.userAgentData's
// platform where the browser has it, navigator.platform otherwise. It decides
// whether the search key is written Ctrl+K or Cmd+K and which modifier the
// shortcut listens for; no event takes part in either.
func chatPlatform() string {
	if chatPlatformOnce.done {
		return chatPlatformOnce.value
	}
	chatPlatformOnce.done = true
	nav := js.Global().Get("navigator")
	if !nav.Truthy() {
		return ""
	}
	if data := nav.Get("userAgentData"); data.Truthy() {
		if p := data.Get("platform"); p.Type() == js.TypeString && p.String() != "" {
			chatPlatformOnce.value = p.String()
			return chatPlatformOnce.value
		}
	}
	if p := nav.Get("platform"); p.Type() == js.TypeString {
		chatPlatformOnce.value = p.String()
	}
	return chatPlatformOnce.value
}

// bindChatSearchShortcut makes Ctrl+K (Cmd+K on Apple systems) put the caret in
// Chat's search box from anywhere on the page, except while a text field inside
// a dialog has focus: there the key belongs to the dialog. The listener is on
// the document so it works whichever element holds focus, and it stands down
// when the workspace is no longer on the page. The returned function removes it.
func bindChatSearchShortcut(model Model) func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil
	}
	apple := chatux010IsApple(chatPlatform())
	listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		if event.Get("isComposing").Truthy() || event.Get("defaultPrevented").Bool() {
			return nil
		}
		if !chatux010ShortcutMatches(apple, event.Get("key").String(), event.Get("ctrlKey").Bool(), event.Get("metaKey").Bool(), event.Get("altKey").Bool(), event.Get("shiftKey").Bool()) {
			return nil
		}
		root := chatLayerRoot()
		if !root.Truthy() || !root.Get("isConnected").Bool() {
			return nil
		}
		if target := event.Get("target"); target.Truthy() && target.Get("closest").Type() == js.TypeFunction {
			if target.Call("closest", "dialog,[role=dialog],[aria-modal=true]").Truthy() && target.Call("closest", "input,textarea,select,[contenteditable='true'],[contenteditable='']").Truthy() {
				return nil
			}
		}
		event.Call("preventDefault")
		focusChatSearchShortcut(model)
		return nil
	})
	doc.Call("addEventListener", "keydown", listener, true)
	return func() {
		doc.Call("removeEventListener", "keydown", listener, true)
		listener.Release()
	}
}

// focusChatSearchShortcut opens the conversation drawer first on a phone, where
// the box lives inside it, then focuses the box. The drawer's state is read from
// the page, not from the model the listener was bound with, which may be older.
func focusChatSearchShortcut(model Model) {
	if root := chatLayerRoot(); root.Truthy() && mobileRailActive() && model.Callbacks.ToggleSidebar != nil && root.Get("dataset").Get("sidebarOpen").String() != "true" {
		model.Callbacks.ToggleSidebar(true)
	}
	focusNow := func() {
		if field := js.Global().Get("document").Call("getElementById", "chat-search"); field.Truthy() {
			field.Call("focus")
			if field.Get("select").Type() == js.TypeFunction {
				field.Call("select")
			}
		}
	}
	focusNow()
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		defer frame.Release()
		focusNow()
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}
