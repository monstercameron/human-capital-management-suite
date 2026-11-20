//go:build js && wasm

package chatui

import "syscall/js"

// dateJumpFocusZone says where focus is: "field" in the date field, "bar"
// elsewhere in the bar, "" outside it.
func dateJumpFocusZone() string {
	active := js.Global().Get("document").Get("activeElement")
	if !active.Truthy() || active.Get("closest").Type() != js.TypeFunction {
		return ""
	}
	if active.Get("id").String() == "chat-date-jump" {
		return "field"
	}
	if active.Call("closest", "#chat-date-jump-bar").Truthy() {
		return "bar"
	}
	return ""
}

func dateJumpFrame(focus func()) {
	raf := js.Global().Get("requestAnimationFrame")
	if raf.Type() != js.TypeFunction {
		focus()
		return
	}
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		defer frame.Release()
		focus()
		return nil
	})
	raf.Invoke(frame)
}

// focusDateJumpField puts the caret in the date field once it is drawn.
func focusDateJumpField() {
	dateJumpFrame(func() {
		if field := js.Global().Get("document").Call("getElementById", "chat-date-jump"); field.Truthy() {
			field.Call("focus")
		}
	})
}

// focusDateJumpTrigger returns focus to the header button that opened the bar.
func focusDateJumpTrigger() {
	dateJumpFrame(func() {
		if button := js.Global().Get("document").Call("querySelector", "[data-action=date-jump-toggle]"); button.Truthy() {
			button.Call("focus", map[string]any{"preventScroll": true})
		}
	})
}

// VisibleMessageID is the id of the first message in view in the timeline, or ""
// when none is drawn.
func VisibleMessageID() string {
	doc := js.Global().Get("document")
	list := doc.Call("querySelector", "["+listAnchorAttr+"]")
	if !list.Truthy() {
		return ""
	}
	top := list.Call("getBoundingClientRect").Get("top").Float()
	rows := list.Call("querySelectorAll", "[data-message-id]")
	for i := 0; i < rows.Get("length").Int(); i++ {
		row := rows.Index(i)
		if row.Call("getBoundingClientRect").Get("bottom").Float() > top+1 {
			return row.Get("dataset").Get("messageId").String()
		}
	}
	return ""
}
