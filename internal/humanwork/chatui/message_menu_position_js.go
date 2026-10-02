//go:build js && wasm

package chatui

import (
	"syscall/js"
)

func positionMessageMenu(menu js.Value) {
	parent := menu.Get("parentElement")
	if !parent.Truthy() {
		return
	}
	trigger := parent.Call("querySelector", ".message-action[data-action=menu]")
	positionChatLayer(menu, trigger, true)
}
