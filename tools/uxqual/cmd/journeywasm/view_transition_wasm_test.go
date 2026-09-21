//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"
)

func TestSettleViewTransitionsHandlesSkippedTransitions(t *testing.T) {
	global := js.Global()
	saved := global.Get("document")
	defer global.Set("document", saved)

	fake := global.Get("Object").New()
	calls := 0
	var handled []string
	start := js.FuncOf(func(this js.Value, args []js.Value) any {
		calls++
		if len(args) == 1 && args[0].Type() == js.TypeFunction {
			args[0].Invoke()
		}
		transition := global.Get("Object").New()
		for _, name := range []string{"ready", "finished", "updateCallbackDone"} {
			name := name
			promise := global.Get("Object").New()
			promise.Set("catch", js.FuncOf(func(js.Value, []js.Value) any {
				handled = append(handled, name)
				return nil
			}))
			transition.Set(name, promise)
		}
		return transition
	})
	defer start.Release()
	fake.Set("startViewTransition", start)
	global.Set("document", fake)

	settleViewTransitions()
	settleViewTransitions() // idempotent: must not wrap twice

	applied := false
	apply := js.FuncOf(func(js.Value, []js.Value) any { applied = true; return nil })
	defer apply.Release()
	transition := global.Get("document").Call("startViewTransition", apply)

	if calls != 1 || !applied {
		t.Fatalf("original startViewTransition calls = %d, callback applied = %v; want 1, true", calls, applied)
	}
	if !transition.Get("ready").Truthy() {
		t.Fatal("the wrapper must return the browser's ViewTransition")
	}
	if len(handled) != 3 {
		t.Fatalf("rejection handlers attached to %v, want ready, finished and updateCallbackDone", handled)
	}
}

func TestSettleViewTransitionsWithoutSupportIsANoop(t *testing.T) {
	global := js.Global()
	saved := global.Get("document")
	defer global.Set("document", saved)

	fake := global.Get("Object").New()
	global.Set("document", fake)
	settleViewTransitions()
	if fake.Get("startViewTransition").Truthy() {
		t.Fatal("a browser without view transitions must not gain a startViewTransition")
	}
}
