//go:build js && wasm

package main

import "syscall/js"

// bootMark records one cold-start phase as a User Timing mark
// ("hcm:<phase>"). The marks are how the cold-load budget (UXBLIND-089) is
// measured in a real browser: the loader marks the fetch and instantiate
// phases, and this client marks the phases only Go can see. A phase is
// marked once; a phase that runs again on a later navigation (every route
// load) keeps its first, cold-start mark.
func bootMark(phase string) {
	performance := js.Global().Get("performance")
	if !performance.Truthy() || !performance.Get("mark").Truthy() {
		return
	}
	name := bootMarkPrefix + phase
	if performance.Call("getEntriesByName", name, "mark").Length() > 0 {
		return
	}
	performance.Call("mark", name)
}
