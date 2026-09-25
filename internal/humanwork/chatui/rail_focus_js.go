//go:build js && wasm

package chatui

import "syscall/js"

// revealSelectedRailRow keeps the selected conversation visible in the rail
// on mount and whenever the selection changes (C-9): on a shorter viewport
// the Quiet hours footer sits right below the scroller and can cover the
// last row or two without this.
//
// Round 3 C-6: scrollIntoView({block:"nearest"}) parked the row on the
// scroller's bottom edge, slicing the next row under the footer and
// scrolling the section header away, and it also scrolled any scrollable
// ancestor. The rail now leaves a row that is already comfortably in view
// alone and otherwise scrolls the rail scroller's own scrollTop by the
// smallest amount that brings it inside the clearances.
func revealSelectedRailRow(id string) {
	if id == "" || js.Global().Get("requestAnimationFrame").Type() != js.TypeFunction {
		return
	}
	var step js.Func
	frames := 0
	step = js.FuncOf(func(js.Value, []js.Value) any {
		frames++
		if frames < 2 {
			js.Global().Call("requestAnimationFrame", step)
			return nil
		}
		defer step.Release()
		el := js.Global().Get("document").Call("querySelector", ".chat-row.selected")
		if !el.Truthy() {
			return nil
		}
		scroller := el.Call("closest", ".rail-scroll")
		if !scroller.Truthy() {
			el.Call("scrollIntoView", js.ValueOf(map[string]any{"block": "nearest"}))
			return nil
		}
		row := el.Call("getBoundingClientRect")
		box := scroller.Call("getBoundingClientRect")
		top, bottom := row.Get("top").Float(), row.Get("bottom").Float()
		boxTop, boxBottom := box.Get("top").Float(), box.Get("bottom").Float()
		// The same clearances as .rail-scroll's scroll-padding: the fade
		// mask at the top, the footer shadow and one following row below.
		const clearTop, clearBottom = 40.0, 56.0
		if top >= boxTop+clearTop && bottom <= boxBottom-clearBottom {
			return nil
		}
		// Round 3 C-4: move the list only as far as the row needs, not to
		// the middle. Centring a direct message near the end scrolled the
		// whole Channels group, header and all, out of a laptop-height rail;
		// the smallest move keeps as much of the list above it as fits (and
		// the section headers are sticky, so the group stays named).
		offset := top - (boxTop + clearTop)
		if bottom > boxBottom-clearBottom && top >= boxTop+clearTop {
			offset = bottom - (boxBottom - clearBottom)
		}
		scroller.Set("scrollTop", scroller.Get("scrollTop").Float()+offset)
		return nil
	})
	js.Global().Call("requestAnimationFrame", step)
}
