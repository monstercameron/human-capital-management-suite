//go:build js && wasm

package productui

import (
	"syscall/js"
	"testing"
)

// drawerTrapFixture is a stand-in document holding a trigger outside a dialog
// with two visible focusable children and one hidden one. modal sets the
// dialog's aria-modal, which is what decides whether Tab wraps.
type drawerTrapFixture struct {
	doc, trigger, first, last, outside js.Value
	handlers                           map[string]js.Value
	focused                            *js.Value
}

func newDrawerTrapFixture(t *testing.T, modal bool) drawerTrapFixture {
	t.Helper()
	global := js.Global()
	previousDoc := global.Get("document")
	doc := global.Get("Object").New()

	trigger := global.Get("Object").New()
	first := global.Get("Object").New()
	last := global.Get("Object").New()
	hidden := global.Get("Object").New()
	outside := global.Get("Object").New()
	dialog := global.Get("Object").New()

	focused := trigger
	focusOf := func(target js.Value) js.Func {
		return js.FuncOf(func(js.Value, []js.Value) any { focused = target; return nil })
	}
	trigger.Set("focus", focusOf(trigger))
	trigger.Set("isConnected", true)
	first.Set("focus", focusOf(first))
	last.Set("focus", focusOf(last))
	hidden.Set("focus", focusOf(hidden))
	outside.Set("focus", focusOf(outside))
	visibleRects := global.Get("Object").New()
	visibleRects.Set("length", 1)
	hiddenRects := global.Get("Object").New()
	hiddenRects.Set("length", 0)
	for _, element := range []js.Value{first, last, hidden} {
		element.Set("closest", js.FuncOf(func(js.Value, []js.Value) any { return js.Undefined() }))
		element.Set("getClientRects", js.FuncOf(func(js.Value, []js.Value) any { return visibleRects }))
	}
	hidden.Set("getClientRects", js.FuncOf(func(js.Value, []js.Value) any { return hiddenRects }))

	list := global.Get("Object").New()
	list.Set("length", 3)
	itemOf := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].Int() == 0 {
			return first
		}
		if args[0].Int() == 1 {
			return last
		}
		return hidden
	})
	list.Set("item", itemOf)

	handlers := map[string]js.Value{}
	dialog.Set("querySelectorAll", js.FuncOf(func(js.Value, []js.Value) any { return list }))
	dialog.Set("getAttribute", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if modal && args[0].String() == "aria-modal" {
			return "true"
		}
		return js.Null()
	}))
	dialog.Set("addEventListener", js.FuncOf(func(_ js.Value, args []js.Value) any {
		handlers[args[0].String()] = args[1]
		return nil
	}))
	dialog.Set("removeEventListener", js.FuncOf(func(_ js.Value, args []js.Value) any {
		delete(handlers, args[0].String())
		return nil
	}))
	dialog.Set("contains", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return args[0].Equal(first) || args[0].Equal(last) || args[0].Equal(hidden)
	}))

	doc.Set("getElementById", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == "nav-drawer-trigger" {
			return trigger
		}
		return dialog
	}))
	doc.Set("activeElement", trigger)
	global.Set("document", doc)
	t.Cleanup(func() {
		global.Set("document", previousDoc)
		itemOf.Release()
	})
	return drawerTrapFixture{doc: doc, trigger: trigger, first: first, last: last, outside: outside, handlers: handlers, focused: &focused}
}

// tab presses Tab (or Shift+Tab) with focus on active and reports whether the
// trap intercepted it.
func (f drawerTrapFixture) tab(shift bool, active js.Value) bool {
	f.doc.Set("activeElement", active)
	event := js.Global().Get("Object").New()
	event.Set("key", "Tab")
	event.Set("shiftKey", shift)
	prevented := false
	event.Set("preventDefault", js.FuncOf(func(js.Value, []js.Value) any { prevented = true; return nil }))
	f.handlers["keydown"].Invoke(event)
	return prevented
}

