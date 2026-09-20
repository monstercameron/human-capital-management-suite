//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"
)

// TestTodo_UXLIVE_028_Browser drives the production route effect
// (focusProductRouteAfterNavigation), the product history ledger that stamps
// history.state, and the scroll controller against a browser-shaped history
// and main region under Node.
func TestTodo_UXLIVE_028_Browser(t *testing.T) {
	browser := installWASMHistory(t, "/workspace/app/people")
	object := js.Global().Get("Object")
	oldDocument := js.Global().Get("document")
	oldAnimationFrame := js.Global().Get("requestAnimationFrame")
	oldFocusedRoute, oldHistory, oldScroll := lastFocusedProductRoute, productHistory, productScroll

	main := object.New()
	main.Set("id", productMainScrollOwnerID)
	main.Set("scrollTop", 0)
	heading := object.New()
	focusCalls := 0
	callbacks := make([]js.Func, 0, 8)
	bind := func(target js.Value, name string, fn func(js.Value, []js.Value) any) {
		callback := js.FuncOf(fn)
		callbacks = append(callbacks, callback)
		target.Set(name, callback)
	}
	bind(heading, "hasAttribute", func(js.Value, []js.Value) any { return true })
	bind(heading, "focus", func(js.Value, []js.Value) any {
		focusCalls++
		// Like a browser: the focused heading becomes document.activeElement,
		// so the route-focus keeper sees focus held and does not refocus.
		js.Global().Get("document").Set("activeElement", heading)
		return nil
	})
	document := object.New()
	bind(document, "getElementById", func(_ js.Value, args []js.Value) any {
		if len(args) == 1 && args[0].String() == productMainScrollOwnerID {
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
		lastFocusedProductRoute, productHistory, productScroll = oldFocusedRoute, oldHistory, oldScroll
		js.Global().Set("document", oldDocument)
		js.Global().Set("requestAnimationFrame", oldAnimationFrame)
		for _, callback := range callbacks {
			callback.Release()
		}
	})

	lastFocusedProductRoute = ""
	productHistory = newBrowserProductHistoryController()
	productScroll = newBrowserProductScrollController()
	push := func(href string) {
		productScroll.BeginSoftwareNavigation()
		productHistory.Navigate(func(target string) {
			js.Global().Get("history").Call("pushState", js.Null(), "", target)
		}, href)
		focusProductRouteAfterNavigation()
	}
	traverse := func(offset int, clampTo int) {
		productScroll.BeginTraversal()
		js.Global().Get("history").Call("go", offset)
		// The browser clamps the old page against the next render's content
		// and reports it; that event must not overwrite the saved position.
		main.Set("scrollTop", clampTo)
		productScroll.ledger.Observe(float64(clampTo))
		focusProductRouteAfterNavigation()
	}
	scrollTo := func(top int) {
		main.Set("scrollTop", top)
		productScroll.ledger.Observe(float64(top))
	}
	top := func() int { return main.Get("scrollTop").Int() }

	focusProductRouteAfterNavigation() // cold People document
	scrollTo(900)

	push("/workspace/app/organization")
	if got := top(); got != 0 {
		t.Fatalf("Organization opened at %d, want 0 (inherited People's offset)", got)
	}
	if focusCalls != 1 {
		t.Fatalf("new page focus calls = %d, want 1", focusCalls)
	}
	scrollTo(240)

	push("/workspace/app/organization?q=Jane")
	if got := top(); got != 240 {
		t.Fatalf("organization filter moved the reader to %d, want 240", got)
	}
	if focusCalls != 1 {
		t.Fatalf("filter change stole focus; calls = %d", focusCalls)
	}
	// A re-mounted region would start at 0; the kept position is put back.
	main.Set("scrollTop", 0)
	lastFocusedProductRoute = "/workspace/app/organization?q=Jane&sort=x"
	browser.applyHref("/workspace/app/organization?q=Jane")
	focusProductRouteAfterNavigation()
	if got := top(); got != 240 {
		t.Fatalf("re-mounted main region left at %d, want the kept 240", got)
	}

	push("/workspace/app/insights")
	if got := top(); got != 0 {
		t.Fatalf("Insights opened at %d, want 0", got)
	}
	scrollTo(60)

	traverse(-1, 0)
	if got := top(); got != 240 {
		t.Fatalf("Back to Organization restored %d, want 240", got)
	}
	traverse(-1, 0)
	traverse(-1, 0)
	if got := top(); got != 900 {
		t.Fatalf("Back to People restored %d, want 900", got)
	}
	traverse(1, 0)
	if got := top(); got != 240 {
		t.Fatalf("Forward to Organization restored %d, want 240", got)
	}
	if focusCalls < 3 {
		t.Fatalf("Back/Forward to another page did not focus its identity; calls = %d", focusCalls)
	}
}

// TestTodo_UXLIVE_028_Accessibility_FocusSurvivesLateCommit: live, a late
// commit replaced the focused page heading and left focus on <body>. The
// route-focus keeper puts it back on the current heading, and never takes it
// from an element the reader has moved to.
func TestTodo_UXLIVE_028_Accessibility_FocusSurvivesLateCommit(t *testing.T) {
	object := js.Global().Get("Object")
	oldDocument := js.Global().Get("document")
	oldAnimationFrame := js.Global().Get("requestAnimationFrame")
	document := object.New()
	body := object.New()
	document.Set("body", body)
	document.Set("activeElement", body)
	heading := object.New()
	focusCalls := 0
	focus := js.FuncOf(func(js.Value, []js.Value) any {
		focusCalls++
		document.Set("activeElement", heading)
		return nil
	})
	query := js.FuncOf(func(js.Value, []js.Value) any { return heading })
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any {
		args[0].Invoke()
		return 1
	})
	heading.Set("focus", focus)
	document.Set("querySelector", query)
	js.Global().Set("document", document)
	js.Global().Set("requestAnimationFrame", frame)
	t.Cleanup(func() {
		js.Global().Set("document", oldDocument)
		js.Global().Set("requestAnimationFrame", oldAnimationFrame)
		focus.Release()
		query.Release()
		frame.Release()
	})

	keepRouteFocus(productPageFocusSelector, 0)
	if focusCalls != 1 {
		t.Fatalf("focus lost to <body> was restored %d times, want once", focusCalls)
	}
	other := object.New()
	document.Set("activeElement", other)
	keepRouteFocus(productPageFocusSelector, 0)
	if focusCalls != 1 {
		t.Fatal("the keeper took focus from an element the reader moved to")
	}
}
