//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"
)

// TestProductRouteEffectsResetOnlyNewDestinations exercises the browser DOM
// seam used by the persistent product shell. New records start at the top;
// a table sort keeps the collection viewport and the initiating control.
func TestProductRouteEffectsResetOnlyNewDestinations(t *testing.T) {
	browser := installWASMHistory(t, "/workspace/app/journeys?journey=journey-b")
	object := js.Global().Get("Object")
	oldDocument := js.Global().Get("document")
	oldAnimationFrame := js.Global().Get("requestAnimationFrame")
	oldFocusedRoute := lastFocusedProductRoute

	main := object.New()
	main.Set("scrollTop", 640)
	heading := object.New()
	focusCalls := 0
	callbacks := make([]js.Func, 0, 4)
	bind := func(target js.Value, name string, fn func(js.Value, []js.Value) any) {
		callback := js.FuncOf(fn)
		callbacks = append(callbacks, callback)
		target.Set(name, callback)
	}
	bind(heading, "hasAttribute", func(js.Value, []js.Value) any { return true })
	bind(heading, "focus", func(js.Value, []js.Value) any {
		focusCalls++
		return nil
	})
	document := object.New()
	bind(document, "getElementById", func(_ js.Value, args []js.Value) any {
		if len(args) == 1 && args[0].String() == "main-content" {
			return main
		}
		return nil
	})
	bind(document, "querySelector", func(js.Value, []js.Value) any { return heading })
	animationFrame := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 1 {
			args[0].Invoke()
		}
		return 1
	})
	callbacks = append(callbacks, animationFrame)
	js.Global().Set("document", document)
	js.Global().Set("requestAnimationFrame", animationFrame)
	t.Cleanup(func() {
		lastFocusedProductRoute = oldFocusedRoute
		js.Global().Set("document", oldDocument)
		js.Global().Set("requestAnimationFrame", oldAnimationFrame)
		for _, callback := range callbacks {
			callback.Release()
		}
	})

	lastFocusedProductRoute = "/workspace/app/journeys?journey=journey-a"
	focusProductRouteAfterNavigation()
	if got := main.Get("scrollTop").Int(); got != 0 {
		t.Fatalf("new journey scrollTop = %d, want 0", got)
	}
	if focusCalls != 1 {
		t.Fatalf("new journey focus calls = %d, want 1", focusCalls)
	}

	main.Set("scrollTop", 420)
	lastFocusedProductRoute = "/workspace/app/people?page=2&sort=name"
	browser.applyHref("/workspace/app/people?dir=desc&page=2&sort=name")
	focusProductRouteAfterNavigation()
	if got := main.Get("scrollTop").Int(); got != 420 {
		t.Fatalf("people sort scrollTop = %d, want 420", got)
	}
	if focusCalls != 1 {
		t.Fatalf("people sort moved focus; calls = %d, want 1", focusCalls)
	}
}
