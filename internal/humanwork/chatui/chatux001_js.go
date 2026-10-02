//go:build js && wasm

package chatui

import "syscall/js"

// chatux001Pending is the details section the header asked to be shown at, and
// chatux001Budget how many more renders may look for it. The details pane is
// not on the page until the render after the click commits, so the request is
// kept and retried after each commit (see rebindChatRootListeners) instead of
// waiting on a timer or an animation frame.
var (
	chatux001Pending string
	chatux001Budget  int
)

// chatux001ScrollDetails scrolls the details pane to its pinned list or member
// list and puts the keyboard there. When the pane is not drawn yet the request
// waits for the render that draws it.
func chatux001ScrollDetails(section string) {
	chatux001Pending, chatux001Budget = section, 12
	chatux001Settle()
}

// chatux001Settle is called after every commit. It does nothing unless a section
// is waiting.
func chatux001Settle() {
	if chatux001Pending == "" {
		return
	}
	selector := ".chat-details .member-list"
	if chatux001Pending == "pinned" {
		selector = ".chat-details .pinned-list"
	}
	list := js.Global().Get("document").Call("querySelector", selector)
	if !list.Truthy() {
		chatux001Budget--
		if chatux001Budget <= 0 {
			chatux001Pending = ""
		}
		return
	}
	chatux001Pending = ""
	section := list.Call("closest", ".details-section")
	if !section.Truthy() {
		section = list
	}
	section.Call("scrollIntoView", map[string]any{"block": "start"})
	if heading := section.Call("querySelector", "h3"); heading.Truthy() {
		heading.Call("setAttribute", "tabindex", "-1")
		heading.Call("focus", map[string]any{"preventScroll": true})
	}
}
