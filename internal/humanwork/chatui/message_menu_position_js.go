//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"
)

func positionMessageMenu(menu js.Value) {
	parent := menu.Get("parentElement")
	style := menu.Get("style")
	if !parent.Truthy() || !style.Truthy() || parent.Get("getBoundingClientRect").Type() != js.TypeFunction {
		return
	}
	trigger := parent.Call("querySelector", ".message-action[data-action=menu]")
	if !trigger.Truthy() || trigger.Get("getBoundingClientRect").Type() != js.TypeFunction {
		return
	}
	bound := parent.Call("closest", ".message-list,.thread-scroll")
	if !bound.Truthy() {
		return
	}
	parentRect := parent.Call("getBoundingClientRect")
	triggerRect := trigger.Call("getBoundingClientRect")
	boundRect := bound.Call("getBoundingClientRect")
	menuHeight := menu.Get("scrollHeight").Float()
	viewportHeight := js.Global().Get("innerHeight").Float()
	boundTop, boundBottom := boundRect.Get("top").Float(), boundRect.Get("bottom").Float()
	if boundTop < 8 {
		boundTop = 8
	}
	if boundBottom > viewportHeight-8 {
		boundBottom = viewportHeight - 8
	}
	top, maxHeight := menuTop(triggerRect.Get("top").Float(), triggerRect.Get("bottom").Float(), menuHeight, boundTop, boundBottom)
	style.Call("setProperty", "top", strconv.FormatFloat(top-parentRect.Get("top").Float(), 'f', 2, 64)+"px")
	style.Call("setProperty", "max-height", strconv.FormatFloat(maxHeight, 'f', 2, 64)+"px")
	style.Call("setProperty", "overflow-y", "auto")
}
