//go:build js && wasm

package productui

import (
	"syscall/js"
	"testing"
)

// TestTodo_UXBLIND_083_Browser runs the shell's popover controller effect in
// the real js/wasm runtime (node with the toolchain's wasm_exec shim) against
// a minimal document. Before the fix, js.ValueOf panicked on the observer's
// []string attribute filter inside this effect; in the browser that panic
// aborted the hydration commit and left every workspace page on its skeleton.
//
//	GOOS=js GOARCH=wasm go test -exec "node $(go env GOROOT)/lib/wasm/wasm_exec_node.js" -run TestTodo_UXBLIND_083_Browser ./internal/humanwork/productui/
func TestTodo_UXBLIND_083_Browser(t *testing.T) {
	global := js.Global()
	previousDocument, previousObserver := global.Get("document"), global.Get("MutationObserver")
	t.Cleanup(func() {
		global.Set("document", previousDocument)
		global.Set("MutationObserver", previousObserver)
		global.Delete("__uxblind083")
	})
	global.Call("eval", `
globalThis.__uxblind083 = { listeners: 0, observed: null, disconnected: false };
globalThis.document = {
  body: { nodeName: "BODY" },
  activeElement: null,
  querySelectorAll() { return { length: 0, item() { return null; } }; },
  querySelector() { return null; },
  addEventListener() { globalThis.__uxblind083.listeners++; },
  removeEventListener() { globalThis.__uxblind083.listeners--; },
};
globalThis.MutationObserver = class {
  constructor(callback) { this.callback = callback; }
  observe(target, options) { globalThis.__uxblind083.observed = { target, options }; }
  disconnect() { globalThis.__uxblind083.disconnected = true; }
};`)

	var cleanup func()
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("popover controller effect panicked in the js/wasm runtime: %v", recovered)
			}
		}()
		cleanup = bindUXBlindQPopoverController()
	}()
	if cleanup == nil {
		t.Fatal("controller did not bind against a document")
	}
	state := global.Get("__uxblind083")
	if got := state.Get("listeners").Int(); got != 3 {
		t.Fatalf("document listeners = %d, want pointerdown, click and keydown", got)
	}
	observed := state.Get("observed")
	if !observed.Truthy() || observed.Get("target").Get("nodeName").String() != "BODY" {
		t.Fatal("observer was not attached to the document body")
	}
	filter := observed.Get("options").Get("attributeFilter")
	if !global.Get("Array").Call("isArray", filter).Bool() || filter.Length() != 5 || filter.Index(4).String() != "role" {
		t.Fatalf("attributeFilter did not arrive as a JavaScript array of five names")
	}
	cleanup()
	if state.Get("listeners").Int() != 0 || !state.Get("disconnected").Bool() {
		t.Fatal("cleanup left listeners or the observer attached")
	}
}
