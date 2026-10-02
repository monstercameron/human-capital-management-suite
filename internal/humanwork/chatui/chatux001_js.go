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
	if chatux001Pending == "crewmap" {
		selector = ".chat-details .chatmap-crew-section"
	}
	if chatux001Pending == "restore" {
		chatux001SettleRestore()
		return
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

// chatux001SettleRestore opens the Status row of Manage channel and puts the
// keyboard in its choice. The group itself is opened by the click handler; the
// row is a DOM-managed disclosure, so it is opened here once it is on the page.
func chatux001SettleRestore() {
	row := js.Global().Get("document").Call("querySelector", ".chat-details .manage-status")
	if !row.Truthy() {
		chatux001Budget--
		if chatux001Budget <= 0 {
			chatux001Pending = ""
		}
		return
	}
	chatux001Pending = ""
	summary := row.Call("querySelector", "[data-chat-disclosure-toggle]")
	if summary.Truthy() && summary.Call("getAttribute", "aria-expanded").String() != "true" {
		summary.Call("click")
	}
	row.Call("scrollIntoView", map[string]any{"block": "center"})
	if choice := js.Global().Get("document").Call("getElementById", "chatstate-choice"); choice.Truthy() {
		choice.Call("focus", map[string]any{"preventScroll": true})
	}
}
