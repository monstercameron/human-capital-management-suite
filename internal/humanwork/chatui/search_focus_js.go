//go:build js && wasm

package chatui

import (
	"syscall/js"
	"time"
)

// FocusSearchMessage puts a search hit in view and gives keyboard focus to its
// article after the next render has attached the targeted timeline page.
func FocusSearchMessage(id string) {
	if id == "" {
		return
	}
	global := js.Global()
	request := global.Get("requestAnimationFrame")
	if !request.Truthy() {
		focusSearchMessageNow(id)
		return
	}
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		focusSearchMessageNow(id)
		frame.Release()
		return nil
	})
	func() {
		defer func() {
			if recover() != nil {
				frame.Release()
				focusSearchMessageNow(id)
			}
		}()
		global.Call("requestAnimationFrame", frame)
	}()
}

func focusSearchMessageNow(id string) {
	defer func() { _ = recover() }()
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	rows := doc.Call("querySelectorAll", "[data-message-id]")
	for i := 0; i < rows.Length(); i++ {
		row := rows.Index(i)
		if row.Get("dataset").Get("messageId").String() != id {
			continue
		}
		if row.Get("closest").Type() == js.TypeFunction {
			list := row.Call("closest", "["+listAnchorAttr+"]")
			if list.Truthy() {
				list.Set("__chatScrollAwayIntent", true)
				list.Set("__chatNearBottom", false)
				list.Set("__chatScrollTop", list.Get("scrollTop"))
				markAway(list, true)
			}
		}
		row.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		row.Call("scrollIntoView", js.ValueOf(map[string]any{"block": "center"}))
		row.Get("classList").Call("add", "search-target")
		var remove js.Func
		remove = js.FuncOf(func(js.Value, []js.Value) any {
			if row.Get("isConnected").Truthy() {
				row.Get("classList").Call("remove", "search-target")
			}
			remove.Release()
			return nil
		})
		js.Global().Call("setTimeout", remove, int(time.Second*3/time.Millisecond))
		return
	}
}
