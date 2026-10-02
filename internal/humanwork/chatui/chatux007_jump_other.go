//go:build !(js && wasm)

package chatui

// The list is scrolled by the browser; outside it there is nothing to measure.
func chatux007SyncJump()       {}
func chatux007JumpTo(_ string) {}
