//go:build js && wasm

package chatui

import (
	"math"
	"sync"
	"syscall/js"
)

// CHATSIDE-001: on touch a long press on a sidebar row opens the row menu (the
// drag is for a mouse or a pen). The press is cancelled by a move, a lift or a
// cancelled pointer, so scrolling the list never opens a menu, and the lift that
// ends a long press does not also open the conversation.

var chatside001Open func(string)
var chatside001TouchOnce sync.Once

func chatside001InstallTouch(open func(string)) {
	chatside001Open = open
	chatside001TouchOnce.Do(func() {
		doc := js.Global().Get("document")
		if !doc.Truthy() {
			return
		}
		var timer, row js.Value
		var fire js.Func
		var x, y float64
		fired := false
		cancel := func() {
			if timer.Truthy() {
				js.Global().Call("clearTimeout", timer)
				fire.Release()
				timer = js.Undefined()
			}
		}
		down := js.FuncOf(func(_ js.Value, args []js.Value) any {
			cancel()
			fired = false
			if len(args) == 0 {
				return nil
			}
			e := args[0]
			if _, long := chatside001Gesture(e.Get("pointerType").String()); !long || chatside001Open == nil {
				return nil
			}
			target := e.Get("target")
			if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction || target.Call("closest", ".rail-row-more").Truthy() {
				return nil
			}
			row = target.Call("closest", ".chat-rail-row[data-conversation-id]")
			if !row.Truthy() {
				return nil
			}
			x, y = e.Get("clientX").Float(), e.Get("clientY").Float()
			id := row.Get("dataset").Get("conversationId").String()
			fire = js.FuncOf(func(js.Value, []js.Value) any {
				timer = js.Undefined()
				defer fire.Release()
				if row.Get("isConnected").Truthy() && chatside001Open != nil {
					fired = true
					chatside001Open(id)
				}
				return nil
			})
			timer = js.Global().Call("setTimeout", fire, chatside001LongPressMs)
			return nil
		})
		move := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 && (math.Abs(args[0].Get("clientX").Float()-x) > chatside001LongPressSlop || math.Abs(args[0].Get("clientY").Float()-y) > chatside001LongPressSlop) {
				cancel()
			}
			return nil
		})
		end := js.FuncOf(func(js.Value, []js.Value) any {
			cancel()
			if fired {
				// The lift that ends a long press must not also press the row.
				chatside001State.suppressClick = true
				js.Global().Call("setTimeout", js.FuncOf(func(js.Value, []js.Value) any {
					chatside001State.suppressClick = false
					return nil
				}), 0)
				fired = false
			}
			return nil
		})
		// A touch browser may also raise its own context menu on a long press; on a
		// row the person's menu replaces it.
		context := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 && (fired || timer.Truthy()) {
				args[0].Call("preventDefault")
			}
			return nil
		})
		doc.Call("addEventListener", "pointerdown", down)
		doc.Call("addEventListener", "pointermove", move)
		doc.Call("addEventListener", "pointerup", end)
		doc.Call("addEventListener", "pointercancel", end)
		doc.Call("addEventListener", "contextmenu", context)
	})
}
