//go:build js && wasm

package chatui

import "syscall/js"

func issue3744El(doc js.Value, tag, class, text string) js.Value {
	el := doc.Call("createElement", tag)
	if class != "" {
		el.Set("className", class)
	}
	if text != "" {
		el.Set("textContent", text)
	}
	return el
}

// openChatShortcutSheet shows the list of shortcuts in a native modal dialog:
// the browser keeps focus inside it, Esc closes it, and focus goes back to
// where it was.
func openChatShortcutSheet(locale string) {
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Call("getElementById", "chat-shortcuts-sheet").Truthy() {
		return
	}
	title, closeLabel, _ := issue3744SheetCopy(locale)
	dialog := issue3744El(doc, "dialog", "chat-shortcuts", "")
	dialog.Set("id", "chat-shortcuts-sheet")
	dialog.Call("setAttribute", "aria-labelledby", "chat-shortcuts-title")
	dialog.Call("setAttribute", "lang", []string{"en", "de", "ar"}[chatbug039LocaleIndex(locale)])
	if chatbug039LocaleIndex(locale) == 2 {
		dialog.Call("setAttribute", "dir", "rtl")
	}
	head := issue3744El(doc, "div", "chat-shortcuts-head", "")
	heading := issue3744El(doc, "h2", "", title)
	heading.Set("id", "chat-shortcuts-title")
	closeButton := issue3744El(doc, "button", "chat-shortcuts-close", "×")
	closeButton.Set("type", "button")
	closeButton.Call("setAttribute", "aria-label", closeLabel)
	head.Call("appendChild", heading)
	head.Call("appendChild", closeButton)
	list := issue3744El(doc, "ul", "chat-shortcuts-list", "")
	for _, row := range issue3744Shortcuts(locale, chatPlatform()) {
		item := issue3744El(doc, "li", "", "")
		item.Call("appendChild", issue3744El(doc, "span", "", row.Action))
		keys := issue3744El(doc, "span", "chat-shortcuts-keys", "")
		for _, key := range row.Keys {
			keys.Call("appendChild", issue3744El(doc, "kbd", "", key))
		}
		item.Call("appendChild", keys)
		list.Call("appendChild", item)
	}
	dialog.Call("appendChild", head)
	dialog.Call("appendChild", list)
	opener := doc.Get("activeElement")
	var closeFn, clickFn, doneFn js.Func
	release := func() {
		closeFn.Release()
		clickFn.Release()
		doneFn.Release()
	}
	closeFn = js.FuncOf(func(js.Value, []js.Value) any {
		if dialog.Get("open").Bool() {
			dialog.Call("close")
		}
		return nil
	})
	clickFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		// A press on the dimmed backdrop lands on the dialog element itself.
		if len(args) > 0 && args[0].Get("target").Equal(dialog) {
			dialog.Call("close")
		}
		return nil
	})
	doneFn = js.FuncOf(func(js.Value, []js.Value) any {
		dialog.Call("remove")
		if opener.Truthy() && opener.Get("isConnected").Bool() && opener.Get("focus").Type() == js.TypeFunction {
			opener.Call("focus")
		}
		release()
		return nil
	})
	closeButton.Call("addEventListener", "click", closeFn)
	dialog.Call("addEventListener", "click", clickFn)
	dialog.Call("addEventListener", "close", doneFn)
	doc.Get("body").Call("appendChild", dialog)
	if dialog.Get("showModal").Type() == js.TypeFunction {
		dialog.Call("showModal")
	} else {
		dialog.Call("setAttribute", "open", "")
	}
	closeButton.Call("focus")
}

func announceChatShortcut(text string) {
	doc := js.Global().Get("document")
	region := doc.Call("getElementById", "chat-shortcuts-status")
	if region.IsNull() {
		region = issue3744El(doc, "div", "chat-shortcuts-status", "")
		region.Set("id", "chat-shortcuts-status")
		region.Call("setAttribute", "role", "status")
		region.Call("setAttribute", "aria-live", "polite")
		doc.Get("body").Call("appendChild", region)
	}
	region.Set("textContent", text)
}

// chatKeyboardBlocked is true while a person types or a dialog, menu or panel
// is open: the keys then belong to that control.
func chatKeyboardBlocked(root, target js.Value) bool {
	if target.Truthy() && target.Get("closest").Type() == js.TypeFunction {
		if target.Call("closest", "input,textarea,select,[contenteditable='true'],[contenteditable='']").Truthy() {
			return true
		}
	}
	doc := js.Global().Get("document")
	return chatKeyboardLayerVisible(doc, "dialog[open],[role=dialog]:not([hidden]),[aria-modal=true]:not([hidden]),[role=menu]:not([hidden])") ||
		chatKeyboardLayerVisible(root, "[data-chat-layer]:not([hidden])")
}

