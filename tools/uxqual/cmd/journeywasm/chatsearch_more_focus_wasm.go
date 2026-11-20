//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatsearchFocusNewResults puts keyboard focus on the first result a page of
// "Show next results" added (#3899). The control that was activated is gone or
// has moved, so focus would otherwise fall to the document body. The page is
// drawn a moment after the model changes, so it looks for the seam a few times;
// when the page has no result control to focus it focuses the seam itself, which
// is where the announcement lives.
func chatsearchFocusNewResults() {
	attempts := 0
	var tick js.Func
	tick = js.FuncOf(func(js.Value, []js.Value) any {
		attempts++
		seam := js.Global().Get("document").Call("getElementById", chatui.Issue3899BoundaryID)
		if !seam.Truthy() {
			if attempts < 12 {
				js.Global().Call("setTimeout", tick, 40)
				return nil
			}
			tick.Release()
			return nil
		}
		target := seam.Get("nextElementSibling")
		if target.Truthy() {
			if control := target.Call("querySelector", ".chatsearch-open"); control.Truthy() {
				target = control
			}
		}
		if !target.Truthy() {
			target = seam
		}
		// Focus scrolls the control into view only as far as needed, so the rows
		// already read stay where they were.
		target.Call("focus", map[string]any{"preventScroll": false})
		target.Call("scrollIntoView", map[string]any{"block": "nearest"})
		tick.Release()
		return nil
	})
	js.Global().Call("setTimeout", tick, 0)
}
