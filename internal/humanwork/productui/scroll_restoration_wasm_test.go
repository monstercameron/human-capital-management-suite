//go:build js && wasm

package productui

import (
	"syscall/js"
	"testing"
)

// resetScrollPositions clears the package-level restoration map so each test
// starts from a known state regardless of what earlier tests recorded.
func resetScrollPositions(t *testing.T) {
	t.Helper()
	scrollPositions.mu.Lock()
	scrollPositions.values = map[string]float64{}
	scrollPositions.mu.Unlock()
}

// TestBindScrollRestorationRestoresSavedPosition proves the half of the
// contract a live browser cannot easily be scripted to assert: a region
// with a previously recorded scrollTop is set back to it as soon as it is
// bound, before any scroll event has fired on the new DOM node.
func TestBindScrollRestorationRestoresSavedPosition(t *testing.T) {
	resetScrollPositions(t)
	scrollPositions.mu.Lock()
	scrollPositions.values["main-content"] = 240
	scrollPositions.mu.Unlock()

	global := js.Global()
	previousDoc := global.Get("document")
	doc := global.Get("Object").New()
	element := global.Get("Object").New()
	element.Set("scrollTop", 0)
	listeners := map[string]js.Value{}
	element.Set("addEventListener", js.FuncOf(func(_ js.Value, args []js.Value) any {
		listeners[args[0].String()] = args[1]
		return nil
	}))
	element.Set("removeEventListener", js.FuncOf(func(_ js.Value, args []js.Value) any {
		delete(listeners, args[0].String())
		return nil
	}))
	doc.Set("getElementById", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == "main-content" {
			return element
		}
		return js.Undefined()
	}))
	global.Set("document", doc)
	t.Cleanup(func() { global.Set("document", previousDoc) })

	cleanup := bindScrollRestoration("main-content")
	if cleanup == nil {
		t.Fatal("binding onto a real element returned no cleanup")
	}
	if got := element.Get("scrollTop").Float(); got != 240 {
		t.Fatalf("scrollTop after binding = %v, want 240 (the previously recorded position)", got)
	}
	if _, ok := listeners["scroll"]; !ok {
		t.Fatal("binding did not attach a scroll listener")
	}

	// A later scroll event must persist the new position, not the restored one.
	element.Set("scrollTop", 500)
	listeners["scroll"].Invoke(global.Get("Object").New())
	scrollPositions.mu.Lock()
	got := scrollPositions.values["main-content"]
	scrollPositions.mu.Unlock()
	if got != 500 {
		t.Fatalf("recorded position after a scroll event = %v, want 500", got)
	}

	cleanup()
	if _, ok := listeners["scroll"]; ok {
		t.Fatal("cleanup left a live scroll listener")
	}
}

// TestBindScrollRestorationNoopsWithoutAnElement proves the binding degrades
// safely (no panic, no listener, nil cleanup) when the id resolves to
// nothing -- e.g. a render that has not mounted the region yet.
func TestBindScrollRestorationNoopsWithoutAnElement(t *testing.T) {
	resetScrollPositions(t)
	global := js.Global()
	previousDoc := global.Get("document")
	doc := global.Get("Object").New()
	doc.Set("getElementById", js.FuncOf(func(js.Value, []js.Value) any { return js.Undefined() }))
	global.Set("document", doc)
	t.Cleanup(func() { global.Set("document", previousDoc) })

	if cleanup := bindScrollRestoration("missing-region"); cleanup != nil {
		t.Fatal("binding onto a missing element returned a live cleanup")
	}
}
