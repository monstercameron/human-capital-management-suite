//go:build !js || !wasm

package chatui

func dateJumpFocusZone() string { return "" }
func focusDateJumpField()       {}
func focusDateJumpTrigger()     {}

// VisibleMessageID is the first message in view; there is no view outside the browser.
func VisibleMessageID() string { return "" }
