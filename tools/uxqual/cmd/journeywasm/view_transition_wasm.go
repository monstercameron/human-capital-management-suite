//go:build js && wasm

package main

import "syscall/js"

// settleViewTransitions wraps document.startViewTransition so every
// transition's promises carry a rejection handler. GWC's router and
// ui.ViewTransition discard the ViewTransition object they start, and the
// browser rejects its ready/finished/updateCallbackDone promises with
// "Transition was skipped" whenever a later navigation supersedes it, which
// surfaced as an uncaught page error on ordinary navigation. The wrapper
// changes nothing else: the same callback runs and the same transition
// animates.
func settleViewTransitions() {
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	original := document.Get("startViewTransition")
	if original.Type() != js.TypeFunction || document.Get("__hcmSettledViewTransitions").Truthy() {
		return
	}
	ignore := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	wrapped := js.FuncOf(func(this js.Value, args []js.Value) any {
		call := make([]any, 0, len(args)+1)
		call = append(call, document)
		for _, arg := range args {
			call = append(call, arg)
		}
		transition := original.Call("call", call...)
		settleTransition(transition, ignore)
		return transition
	})
	document.Set("startViewTransition", wrapped)
	document.Set("__hcmSettledViewTransitions", true)
}

// settleTransition attaches ignore as the rejection handler of each of the
// transition's promises that exists.
func settleTransition(transition js.Value, ignore js.Func) {
	if !transition.Truthy() {
		return
	}
	for _, name := range []string{"ready", "finished", "updateCallbackDone"} {
		if promise := transition.Get(name); promise.Truthy() && promise.Get("catch").Type() == js.TypeFunction {
			promise.Call("catch", ignore)
		}
	}
}