// chatKeyboardLayerVisible is true when something the selector names is drawn.
// A closed popover, drawer or menu can carry role=dialog or role=menu without
// the hidden attribute (the browser hides it with display:none), and counting
// it would leave the question mark key dead on the page (#900005).
func chatKeyboardLayerVisible(scope js.Value, selector string) bool {
	nodes := scope.Call("querySelectorAll", selector)
	for i := 0; i < nodes.Length(); i++ {
		node := nodes.Index(i)
		if node.Get("checkVisibility").Type() == js.TypeFunction {
			if node.Call("checkVisibility").Bool() {
				return true
			}
			continue
		}
		if node.Get("getClientRects").Type() == js.TypeFunction && node.Call("getClientRects").Length() > 0 {
			return true
		}
	}
	return false
}

func chatKeyboardRows(root js.Value) (ids []string, unread []bool, current string, rows map[string]js.Value) {
	rows = map[string]js.Value{}
	nodes := root.Call("querySelectorAll", ".chat-rail-row[data-conversation-id]")
	for i := 0; i < nodes.Length(); i++ {
		row := nodes.Index(i)
		if row.Call("closest", "[hidden]").Truthy() {
			continue
		}
		id := row.Get("dataset").Get("conversationId").String()
		ids = append(ids, id)
		unread = append(unread, row.Call("querySelector", ".chat-badge").Truthy())
		rows[id] = row
		if row.Call("querySelector", "[aria-current='true']").Truthy() {
			current = id
		}
	}
	return ids, unread, current, rows
}

// bindChatKeyboardModel makes the documented keyboard model work from anywhere
// on the page (issue #3744). It stands down while a person types and while a
// dialog, menu or panel is open. The returned function removes it.
func bindChatKeyboardModel(model Model) func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil
	}
	listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		if event.Get("isComposing").Truthy() || event.Get("defaultPrevented").Bool() {
			return nil
		}
		action := issue3744Match(event.Get("key").String(), event.Get("code").String(), event.Get("altKey").Bool(), event.Get("shiftKey").Bool(), event.Get("ctrlKey").Bool(), event.Get("metaKey").Bool())
		if action == "" {
			return nil
		}
		root := chatLayerRoot()
		if !root.Truthy() || !root.Get("isConnected").Bool() || chatKeyboardBlocked(root, event.Get("target")) {
			return nil
		}
		switch action {
		case issue3744Sheet:
			if event.Get("repeat").Bool() {
				return nil
			}
			event.Call("preventDefault")
			openChatShortcutSheet(model.Locale)
		case issue3744Thread:
			target := event.Get("target")
			if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
				return nil
			}
			message := target.Call("closest", ".message")
			if !message.Truthy() {
				return nil
			}
			// The message's own Reply button is the authority when the hover
			// toolbar is drawn. The toolbar exists only while the row is
			// hovered or focused, so without it the same callback the button
			// calls is used.
			if button := message.Call("querySelector", "button[data-action='reply']"); button.Truthy() {
				if !button.Get("disabled").Bool() {
					event.Call("preventDefault")
					button.Call("click")
				}
				return nil
			}
			if id := message.Get("dataset").Get("messageId"); id.Truthy() && model.Callbacks.OpenThread != nil && !model.selected().Agent {
				event.Call("preventDefault")
				model.Callbacks.OpenThread(id.String())
			}
		case issue3744MarkRead:
			_, _, current, _ := chatKeyboardRows(root)
			if current == "" || model.Callbacks.MarkConversationRead == nil || event.Get("repeat").Bool() {
				return nil
			}
			event.Call("preventDefault")
			model.Callbacks.MarkConversationRead(current)
			_, _, status := issue3744SheetCopy(model.Locale)
			announceChatShortcut(status)
		default:
			ids, unread, current, rows := chatKeyboardRows(root)
			next := issue3744Step(ids, unread, current, action)
			if next == "" {
				return nil
			}
			event.Call("preventDefault")
			if next == current {
				return nil
			}
			if button := rows[next].Call("querySelector", "button[data-action='select']"); button.Truthy() && !button.Get("disabled").Bool() {
				button.Call("click")
				var frame js.Func
				frame = js.FuncOf(func(js.Value, []js.Value) any {
					defer frame.Release()
					if row := chatLayerRoot().Call("querySelector", ".chat-rail-row [aria-current='true']"); row.Truthy() {
						row.Call("scrollIntoView", map[string]any{"block": "nearest"})
					}
					return nil
				})
				js.Global().Call("requestAnimationFrame", frame)
			}
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", listener)
	return func() {
		doc.Call("removeEventListener", "keydown", listener)
		listener.Release()
	}
}
