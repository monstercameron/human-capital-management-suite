//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestChatDialogFocusEntersTrapsAndRestores(t *testing.T) {
	global := js.Global()
	oldDoc, oldFrame := global.Get("document"), global.Get("requestAnimationFrame")
	defer func() {
		global.Set("document", oldDoc)
		global.Set("requestAnimationFrame", oldFrame)
		chatDialogTrigger = js.Undefined()
		chatDialogTriggerAction = ""
		chatDialogOpeningPending = false
	}()

	var doc js.Value
	var callbacks []js.Func
	defer func() {
		for _, callback := range callbacks {
			callback.Release()
		}
	}()
	visible := js.ValueOf([]any{map[string]any{}})
	hidden := js.ValueOf([]any{})
	makeFocusable := func(name string) js.Value {
		var element js.Value
		focus := js.FuncOf(func(js.Value, []js.Value) any {
			doc.Set("activeElement", element)
			return nil
		})
		callbacks = append(callbacks, focus)
		closest := js.FuncOf(func(js.Value, []js.Value) any { return js.Null() })
		callbacks = append(callbacks, closest)
		rects := js.FuncOf(func(js.Value, []js.Value) any { return visible })
		callbacks = append(callbacks, rects)
		element = js.ValueOf(map[string]any{
			"name": name, "focus": focus, "closest": closest, "getClientRects": rects, "isConnected": true,
		})
		return element
	}

	first := makeFocusable("close")
	autofocus := makeFocusable("name")
	last := makeFocusable("cancel")
	opener := makeFocusable("new-conversation")
	currentOpener := opener
	triggerAvailable := true
	triggerAvailableAtFrame := 0
	frameCount := 0
	activeOutside := makeFocusable("search")
	doc = js.ValueOf(map[string]any{"activeElement": opener})
	open := true
	var dialog js.Value
	queryDialog := js.FuncOf(func(js.Value, []js.Value) any {
		if open {
			return dialog
		}
		return js.Null()
	})
	callbacks = append(callbacks, queryDialog)
	contains := js.FuncOf(func(_ js.Value, args []js.Value) any {
		active := args[0]
		return js.ValueOf(active.Equal(first) || active.Equal(autofocus) || active.Equal(last))
	})
	callbacks = append(callbacks, contains)
	getRects := js.FuncOf(func(js.Value, []js.Value) any {
		if open {
			return visible
		}
		return hidden
	})
	callbacks = append(callbacks, getRects)
	autofocusQuery := js.FuncOf(func(js.Value, []js.Value) any { return autofocus })
	callbacks = append(callbacks, autofocusQuery)
	focusables := js.FuncOf(func(js.Value, []js.Value) any {
		return js.ValueOf([]any{first, autofocus, last})
	})
	callbacks = append(callbacks, focusables)
	dialog = js.ValueOf(map[string]any{
		"contains": contains, "getClientRects": getRects,
		"querySelector": autofocusQuery, "querySelectorAll": focusables,
	})
	docQuery := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == ".create-dialog, .browse-dialog" {
			return dialog
		}
		if args[0].String() == "button[data-action='open-create']" {
			if !triggerAvailable {
				return js.Null()
			}
			return currentOpener
		}
		return js.Null()
	})
	callbacks = append(callbacks, docQuery)
	doc.Set("querySelector", docQuery)
	global.Set("document", doc)
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any {
		frameCount++
		if triggerAvailableAtFrame > 0 && frameCount >= triggerAvailableAtFrame {
			triggerAvailable = true
			triggerAvailableAtFrame = 0
		}
		args[0].Invoke()
		return nil
	})
	callbacks = append(callbacks, frame)
	global.Set("requestAnimationFrame", frame)

	// A Tab pressed after the opener click but before the dialog render must not
	// move focus into the background. Once mounted, the same capture guard moves
	// focus into the dialog.
	open = false
	chatDialogOpeningPending = true
	doc.Set("activeElement", opener)
	prevented := false
	preventDefault := js.FuncOf(func(js.Value, []js.Value) any {
		prevented = true
		return nil
	})
	callbacks = append(callbacks, preventDefault)
	tabEvent := js.ValueOf(map[string]any{"key": "Tab", "shiftKey": false, "preventDefault": preventDefault})
	guardChatDialogTab(tabEvent)
	if !prevented || !doc.Get("activeElement").Equal(opener) {
		t.Fatal("Tab during dialog mounting escaped focus into the background")
	}
	open = true
	prevented = false
	doc.Set("activeElement", activeOutside)
	guardChatDialogTab(tabEvent)
	if !prevented || !doc.Get("activeElement").Equal(first) || !chatDialogOpeningPending {
		t.Fatal("Tab after dialog mount did not enter the dialog while keeping the opening guard armed")
	}
	focusChatDialog()
	if chatDialogOpeningPending {
		t.Fatal("focus completion did not clear the opening guard")
	}

	chatDialogTrigger = opener
	chatDialogTriggerAction = "open-create"
	chatDialogOpeningPending = false
	doc.Set("activeElement", opener)
	focusChatDialog()
	if !doc.Get("activeElement").Equal(autofocus) {
		t.Fatal("opening create dialog did not move focus to its autofocus field")
	}

	doc.Set("activeElement", last)
	if !trapChatDialogFocusJS(js.ValueOf(map[string]any{"key": "Tab", "shiftKey": false})) || !doc.Get("activeElement").Equal(first) {
		t.Fatal("Tab from the last control did not wrap to the first control")
	}
	doc.Set("activeElement", first)
	if !trapChatDialogFocusJS(js.ValueOf(map[string]any{"key": "Tab", "shiftKey": true})) || !doc.Get("activeElement").Equal(last) {
		t.Fatal("Shift+Tab from the first control did not wrap to the last control")
	}
	doc.Set("activeElement", activeOutside)
	if !trapChatDialogFocusJS(js.ValueOf(map[string]any{"key": "Tab", "shiftKey": false})) || !doc.Get("activeElement").Equal(first) {
		t.Fatal("Tab from outside the open dialog was not moved inside")
	}
	doc.Set("activeElement", autofocus)
	if trapChatDialogFocusJS(js.ValueOf(map[string]any{"key": "Tab", "shiftKey": false})) {
		t.Fatal("Tab from a middle dialog control was unexpectedly intercepted")
	}

	open = false
	opener.Set("isConnected", false) // A state render can replace the opener node.
	currentOpener = makeFocusable("new-conversation-after-render")
	triggerAvailable = false
	triggerAvailableAtFrame = frameCount + 2
	restoreChatDialogFocus()
	if !doc.Get("activeElement").Equal(currentOpener) {
		t.Fatal("closing a mounted but hidden dialog did not restore focus to the rendered opener")
	}

	open = true
	doc.Set("activeElement", currentOpener)
	chatDialogTrigger = currentOpener
	chatDialogTriggerAction = "open-create"
	focusChatDialog()
	if !doc.Get("activeElement").Equal(autofocus) {
		t.Fatal("reopening the dialog did not focus its autofocus field")
	}
	doc.Set("activeElement", last)
	if !trapChatDialogFocusJS(js.ValueOf(map[string]any{"key": "Tab", "shiftKey": false})) || !doc.Get("activeElement").Equal(first) {
		t.Fatal("Tab after reopening did not stay within the dialog")
	}
	open = false // The closed dialog remains mounted, as it can during UI reconciliation.
	currentOpener.Set("isConnected", false)
	currentOpener = makeFocusable("new-conversation-after-second-render")
	restoreChatDialogFocus()
	if !doc.Get("activeElement").Equal(currentOpener) {
		t.Fatal("Escape after reopening and tabbing did not restore focus to the rendered opener")
	}
}
