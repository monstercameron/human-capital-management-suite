package chatui

import (
	"strconv"
	"syscall/js"
)

// PositionSavedMessagesPanel docks the Saved panel under the application header,
// where the thread and details panels start. The panel is a fixed layer, so the
// header's height is read from where the conversation layout begins and handed
// to the stylesheet as one custom property.
func PositionSavedMessagesPanel(layer js.Value) {
	if !layer.Truthy() {
		return
	}
	top := 0.0
	if layout := js.Global().Get("document").Call("querySelector", ".chat-layout"); layout.Truthy() {
		top = layout.Call("getBoundingClientRect").Get("top").Float()
	}
	if top < 0 {
		top = 0
	}
	if top > 0 {
		layer.Get("style").Call("setProperty", "--chatsave-top", strconv.FormatFloat(top, 'f', 2, 64)+"px")
	}
}
