//go:build js && wasm

package main

import "syscall/js"

// publishChatStreamLog puts the subscription log on window.__chatStreamLog as
// an array of lines, so a live run can be diagnosed from the browser console
// without a build flag or a debugger attached.
func publishChatStreamLog(ring *chatStreamRing) {
	lines := ring.Lines()
	values := make([]any, 0, len(lines))
	for _, line := range lines {
		values = append(values, line)
	}
	global := js.Global()
	if !global.Truthy() {
		return
	}
	global.Set("__chatStreamLog", js.ValueOf(values))
}
