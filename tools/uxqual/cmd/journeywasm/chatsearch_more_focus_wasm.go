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
		chatsearchKeepNewResultFocus()
		return nil
	})
	js.Global().Call("setTimeout", tick, 0)
}

// chatsearchKeepNewResultFocus checks a few times that the focus just placed on
// the first new result survived the page's follow-up redraws (the recent
// searches refresh and avatar loads redraw the list a moment later and replace
// the focused control, which drops focus to the body). Each check looks the seam
// up again, so it follows the redrawn control.
func chatsearchKeepNewResultFocus() {
	document := js.Global().Get("document")
	for _, delay := range issue3899RefocusDelays {
		var check js.Func
		check = js.FuncOf(func(js.Value, []js.Value) any {
			defer check.Release()
			active := document.Get("activeElement")
			isBody := !active.Truthy() || active.Equal(document.Get("body"))
			seam := document.Call("getElementById", chatui.Issue3899BoundaryID)
			var target js.Value
			if seam.Truthy() {
				target = seam.Get("nextElementSibling")
				if target.Truthy() {
					if control := target.Call("querySelector", ".chatsearch-open"); control.Truthy() {
						target = control
					}
				} else {
					target = seam
				}
			}
			if issue3899ShouldRefocus(isBody, target.Truthy()) {
				target.Call("focus", map[string]any{"preventScroll": true})
			}
			return nil
		})
		js.Global().Call("setTimeout", check, delay)
	}
}
