//go:build js && wasm

package chatui

import "syscall/js"

// chatux007UnreadRows are the list's unread conversations other than the open
// one and muted ones, in list order, with where each is on screen.
func chatux007UnreadRows(scroll js.Value) ([]js.Value, []chatux007Span) {
	nodes := scroll.Call("querySelectorAll", ".chat-row.unread:not(.selected):not(.muted)")
	rows := make([]js.Value, 0, nodes.Get("length").Int())
	spans := make([]chatux007Span, 0, cap(rows))
	for i := 0; i < nodes.Get("length").Int(); i++ {
		row := nodes.Index(i)
		box := row.Call("getBoundingClientRect")
		rows = append(rows, row)
		spans = append(spans, chatux007Span{Top: box.Get("top").Float(), Bottom: box.Get("bottom").Float()})
	}
	return rows, spans
}

// chatux007SyncJump opens each Jump to unread control while an unread
// conversation is out of view on its side of the list, and closes it otherwise.
// It runs from the page's layer sync, which hears scrolls, resizes and every
// change to the tree, and it writes only when the answer changed: a write that
// changed nothing would still wake the observer that called it.
func chatux007SyncJump() {
	root := chatLayerRoot()
	if !root.Truthy() {
		return
	}
	scroll := root.Call("querySelector", ".rail-scroll")
	if !scroll.Truthy() {
		return
	}
	up := scroll.Call("querySelector", ".chatux007-jump[data-jump=up]")
	down := scroll.Call("querySelector", ".chatux007-jump[data-jump=down]")
	if !up.Truthy() && !down.Truthy() {
		return
	}
	view := scroll.Call("getBoundingClientRect")
	_, spans := chatux007UnreadRows(scroll)
	above, below := false, false
	if view.Get("height").Float() > 0 {
		above, below = chatux007OutOfView(view.Get("top").Float(), view.Get("bottom").Float(), spans)
	}
	for _, control := range []struct {
		node js.Value
		show bool
	}{{up, above}, {down, below}} {
		if control.node.Truthy() && control.node.Get("hidden").Bool() == control.show {
			control.node.Set("hidden", !control.show)
		}
	}
}

// chatux007JumpTo scrolls the nearest unread conversation out of view in
// direction into view. It scrolls at once and waits for no frame or timer.
func chatux007JumpTo(direction string) {
	root := chatLayerRoot()
	if !root.Truthy() {
		return
	}
	scroll := root.Call("querySelector", ".rail-scroll")
	if !scroll.Truthy() {
		return
	}
	view := scroll.Call("getBoundingClientRect")
	rows, spans := chatux007UnreadRows(scroll)
	if target := chatux007Target(direction, view.Get("top").Float(), view.Get("bottom").Float(), spans); target >= 0 {
		rows[target].Call("scrollIntoView", map[string]any{"block": "nearest"})
	}
}
