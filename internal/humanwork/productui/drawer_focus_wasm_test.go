//go:build js && wasm

package productui

import (
	"syscall/js"
	"testing"
)

// TestDrawerFocusTrapWrapsTabAndRestoresFocusOnClose proves the drawer's
// focus containment: opening moves focus inside, Tab and Shift+Tab wrap at
// the dialog's own boundary instead of escaping it, and tearing the binding
// down (the drawer closing) returns focus to whatever had it before —  the
// trigger, in every real path.
func TestDrawerFocusTrapWrapsTabAndRestoresFocusOnClose(t *testing.T) {
	global := js.Global()
	previousDoc := global.Get("document")
	doc := global.Get("Object").New()

	trigger := global.Get("Object").New()
	first := global.Get("Object").New()
	last := global.Get("Object").New()
	hidden := global.Get("Object").New()
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

	cleanup := bindDrawerFocusTrap("workspace-navigation", "nav-drawer-trigger")
	if cleanup == nil {
		t.Fatal("binding onto a real dialog returned no cleanup")
	}
	if !focused.Equal(first) {
		t.Fatal("opening the drawer did not move focus to its first focusable child")
	}

	tab := func(shift bool, active js.Value) {
		doc.Set("activeElement", active)
		event := global.Get("Object").New()
		event.Set("key", "Tab")
		event.Set("shiftKey", shift)
		prevented := false
		event.Set("preventDefault", js.FuncOf(func(js.Value, []js.Value) any { prevented = true; return nil }))
		handlers["keydown"].Invoke(event)
		if !prevented {
			t.Fatalf("Tab (shift=%v) from the boundary was not intercepted", shift)
		}
	}
	tab(false, last)
	if !focused.Equal(first) {
		t.Fatal("Tab past the last focusable item did not wrap to the first")
	}
	tab(true, first)
	if !focused.Equal(last) {
		t.Fatal("Shift+Tab past the first focusable item did not wrap to the last")
	}

	doc.Set("activeElement", last)
	cleanup()
	if !focused.Equal(trigger) {
		t.Fatal("closing the drawer did not restore focus to the trigger")
	}
	if len(handlers) != 0 {
		t.Fatal("cleanup left a live keydown listener")
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
