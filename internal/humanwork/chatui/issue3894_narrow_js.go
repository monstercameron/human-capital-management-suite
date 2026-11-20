//go:build js && wasm

package chatui

import "syscall/js"

// issue3894Narrow reports whether the thread covers the channel: the phone
// layout, where the side column is a full-width overlay.
var issue3894Narrow = func() bool {
	return js.Global().Call("matchMedia", "(max-width:760px)").Get("matches").Bool()
}
