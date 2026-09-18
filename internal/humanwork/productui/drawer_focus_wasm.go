//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// useDrawerFocusTrap moves focus into an overlay when it opens and gives it
// back to whatever had it beforehand (the trigger, in every real path) once
// it closes. It mirrors usePopoverFocusDismissal's effect lifecycle
// (popover_focus_wasm.go): bind on open, clean up on close or unmount.
//
// What it does in between follows what the dialog declares. A dialog marked
// aria-modal="true" -- the mobile navigation drawer, the appearance preview --
// has promised that the page behind it is inert, so Tab wraps at its edges.
// A non-modal popover -- Start an action, Page utilities -- has promised the
// opposite, and trapping Tab there made the reader's only way out of it the
// Escape key: Tab from the last item wrapped to the first, so the focus-leaves
// dismissal in usePopoverFocusDismissal could never fire from the keyboard.
func useDrawerFocusTrap(dialogID, triggerID string, open bool) {
	ui.UseEffectOf(func() func() {
		if !open {
			return nil
		}
		return bindDrawerFocusTrap(dialogID, triggerID)
	}, struct {
		Dialog, Trigger string
		Open            bool
	}{dialogID, triggerID, open})
}

const drawerFocusableSelector = `a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])`

func bindDrawerFocusTrap(dialogID, triggerID string) func() {
	doc := js.Global().Get("document")
	dialog := doc.Call("getElementById", dialogID)
	if !dialog.Truthy() {
		return nil
	}
	previouslyFocused := doc.Get("activeElement")
	modal := dialog.Call("getAttribute", "aria-modal").String() == "true"

	focusable := func() []js.Value {
		list := dialog.Call("querySelectorAll", drawerFocusableSelector)
		length := list.Get("length").Int()
		items := make([]js.Value, 0, length)
		for i := 0; i < length; i++ {
			item := list.Call("item", i)
			if drawerFocusableVisible(item) {
				items = append(items, item)
			}
		}
		return items
	}

	// Move focus into the dialog the moment it opens; fall back to the
	// dialog itself (still reachable via a synthetic tabindex) if it somehow
	// renders with nothing focusable.
	if items := focusable(); len(items) > 0 {
		items[0].Call("focus")
	} else {
		dialog.Call("focus")
	}

	listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !modal || len(args) == 0 || args[0].Get("key").String() != "Tab" {
			return nil
		}
		event := args[0]
		items := focusable()
		if len(items) == 0 {
			return nil
		}
		first, last := items[0], items[len(items)-1]
		active := doc.Get("activeElement")
		inside := active.Truthy() && dialog.Call("contains", active).Bool()
		if event.Get("shiftKey").Bool() {
			if !inside || active.Equal(first) {
				event.Call("preventDefault")
				last.Call("focus")
			}
			return nil
		}
		if !inside || active.Equal(last) {
			event.Call("preventDefault")
			first.Call("focus")
		}
		return nil
	})
	dialog.Call("addEventListener", "keydown", listener)

	return func() {
		dialog.Call("removeEventListener", "keydown", listener)
		listener.Release()
		// Give focus back only if the dialog still had it, or had it until it
		// hid and the browser dropped it to the body. If the reader has already
		// put focus somewhere else -- tabbed past the popover, clicked another
		// control -- that is where they meant to be, and pulling it back to the
		// trigger would undo their move.
		if active := doc.Get("activeElement"); active.Truthy() && !active.Equal(doc.Get("body")) && !dialog.Call("contains", active).Bool() {
			return
		}
		if previouslyFocused.Truthy() && previouslyFocused.Get("isConnected").Bool() && previouslyFocused.Get("focus").Truthy() && !previouslyFocused.Equal(doc.Get("body")) {
			previouslyFocused.Call("focus")
			return
		}
		if trigger := doc.Call("getElementById", triggerID); trigger.Truthy() {
			trigger.Call("focus")
		}
	}
}

// drawerFocusableVisible keeps the trap's boundary aligned with the controls
// a keyboard user can actually reach. querySelectorAll also returns controls
// hidden by a responsive ancestor; focusing one of those is a no-op and used
// to strand reverse traversal on the first visible control.
func drawerFocusableVisible(element js.Value) bool {
	if !element.Truthy() {
		return false
	}
	if hidden := element.Call("closest", `[hidden],[inert],[aria-hidden="true"]`); hidden.Truthy() {
		return false
	}
	rects := element.Call("getClientRects")
	return rects.Truthy() && rects.Get("length").Int() > 0
}
