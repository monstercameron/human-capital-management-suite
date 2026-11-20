//go:build js && wasm

package chatui

import "syscall/js"

// focusMessageRowForMenu is where focus goes when a message's menu closes and
// its More button is not on the page (issue #3949). The button is drawn only
// while its row is active, and a focused row is active, so the row takes focus
// first and focus moves on to More as soon as the row has drawn it. Nothing
// waits on a timer or a frame: the row's own change is what is observed. Focus
// that the person has moved in the meantime stays where they put it.
func focusMessageRowForMenu(id string) {
	document := js.Global().Get("document")
	if !document.Truthy() || document.Get("querySelector").Type() != js.TypeFunction {
		return
	}
	scope, post := MenuRowScope(id)
	if css := js.Global().Get("CSS"); css.Truthy() && css.Get("escape").Type() == js.TypeFunction {
		post = css.Call("escape", post).String()
	}
	row := document.Call("querySelector", scope+` [data-message-id="`+post+`"]`)
	more := js.Null()
	if row.Truthy() {
		more = row.Call("querySelector", `.message-action[data-action=menu]`)
	}
	switch MenuReturnTarget(false, more.Truthy(), row.Truthy()) {
	case "button":
		more.Call("focus", map[string]any{"preventScroll": true})
		return
	case "row":
	default:
		return
	}
	row.Call("focus", map[string]any{"preventScroll": true})
	observerClass := js.Global().Get("MutationObserver")
	if observerClass.Type() != js.TypeFunction {
		return
	}
	var observer js.Value
	var drawn js.Func
	changes := 0
	drawn = js.FuncOf(func(js.Value, []js.Value) any {
		changes++
		button := row.Call("querySelector", `.message-action[data-action=menu]`)
		active := document.Get("activeElement")
		held := active.Truthy() && (active.Equal(row) || active.Equal(document.Get("body")))
		if button.Truthy() && held {
			button.Call("focus", map[string]any{"preventScroll": true})
		}
		// A few changes are enough to see the row draw its bar; watching longer
		// would pull focus to More when the person only hovers the row later.
		if button.Truthy() || !held || changes >= 10 {
			observer.Call("disconnect")
			drawn.Release()
		}
		return nil
	})
	observer = observerClass.New(drawn)
	observer.Call("observe", row, map[string]any{"childList": true, "subtree": true})
}
