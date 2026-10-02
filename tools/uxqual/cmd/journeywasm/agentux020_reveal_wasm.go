//go:build js && wasm

package main

import "syscall/js"

// revealAgentTaskDetail brings a task the person just opened into view and
// gives its heading focus. On a narrow screen the detail sits below the whole
// list and the composer, so a click that only swapped the pane looked as if
// nothing had happened (AGENTUX-020).
func revealAgentTaskDetail() {
	heading := js.Global().Get("document").Call("querySelector", "#agents-task-title")
	if !heading.Truthy() {
		return
	}
	heading.Call("scrollIntoView", map[string]any{"block": "nearest"})
	heading.Call("focus", map[string]any{"preventScroll": true})
}
