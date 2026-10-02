//go:build js && wasm

package chatui

import (
	"math"
	"syscall/js"
)

func pinAgentChatAfterSend(target string) {
	if target == "chat-composer" {
		BeginScrollToNewest()
		return
	}
	doc := js.Global().Get("document")
	if doc.Truthy() && doc.Get("querySelector").Type() == js.TypeFunction {
		if list := doc.Call("querySelector", ".thread-scroll"); list.Truthy() {
			scrollToEnd(list)
		}
	}
}

func focusAgentChatComposer() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		defer frame.Release()
		if input := doc.Call("getElementById", "chat-composer"); input.Truthy() {
			input.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}

func bindChatMessageLongPress(model Model) func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() || model.Callbacks.OpenMenu == nil {
		return nil
	}
	var timer js.Value
	var fire js.Func
	var x, y float64
	cancel := func() {
		if timer.Truthy() {
			js.Global().Call("clearTimeout", timer)
			fire.Release()
			timer = js.Undefined()
		}
	}
	messageAt := func(event js.Value) js.Value {
		target := event.Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction || target.Call("closest", "button,a,input,textarea,summary").Truthy() {
			return js.Null()
		}
		row := target.Call("closest", ".message[data-message-id],.thread-message[data-message-id]")
		if !row.Truthy() {
			return row
		}
		workspace := row.Call("closest", ".chat-workspace")
		if !workspace.Truthy() || workspace.Get("dataset").Get("selectedId").String() != model.SelectedID {
			return js.Null()
		}
		return row
	}
	down := js.FuncOf(func(_ js.Value, args []js.Value) any {
		cancel()
		if len(args) == 0 || args[0].Get("pointerType").String() == "mouse" {
			return nil
		}
		event := args[0]
		row := messageAt(event)
		if !row.Truthy() {
			return nil
		}
		x, y = event.Get("clientX").Float(), event.Get("clientY").Float()
		id := row.Get("dataset").Get("messageId").String()
		fire = js.FuncOf(func(js.Value, []js.Value) any {
			timer = js.Undefined()
			defer fire.Release()
			if row.Get("isConnected").Truthy() {
				model.Callbacks.OpenMenu(id)
				focusMessageMenu(id, true)
			}
			return nil
		})
		timer = js.Global().Call("setTimeout", fire, 550)
		return nil
	})
	move := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && (math.Abs(args[0].Get("clientX").Float()-x) > 8 || math.Abs(args[0].Get("clientY").Float()-y) > 8) {
			cancel()
		}
		return nil
	})
	end := js.FuncOf(func(js.Value, []js.Value) any { cancel(); return nil })
	context := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			if row := messageAt(args[0]); row.Truthy() {
				args[0].Call("preventDefault")
				cancel()
				model.Callbacks.OpenMenu(row.Get("dataset").Get("messageId").String())
			}
		}
		return nil
	})
	for name, handler := range map[string]js.Func{"pointerdown": down, "pointermove": move, "pointerup": end, "pointercancel": end, "contextmenu": context} {
		doc.Call("addEventListener", name, handler)
	}
	return func() {
		cancel()
		for name, handler := range map[string]js.Func{"pointerdown": down, "pointermove": move, "pointerup": end, "pointercancel": end, "contextmenu": context} {
			doc.Call("removeEventListener", name, handler)
		}
		down.Release()
		move.Release()
		end.Release()
		context.Release()
	}
}
