//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// useDrawerFocusTrap contains keyboard focus inside the overlay drawer while
// it is open and restores focus to whatever had it beforehand (the trigger,
// in every real path) once it closes. It mirrors
// usePopoverFocusDismissal's effect lifecycle (popover_focus_wasm.go): bind
// on open, clean up on close or unmount.
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
		if len(args) == 0 || args[0].Get("key").String() != "Tab" {
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