// TestDrawerFocusTrapWrapsTabAndRestoresFocusOnClose proves the modal
// drawer's focus containment: opening moves focus inside, Tab and Shift+Tab
// wrap at the dialog's own boundary instead of escaping it, and tearing the
// binding down (the drawer closing) returns focus to whatever had it before
// -- the trigger, in every real path.
func TestDrawerFocusTrapWrapsTabAndRestoresFocusOnClose(t *testing.T) {
	f := newDrawerTrapFixture(t, true)
	cleanup := bindDrawerFocusTrap("workspace-navigation", "nav-drawer-trigger")
	if cleanup == nil {
		t.Fatal("binding onto a real dialog returned no cleanup")
	}
	if !f.focused.Equal(f.first) {
		t.Fatal("opening the drawer did not move focus to its first focusable child")
	}
	if !f.tab(false, f.last) || !f.focused.Equal(f.first) {
		t.Fatal("Tab past the last focusable item did not wrap to the first")
	}
	if !f.tab(true, f.first) || !f.focused.Equal(f.last) {
		t.Fatal("Shift+Tab past the first focusable item did not wrap to the last")
	}

	f.doc.Set("activeElement", f.last)
	cleanup()
	if !f.focused.Equal(f.trigger) {
		t.Fatal("closing the drawer did not restore focus to the trigger")
	}
	if len(f.handlers) != 0 {
		t.Fatal("cleanup left a live keydown listener")
	}
}

// TestNonModalPopoverLetsTabLeave: Start an action and Page utilities are
// non-modal. Tab from their last item has to leave them -- that is what lets
// usePopoverFocusDismissal close them when the reader tabs away -- where the
// trap used to wrap it back to the first item.
func TestNonModalPopoverLetsTabLeave(t *testing.T) {
	f := newDrawerTrapFixture(t, false)
	cleanup := bindDrawerFocusTrap("utility-drawer-dialog", "utility-drawer-trigger")
	if !f.focused.Equal(f.first) {
		t.Fatal("opening the popover did not move focus to its first item")
	}
	if f.tab(false, f.last) {
		t.Fatal("Tab from the last item of a non-modal popover was trapped")
	}
	if f.tab(true, f.first) {
		t.Fatal("Shift+Tab from the first item of a non-modal popover was trapped")
	}
	cleanup()
}

// TestClosingDoesNotPullFocusBackFromWhereTheReaderMovedIt: when the popover
// closes because focus left it, focus is already where the reader put it.
// Restoring to the trigger then would undo the move. A close from inside
// (Escape, the close button) or one that dropped focus to the body still
// restores.
func TestClosingDoesNotPullFocusBackFromWhereTheReaderMovedIt(t *testing.T) {
	f := newDrawerTrapFixture(t, false)
	cleanup := bindDrawerFocusTrap("utility-drawer-dialog", "utility-drawer-trigger")
	f.outside.Call("focus")
	f.doc.Set("activeElement", f.outside)
	cleanup()
	if !f.focused.Equal(f.outside) {
		t.Fatal("closing the popover pulled focus away from the control the reader moved to")
	}

	body := js.Global().Get("Object").New()
	f.doc.Set("body", body)
	f.doc.Set("activeElement", f.trigger)
	cleanup = bindDrawerFocusTrap("utility-drawer-dialog", "utility-drawer-trigger")
	f.doc.Set("activeElement", body)
	cleanup()
	if !f.focused.Equal(f.trigger) {
		t.Fatal("focus dropped to the body when the popover hid was not given back")
	}
}

// TestDrawerFocusTrapNoopsWithoutADialog proves the effect degrades safely
// (no panic, no listener leak) when the dialog id resolves to nothing —
// e.g. a render that has not mounted the drawer yet.
func TestDrawerFocusTrapNoopsWithoutADialog(t *testing.T) {
	global := js.Global()
	previousDoc := global.Get("document")
	doc := global.Get("Object").New()
	missing := js.Undefined()
	doc.Set("getElementById", js.FuncOf(func(js.Value, []js.Value) any { return missing }))
	global.Set("document", doc)
	t.Cleanup(func() { global.Set("document", previousDoc) })

	if cleanup := bindDrawerFocusTrap("workspace-navigation", "nav-drawer-trigger"); cleanup != nil {
		t.Fatal("binding onto a missing dialog returned a live cleanup")
	}
}
